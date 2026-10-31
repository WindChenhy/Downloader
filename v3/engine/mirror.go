package engine

import (
	"net/url"
	"strings"
)

// mirrorHosts 命中这些域名才应用镜像模板。
var mirrorHosts = map[string]bool{
	"github.com":                    true,
	"codeload.github.com":           true,
	"objects.githubusercontent.com": true,
	"raw.githubusercontent.com":     true,
	"gist.github.com":               true,
}

// resolveFetchURL 依据设置返回实际请求 URL：开启 GitHub 镜像且命中
// GitHub 相关域名时按模板改写，{url} 为原始完整链接；模板不含占位符
// 时视为前缀拼接。其余情况原样返回。
func resolveFetchURL(st Settings, rawURL string) string {
	if !st.GitHubMirror {
		return rawURL
	}
	tpl := strings.TrimSpace(st.MirrorTemplate)
	if tpl == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || !mirrorHosts[strings.ToLower(u.Host)] {
		return rawURL
	}
	if strings.Contains(tpl, "{url}") {
		return strings.ReplaceAll(tpl, "{url}", rawURL)
	}
	return strings.TrimSuffix(tpl, "/") + "/" + rawURL
}
