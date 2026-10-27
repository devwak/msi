//go:build mobile

package msi

// 供 gomobile 绑定的导出类型与方法。
// 使用方式：
//   gomobile bind -target=android -androidapi=24 -o msi.aar ./
//
// 生成的 Java 类：msi.Msi / msi.Tester

// Tester 是给 Android 调用的入口。
type Tester struct{}

// NewTester 构造（gomobile 需要）。
func NewTester() *Tester {
	return &Tester{}
}

// Test 测试单个 IP。
// 参数与 TestOne 相同，返回相同格式字符串。
func (t *Tester) Test(ip, domain, echConfig string, useEch, useH3 bool, timeoutMs int32) string {
	return TestOne(ip, domain, echConfig, useEch, useH3, timeoutMs)
}

// Version 返回库版本，方便确认 so/aar 是否加载成功。
func (t *Tester) Version() string {
	return "msi-go/1.0.0"
}
