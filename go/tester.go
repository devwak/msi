package msi

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// TestOne 测试单个 IP。
// 返回格式：
//
//	成功: "ok|<latencyMs>|<proto>"
//	失败: "fail|<reason>"
//
// proto 为 "H2" 或 "H3"
func TestOne(ip, domain, echConfig string, useEch, useH3 bool, timeoutMs int) string {
	ip = strings.TrimSpace(ip)
	domain = strings.TrimSpace(domain)
	if ip == "" || domain == "" {
		return "fail|empty ip or domain"
	}
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	var code int
	var proto string
	var err error

	if useH3 {
		code, err = doH3(ctx, ip, domain, echConfig, useEch, timeout)
		proto = "H3"
	} else {
		code, err = doH2(ctx, ip, domain, echConfig, useEch, timeout)
		proto = "H2"
	}

	if err != nil {
		return fmt.Sprintf("fail|%v", err)
	}
	if code != 200 {
		return fmt.Sprintf("fail|http %d", code)
	}
	ms := time.Since(start).Milliseconds()
	return fmt.Sprintf("ok|%d|%s", ms, proto)
}

func buildTLSConfig(domain, echConfig string, useEch bool) (*tls.Config, error) {
	cfg := &tls.Config{
		ServerName: domain,
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
	}
	if useEch {
		echBytes, err := getECHConfig(context.Background(), echConfig)
		if err != nil {
			return nil, fmt.Errorf("ech config: %w", err)
		}
		cfg.EncryptedClientHelloConfigList = echBytes
	}
	return cfg, nil
}

func formatAddr(ip string, port int) string {
	if strings.Contains(ip, ":") {
		return fmt.Sprintf("[%s]:%d", ip, port)
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

func doH2(ctx context.Context, ip, domain, echConfig string, useEch bool, timeout time.Duration) (int, error) {
	tlsCfg, err := buildTLSConfig(domain, echConfig, useEch)
	if err != nil {
		return 0, err
	}
	// 明确要 h2
	tlsCfg.NextProtos = []string{"h2", "http/1.1"}

	dialer := &net.Dialer{Timeout: timeout}

	transport := &http.Transport{
		Proxy:               nil,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true, // 关键：自定义 Dial/TLS 时必须开，否则不会用 h2
		TLSHandshakeTimeout: timeout,
		TLSClientConfig:     tlsCfg,
		// 用 DialTLSContext，自己拨 IP + 做 TLS，避免 IP 当 Host 搞乱 SNI
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			raw, err := dialer.DialContext(ctx, "tcp", formatAddr(ip, 443))
			if err != nil {
				return nil, err
			}
			cfg := tlsCfg.Clone()
			if cfg.ServerName == "" {
				cfg.ServerName = domain
			}
			tlsConn := tls.Client(raw, cfg)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = raw.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 用域名拼 URL（SNI/Host 都正确），真正连的还是上面 Dial 里的 IP
	url := "https://" + domain + "/"

	// 先 GET（CF 对 HEAD 有时不友好），失败再 HEAD
	doReq := func(method string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, err
		}
		req.Host = domain
		req.Header.Set("User-Agent", "msi/1.0")
		req.Close = true
		return client.Do(req)
	}

	resp, err := doReq(http.MethodGet)
	if err != nil {
		resp, err = doReq(http.MethodHead)
		if err != nil {
			return 0, err
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func doH3(ctx context.Context, ip, domain, echConfig string, useEch bool, timeout time.Duration) (int, error) {
	tlsCfg, err := buildTLSConfig(domain, echConfig, useEch)
	if err != nil {
		return 0, err
	}
	tlsCfg.NextProtos = []string{http3.NextProtoH3}

	targetAddr := formatAddr(ip, 443)
	rt := &http3.Transport{
		TLSClientConfig: tlsCfg,
		Dial: func(ctx context.Context, addr string, tlsConf *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
			if tlsConf == nil {
				tlsConf = &tls.Config{}
			} else {
				tlsConf = tlsConf.Clone()
			}
			if len(tlsConf.NextProtos) == 0 {
				tlsConf.NextProtos = []string{http3.NextProtoH3}
			}
			if tlsConf.ServerName == "" {
				tlsConf.ServerName = domain
			}
			return quic.DialAddr(ctx, targetAddr, tlsConf, cfg)
		},
	}
	defer rt.Close()

	client := &http.Client{
		Transport: rt,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := "https://" + formatAddr(ip, 443) + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	req.Host = domain
	req.Header.Set("User-Agent", "msi/1.0")

	resp, err := client.Do(req)
	if err != nil {
		req2, err2 := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err2 != nil {
			return 0, err
		}
		req2.Host = domain
		req2.Header.Set("User-Agent", "msi/1.0")
		resp, err = client.Do(req2)
		if err != nil {
			return 0, err
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
