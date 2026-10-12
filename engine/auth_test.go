package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// headerRecorder 记录每个请求的头，并像静态文件一样响应 HEAD/Range。
type headerRecorder struct {
	content []byte

	mu   sync.Mutex
	seen []http.Header
}

func (h *headerRecorder) headers() []http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]http.Header(nil), h.seen...)
}

func (h *headerRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.seen = append(h.seen, r.Header.Clone())
	h.mu.Unlock()
	// ServeContent 自带 HEAD、Range/206 与 Accept-Ranges 处理
	w.Header().Set("Content-Disposition", `attachment; filename="auth.bin"`)
	http.ServeContent(w, r, "auth.bin", time.Time{}, bytes.NewReader(h.content))
}

// TestPerTaskAuthHeaders 每任务 Cookie/Referer 覆盖全局自定义请求头中的同名项，
// 未填写的任务仍沿用全局值。
func TestPerTaskAuthHeaders(t *testing.T) {
	h := &headerRecorder{content: make([]byte, 2000)}
	srv := httptest.NewServer(h)
	defer srv.Close()

	m, saveDir := mustManager(t)
	st := m.GetSettings()
	st.ExtraHeaders = "Cookie: global=1\nReferer: https://global.example\nX-Trace: keep"
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}

	authTask, err := m.AddTaskWithChecksum(AddTaskParams{
		URL:         srv.URL,
		SaveDir:     saveDir,
		Connections: 2,
		CustomName:  "auth.bin",
		Cookie:      "site=abc",
		Referer:     "https://page.example/file",
	})
	if err != nil {
		t.Fatal(err)
	}
	plainTask, err := m.AddTask(srv.URL, saveDir, 2, "plain.bin")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		a, b := authTask.ID, plainTask.ID
		for _, tk := range m.GetTasks() {
			if tk.ID == a && tk.Status != StatusCompleted {
				return false
			}
			if tk.ID == b && tk.Status != StatusCompleted {
				return false
			}
		}
		return true
	}, 15*time.Second)

	var authSeen, plainSeen []http.Header
	for _, hd := range h.headers() {
		// 两个任务打同一台服务器，用 X-Trace 之外的 Referer 区分来源
		if hd.Get("Referer") == "https://page.example/file" {
			authSeen = append(authSeen, hd)
		} else {
			plainSeen = append(plainSeen, hd)
		}
	}
	if len(authSeen) == 0 || len(plainSeen) == 0 {
		t.Fatalf("未按预期区分两个任务的请求: auth=%d plain=%d", len(authSeen), len(plainSeen))
	}
	for _, hd := range authSeen {
		if got := hd.Get("Cookie"); got != "site=abc" {
			t.Fatalf("每任务 Cookie 应覆盖全局: got %q, want site=abc", got)
		}
		if got := hd.Get("X-Trace"); got != "keep" {
			t.Fatalf("全局自定义头应保留其他项: got %q", got)
		}
	}
	for _, hd := range plainSeen {
		if got := hd.Get("Cookie"); got != "global=1" {
			t.Fatalf("未填 Cookie 的任务应沿用全局值: got %q", got)
		}
	}
}

// scrubRedirectRT 第一次请求返回 302 到另一个子域，第二次记录请求头并给出内容。
type scrubRedirectRT struct {
	target string

	calls  int
	second http.Header
}

func (rt *scrubRedirectRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls++
	if rt.calls == 1 {
		res := &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{},
			Body:       http.NoBody,
			Request:    req,
		}
		res.Header.Set("Location", rt.target)
		return res, nil
	}
	rt.second = req.Header.Clone()
	body := "0123456789"
	return &http.Response{
		StatusCode:    http.StatusPartialContent,
		Header:        http.Header{},
		Body:          io.NopCloser(strings.NewReader(body)),
		Request:       req,
		ContentLength: int64(len(body)),
	}, nil
}

// TestRedirectReappliesAuthHeaders 跨子域（同注册域）重定向会剥掉 Cookie，
// 本轮的重定向策略必须把 Cookie/Referer/UA 重新施加回去，同时绝不覆盖 Range。
func TestRedirectReappliesAuthHeaders(t *testing.T) {
	r := &taskRunner{
		extraHeaders: map[string]string{"Cookie": "token=abc", "Referer": "https://dl.example.com/page"},
		userAgent:    "downloader/test",
	}

	// 先确认标准库在这个域名对上确实会剥掉 Cookie（策略存在的前提）
	ctrlRT := &scrubRedirectRT{target: "https://cdn.dl.example.com/f.bin"}
	ctrl := &http.Client{Transport: ctrlRT}
	ctrlReq, err := http.NewRequest(http.MethodGet, "https://dl.example.com/f.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctrlReq.Header.Set("Cookie", "token=abc")
	ctrlReq.Header.Set("Range", "bytes=0-9")
	resp, err := ctrl.Do(ctrlReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if scrubbed := ctrlRT.second.Get("Cookie") == ""; !scrubbed {
		t.Log("注意：标准库未剥离跨子域 Cookie，重定向策略可能已非必要")
	}

	rt := &scrubRedirectRT{target: "https://cdn.dl.example.com/f.bin"}
	c := &http.Client{Transport: rt, CheckRedirect: r.keepHeadersOnRedirect}
	req, err := http.NewRequest(http.MethodGet, "https://dl.example.com/f.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "token=abc")
	req.Header.Set("Referer", "https://dl.example.com/page")
	req.Header.Set("User-Agent", "downloader/test")
	req.Header.Set("Range", "bytes=0-9")
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if rt.calls != 2 {
		t.Fatalf("应跟随一次重定向, calls = %d", rt.calls)
	}
	if got := rt.second.Get("Cookie"); got != "token=abc" {
		t.Fatalf("重定向后 Cookie = %q, want token=abc", got)
	}
	if got := rt.second.Get("Referer"); got != "https://dl.example.com/page" {
		t.Fatalf("重定向后 Referer = %q", got)
	}
	if got := rt.second.Get("User-Agent"); got != "downloader/test" {
		t.Fatalf("重定向后 User-Agent = %q", got)
	}
	if got := rt.second.Get("Range"); got != "bytes=0-9" {
		t.Fatalf("重定向后 Range = %q, want bytes=0-9（覆盖会写坏分段偏移）", got)
	}
}

// TestRedirectStripsAuthHeadersCrossSite 异站重定向不得携带凭据：
// 站点被攻破或直链被劫持时，Cookie 不能跟着 302 去往第三方收集端。
// UA/Referer 等非敏感头照发（防盗链站点仍能识别来源），Range 保持不动。
func TestRedirectStripsAuthHeadersCrossSite(t *testing.T) {
	r := &taskRunner{
		extraHeaders: map[string]string{
			"Cookie":        "token=abc",
			"Authorization": "Bearer secret",
			"Referer":       "https://dl.example.com/page",
		},
		userAgent: "downloader/test",
	}
	rt := &scrubRedirectRT{target: "https://cdn.other.com/f.bin"}
	c := &http.Client{Transport: rt, CheckRedirect: r.keepHeadersOnRedirect}
	req, err := http.NewRequest(http.MethodGet, "https://dl.example.com/f.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "token=abc")
	req.Header.Set("Referer", "https://dl.example.com/page")
	req.Header.Set("User-Agent", "downloader/test")
	req.Header.Set("Range", "bytes=0-9")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := rt.second.Get("Cookie"); got != "" {
		t.Fatalf("异站重定向不应携带 Cookie: %q", got)
	}
	if got := rt.second.Get("Authorization"); got != "" {
		t.Fatalf("异站重定向不应携带 Authorization: %q", got)
	}
	if got := rt.second.Get("Referer"); got != "https://dl.example.com/page" {
		t.Fatalf("异站重定向应保留 Referer: %q", got)
	}
	if got := rt.second.Get("Range"); got != "bytes=0-9" {
		t.Fatalf("异站重定向后 Range = %q, want bytes=0-9", got)
	}
}

// TestRedirectStripsAuthHeadersOnDowngrade https → http 降级（即便同主机）
// 也不得携带凭据：降级后 Cookie 会走明文。
func TestRedirectStripsAuthHeadersOnDowngrade(t *testing.T) {
	r := &taskRunner{
		extraHeaders: map[string]string{"Cookie": "token=abc"},
		userAgent:    "downloader/test",
	}
	rt := &scrubRedirectRT{target: "http://dl.example.com/f.bin"}
	c := &http.Client{Transport: rt, CheckRedirect: r.keepHeadersOnRedirect}
	req, err := http.NewRequest(http.MethodGet, "https://dl.example.com/f.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "token=abc")
	req.Header.Set("User-Agent", "downloader/test")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := rt.second.Get("Cookie"); got != "" {
		t.Fatalf("协议降级重定向不应携带 Cookie: %q", got)
	}
}

// TestRedirectStopsAfterTooManyHops 重定向链长度沿用标准库的 10 跳上限，
// 超过时返回最后一个响应而不是无限跟随。
func TestRedirectStopsAfterTooManyHops(t *testing.T) {
	r := &taskRunner{extraHeaders: map[string]string{"Cookie": "token=abc"}, userAgent: "ua"}
	c := &http.Client{
		Transport:     &loopRedirectRT{},
		CheckRedirect: r.keepHeadersOnRedirect,
	}
	req, err := http.NewRequest(http.MethodGet, "https://dl.example.com/f.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("超出跳数应返回最后一个 302, got %d", resp.StatusCode)
	}
}

// loopRedirectRT 每次都返回指向自身的 302。
type loopRedirectRT struct{ calls int }

func (rt *loopRedirectRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls++
	res := &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}
	res.Header.Set("Location", "https://dl.example.com/f.bin")
	return res, nil
}

// TestExportStripsAuthHeaders 导出的任务包默认剥离 Cookie/Referer（常含会话凭据），
// 显式 withAuth 才带出；带认证的包导入回来得能还原这两项。
func TestExportStripsAuthHeaders(t *testing.T) {
	content := newContent(t, 400)
	ts := &testServer{content: content, etag: `"e"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	if _, err := m.AddTaskWithChecksum(AddTaskParams{
		URL: srv.URL, SaveDir: saveDir, Connections: 1,
		Cookie: "session=secret", Referer: "https://site.example/item",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusCompleted }, 10*time.Second)
	if got := m.GetTasks()[0].Cookie; got != "session=secret" {
		t.Fatalf("任务上的 Cookie 应保留在本地记录: got %q", got)
	}

	plain, err := m.ExportTasksJSON(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "session=secret") || strings.Contains(string(plain), "site.example/item") {
		t.Fatal("默认导出不应出现认证信息")
	}
	var arr []Task
	if err := json.Unmarshal(plain, &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0].Cookie != "" || arr[0].Referer != "" {
		t.Fatalf("默认导出应剥离认证字段: %+v", arr)
	}

	withAuth, err := m.ExportTasksJSON(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(withAuth, &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0].Cookie != "session=secret" || arr[0].Referer != "https://site.example/item" {
		t.Fatalf("withAuth 导出应保留认证字段: %+v", arr)
	}

	// 导入回填：闭闸 + 不可达地址，避免测试期间真的发起下载
	pkg, err := json.Marshal([]Task{{
		URL: "http://127.0.0.1:1/x.bin", Connections: 1,
		Status:  StatusPaused,
		Cookie:  "session=secret",
		Referer: "https://site.example/item",
	}})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(t.TempDir(), nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m2)
	if err := m2.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	res, err := m2.ImportTasksJSON(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("导入报错: %v", res.Errors)
	}
	if len(res.Tasks) != 1 || res.Tasks[0].Cookie != "session=secret" || res.Tasks[0].Referer != "https://site.example/item" {
		t.Fatalf("导入未回填认证字段: %+v", res.Tasks)
	}
}

// TestRunClientWiresRedirectPolicy runClient 必须把重定向策略装进本轮下载
// 所用的客户端。只测 keepHeadersOnRedirect 本体不够：一旦有人把 r.client 改回
// m.httpClient()，跨子域重定向又会丢 Cookie，而策略函数的用例照样全绿。
func TestRunClientWiresRedirectPolicy(t *testing.T) {
	m, _ := mustManager(t)
	r := &taskRunner{m: m, extraHeaders: map[string]string{"Cookie": "token=abc"}, userAgent: "downloader/test"}

	shared := m.httpClient()
	c := r.runClient()
	if c == shared {
		t.Fatal("runClient 应返回副本，不能直接交回 Manager 共享的客户端")
	}
	if c.Transport != shared.Transport {
		t.Fatal("应复用同一 Transport，不得每轮重建连接池")
	}
	if shared.CheckRedirect != nil {
		t.Fatal("策略只能挂在副本上，不能污染共享客户端")
	}
	if c.CheckRedirect == nil {
		t.Fatal("runClient 未安装重定向策略")
	}

	req, err := http.NewRequest(http.MethodGet, "https://cdn.other.com/f.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=100-199")
	if err := c.CheckRedirect(req, []*http.Request{req}); err != nil {
		t.Fatalf("单跳重定向不应报错: %v", err)
	}
	if got := req.Header.Get("Cookie"); got != "token=abc" {
		t.Fatalf("装配后的策略未重施 Cookie: %q", got)
	}
	if got := req.Header.Get("Range"); got != "bytes=100-199" {
		t.Fatalf("重定向不得覆盖 Range: %q", got)
	}
}

// hopRedirectRT 用假主机名制造跨子域重定向：origin 第一跳返 302 到 cdn，
// 之后的请求改写回真实的 httptest 后端并记录请求头。因为只换了 Transport，
// 重定向仍由引擎本轮实际使用的客户端（runClient 的产物）处理。
type hopRedirectRT struct {
	baseHost string // 127.0.0.1:port
	origHost string // 需登录的第一跳主机
	cdnHost  string // 签名 CDN（与 orig 不同域，标准库会剥 Cookie）

	mu        sync.Mutex
	forwarded []http.Header
}

func (rt *hopRedirectRT) records() []http.Header {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]http.Header(nil), rt.forwarded...)
}

func (rt *hopRedirectRT) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Host {
	case rt.origHost:
		res := &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{},
			Body:       http.NoBody,
			Request:    req,
		}
		res.Header.Set("Location", "http://"+rt.cdnHost+req.URL.Path)
		return res, nil
	case rt.cdnHost:
		rt.mu.Lock()
		rt.forwarded = append(rt.forwarded, req.Header.Clone())
		rt.mu.Unlock()
		proxyReq, err := http.NewRequestWithContext(req.Context(), req.Method,
			"http://"+rt.baseHost+req.URL.RequestURI(), req.Body)
		if err != nil {
			return nil, err
		}
		proxyReq.Header = req.Header.Clone()
		return http.DefaultTransport.RoundTrip(proxyReq)
	default:
		return nil, fmt.Errorf("未预期的主机 %q", req.URL.Host)
	}
}

// TestDownloadAcrossRedirectKeepsAuthAndRange 在真实下载路径上验证 L1+L2：
// 来路返 302 到另一子域的签名 CDN，第二跳必须仍带上任务 Cookie，且各分段
// Range 不被重施头覆盖——后者一旦失效，写盘偏移就乱了，校验和会报 mismatch。
func TestDownloadAcrossRedirectKeepsAuthAndRange(t *testing.T) {
	content := newContent(t, 8000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := ":" + base.Port()
	rt := &hopRedirectRT{baseHost: base.Host, origHost: "dl.example.test" + port, cdnHost: "cdn.example.test" + port}

	m, saveDir := mustManagerWithClient(t, rt)
	sum := sha256.Sum256(content)
	task, err := m.AddTaskWithChecksum(AddTaskParams{
		URL:              "http://" + rt.origHost + "/f.bin",
		SaveDir:          saveDir,
		Connections:      2,
		Cookie:           "session=abc",
		ChecksumAlgo:     "sha256",
		ChecksumExpected: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, tk := range m.GetTasks() {
			if tk.ID == task.ID {
				return tk.Status == StatusCompleted || tk.Status == StatusFailed
			}
		}
		return false
	}, 20*time.Second)

	final := m.GetTasks()[0]
	if final.Status != StatusCompleted {
		t.Fatalf("带 Cookie 的重定向下载应成功: %s %s", final.Status, final.Error)
	}
	if final.ChecksumStatus != ChecksumOK {
		t.Fatalf("分段偏移应在重定向后保持不变，校验应通过: %s", final.ChecksumStatus)
	}

	records := rt.records()
	if len(records) < 3 {
		t.Fatalf("应有多次第二跳请求（探测 + 分段）: %d", len(records))
	}
	var ranged int
	for _, hd := range records {
		if got := hd.Get("Cookie"); got != "session=abc" {
			t.Fatalf("重定向后 Cookie 丢失: %q", got)
		}
		if rng := hd.Get("Range"); rng != "" && rng != "bytes=0-0" {
			ranged++
		}
	}
	if ranged < 2 {
		t.Fatalf("分段请求的 Range 应在重定向后保留, got %d 次", ranged)
	}
}
