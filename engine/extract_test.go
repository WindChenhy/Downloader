package engine

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildZip 把 name→content 映射打包成 zip 文件；content 为 nil 表示目录项。
func buildZip(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, content := range entries {
		hdr := &zip.FileHeader{Name: name}
		if content == nil {
			hdr.SetMode(os.ModeDir)
		}
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func buildTarGz(t *testing.T, path string, write func(*tar.Writer)) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	write(tw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.zip")
	buildZip(t, archive, map[string][]byte{
		"top.txt":       []byte("hello"),
		"nested/sub.md": []byte("world"),
	})
	if err := extractArchive(archive); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "pkg", "nested", "sub.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "world" {
		t.Fatalf("解压内容不符: %q", got)
	}
	if !fileExists(filepath.Join(dir, "pkg", "top.txt")) {
		t.Fatal("顶层文件未解出")
	}
}

// TestExtractZipSlip zip 内携带 ../ 逃逸路径时必须报错，且目标目录外无落盘。
func TestExtractZipSlip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")
	buildZip(t, archive, map[string][]byte{
		"../escaped.txt": []byte("pwned"),
	})
	if err := extractArchive(archive); err == nil {
		t.Fatal("zip-slip 路径应返回错误")
	}
	if fileExists(filepath.Join(filepath.Dir(dir), "escaped.txt")) {
		t.Fatal("逃逸文件落盘到了目标目录之外")
	}
}

func TestExtractTarGzSkipsSymlink(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.tar.gz")
	buildTarGz(t, archive, func(tw *tar.Writer) {
		writeReg := func(name, content string) {
			hdr := &tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(content))}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
		writeReg("real.txt", "data")
		// 指向任意路径的符号链接：解压必须跳过，避免借链接写入
		link := &tar.Header{Name: "danger", Typeflag: tar.TypeSymlink, Linkname: "../../../outside.txt"}
		if err := tw.WriteHeader(link); err != nil {
			t.Fatal(err)
		}
		writeReg("danger/payload.txt", "should not exist")
	})
	if err := extractArchive(archive); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(filepath.Dir(dir), "outside.txt")) {
		t.Fatal("符号链接不应被解出")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "pkg", "real.txt")); err != nil || string(got) != "data" {
		t.Fatalf("普通文件解压失败: %v %q", err, got)
	}
}

func TestExtractGzSingleFile(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "notes.txt.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte("plain notes")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "notes.txt", "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "plain notes" {
		t.Fatalf("解压内容不符: %q", got)
	}
}

func TestIsArchivePathAndStripExt(t *testing.T) {
	cases := []struct {
		path     string
		isArch   bool
		expected string
	}{
		{"a.zip", true, "a"},
		{"b.tar.gz", true, "b"},
		{"c.tgz", true, "c"},
		{"d.tar.bz2", true, "d"},
		{"e.txt", false, ""},
		{"f.rar", false, ""},
	}
	for _, c := range cases {
		if got := isArchivePath(c.path); got != c.isArch {
			t.Errorf("isArchivePath(%q) = %v, want %v", c.path, got, c.isArch)
		}
		if c.isArch {
			if got := stripArchiveExt(c.path); got != c.expected {
				t.Errorf("stripArchiveExt(%q) = %q, want %q", c.path, got, c.expected)
			}
		}
	}
}

// TestSanitizeReservedDeviceNames Windows 保留设备名追加下划线，含带扩展名形态。
func TestSanitizeReservedDeviceNames(t *testing.T) {
	cases := map[string]string{
		"CON":      "CON_",
		"nul.txt":  "nul_.txt",
		"COM1":     "COM1_",
		"lpt3.zip": "lpt3_.zip",
		"normal":   "normal",
		"a/CON/b":  "a_CON_b", // 非法字符替换在前，替换后不再命中保留名
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(want, `<>:"/\|?*`) {
			t.Errorf("结果仍含非法字符: %q", want)
		}
	}
}
