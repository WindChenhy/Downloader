package engine

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAddTaskCustomName 自定义文件名：优先于服务器提供的名字，
// 且非法字符会被清洗；留空时沿用自动推断。
func TestAddTaskCustomName(t *testing.T) {
	content := newContent(t, 2000)
	ts := &testServer{content: content, etag: `"abc"`} // Content-Disposition: test.bin
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)

	download := func(custom string) string {
		t.Helper()
		if _, err := m.AddTask(srv.URL, saveDir, 2, custom); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool {
			return m.GetTasks()[0].Status == StatusCompleted
		}, 10*time.Second)
		tk := m.GetTasks()[0]
		if err := m.RemoveTask(tk.ID, true); err != nil {
			t.Fatal(err)
		}
		return tk.FileName
	}

	// 自定义名优先
	if got := download("我的安装包.bin"); got != "我的安装包.bin" {
		t.Fatalf("自定义文件名未生效: %q", got)
	}
	if _, err := os.Stat(filepath.Join(saveDir, "我的安装包.bin")); !os.IsNotExist(err) {
		t.Fatal("连文件删除后不应残留")
	}

	// 非法字符被清洗
	if got := download(`bad<name>:.bin`); got != `bad_name__.bin` {
		t.Fatalf("非法字符应被清洗: %q", got)
	}

	// 留空 → 服务器 Content-Disposition 决定
	if got := download(""); got != "test.bin" {
		t.Fatalf("留空时应使用服务器提供的文件名: %q", got)
	}
}
