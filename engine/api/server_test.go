package api

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"downloader/engine"
)

// 本文件覆盖 Server.Sync 的真实生命周期：真绑 127.0.0.1 端口、换端口、
// 关闭，以及启动失败后的回滚与重试。其余 api_test.go 里的用例都走
// httptest.NewServer(Handler())，绕过了这一层，Sync 因此长期只有 ~32% 覆盖。

// pickPorts 取两个互不相同、且此刻空闲的回环端口。
func pickPorts(t *testing.T) (int, int) {
	t.Helper()
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		l1.Close()
		t.Fatal(err)
	}
	p1 := l1.Addr().(*net.TCPAddr).Port
	p2 := l2.Addr().(*net.TCPAddr).Port
	l1.Close()
	l2.Close()
	if p1 == p2 {
		t.Fatalf("两个端口撞了: %d", p1)
	}
	return p1, p2
}

func baseFor(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// pingClient 必须带超时：端口被一个「只 listen 不响应」的 socket 占住时，
// http.DefaultClient 会一直等响应把用例挂死。
var pingClient = &http.Client{Timeout: 2 * time.Second}

func pingOK(base string) bool {
	resp, err := pingClient.Get(base + "/api/v1/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitListening 等到 base 可访问（want=true）或不可访问（want=false）。
func waitListening(t *testing.T, base string, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pingOK(base) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("监听状态未在 5s 内变为 %v：%s", want, base)
}

// enableOn 把设置里的 API 指向指定端口（令牌留空 = 保留现有令牌）。
func enableOn(t *testing.T, m *engine.Manager, port int) {
	t.Helper()
	st := m.GetSettings()
	st.APIEnabled = true
	st.APIPort = port
	st.APIToken = ""
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
}

func disableAPI(t *testing.T, m *engine.Manager) {
	t.Helper()
	st := m.GetSettings()
	st.APIEnabled = false
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
}

func (s *Server) currentAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// TestServerSyncStartsSwitchesPortAndStops 起监听 → 换端口（旧端口释放、新端口可用，
// 令牌依旧有效）→ 关闭（端口全部释放、内部状态清空）。
func TestServerSyncStartsSwitchesPortAndStop(t *testing.T) {
	m := newBackend(t)
	portA, portB := pickPorts(t)
	enableOn(t, m, portA)
	token := m.GetSettings().APIToken
	if token == "" {
		t.Fatal("开启 API 后应存在令牌")
	}

	srv := New(m)
	srv.Sync()
	t.Cleanup(func() {
		disableAPI(t, m)
		srv.Sync()
	})

	baseA, baseB := baseFor(portA), baseFor(portB)
	waitListening(t, baseA, true)

	// 换端口：旧监听必须停掉、新监听必须起来
	enableOn(t, m, portB)
	srv.Sync()
	waitListening(t, baseB, true)
	waitListening(t, baseA, false)

	// 换端口不影响令牌：新端口上仍能正常鉴权取列表
	if tasks := apiTasks(t, baseB, token); len(tasks) != 0 {
		t.Fatalf("换端口后令牌应仍可用, 任务列表 = %v", tasks)
	}

	// 关闭
	disableAPI(t, m)
	srv.Sync()
	waitListening(t, baseB, false)
	if addr := srv.currentAddr(); addr != "" {
		t.Fatalf("关闭后 addr 应清空, got %q", addr)
	}
}

// TestServerSyncRollsBackWhenPortTaken 端口被占用导致启动失败时，实例必须被摘掉
// （srv/addr 置空），端口腾出来后再次 Sync 才能重试成功。
// 回归点：早先启动失败只打日志不回滚，srv 与 addr 都还留着，后续 Sync 会因
// 「地址没变、已在运行」直接 return，端口空出来也永远不会再试一次。
func TestServerSyncRollsBackWhenPortTaken(t *testing.T) {
	m := newBackend(t)

	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := blocker.Addr().(*net.TCPAddr).Port
	enableOn(t, m, port)

	srv := New(m)
	srv.Sync()
	t.Cleanup(func() {
		blocker.Close()
		disableAPI(t, m)
		srv.Sync()
	})

	base := baseFor(port)
	// 启动失败 → 异步回滚（ListenAndServe 立刻返回错误）
	waitFor(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.srv == nil
	}, 5*time.Second)
	if addr := srv.currentAddr(); addr != "" {
		t.Fatalf("回滚后 addr 应清空, got %q", addr)
	}
	if pingOK(base) {
		t.Fatal("端口被他人占用时本服务不应可访问")
	}

	// 端口腾出来 → 再次 Sync 必须能起来（旧实现会直接 return，永不重试）
	blocker.Close()
	up := false
	for i := 0; i < 25; i++ {
		srv.Sync()
		if pingOK(base) {
			up = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !up {
		t.Fatal("端口释放后 Sync 应能重新监听")
	}
}
