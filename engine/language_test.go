package engine

import (
	"testing"
)

// TestLanguageNormalize 界面语言字段的归一化：默认/非法值回落 zh-CN，en-US 保留。
func TestLanguageNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "zh-CN"},
		{"zh-CN", "zh-CN"},
		{"en-US", "en-US"},
		{"fr-FR", "zh-CN"},
		{"EN-us", "zh-CN"}, // 大小写敏感，非法值回落
	}
	for _, c := range cases {
		s := Settings{Language: c.in}
		s.normalize()
		if s.Language != c.want {
			t.Errorf("normalize Language=%q → %q, want %q", c.in, s.Language, c.want)
		}
	}
}

// TestLanguageRoundtrip 语言经保存/重载保持不变。
func TestLanguageRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	st := m.GetSettings()
	st.Language = "en-US"
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(dir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	if got := m2.GetSettings().Language; got != "en-US" {
		t.Fatalf("重启后语言应保持 en-US, got %q", got)
	}
}
