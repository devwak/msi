package msi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// 与 cfdata / CFST 保持一致的默认 ECH 来源
var (
	defaultECHConfigList = "cloudflare-ech.com+https://dns.alidns.com/dns-query"
	echCache             = struct {
		sync.Mutex
		data map[string][]byte
	}{data: make(map[string][]byte)}
)

func getECHConfig(ctx context.Context, echConfigList string) ([]byte, error) {
	if echConfigList == "" {
		echConfigList = defaultECHConfigList
	}
	coverName, resolver, ok := strings.Cut(echConfigList, "+")
	if !ok || coverName == "" || resolver == "" {
		return nil, errors.New("invalid echConfigList, need coverName+resolver")
	}

	echCache.Lock()
	if b, ok := echCache.data[coverName]; ok {
		echCache.Unlock()
		return b, nil
	}
	echCache.Unlock()

	b, err := queryECHConfig(ctx, coverName, resolver)
	if err != nil {
		return nil, err
	}

	echCache.Lock()
	echCache.data[coverName] = b
	echCache.Unlock()
	return b, nil
}

func queryECHConfig(ctx context.Context, coverName, resolver string) ([]byte, error) {
	reqMsg := new(dns.Msg)
	reqMsg.SetQuestion(dns.Fqdn(coverName), dns.TypeHTTPS)
	reqMsg.RecursionDesired = true
	reqMsg.SetEdns0(1232, true)

	wire, err := reqMsg.Pack()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolver, bytes.NewReader(wire))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/dns-message")
	req.Header.Set("accept", "application/dns-message")
	req.Header.Set("user-agent", "msi/ech")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ECH resolver status: %s", resp.Status)
	}

	respMsg := new(dns.Msg)
	if err := respMsg.Unpack(body); err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`(?i)\bech\s*=\s*"?([A-Za-z0-9_\-+/=]+)"?`)
	for _, rr := range respMsg.Answer {
		m := re.FindStringSubmatch(rr.String())
		if len(m) != 2 {
			continue
		}
		echStr := m[1]
		for _, dec := range []func(string) ([]byte, error){
			base64.StdEncoding.DecodeString,
			base64.RawStdEncoding.DecodeString,
			base64.URLEncoding.DecodeString,
			base64.RawURLEncoding.DecodeString,
		} {
			if b, err := dec(echStr); err == nil && len(b) > 0 {
				return b, nil
			}
		}
	}
	return nil, errors.New("ECH config not found")
}
