package engine

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultUA 未在设置中自定义时的兜底 User-Agent。
const defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) downloader/1.0"

// buildHTTPClient 只对建连/TLS/响应头设超时，不对响应体设整体超时——
// 大文件慢速传输不应该被一个端到端超时杀掉（旧实现的缺陷）。
// 代理模式随设置即时生效（保存设置时重建客户端）。
func buildHTTPClient(st Settings) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	switch st.ProxyMode {
	case ProxySystem:
		transport.Proxy = http.ProxyFromEnvironment
	case ProxyCustom:
		if u, err := url.Parse(strings.TrimSpace(st.ProxyURL)); err == nil && u.Scheme != "" && u.Host != "" {
			transport.Proxy = http.ProxyURL(u) // http/https/socks5 均由标准库支持
		}
	}
	return &http.Client{Transport: transport}
}

// parseExtraHeaders 解析每行一条 "Key: Value" 的自定义请求头，# 开头为注释。
func parseExtraHeaders(raw string) map[string]string {
	h := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		h[k] = v
	}
	return h
}

// applyExtraHeaders 把自定义请求头写入请求，但保留引擎控制的关键字段。
func applyExtraHeaders(req *http.Request, extra map[string]string) {
	for k, v := range extra {
		switch http.CanonicalHeaderKey(k) {
		case "Range", "Host", "Content-Length", "User-Agent":
			continue
		}
		req.Header.Set(k, v)
	}
}
