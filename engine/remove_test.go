package engine

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRemoveTaskFileHandling 删除任务的两种语义：
// 仅删除记录（保留已下载文件） vs 连文件一起删除。
func TestRemoveTaskFileHandling(t *testing.T) {
	content := newContent(t, 2000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)

	download := func() string {
		t.Helper()
		if _, err := m.AddTask(srv.URL, saveDir, 2); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool {
			return m.GetTasks()[0].Status == StatusCompleted
		}, 10*time.Second)
		return m.GetTasks()[0].ID
	}

	finalPath := filepath.Join(saveDir, "test.bin")

	// 场景一：仅删除记录 → 正式文件保留
	id := download()
	if err := m.RemoveTask(id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatal("仅删除记录时不应删除已下载的文件")
	}
	if _, err := os.Stat(filepath.Join(saveDir, "test.bin.part")); !os.IsNotExist(err) {
		t.Fatal("仅删除记录时 .part 应被清理")
	}

	// 场景二：删除记录和文件 → 磁盘清空
	// （第二次下载因同名文件自动改名为 test.bin (1)，断言用任务实际的文件名）
	id = download()
	tasks := m.GetTasks()
	finalName := tasks[0].FileName
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
