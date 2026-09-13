package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAPIEndToEnd 通过 REST API 完整走一遍：新建 → 进度 → 暂停 → 恢复 → 删除。
func TestAPIEndToEnd(t *testing.T) {
	content := newContent(t, 10000)
	ts := &testServer{content: content, etag: `"abc"`}
	ts.gate = make(chan struct{})
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	api := httptest.NewServer(m.APIHandler())
	defer api.Close()

	// ping
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
	resp, err = http.Post(api.URL+"/api/v1/tasks", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("新建任务状态码 = %d", resp.StatusCode)
	}
	var task Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// 任务列表可见且在运行
	waitFor(t, func() bool {
		tasks := apiTasks(t, api.URL)
		return len(tasks) == 1 && tasks[0].Status == StatusRunning
	}, 5*time.Second)

	// 暂停
	resp, err = http.Post(api.URL+"/api/v1/tasks/"+task.ID+"/pause", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waitFor(t, func() bool {
		return apiTasks(t, api.URL)[0].Status == StatusPaused
	}, 5*time.Second)

	// 恢复
	resp, err = http.Post(api.URL+"/api/v1/tasks/"+task.ID+"/continue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ts.releaseGate()
	waitFor(t, func() bool {
		return apiTasks(t, api.URL)[0].Status == StatusCompleted
	}, 10*time.Second)

	// 删除
	req, _ := http.NewRequest(http.MethodDelete, api.URL+"/api/v1/tasks/"+task.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(apiTasks(t, api.URL)) != 0 {
		t.Fatal("删除后任务列表应为空")
	}

	// 非法 URL → 400
	resp, err = http.Post(api.URL+"/api/v1/tasks", "application/json", bytes.NewBufferString(`{"url":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 URL 应返回 400, got %d", resp.StatusCode)
	}
}

// TestAPISettingsRoundtrip 设置经 API 读写一致，并即时生效到引擎。
func TestAPISettingsRoundtrip(t *testing.T) {
	m, _ := mustManager(t)
	api := httptest.NewServer(m.APIHandler())
	defer api.Close()

	st := m.GetSettings()
	st.SpeedLimit = 512 << 10
	st.ProxyMode = ProxySystem
	raw, _ := json.Marshal(st)
	req, _ := http.NewRequest(http.MethodPut, api.URL+"/api/v1/settings", bytes.NewReader(raw))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got Settings
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.SpeedLimit != 512<<10 || got.ProxyMode != ProxySystem {
		t.Fatalf("设置回读不一致: %+v", got)
	}
	if cur := m.GetSettings(); cur.SpeedLimit != 512<<10 {
		t.Fatalf("引擎内设置未更新: %+v", cur)
	}
}

func apiTasks(t *testing.T, base string) []Task {
	t.Helper()
	resp, err := http.Get(base + "/api/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tasks []Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	return tasks
}
