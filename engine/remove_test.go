package engine

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRemoveTaskFileHandling 删除任务的两种语义：
// 仅删除记录（保留已下载文件） vs 连文件一起删除；
// 同时验证暂存目录在删除后随之清理。
func TestRemoveTaskFileHandling(t *testing.T) {
	content := newContent(t, 2000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)

	download := func() (string, string) {
		t.Helper()
		if _, err := m.AddTask(srv.URL, saveDir, 2); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool {
			return m.GetTasks()[0].Status == StatusCompleted
		}, 10*time.Second)
		tk := m.GetTasks()[0]
		return tk.ID, tk.FileName
	}

	// 场景一：仅删除记录 → 正式文件保留，暂存目录随之清理
	id, _ := download()
	if err := m.RemoveTask(id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(saveDir, "test.bin")); err != nil {
		t.Fatal("仅删除记录时不应删除已下载的文件")
	}
	if _, err := os.Stat(m.stagingPath(saveDir, id)); !os.IsNotExist(err) {
		t.Fatal("仅删除记录时暂存数据应被清理")
	}
	if _, err := os.Stat(m.stagingDir(saveDir)); !os.IsNotExist(err) {
		t.Fatal("仅删除记录后暂存目录应为空并移除")
	}

	// 场景二：删除记录和文件 → 磁盘清空
	// （第二次下载因同名文件自动改名为 test.bin (1)，断言用任务实际的文件名）
	id, finalName := download()
	if err := m.RemoveTask(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(saveDir, finalName)); !os.IsNotExist(err) {
		t.Fatal("删除记录和文件后正式文件应被删除")
	}
	if len(m.GetTasks()) != 0 {
		t.Fatal("任务列表应为空")
	}
}

// TestOrphanStagingCleanupOnStartup 启动时清理不属于任何现存任务的孤儿暂存文件，
// 并把旧版本遗留在保存目录的 <文件名>.part 迁移到暂存目录。
func TestOrphanStagingCleanupOnStartup(t *testing.T) {
	dataDir := t.TempDir()
	saveDir := t.TempDir()

	// 预置任务列表（一个任务）与暂存目录：一份属于该任务的暂存数据、
	// 一份孤儿数据、一份旧版本位置的遗留数据
	store := NewStore(dataDir)
	task := Task{
		ID: "live-task", URL: "http://127.0.0.1:1/x", FileName: "x.bin",
		SaveDir: saveDir, Status: StatusPaused, Connections: 2,
	}
	if err := store.SaveTasks([]Task{task}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(saveDir, ".downloader")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, "live-task.part")
	orphan := filepath.Join(dir, "orphan-task.part")
	if err := os.WriteFile(kept, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("drop"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(saveDir, "x.bin.part")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(kept); err != nil {
		t.Fatal("属于现存任务的暂存数据不应被清理")
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("孤儿暂存数据应在启动时被清理")
	}
	// 旧版本遗留的 part 应被迁移进暂存目录并改名为 <任务ID>.part
	if data, err := os.ReadFile(m.stagingPath(saveDir, "live-task")); err != nil || string(data) != "legacy" {
		t.Fatalf("旧位置 part 应迁移到暂存目录: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("旧位置的 part 文件应在迁移后移除")
	}
	// live-task.part 迁移进来后覆盖了 kept 的内容——这正是预期（同一任务的最新数据）
}
