package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemFontCandidates(t *testing.T) {
	env := func(key string) string {
		return map[string]string{"WINDIR": `D:\Win`}[key]
	}
	windows := systemFontCandidates("windows", env)
	if len(windows) == 0 || windows[0].name != `D:\Win\Fonts\YuGothM.ttc` {
		t.Errorf("Windows candidates = %+v", windows)
	}
	if got := systemFontCandidates("windows", func(string) string { return "" })[0].name; !strings.HasPrefix(got, `C:\Windows\Fonts\`) {
		t.Errorf("Windows without WINDIR = %q", got)
	}
	darwin := systemFontCandidates("darwin", env)
	if len(darwin) == 0 || darwin[0].fullName != "Hiragino Sans W5" || !filepath.IsAbs(darwin[0].name) {
		t.Errorf("macOS candidates = %+v", darwin)
	}
	for _, candidate := range systemFontCandidates("linux", env) {
		if strings.ContainsRune(candidate.name, '/') {
			t.Errorf("Linux candidate %q is a path, want a file name", candidate.name)
		}
	}
}

func TestFindFontFiles(t *testing.T) {
	system, user := t.TempDir(), t.TempDir()
	for _, path := range []string{
		filepath.Join(system, "opentype", "noto", "NotoSansCJK-Regular.ttc"),
		filepath.Join(user, "ipaexg.ttf"),
		filepath.Join(user, "nested", "NotoSansCJK-Regular.ttc"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	candidates := []systemFontCandidate{{name: "NotoSansCJK-Regular.ttc"}, {name: "ipaexg.ttf"}, {name: "TakaoGothic.ttf"}}
	found := findFontFiles([]string{filepath.Join(system, "missing"), system, user}, candidates)
	if want := filepath.Join(system, "opentype", "noto", "NotoSansCJK-Regular.ttc"); found["NotoSansCJK-Regular.ttc"] != want {
		t.Errorf("Noto = %q, want the first directory's %q", found["NotoSansCJK-Regular.ttc"], want)
	}
	if found["ipaexg.ttf"] != filepath.Join(user, "ipaexg.ttf") || found["TakaoGothic.ttf"] != "" {
		t.Errorf("found = %v", found)
	}
}

func TestLoadSystemMenuFontSkipsUnusableFiles(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.ttf")
	if err := os.WriteFile(broken, []byte("not a font"), 0o600); err != nil {
		t.Fatal(err)
	}
	candidates := []systemFontCandidate{{name: filepath.Join(dir, "missing.ttc")}, {name: broken}}
	if font, path := loadSystemMenuFont(candidates, func(name string) string { return name }); font != nil || path != "" {
		t.Errorf("got a font from %q", path)
	}
}

// The macOS fonts are only there on a Mac; elsewhere the test is skipped.
func TestLoadSystemMenuFontOnMac(t *testing.T) {
	const hiragino = "/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc"
	data, err := os.ReadFile(hiragino)
	if err != nil {
		t.Skip("Hiragino Sans is not installed")
	}
	// The collection holds Hiragino Sans first and Kaku Gothic ProN third.
	if index, ok := japaneseFontIndex(data, "Hiragino Sans W3"); !ok || index != 0 {
		t.Errorf("Hiragino Sans = %d, %t", index, ok)
	}
	if index, ok := japaneseFontIndex(data, "Hiragino Kaku Gothic ProN"); !ok || index != 2 {
		t.Errorf("Kaku Gothic ProN = %d, %t", index, ok)
	}
	if _, ok := japaneseFontIndex(data, "No Such Font"); ok {
		t.Error("a missing font name was found")
	}
	// A font without Japanese is passed over.
	if latin, err := os.ReadFile("/System/Library/Fonts/Supplemental/Arial.ttf"); err == nil {
		if _, ok := japaneseFontIndex(latin, ""); ok {
			t.Error("Arial was taken as a Japanese font")
		}
	}
	font, path := loadSystemMenuFont(systemFontCandidates("darwin", os.Getenv), func(name string) string { return name })
	if font == nil || !strings.Contains(path, "W5") {
		t.Errorf("font from %q", path)
	}
}
