package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"downloader/engine"
)

// rangeServer 模拟支持 Range 的静态文件服务，供端到端下载用。
type rangeServer struct {
	content []byte
	gate    chan struct{} // 非 nil 时阻塞第一个 GET，直到被关闭
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.gate != nil && r.Method == http.MethodGet {
		<-s.gate
	}
	w.Header().Set("ETag", `"abc"`)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.Itoa(len(s.content)))
	if r.Method == http.MethodHead {
		return
	}
	start, end := 0, len(s.content)-1
	if rng := r.Header.Get("Range"); rng != "" {
		if _, err := fmt.Sscanf(rng, "bytes=%d-%d", &start, &end); err != nil {
			start, end = 0, len(s.content)-1
		}
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_, _ = w.Write(s.content[start : end+1])
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("等待超时")
}

func newBackend(t *testing.T) *engine.Manager {
	t.Helper()
	m, err := engine.NewManager(t.TempDir(), func(engine.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// apiDo 向被测 API 发送带令牌的请求并返回响应。
func apiDo(t *testing.T, method, url, token, contentType, body string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("X-API-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// apiTasks 读取任务列表（须带令牌）。
func apiTasks(t *testing.T, base, token string) []engine.Task {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("任务列表状态码 = %d", resp.StatusCode)
	}
	var tasks []engine.Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	return tasks
}

// TestEndToEnd 通过 REST API 完整走一遍：新建 → 进度 → 暂停 → 恢复 → 删除。
func TestEndToEnd(t *testing.T) {
	content := make([]byte, 10000)
	rs := &rangeServer{content: content, gate: make(chan struct{})}
	srv := httptest.NewServer(rs)
	defer srv.Close()

	m := newBackend(t)
	saveDir := m.GetSettings().SaveDir
	st := m.GetSettings()
	st.APIEnabled = false // 测试不真正监听端口
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	// ping（免令牌）
	resp, err := http.Get(api.URL + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ping 状态码 = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 新建任务
	body := fmt.Sprintf(`{"url": %q, "saveDir": %q, "connections": 4}`, srv.URL, saveDir)
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("新建任务状态码 = %d", resp.StatusCode)
	}
	var task engine.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// 任务列表可见且在运行
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		return len(tasks) == 1 && tasks[0].Status == engine.StatusRunning
	}, 5*time.Second)

	// 不存在的任务 → 404
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks/no-such-id/pause", token, "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("暂停不存在的任务应 404, got %d", resp.StatusCode)
	}

	// 暂停
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks/"+task.ID+"/pause", token, "", "")
	resp.Body.Close()
	waitFor(t, func() bool {
		return apiTasks(t, api.URL, token)[0].Status == engine.StatusPaused
	}, 5*time.Second)

	// 恢复
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks/"+task.ID+"/continue", token, "", "")
	resp.Body.Close()
	close(rs.gate) // 放行被 gate 阻塞的下载请求
	waitFor(t, func() bool {
		return apiTasks(t, api.URL, token)[0].Status == engine.StatusCompleted
	}, 10*time.Second)

	// 删除
	resp = apiDo(t, http.MethodDelete, api.URL+"/api/v1/tasks/"+task.ID, token, "", "")
	resp.Body.Close()
	if len(apiTasks(t, api.URL, token)) != 0 {
		t.Fatal("删除后任务列表应为空")
	}

	// 非法 URL → 400
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", `{"url":"nope"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 URL 应返回 400, got %d", resp.StatusCode)
	}
}

// TestSettingsRoundtrip 设置经 API 读写一致，并即时生效到引擎。
func TestSettingsRoundtrip(t *testing.T) {
	m := newBackend(t)
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	st := m.GetSettings()
	st.SpeedLimit = 512 << 10
	st.ProxyMode = engine.ProxySystem
	st.APIEnabled = false // 测试不真正监听端口
	st.AllowSaveDirs = []string{"D:/downloader-allowed"}
	raw, _ := json.Marshal(st)
	req, _ := http.NewRequest(http.MethodPut, api.URL+"/api/v1/settings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got engine.Settings
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.SpeedLimit != 512<<10 || got.ProxyMode != engine.ProxySystem {
		t.Fatalf("设置回读不一致: %+v", got)
	}
	if len(got.AllowSaveDirs) != 1 || got.AllowSaveDirs[0] != `D:\downloader-allowed` {
		t.Fatalf("白名单应清洗保留: %+v", got.AllowSaveDirs)
	}
	if cur := m.GetSettings(); cur.SpeedLimit != 512<<10 {
		t.Fatalf("引擎内设置未更新: %+v", cur)
	}
}

// TestGuard 鉴权守卫逐层拦截：令牌、Host、Origin、Content-Type。
func TestGuard(t *testing.T) {
	m := newBackend(t)
	token := m.GetSettings().APIToken
	h := New(m).Handler()

	do := func(host, origin, ct, tok string, body string) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
		if body != "" {
			req = httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString(body))
		}
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if tok != "" {
			req.Header.Set("X-API-Token", tok)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Result()
	}

	// 无令牌 → 401
	if resp := do("127.0.0.1:8199", "", "application/json", "", `{"url":"https://x/a.zip"}`); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无令牌应 401, got %d", resp.StatusCode)
	}
	// 错误令牌 → 401
	if resp := do("127.0.0.1:8199", "", "application/json", "wrong-token", `{"url":"https://x/a.zip"}`); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错误令牌应 401, got %d", resp.StatusCode)
	}
	// 非回环 Host（DNS rebinding 形态）→ 403
	if resp := do("evil.example.com:8199", "", "application/json", token, `{"url":"https://x/a.zip"}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("非回环 Host 应 403, got %d", resp.StatusCode)
	}
	// 跨站 Origin → 403
	if resp := do("127.0.0.1:8199", "https://evil.example.com", "application/json", token, `{"url":"https://x/a.zip"}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("跨站 Origin 应 403, got %d", resp.StatusCode)
	}
	// 带 body 但 Content-Type 非 JSON（CSRF 简单请求形态）→ 403
	if resp := do("127.0.0.1:8199", "", "text/plain", token, `{"url":"https://x/a.zip"}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("text/plain 请求体应 403, got %d", resp.StatusCode)
	}
	// 合法回环 + 令牌 + JSON → 通过守卫（失败只能来自业务校验，不是 401/403）
	resp := do("127.0.0.1:8199", "", "application/json", token, `{"url":"https://x/a.zip"}`)
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		t.Fatalf("合法请求不应被守卫拦截, got %d", resp.StatusCode)
	}
}

// TestSaveDirWhitelist REST 新建任务的 saveDir 必须在白名单内。
func TestSaveDirWhitelist(t *testing.T) {
	m := newBackend(t)
	saveDir := m.GetSettings().SaveDir
	st := m.GetSettings()
	st.SaveDir = saveDir
	st.APIEnabled = false // 测试不真正监听端口
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	// 白名单外目录 → 403
	body := `{"url": "https://127.0.0.1/x.zip", "saveDir": "C:/Windows/System32"}`
	resp := apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("白名单外 saveDir 应 403, got %d", resp.StatusCode)
	}

	// 白名单内（默认下载目录）→ 正常创建
	body = fmt.Sprintf(`{"url": "https://127.0.0.1/x.zip", "saveDir": %q}`, saveDir)
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("默认目录内 saveDir 应 201, got %d", resp.StatusCode)
	}

	// 连文件删除同样受白名单约束：白名单只含默认下载目录时，
	// 经普通 AddTask 建在白名单外目录的任务不能连文件删
	task, err := m.AddTask("https://127.0.0.1/y.zip", "Z:/elsewhere", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	resp = apiDo(t, http.MethodDelete, api.URL+"/api/v1/tasks/"+task.ID+"?files=1", token, "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("白名单外连文件删除应 403, got %d", resp.StatusCode)
	}
	// 仅删记录不受限
	resp = apiDo(t, http.MethodDelete, api.URL+"/api/v1/tasks/"+task.ID, token, "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("仅删记录应 200, got %d", resp.StatusCode)
	}
}
