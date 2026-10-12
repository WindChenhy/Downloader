package engine

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// recordingTransport 记录是否有请求经过，用于验证注入的客户端生效。
// 下载是多 goroutine 并发的，标志位必须原子访问。
type recordingTransport struct {
	inner http.RoundTripper
	hit   atomic.Bool
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.hit.Store(true)
	return rt.inner.RoundTrip(req)
}

// TestWithHTTPClientOption 注入的 HTTP 客户端应被探测与下载全程使用。
func TestWithHTTPClientOption(t *testing.T) {
	content := newContent(t, 2048)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	rt := &recordingTransport{inner: http.DefaultTransport}
	m, saveDir := mustManagerWithClient(t, rt)

	if _, err := m.AddTask(srv.URL, saveDir, 2, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 10*time.Second)
	if !rt.hit.Load() {
		t.Fatal("注入的 HTTP 客户端未被使用")
	}
	got, err := os.ReadFile(filepath.Join(saveDir, "test.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatal("下载内容与原始内容不一致")
	}
}

func mustManagerWithClient(t *testing.T, rt http.RoundTripper) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, nopNotify, WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	return m, t.TempDir()
}
