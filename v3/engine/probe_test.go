package engine

import (
	"testing"
)

func TestParseFilenameFromCD(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`attachment; filename="foo.zip"`, "foo.zip"},
		{`attachment; filename=bar.bin`, "bar.bin"},
		{`attachment; filename*=UTF-8''%E4%B8%AD%E6%96%87.txt`, "中文.txt"},
		{`attachment; filename="a"; filename*=UTF-8''%E4%B8%AD.txt`, "中.txt"},
		{"", ""},
		{"attachment", ""},
	}
	for _, c := range cases {
		if got := parseFilenameFromCD(c.in); got != c.want {
			t.Errorf("parseFilenameFromCD(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseContentRangeTotal(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"bytes 0-0/12345", 12345},
		{"bytes 5-5/100", 100},
		{"bytes 0-0/*", 0},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := parseContentRangeTotal(c.in); got != c.want {
			t.Errorf("parseContentRangeTotal(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`a<b>c:d"e/f\g|h?i*j`, "a_b_c_d_e_f_g_h_i_j"},
		{"  x  ", "x"},
		{".hidden.", "hidden"},
		{"", ""},
		{"normal.zip", "normal.zip"},
	}
	for _, c := range cases {
		if got := sanitizeFileName(c.in); got != c.want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFileNameFromURL(t *testing.T) {
	u, err := validateURL("https://example.com/path/to/%E4%B8%AD%E6%96%87%E6%96%87%E4%BB%B6.zip?query=1")
	if err != nil {
		t.Fatal(err)
	}
	if got := fileNameFromURL(u); got != "中文文件.zip" {
		t.Errorf("fileNameFromURL = %q, want %q", got, "中文文件.zip")
	}
}
