package com.msi.ipcheck

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.msi.ipcheck.databinding.ActivityMainBinding
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.asCoroutineDispatcher
import kotlinx.coroutines.cancel
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.BufferedReader
import java.io.InputStreamReader
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

data class TestResult(val ip: String, val latencyMs: Long, val protocol: String, val colo: String = "")

class MainActivity : AppCompatActivity() {

    private lateinit var binding: ActivityMainBinding
    private val results = mutableListOf<TestResult>()
    private lateinit var adapter: ResultAdapter
    private var testJob: Job? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    // gomobile 生成的类：msi.Tester
    private var goTester: Any? = null
    private var goTestMethod: java.lang.reflect.Method? = null

    private val filePicker = registerForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri != null) loadIpFromUri(uri)
    }
    private val savePicker = registerForActivityResult(ActivityResultContracts.CreateDocument("text/plain")) { uri: Uri? ->
        if (uri != null) saveResultsToUri(uri)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityMainBinding.inflate(layoutInflater)
        setContentView(binding.root)

        adapter = ResultAdapter(results) { item ->
            val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            cm.setPrimaryClip(ClipData.newPlainText("ip", item.ip))
            Toast.makeText(this, "已复制 ${item.ip}", Toast.LENGTH_SHORT).show()
        }
        binding.rvResults.layoutManager = LinearLayoutManager(this)
        binding.rvResults.adapter = adapter

        // 尝试加载 Go 绑定
        loadGoLib()

        binding.btnLoadFile.setOnClickListener { filePicker.launch(arrayOf("text/*", "*/*")) }
        binding.btnStart.setOnClickListener { startTest() }
        binding.btnStop.setOnClickListener { stopTest() }
        binding.btnSave.setOnClickListener { savePicker.launch("msi_ok_ips.txt") }
    }

    private fun loadGoLib() {
        try {
            // gomobile bind 生成包名一般是 go 模块名，这里是 msi
            val testerClass = Class.forName("msi.Tester")
            val ctor = testerClass.getConstructor()
            goTester = ctor.newInstance()
            goTestMethod = testerClass.getMethod(
                "test",
                String::class.java, String::class.java, String::class.java,
                Boolean::class.javaPrimitiveType, Boolean::class.javaPrimitiveType,
                Int::class.javaPrimitiveType
            )
            // 也可调 version()
            val ver = testerClass.getMethod("version").invoke(goTester) as? String
            binding.tvProgress.text = "Go 库已加载: $ver"
            binding.tvEchHint.text = " (Go TLS，任意 Android 可用)"
        } catch (e: Exception) {
            binding.tvProgress.text = "未找到 msi.aar，请先构建 Go 库（见 README）"
            binding.tvEchHint.text = " (需先放入 app/libs/msi.aar)"
            Toast.makeText(this, "Go 库未加载: ${e.message}", Toast.LENGTH_LONG).show()
        }
    }

    private fun callGoTest(ip: String, domain: String, echCfg: String, useEch: Boolean, useH3: Boolean, timeout: Int): String {
        val m = goTestMethod ?: return "fail|go lib not loaded"
        val t = goTester ?: return "fail|go lib not loaded"
        return try {
            m.invoke(t, ip, domain, echCfg, useEch, useH3, timeout) as String
        } catch (e: Exception) {
            "fail|${e.message}"
        }
    }

    private fun loadIpFromUri(uri: Uri) {
        try {
            contentResolver.openInputStream(uri)?.use { input ->
                binding.etIpList.setText(BufferedReader(InputStreamReader(input)).readText())
                Toast.makeText(this, "已加载文件", Toast.LENGTH_SHORT).show()
            }
        } catch (e: Exception) {
            Toast.makeText(this, "读取失败: ${e.message}", Toast.LENGTH_SHORT).show()
        }
    }

    private fun parseIpList(raw: String): List<String> =
        raw.replace("\r\n", "\n").replace("\r", "\n")
            .split("\n").map { it.trim() }.filter { it.isNotEmpty() }.distinct()

    private fun startTest() {
        if (goTester == null) {
            Toast.makeText(this, "请先构建并放入 msi.aar", Toast.LENGTH_LONG).show()
            return
        }
        val domain = binding.etDomain.text.toString().trim()
        if (domain.isEmpty()) {
            Toast.makeText(this, "请填写 Domain", Toast.LENGTH_SHORT).show()
            return
        }
        val ips = parseIpList(binding.etIpList.text.toString())
        if (ips.isEmpty()) {
            Toast.makeText(this, "IP 池为空", Toast.LENGTH_SHORT).show()
            return
        }

        val useEch = binding.cbEch.isChecked
        val echCfg = binding.etEchConfig.text.toString().trim()
            .ifEmpty { "cloudflare-ech.com+https://dns.alidns.com/dns-query" }
        val useH3 = binding.rbH3.isChecked
        val concurrency = (binding.etConcurrency.text.toString().toIntOrNull() ?: 20).coerceIn(1, 100)
        val timeout = binding.etTimeout.text.toString().toIntOrNull() ?: 5000

        results.clear()
        adapter.notifyDataSetChanged()
        binding.btnStart.isEnabled = false
        binding.btnStop.isEnabled = true
        binding.btnSave.isEnabled = false
        binding.tvProgress.text = "准备中… 共 ${ips.size} 个 IP"

        testJob = scope.launch {
            val done = AtomicInteger(0)
            val okCount = AtomicInteger(0)
            val dispatcher = Executors.newFixedThreadPool(concurrency).asCoroutineDispatcher()
            try {
                val jobs = ips.map { ip ->
                    launch(dispatcher) {
                        if (!isActive) return@launch
                        val raw = withContext(Dispatchers.IO) {
                            callGoTest(ip, domain, echCfg, useEch, useH3, timeout)
                        }
                        val d = done.incrementAndGet()
                        // 解析 "ok|123|H3|HKG" 或 "ok|123|H3|" 或旧版 "ok|123|H3" 或 "fail|..."
                        if (raw.startsWith("ok|")) {
                            val parts = raw.split("|")
                            val ms = parts.getOrNull(1)?.toLongOrNull() ?: 0L
                            val proto = parts.getOrNull(2) ?: if (useH3) "H3" else "H2"
                            val colo = parts.getOrNull(3)?.takeIf { it.isNotBlank() } ?: ""
                            okCount.incrementAndGet()
                            withContext(Dispatchers.Main) {
                                results.add(TestResult(ip, ms, proto, colo))
                                results.sortBy { it.latencyMs }
                                adapter.notifyDataSetChanged()
                                binding.btnSave.isEnabled = true
                            }
                        }
                        withContext(Dispatchers.Main) {
                            binding.tvProgress.text = "进度 $d/${ips.size}  通过 ${okCount.get()}"
                        }
                    }
                }
                jobs.forEach { it.join() }
            } finally {
                dispatcher.close()
            }
            withContext(Dispatchers.Main) {
                binding.tvProgress.text = "完成  通过 ${results.size}/${ips.size}  （已按耗时升序）"
                binding.btnStart.isEnabled = true
                binding.btnStop.isEnabled = false
                binding.btnSave.isEnabled = results.isNotEmpty()
            }
        }
    }

    private fun stopTest() {
        testJob?.cancel()
        binding.btnStart.isEnabled = true
        binding.btnStop.isEnabled = false
        binding.tvProgress.text = "已停止  当前通过 ${results.size}"
    }

    private fun saveResultsToUri(uri: Uri) {
        try {
            contentResolver.openOutputStream(uri)?.use { out ->
                results.forEach { out.write((it.ip + "\n").toByteArray()) }
            }
            Toast.makeText(this, "已保存 ${results.size} 个 IP", Toast.LENGTH_SHORT).show()
        } catch (e: Exception) {
            Toast.makeText(this, "保存失败: ${e.message}", Toast.LENGTH_SHORT).show()
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        scope.cancel()
    }
}

class ResultAdapter(
    private val data: List<TestResult>,
    private val onClick: (TestResult) -> Unit
) : RecyclerView.Adapter<ResultAdapter.VH>() {
    class VH(v: View) : RecyclerView.ViewHolder(v) {
        val tv: TextView = v.findViewById(android.R.id.text1)
    }
    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): VH {
        val v = LayoutInflater.from(parent.context)
            .inflate(android.R.layout.simple_list_item_1, parent, false)
        return VH(v)
    }
    override fun onBindViewHolder(holder: VH, position: Int) {
        val item = data[position]
        val coloPart = if (item.colo.isNotBlank()) "  ${item.colo}" else ""
        holder.tv.text = "${item.ip}  ${item.latencyMs}ms  ${item.protocol}$coloPart"
        holder.itemView.setOnClickListener { onClick(item) }
    }
    override fun getItemCount() = data.size
}
