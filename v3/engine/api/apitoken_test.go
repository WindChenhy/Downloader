package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"downloader/engine"
)

// TestAPITokenGeneratedOnEnable 模拟旧版升级：存量 settings.json 中 API 关闭
// 且没有 apiToken 字段；之后经设置开启 API 必须补发令牌，否则会永久 401。
func TestAPITokenGeneratedOnEnable(t *testing.T) {
	dir := t.TempDir()
	// 旧版构建写出的设置：无 apiToken 键
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"apiEnabled": false, "apiPort": 8199}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := engine.NewManager(dir, func(engine.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if tok := m.GetSettings().APIToken; tok != "" {
		t.Fatalf("API 关闭时不应有令牌, got %q", tok)
	}

	// 经设置开启 API（表单不含令牌）→ 令牌必须被补发
	st := m.GetSettings()
	st.APIEnabled = true
	st.APIToken = ""
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	tok := m.GetSettings().APIToken
	if len(tok) != 32 {
		t.Fatalf("开启 API 后应生成 32 位令牌, got %q", tok)
	}

	// 持久化检查：磁盘上有令牌，下次启动直接复用
	m2, err := engine.NewManager(dir, func(engine.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if m2.GetSettings().APIToken != tok {
		t.Fatal("令牌应持久化并在重启后保持不变")
	}

	// 端到端：新令牌能过守卫，无/错令牌 401
	h := New(m2).Handler()
	do := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		req.Host = "127.0.0.1:8199"
		if token != "" {
			req.Header.Set("X-API-Token", token)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}
	if code := do(""); code != http.StatusUnauthorized {
		t.Fatalf("无令牌应 401, got %d", code)
	}
	if code := do(tok); code != http.StatusOK {
		t.Fatalf("携带令牌应 200, got %d", code)
	}
	if code := do(tok + "x"); code != http.StatusUnauthorized {
		t.Fatalf("错误令牌应 401, got %d", code)
	}
}
