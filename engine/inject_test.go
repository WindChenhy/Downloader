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

// TestWithStagingDirOption 覆盖暂存目录选项：应用层正是用它把分块数据挪到
// 系统本地应用数据目录（%LOCALAPPDATA%\downloader\staging），使下载中的
// .part 不出现在保存目录里。空白值应被忽略并保留默认 <数据目录>/staging。
func TestWithStagingDirOption(t *testing.T) {
	dataDir := t.TempDir()
	stage := filepath.Join(t.TempDir(), "custom-staging")
	m, err := NewManager(dataDir, nopNotify, WithStagingDir(stage))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)

	if got := m.stagingDir(); got != stage {
		t.Fatalf("暂存目录 = %q, want %q", got, stage)
	}
	if got, want := m.stagingPath("abc"), filepath.Join(stage, "abc.part"); got != want {
		t.Fatalf("暂存路径 = %q, want %q", got, want)
	}
	// 暂存目录只在需要时创建，且空目录会在启动收尾时被移除；
	// 关键是路径必须落在注入的暂存根下，绝不能跑回保存目录
	if !dirWithin(m.stagingPath("abc"), stage) {
		t.Fatalf("暂存路径应位于注入的暂存目录内: %q", m.stagingPath("abc"))
	}

	// 空/空白值忽略 → 默认 <数据目录>/staging
	defaultDir := t.TempDir()
	m2, err := NewManager(defaultDir, nopNotify, WithStagingDir("   "))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m2)
	if got, want := m2.stagingDir(), filepath.Join(defaultDir, "staging"); got != want {
		t.Fatalf("空白值应忽略并回落默认目录: got %q want %q", got, want)
	}
}
