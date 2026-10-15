package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	t.Cleanup(m.Stop) // 停掉后台循环，别和 TempDir 清理抢目录
	// 默认保存目录必须落到临时目录：用例里大多沿用 settings.saveDir 建任务，
	// 不收敛的话下载会真的写进用户主目录的 Downloads（实测留下过
	// download、download (1)…(15) 一串 10KB 垃圾文件，测试不该污染用户机器）。
	// 白名单默认只含默认下载目录，这里一并收敛。
	st := m.GetSettings()
	st.SaveDir = t.TempDir()
	st.AllowSaveDirs = nil
	if err := m.SaveSettings(st); err != nil {
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

// TestQueueEndpoints 队列总闸与批量操作的 REST 路径。
func TestQueueEndpoints(t *testing.T) {
	m := newBackend(t)
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	// 闭闸
	resp := apiDo(t, http.MethodPut, api.URL+"/api/v1/queue/suspended", token, "application/json", `{"suspended":true}`)
	var gate map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&gate); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !gate["suspended"] {
		t.Fatalf("闭闸回读 = %v", gate)
	}
	if !m.GetSettings().QueueSuspended {
		t.Fatal("引擎未应用闭闸")
	}

	// 闭闸期间新建的任务停在 queued（该地址不存在，若真启动会立刻失败）
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", `{"url":"http://127.0.0.1:1/x.zip"}`)
	resp.Body.Close()
	time.Sleep(200 * time.Millisecond)
	tasks := apiTasks(t, api.URL, token)
	if len(tasks) != 1 || tasks[0].Status != engine.StatusQueued {
		t.Fatalf("闭闸期间任务应停在 queued: %+v", tasks)
	}

	// 暂停全部：排队中的任务也计入受影响数
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/queue/pause-all", token, "", "")
	var counted map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&counted); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if counted["count"] != 1 {
		t.Fatalf("pause-all count = %v, want 1", counted)
	}
	if got := apiTasks(t, api.URL, token)[0].Status; got != engine.StatusPaused {
		t.Fatalf("pause-all 后状态 = %s", got)
	}

	// 恢复全部 → 开闸
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/queue/resume-all", token, "", "")
	resp.Body.Close()
	resp = apiDo(t, http.MethodPut, api.URL+"/api/v1/queue/suspended", token, "application/json", `{"suspended":false}`)
	resp.Body.Close()
	if m.GetSettings().QueueSuspended {
		t.Fatal("开闸未生效")
	}

	// 清理已完成（当前无已完成任务 → 0，且不影响 paused/queued 校验）
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/queue/clear-completed", token, "", "")
	if err := json.NewDecoder(resp.Body).Decode(&counted); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if counted["count"] != 0 {
		t.Fatalf("clear-completed count = %v, want 0", counted)
	}

	// 闸门请求体必须是 JSON（守卫层已拦非 application/json）
	resp = apiDo(t, http.MethodPut, api.URL+"/api/v1/queue/suspended", token, "text/plain", `{"suspended":true}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("text/plain 闸门请求应 403, got %d", resp.StatusCode)
	}
}

// TestAddTaskFieldParity REST 新建任务的扩展字段（校验和/优先级/定时/每任务限速）
// 必须真正落到任务上，而不是被请求体解码静默丢弃。
func TestAddTaskFieldParity(t *testing.T) {
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

	// 定时设到未来：任务停在 queued，不会真去连一个不存在的地址
	startAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	sum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	body := fmt.Sprintf(`{"url": "https://127.0.0.1:1/x.zip", "saveDir": %q, "connections": 2,`+
		` "fileName": "custom.zip", "priority": 2, "speedLimit": 1048576,`+
		` "startAt": %q, "checksumAlgo": "SHA256", "checksumExpected": %q}`, saveDir, startAt, sum)
	resp := apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("新建任务状态码 = %d: %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	tasks := apiTasks(t, api.URL, token)
	if len(tasks) != 1 {
		t.Fatalf("任务数 = %d", len(tasks))
	}
	got := tasks[0]
	if got.Status != engine.StatusQueued {
		t.Fatalf("定时未到点应停在 queued, got %s", got.Status)
	}
	if got.Priority != engine.PriorityHigh || got.SpeedLimit != 1048576 {
		t.Fatalf("优先级/每任务限速未生效: %+v", got)
	}
	if got.ChecksumAlgo != "sha256" || got.ChecksumExpected != sum {
		t.Fatalf("校验和字段未生效: %+v", got)
	}
	if got.CustomName != "custom.zip" || got.FileName != "custom.zip" {
		t.Fatalf("fileName 未落到自定义文件名: %+v", got)
	}
	if got.Connections != 2 {
		t.Fatalf("连接数未生效: %+v", got)
	}

	// fileName 与 customName 同义；两者都给时 fileName 优先（对外文档用的是 fileName）
	body = fmt.Sprintf(`{"url": "https://127.0.0.1:1/y.zip", "saveDir": %q,`+
		` "fileName": "first.zip", "customName": "second.zip"}`, saveDir)
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("新建任务状态码 = %d", resp.StatusCode)
	}
	tasks = apiTasks(t, api.URL, token)
	if len(tasks) != 2 {
		t.Fatalf("任务数 = %d", len(tasks))
	}
	second := tasks[1]
	if second.CustomName != "first.zip" {
		t.Fatalf("fileName 应优先于 customName: %+v", second)
	}

	// 只写 customName 也应当生效
	body = fmt.Sprintf(`{"url": "https://127.0.0.1:1/z.zip", "saveDir": %q, "customName": "named.zip"}`, saveDir)
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	resp.Body.Close()
	tasks = apiTasks(t, api.URL, token)
	if len(tasks) != 3 || tasks[2].CustomName != "named.zip" {
		t.Fatalf("customName 应生效: %+v", tasks)
	}

	// 非法优先级夹到区间内而不是报错
	body = fmt.Sprintf(`{"url": "https://127.0.0.1:1/w.zip", "saveDir": %q, "priority": 9}`, saveDir)
	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("非法优先级应夹取而非拒绝, got %d", resp.StatusCode)
	}
	tasks = apiTasks(t, api.URL, token)
	if last := tasks[len(tasks)-1]; last.Priority != engine.PriorityHigh {
		t.Fatalf("非法优先级未夹到最高: %+v", last)
	}
}

// TestQueueWindowAndStatsEndpoints 带宽时间窗、队列放行状态与统计的 REST 路径。
func TestQueueWindowAndStatsEndpoints(t *testing.T) {
	m := newBackend(t)
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	// 时间格式非法 → 400（调用方的输入问题，不该记 500）
	resp := apiDo(t, http.MethodPut, api.URL+"/api/v1/queue/window", token, "application/json", `{"enabled":true,"start":"7pm","end":"06:00"}`)
	got := resp.StatusCode
	resp.Body.Close()
	if got != http.StatusBadRequest {
		t.Fatalf("非法时间应 400, got %d", got)
	}

	// 窗口设在当下之外的两小时：无论怎么绕，now 都不在 [now+2h, now+3h) 内
	now := time.Now()
	body := fmt.Sprintf(`{"enabled":true,"start":"%s","end":"%s"}`,
		now.Add(2*time.Hour).Format("15:04"), now.Add(3*time.Hour).Format("15:04"))
	resp = apiDo(t, http.MethodPut, api.URL+"/api/v1/queue/window", token, "application/json", body)
	got = resp.StatusCode
	resp.Body.Close()
	if got != http.StatusOK {
		t.Fatalf("设置时间窗应 200, got %d", got)
	}

	var st engine.QueueStatus
	resp = apiDo(t, http.MethodGet, api.URL+"/api/v1/queue/status", token, "", "")
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !st.WindowEnabled || st.WindowActive || st.Active || st.OpensAt == "" {
		t.Fatalf("窗外状态 = %+v", st)
	}

	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", `{"url":"http://127.0.0.1:1/a.zip"}`)
	resp.Body.Close()
	time.Sleep(300 * time.Millisecond)
	if gs := apiTasks(t, api.URL, token); len(gs) != 1 || gs[0].Status != engine.StatusQueued {
		t.Fatalf("窗外任务应停在 queued: %+v", gs)
	}

	var stats engine.Stats
	resp = apiDo(t, http.MethodGet, api.URL+"/api/v1/stats", token, "", "")
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if stats.TaskCount != 1 || stats.Queued != 1 || stats.WindowMs <= 0 {
		t.Fatalf("统计读数 = %+v", stats)
	}

	// 关掉时间窗：立即放行（地址不可达会失败，但不该停在队列里）
	resp = apiDo(t, http.MethodPut, api.URL+"/api/v1/queue/window", token, "application/json",
		`{"enabled":false,"start":"00:00","end":"00:00"}`)
	resp.Body.Close()
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		return len(tasks) == 1 && tasks[0].Status != engine.StatusQueued
	}, 15*time.Second)

	// 再等它落到终态才结束用例：这一条原本只等到「离开 queued」就返回，
	// 此时下载仍在跑，收尾会与 t.TempDir() 清理抢目录而随机失败
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		return len(tasks) == 1 && tasks[0].Status == engine.StatusFailed
	}, 15*time.Second)
}

// TestTaskStatsEndpoint 单任务曲线端点：运行中任务 200，无统计记录 404。
// GET /api/v1/stats 的汇总不再带 samples，曲线走这个按需端点。
func TestTaskStatsEndpoint(t *testing.T) {
	content := make([]byte, 10000)
	rs := &rangeServer{content: content, gate: make(chan struct{})}
	srv := httptest.NewServer(rs)
	defer srv.Close()

	m := newBackend(t)
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	body := fmt.Sprintf(`{"url": %q, "saveDir": %q, "connections": 2}`, srv.URL, m.GetSettings().SaveDir)
	resp := apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("新建任务状态码 = %d", resp.StatusCode)
	}
	var task engine.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// gate 挡住下载，任务停在 running；统计条目由进度循环采样建立，有节奏
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		if len(tasks) != 1 || tasks[0].Status != engine.StatusRunning {
			return false
		}
		_, ok := m.GetTaskStats(task.ID)
		return ok
	}, 5*time.Second)

	var ts engine.TaskStat
	resp = apiDo(t, http.MethodGet, api.URL+"/api/v1/stats/tasks/"+task.ID, token, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("单任务统计状态码 = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ts.TaskID != task.ID || ts.FileName == "" {
		t.Fatalf("单任务统计 = %+v", ts)
	}

	// 没有统计记录的 ID → 404
	resp = apiDo(t, http.MethodGet, api.URL+"/api/v1/stats/tasks/no-such-id", token, "", "")
	got := resp.StatusCode
	resp.Body.Close()
	if got != http.StatusNotFound {
		t.Fatalf("不存在的任务统计应 404, got %d", got)
	}

	close(rs.gate) // 放行，等下载收尾再让 TempDir 清理（.part 句柄会关掉）
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		return len(tasks) == 1 && tasks[0].Status == engine.StatusCompleted
	}, 15*time.Second)
}

// TestReadSettingsEndpoint GET /api/v1/settings 可直接读取设置。
// 此前只经 PUT 的响应体间接覆盖，GET 路径本身没有用例。
func TestReadSettingsEndpoint(t *testing.T) {
	m := newBackend(t)
	want := m.GetSettings()
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	resp := apiDo(t, http.MethodGet, api.URL+"/api/v1/settings", want.APIToken, "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("读取设置状态码 = %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got engine.Settings
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.SaveDir != want.SaveDir || got.APIPort != want.APIPort || got.ConcurrentTasks != want.ConcurrentTasks {
		t.Fatalf("设置读数与引擎不一致:\n got %+v\nwant %+v", got, want)
	}
}

// TestRetryFailedEndpoint POST /api/v1/queue/retry-failed 只重试失败任务并返回条数。
func TestRetryFailedEndpoint(t *testing.T) {
	m := newBackend(t)
	st := m.GetSettings()
	st.APIEnabled = false // 测试不真正监听端口
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	// 不可达地址：任务探测即失败，很快落到 failed
	body := fmt.Sprintf(`{"url":"http://127.0.0.1:1/x.zip","saveDir":%q}`, m.GetSettings().SaveDir)
	resp := apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("新建任务状态码 = %d", resp.StatusCode)
	}
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		return len(tasks) == 1 && tasks[0].Status == engine.StatusFailed
	}, 15*time.Second)

	resp = apiDo(t, http.MethodPost, api.URL+"/api/v1/queue/retry-failed", token, "", "")
	var counted map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&counted); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if counted["count"] != 1 {
		t.Fatalf("retry-failed count = %v, want 1", counted)
	}
	// 重试后任务会重新入队并再次快速失败（地址不可达）。等它回到终态再结束用例：
	// 任务还在跑时退出，它的收尾/落盘会和 t.TempDir() 清理撞车，
	// Windows 上随机报「目录非空」或 rename tasks.json 失败。
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		return len(tasks) == 1 && tasks[0].Status == engine.StatusFailed
	}, 15*time.Second)
}

// TestDeleteWithFilesRemovesDownloadedFile 连文件删除的成功路径：
// 任务先把文件真的下到（临时）保存目录，随后 ?files=1 要在记录与磁盘上同时移除它。
// 已有的 TestSaveDirWhitelist 只覆盖了「白名单外 → 403」的拒绝分支。
func TestDeleteWithFilesRemovesDownloadedFile(t *testing.T) {
	rs := &rangeServer{content: make([]byte, 4000)}
	srv := httptest.NewServer(rs)
	defer srv.Close()

	m := newBackend(t)
	saveDir := m.GetSettings().SaveDir
	st := m.GetSettings()
	st.APIEnabled = false
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	body := fmt.Sprintf(`{"url":%q,"saveDir":%q,"connections":2,"fileName":"gone.bin"}`, srv.URL, saveDir)
	resp := apiDo(t, http.MethodPost, api.URL+"/api/v1/tasks", token, "application/json", body)
	var task engine.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("新建任务状态码 = %d", resp.StatusCode)
	}
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL, token)
		return len(tasks) == 1 && tasks[0].Status == engine.StatusCompleted
	}, 20*time.Second)

	file := filepath.Join(saveDir, "gone.bin")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("下载完成后文件应存在: %v", err)
	}

	resp = apiDo(t, http.MethodDelete, api.URL+"/api/v1/tasks/"+task.ID+"?files=1", token, "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("连文件删除状态码 = %d", resp.StatusCode)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatalf("连文件删除后磁盘上不应再有该文件: %s", file)
	}
	if n := len(apiTasks(t, api.URL, token)); n != 0 {
		t.Fatalf("连文件删除后任务记录应已移除, 剩 %d", n)
	}
}

// TestBearerTokenAndLoopbackHostVariants 令牌的第二种携带方式与 Host/Origin 的放行分支。
// TestGuard 只覆盖了各种拒绝分支，这里补齐「合法即通过」的那一半。
func TestBearerTokenAndLoopbackHostVariants(t *testing.T) {
	m := newBackend(t)
	token := m.GetSettings().APIToken
	h := New(m).Handler()

	do := func(host, origin, bearer string) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Result()
	}

	// Authorization: Bearer 与 X-API-Token 等价
	if resp := do("127.0.0.1:8199", "", token); resp.StatusCode != http.StatusOK {
		t.Fatalf("Bearer 令牌应 200, got %d", resp.StatusCode)
	}
	if resp := do("127.0.0.1:8199", "", "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错误 Bearer 令牌应 401, got %d", resp.StatusCode)
	}
	// Host 白名单里的另外两种写法
	if resp := do("localhost:8199", "", token); resp.StatusCode != http.StatusOK {
		t.Fatalf("localhost Host 应 200, got %d", resp.StatusCode)
	}
	if resp := do("[::1]:8199", "", token); resp.StatusCode != http.StatusOK {
		t.Fatalf("[::1] Host 应 200, got %d", resp.StatusCode)
	}
	// Origin 为回环地址（前端 dev server 形态）应放行
	if resp := do("127.0.0.1:8199", "http://localhost:5173", token); resp.StatusCode != http.StatusOK {
		t.Fatalf("回环 Origin 应 200, got %d", resp.StatusCode)
	}
}

// TestInvalidJSONBodyRejected 带体但内容不是合法 JSON（Content-Type 已正确）→ 400，
// 而不是 500 或静默建出一个空任务。
func TestInvalidJSONBodyRejected(t *testing.T) {
	m := newBackend(t)
	token := m.GetSettings().APIToken
	api := httptest.NewServer(New(m).Handler())
	defer api.Close()

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/tasks"},
		{http.MethodPut, "/api/v1/queue/suspended"},
		{http.MethodPut, "/api/v1/settings"},
	} {
		resp := apiDo(t, c.method, api.URL+c.path, token, "application/json", "{ not json")
		code := resp.StatusCode
		resp.Body.Close()
		if code != http.StatusBadRequest {
			t.Fatalf("%s %s 非法 JSON 应 400, got %d", c.method, c.path, code)
		}
	}
	if n := len(apiTasks(t, api.URL, token)); n != 0 {
		t.Fatalf("非法请求体不应建出任务, 现有 %d 条", n)
	}
}
