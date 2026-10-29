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
// 刻意禁用 HTTP/2：多连接分段下载必须让每个分段独占一条 TCP 连接才能
// 聚合带宽；H2 会把全部分段多路复用进单条 TCP，慢速链路上互相拖累。
// 代理模式随设置即时生效（保存设置时重建客户端）。
func buildHTTPClient(st Settings) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   64,
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
// key 统一转为 MIME 规范大小写（cookie → Cookie）：每任务认证头经
// extraHeaders["Cookie"] 覆盖全局同名项，若全局 key 大小写不同就会在 map 里
// 留下两个同义键，最终谁生效取决于随机迭代顺序（同一文件的不同分段可能带
// 不同的 Cookie）。
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
		h[http.CanonicalHeaderKey(k)] = v
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

// sensitiveHeaders 跨站重定向时必须剥离的凭据类头。标准库本来就会剥掉它们
// （net/http 的 makeHeadersCopier），重定向策略无差别补回等于把该保护关掉：
// 站点被攻破或直链被劫持时，用户的会话 Cookie 会跟着 302 发往第三方。
var sensitiveHeaders = map[string]bool{
	"Cookie":              true,
	"Cookie2":             true,
	"Authorization":       true,
	"Proxy-Authorization": true,
}

// multiPartSuffixes 常见的多段公共后缀（eTLD）。完整 PSL 体积大可变得不偿失，
// 这里覆盖高频的一批；未命中的按「最后两段 = 注册域」处理，对陌生后缀只会
// 略微放宽同站判定（仍是同一后缀族下的域名才放行）。
var multiPartSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "me.uk": true,
	"com.cn": true, "net.cn": true, "org.cn": true, "gov.cn": true, "edu.cn": true, "ac.cn": true,
	"com.au": true, "net.au": true, "org.au": true, "edu.au": true, "gov.au": true, "id.au": true,
	"co.jp": true, "or.jp": true, "ne.jp": true, "ac.jp": true, "go.jp": true, "gr.jp": true,
	"com.br": true, "net.br": true, "org.br": true, "gov.br": true,
	"co.in": true, "net.in": true, "org.in": true, "gov.in": true, "ac.in": true,
	"com.ru": true, "org.ru": true, "net.ru": true,
	"com.tr": true, "com.mx": true, "com.ar": true, "com.sg": true, "com.hk": true,
	"co.kr": true, "or.kr": true, "co.nz": true, "co.za": true, "com.pl": true,
}

// registrableDomain 返回主机的注册域（eTLD+1）：example.com、example.co.uk。
// IP 字面量原样返回——只有完全相同才算同站。
func registrableDomain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || net.ParseIP(host) != nil {
		return host
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host
	}
	if len(labels) >= 3 && multiPartSuffixes[strings.Join(labels[len(labels)-2:], ".")] {
		return strings.Join(labels[len(labels)-3:], ".")
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// sameSite 判断两个 URL 是否属于同一站点：同主机、同一注册域（含子域），
// 且不允许 https → http 的协议降级（降级后凭据会走明文）。
// 判定失败（空 URL 等）一律视为异站，由调用方剥离敏感头。
func sameSite(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Scheme == "https" && b.Scheme != "https" {
		return false
	}
	if strings.EqualFold(a.Host, b.Host) {
		return true
	}
	ra, rb := registrableDomain(a.Hostname()), registrableDomain(b.Hostname())
	return ra != "" && ra == rb
}
