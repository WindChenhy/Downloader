package engine

import (
	"net"
	"net/http"
	"time"
)

// newHTTPClient 只对建连/TLS/响应头设超时，不对响应体设整体超时——
// 大文件慢速传输不应该被一个端到端超时杀掉（旧实现的缺陷）。
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
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
		},
	}
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) downloader/1.0"
