package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ProbeResult 是对 URL 的预探测结果。
type ProbeResult struct {
	FinalURL     string
	TotalSize    int64 // <=0 表示未知
	AcceptsRange bool
	ETag         string
	LastModified string
	FileName     string
}

// validateURL 校验并解析 URL，仅接受 http/https。
func validateURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("URL 不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("无效的 URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("仅支持 http/https 链接")
	}
	return u, nil
}

// IsDownloadableURL 判断一段文本（如剪贴板内容）是否为可下载的 http(s) 链接。
func IsDownloadableURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'<>()`)
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return false
	}
	_, err := validateURL(raw)
	return err == nil
}

// probeURL 用一次 HEAD（失败或信息不全时补一次 Range: bytes=0-0 的 GET）
// 获取文件大小、Range 支持和文件名。ua 与 extra 应用到全部探测请求。
func probeURL(ctx context.Context, client *http.Client, rawURL string, ua string, extra map[string]string) (*ProbeResult, error) {
	u, err := validateURL(rawURL)
	if err != nil {
		return nil, err
	}
	res := &ProbeResult{FinalURL: rawURL}

	head, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, err
	}
	applyExtraHeaders(head, extra)
	head.Header.Set("User-Agent", ua)
	if resp, err := client.Do(head); err == nil {
		drainAndClose(resp)
		if resp.StatusCode/100 == 2 {
			res.TotalSize = parseContentLength(resp.Header.Get("Content-Length"))
			res.AcceptsRange = strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")
			res.ETag = resp.Header.Get("ETag")
			res.LastModified = resp.Header.Get("Last-Modified")
			res.FileName = parseFilenameFromCD(resp.Header.Get("Content-Disposition"))
			if resp.Request != nil && resp.Request.URL != nil {
				res.FinalURL = resp.Request.URL.String()
			}
		}
	}

	// HEAD 缺信息（被禁用/不完整）时用 GET Range: bytes=0-0 补齐
	if res.TotalSize <= 0 || !res.AcceptsRange {
		get, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		applyExtraHeaders(get, extra)
		get.Header.Set("User-Agent", ua)
		get.Header.Set("Range", "bytes=0-0")
		resp, err := client.Do(get)
		if err != nil {
			if res.TotalSize <= 0 {
				return nil, err
			}
		} else {
			drainAndClose(resp)
			if resp.Request != nil && resp.Request.URL != nil {
				res.FinalURL = resp.Request.URL.String()
			}
			switch resp.StatusCode {
			case http.StatusPartialContent:
				res.AcceptsRange = true
				if total := parseContentRangeTotal(resp.Header.Get("Content-Range")); total > 0 {
					res.TotalSize = total
				}
			case http.StatusOK:
				if res.TotalSize <= 0 {
					res.TotalSize = parseContentLength(resp.Header.Get("Content-Length"))
				}
			}
		}
	}

	if res.FileName == "" {
		res.FileName = fileNameFromURL(u)
	}
	return res, nil
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	_ = resp.Body.Close()
}

func parseContentLength(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseContentRangeTotal 解析 "bytes 0-0/12345" 中的总大小。
func parseContentRangeTotal(s string) int64 {
	_, total, ok := strings.Cut(s, "/")
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseFilenameFromCD 解析 Content-Disposition 中的文件名，
// 优先 RFC 5987 的 filename*=，其次普通 filename=。
func parseFilenameFromCD(h string) string {
	if h == "" {
		return ""
	}
	parts := strings.Split(h, ";")
	for _, p := range parts { // filename*=UTF-8''... 优先
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "filename*") {
			continue
		}
		if name := decodeRFC5987(strings.TrimSpace(v)); name != "" {
			return name
		}
	}
	for _, p := range parts {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "filename") {
			continue
		}
		name := strings.TrimSpace(v)
		name = strings.Trim(name, `"`)
		if name != "" {
			return name
		}
	}
	return ""
}

// decodeRFC5987 解析 charset''percent-encoded 形式，如 UTF-8''%E4%B8%AD.zip
func decodeRFC5987(v string) string {
	charset, rest, ok := strings.Cut(v, "''")
	if !ok {
		charset, rest, ok = strings.Cut(v, "'")
		if !ok {
			return ""
		}
	}
	if charset == "" {
		charset = "UTF-8"
	}
	if !strings.EqualFold(charset, "utf-8") {
		return ""
	}
	name, err := url.PathUnescape(rest)
	if err != nil || !utf8.ValidString(name) {
		return ""
	}
	return name
}

// fileNameFromURL 从 URL 路径推断文件名。
func fileNameFromURL(u *url.URL) string {
	name := ""
	if u != nil && u.Path != "" {
		decoded, err := url.PathUnescape(u.Path)
		if err == nil {
			u.Path = decoded
		}
		segments := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segments) > 0 {
			name = segments[len(segments)-1]
		}
	}
	name = sanitizeFileName(name)
	if name == "" {
		name = "download"
	}
	return name
}

var invalidChars = `<>:"/\|?*`

// sanitizeFileName 去掉 Windows 非法字符与控制字符。
func sanitizeFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(invalidChars, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	if name == "" {
		return ""
	}
	if len(name) > 150 {
		name = name[:150]
	}
	return name
}
