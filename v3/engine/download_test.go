package engine

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// TestCopyAndRemoveSourceGone 覆盖跨盘回退路径的收尾：
// 复制成功后必须真的把源文件删掉。
//
// 这里曾经有个只会在 Windows 上暴露的 bug —— os.Remove(src) 求值时
// 源文件句柄还被 defer 持有未关闭，而 Go 的 os.Open 共享模式不含
// FILE_SHARE_DELETE，删除必然报 sharing violation。结果是文件已经完整
// 复制到目标位置，moveFile 却返回错误，任务被判成「下载失败」。
func TestCopyAndRemoveSourceGone(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.part")
	dst := filepath.Join(dir, "a.bin")
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyAndRemove(src, dst); err != nil {
		t.Fatalf("copyAndRemove 失败: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("源文件应已被删除, stat err = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("目标内容 = %q, want %q", got, content)
	}
}

// TestMoveFileSameVolume 同盘走 os.Rename 的快速路径。
func TestMoveFileSameVolume(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.part")
	dst := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile 失败: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("源文件应已被移走, stat err = %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("目标文件应存在: %v", err)
	}
}

// TestTruncateURLKeepsUTF8 错误提示里截链接不能切出半个 UTF-8 字符。
func TestTruncateURLKeepsUTF8(t *testing.T) {
	long := "https://example.com/" + repeatRunes("文", 40) + ".zip"
	got := truncateURL(long)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateURL 产出非法 UTF-8: %q", got)
	}
	if !hasSuffixRunes(got, "...") {
		t.Fatalf("truncateURL 应以 ... 结尾: %q", got)
	}
	if got := truncateURL(""); got != "(空)" {
		t.Fatalf("空链接应显示 (空), got %q", got)
	}
	if got := truncateURL("https://a.b/c"); got != "https://a.b/c" {
		t.Fatalf("短链接不应被截断, got %q", got)
	}
}

// TestSanitizeFileNameKeepsUTF8 长文件名按字符截断，不能切出半个 UTF-8 字符。
// 旧实现按字节截（len(name) > 150），中文名会被切在字符中间，产出非法 UTF-8。
func TestSanitizeFileNameKeepsUTF8(t *testing.T) {
	// 200 个中文 = 600 字节：字符数超限、字节数更超限
	got := sanitizeFileName(repeatRunes("测", 200))
	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeFileName 产出非法 UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 150 {
		t.Fatalf("截断后应剩 150 个字符, got %d", n)
	}

	// 1 个 ASCII + 50 个中文 = 151 字节但只有 51 个字符：
	// 按旧实现会被误截，按字符算则完全合法、应当原样保留
	keep := "a" + repeatRunes("测", 50)
	if got := sanitizeFileName(keep); got != keep {
		t.Fatalf("51 个字符的文件名不该被截断, got %d 个字符", utf8.RuneCountInString(got))
	}
}

func repeatRunes(s string, n int) string {
	out := make([]rune, 0, n*utf8.RuneCountInString(s))
	for i := 0; i < n; i++ {
		out = append(out, []rune(s)...)
	}
	return string(out)
}

func hasSuffixRunes(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
