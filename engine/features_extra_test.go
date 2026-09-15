package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifySettingsDefaults(t *testing.T) {
	st := defaultSettings()
	if st.NotifyOnCreate || st.NotifyOnPause {
		t.Fatal("创建/暂停通知默认应关闭")
	}
	if !st.NotifyOnComplete || !st.NotifyOnFail {
		t.Fatal("完成/失败通知默认应开启")
	}
}

func TestLoadSettingsNotifyMigration(t *testing.T) {
	dir := t.TempDir()
	// 模拟旧版设置：无 notify 字段
	raw := `{"saveDir":"C:/tmp","connections":8,"concurrentTasks":3,"clipboardWatch":true,"apiEnabled":true,"apiPort":8199,"closeAction":"ask"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(dir).LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !st.NotifyOnComplete || !st.NotifyOnFail {
		t.Fatalf("旧配置迁移后完成/失败应默认开, got complete=%v fail=%v", st.NotifyOnComplete, st.NotifyOnFail)
	}
	if st.NotifyOnCreate || st.NotifyOnPause {
		t.Fatalf("旧配置迁移后创建/暂停应默认关, got create=%v pause=%v", st.NotifyOnCreate, st.NotifyOnPause)
	}
}

func TestAddTasksBatch(t *testing.T) {
	content := newContent(t, 2000)
	srv := httptest.NewServer(&testServer{content: content, etag: `"abc"`})
	defer srv.Close()

	m, saveDir := mustManager(t)
	res := m.AddTasks([]AddTaskParams{
		{URL: srv.URL + "/a.bin", SaveDir: saveDir, Connections: 2},
		{URL: "not-a-url", SaveDir: saveDir},
		{URL: srv.URL + "/b.bin", SaveDir: saveDir, Connections: 2},
	})
	if len(res.Tasks) != 2 {
		t.Fatalf("成功任务数 = %d, want 2; errors=%v", len(res.Tasks), res.Errors)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("失败条数 = %d, want 1", len(res.Errors))
	}
}

func TestNormalizeChecksumAlgo(t *testing.T) {
	if got := normalizeChecksumAlgo("", "d41d8cd98f00b204e9800998ecf8427e"); got != "md5" {
		t.Fatalf("按长度推断 md5, got %q", got)
	}
	if got := normalizeChecksumAlgo("SHA256", "abc"); got != "sha256" {
		t.Fatalf("算法名大小写, got %q", got)
	}
	if got := normalizeChecksumAlgo("", "zzz"); got != "" {
		t.Fatalf("非法期望值不应推断算法, got %q", got)
	}
}

func TestChecksumVerifyAfterDownload(t *testing.T) {
	content := newContent(t, 4096)
	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	_, err := m.AddTaskWithChecksum(AddTaskParams{
		URL:              srv.URL,
		SaveDir:          saveDir,
		Connections:      2,
		ChecksumAlgo:     "sha256",
		ChecksumExpected: expected,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusCompleted }, 10*time.Second)
	tk := m.GetTasks()[0]
	if tk.ChecksumStatus != ChecksumOK {
		t.Fatalf("ChecksumStatus = %s (%s), want ok", tk.ChecksumStatus, tk.ChecksumActual)
	}
	if tk.ChecksumActual != expected {
		t.Fatalf("ChecksumActual = %s, want %s", tk.ChecksumActual, expected)
	}
	if tk.AvgSpeed <= 0 {
		t.Fatalf("AvgSpeed = %d, want > 0", tk.AvgSpeed)
	}
}

func TestChecksumMismatchFailsTask(t *testing.T) {
	content := newContent(t, 2048)
	srv := httptest.NewServer(&testServer{content: content, etag: `"abc"`})
	defer srv.Close()

	m, saveDir := mustManager(t)
	_, err := m.AddTaskWithChecksum(AddTaskParams{
		URL:              srv.URL,
		SaveDir:          saveDir,
		Connections:      1,
		ChecksumExpected: "d41d8cd98f00b204e9800998ecf8427e", // 空文件的 md5，必不匹配
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusFailed }, 10*time.Second)
	tk := m.GetTasks()[0]
	if tk.ChecksumStatus != ChecksumMismatch {
		t.Fatalf("ChecksumStatus = %s, want mismatch", tk.ChecksumStatus)
	}
}

func TestAvgSpeedOf(t *testing.T) {
	if avgSpeedOf(1000, 0) != 0 {
		t.Fatal("activeMs=0 时应返回 0")
	}
	// 1000 字节 / 1 秒
	if got := avgSpeedOf(1000, 1000); got != 1000 {
		t.Fatalf("avgSpeedOf = %d, want 1000", got)
	}
}
