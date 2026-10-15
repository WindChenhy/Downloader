package engine

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseExtraHeaders(t *testing.T) {
	got := parseExtraHeaders(`
Referer: https://example.com/page
# 注释行
Cookie: a=1
BadLineWithoutColon
  Priority : high
`)
	want := map[string]string{
		"Referer":  "https://example.com/page",
		"Cookie":   "a=1",
		"Priority": "high",
	}
	if len(got) != len(want) {
		t.Fatalf("解析结果 = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("header[%q] = %q, want %q", k, got[k], v)
		}
	}
	if len(parseExtraHeaders("")) != 0 {
		t.Fatal("空输入应得到空 map")
	}
}

// TestParseExtraHeadersCanonicalizesKey 小写手填的 cookie 必须归一到 Cookie：
// 每任务认证头靠 extraHeaders["Cookie"] 覆盖全局同名项，两个同义键共存时
// 谁生效取决于 map 的随机迭代顺序，同一文件的不同分段可能带不同 Cookie。
func TestParseExtraHeadersCanonicalizesKey(t *testing.T) {
	got := parseExtraHeaders("cookie: a=1\nx-trace: 1\nREFERER: https://b.example")
	if len(got) != 3 {
		t.Fatalf("归一后不应出现同义重复键: %v", got)
	}
	for k := range got {
		if k != http.CanonicalHeaderKey(k) {
			t.Fatalf("key %q 未规范化", k)
		}
	}
	if got["Cookie"] != "a=1" || got["Referer"] != "https://b.example" || got["X-Trace"] != "1" {
		t.Fatalf("归一后取值错误: %v", got)
	}
}

// TestSameSite 同站判定：同主机、同一注册域（含子域、跨多段后缀）算同站；
// 异站、https→http 降级、IP 不同字面量都算异站（凭据不得外带）。
func TestSameSite(t *testing.T) {
	u := func(s string) *url.URL {
		parsed, err := url.Parse(s)
		if err != nil {
			t.Fatalf("解析 %q 失败: %v", s, err)
		}
		return parsed
	}
	same := [][2]string{
		{"https://dl.example.com/f.bin", "https://dl.example.com/g.bin"},
		{"https://dl.example.com/f.bin", "https://cdn.dl.example.com/f.bin"}, // 子域
		{"https://a.example.co.uk/f", "https://b.example.co.uk/f"},           // 多段后缀
		{"http://example.com/f", "http://www.example.com/f"},
		{"https://EXAMPLE.com/f", "https://cdn.example.com/f"}, // 大小写不敏感
	}
	for _, pair := range same {
		if !sameSite(u(pair[0]), u(pair[1])) {
			t.Fatalf("%s → %s 应判为同站", pair[0], pair[1])
		}
	}
	diff := [][2]string{
		{"https://dl.example.com/f.bin", "https://cdn.other.com/f.bin"},
		{"https://example.com/f", "https://example.co.uk/f"},            // 注册域不同
		{"https://dl.example.com/f.bin", "http://dl.example.com/f.bin"}, // 协议降级
		{"https://127.0.0.1/f.bin", "https://127.0.0.2/f.bin"},          // IP 字面量
	}
	for _, pair := range diff {
		if sameSite(u(pair[0]), u(pair[1])) {
			t.Fatalf("%s → %s 应判为异站", pair[0], pair[1])
		}
	}
	if sameSite(u("https://dl.example.com/f"), nil) {
		t.Fatal("nil URL 应判为异站")
	}
}

func TestResolveFetchURLMirror(t *testing.T) {
	st := defaultSettings()
	gh := "https://github.com/x/y/releases/download/v1/a.zip"
	other := "https://example.com/a.zip"

	if got := resolveFetchURL(st, gh); got != gh {
		t.Fatalf("未开启镜像时应原样返回, got %q", got)
	}

	st.GitHubMirror = true
	want := strings.ReplaceAll(DefaultMirrorTemplate, "{url}", gh)
	if got := resolveFetchURL(st, gh); got != want {
		t.Fatalf("GitHub 链接应走镜像, got %q, want %q", got, want)
	}
	if got := resolveFetchURL(st, other); got != other {
		t.Fatalf("非 GitHub 链接不应改写, got %q", got)
	}

	st.MirrorTemplate = "https://mirror.example.com/proxy/"
	if got := resolveFetchURL(st, gh); got != "https://mirror.example.com/proxy/"+gh {
		t.Fatalf("无占位符模板应按前缀拼接, got %q", got)
	}
}

func TestIsDownloadableURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com/a.zip", true},
		{"  http://example.com/a.zip \n", true},
		{`"https://example.com/a.zip"`, true},
		{"ftp://example.com/a.zip", false},
		{"hello world", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsDownloadableURL(c.in); got != c.want {
			t.Errorf("IsDownloadableURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSettingsFirstRunDefaults(t *testing.T) {
	m, _ := mustManager(t) // 数据目录为空 → 首启默认值
	st := m.GetSettings()
	if !st.ClipboardWatch {
		t.Error("首启应默认开启剪贴板监听")
	}
	if !st.APIEnabled || st.APIPort != DefaultAPIPort {
		t.Errorf("首启 API 默认值不对: enabled=%v port=%d", st.APIEnabled, st.APIPort)
	}
	if st.ProxyMode != ProxyNone {
		t.Errorf("代理默认应为 none, got %q", st.ProxyMode)
	}
}

func TestRateLimitSlowsDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("计时测试，短模式跳过")
	}
	content := newContent(t, 300<<10) // 300KB
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	// 限速 100KB/s → 300KB 至少需要 ~3s
	if err := m.SaveSettings(Settings{
		SaveDir:         saveDir,
		Connections:     2,
		ConcurrentTasks: 1,
		SpeedLimit:      100 << 10,
	}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	first, err := m.AddTask(srv.URL, saveDir, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return taskOf(t, m, first.ID).Status == StatusCompleted
	}, 30*time.Second)
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Fatalf("限速 100KB/s 下载 300KB 只用了 %v，限速未生效", elapsed)
	}

	// 解除限速后任务应明显更快
	if err := m.SaveSettings(Settings{
		SaveDir:         saveDir,
		Connections:     2,
		ConcurrentTasks: 1,
		SpeedLimit:      0,
	}); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	second, err := m.AddTask(srv.URL, saveDir, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	// 必须盯住「第二个任务」本身：GetTasks()[0] 是此刻已完成的第一条记录，
	// 等它会在瞬间返回——断言 elapsed 形同虚设，而且后台那个下载会和
	// t.TempDir() 的清理抢目录（Windows 上报「目录非空」而随机失败）。
	waitFor(t, func() bool {
		return taskOf(t, m, second.ID).Status == StatusCompleted
	}, 10*time.Second)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("不限速下载 300KB 用了 %v，异常缓慢", elapsed)
	}
}

func TestApplyExtraHeadersSkipsControlHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/a", nil)
	applyExtraHeaders(req, map[string]string{
		"Range":  "bytes=0-1",
		"Accept": "application/octet-stream",
	})
	if req.Header.Get("Range") != "" {
		t.Fatal("Range 是引擎控制头，不允许被自定义头覆盖")
	}
	if req.Header.Get("Accept") != "application/octet-stream" {
		t.Fatal("普通自定义头应被写入")
	}
}
