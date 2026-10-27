//go:build !mobile

package msi

// 非 mobile 构建时的空实现，方便本地 go test / go run。
type Tester struct{}

func NewTester() *Tester { return &Tester{} }

func (t *Tester) Test(ip, domain, echConfig string, useEch, useH3 bool, timeoutMs int) string {
	return TestOne(ip, domain, echConfig, useEch, useH3, timeoutMs)
}

func (t *Tester) Version() string { return "msi-go/1.0.0-stub" }
