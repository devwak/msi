# msi — CF CDN IP 连通测试（Go SO / AAR 模式）

**核心测试逻辑全部在 Go 里**（`crypto/tls` + 可选 `quic-go`），通过 gomobile 编译成 Android AAR（内含 `.so`）。  
因此 **ECH 在任意 Android 版本（含卓易通 Android 16）都可用**，不再依赖系统 API 37。

## 本版功能

| 项     | 说明                                                                                                                  |
| ------ | --------------------------------------------------------------------------------------------------------------------- |
| IP 池  | 一行一个纯 IP；兼容 `\r\n`/`\n`；去重；跳过空行；粘贴或选文件                                                         |
| Domain | 必填。连接 IP:443，Host/SNI = domain                                                                                  |
| 判定   | 真实 HEAD（失败再 GET），不跟随重定向，**仅 HTTP 200 才输出**                                                         |
| ECH    | 开关（默认开）+ 配置（默认 `cloudflare-ech.com+https://dns.alidns.com/dns-query`）。开启时必须 ECH 握手成功，否则丢弃 |
| 协议   | H3 / H2 单选（默认 H3）。H3 使用 quic-go，H2 使用标准 net/http                                                        |
| 排序   | 按耗时升序                                                                                                            |
| 结果   | 列表 + 点击复制 IP；可保存为「一行一个 IP」的 txt                                                                     |
| 默认   | 并发 20、超时 5000 ms                                                                                                 |

## 目录结构

```
msi/
├── .github/workflows/build.yml   # GitHub Actions 全自动编译
├── go/                           # Go 核心（ECH + H2/H3）
│   ├── go.mod
│   ├── ech.go
│   ├── tester.go
│   ├── mobile.go                 # //go:build mobile
│   └── mobile_stub.go
├── app/
│   ├── libs/                     # CI 生成的 msi.aar 放这里
│   ├── build.gradle.kts
│   └── src/main/...
├── scripts/build-aar.sh          # 本地编 AAR 脚本
└── README.md
```

---

## 
