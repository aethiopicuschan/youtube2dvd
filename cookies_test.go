package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"
)

func writeCookies(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const netscapeCookies = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\tf6=8\n"

func TestCheckCookiesFile(t *testing.T) {
	for _, content := range []string{
		netscapeCookies,
		"# HTTP Cookie File\n",
		"# Netscape HTTP Cookie File\r\n",
	} {
		if err := checkCookiesFile(writeCookies(t, content)); err != nil {
			t.Errorf("%q: %v", content, err)
		}
	}
	for content, want := range map[string]string{
		`[{"domain": ".youtube.com"}]`:              "JSON",
		".youtube.com\tTRUE\t/\tTRUE\t0\tPREF\tx\n": "Netscape形式",
		"": "Netscape形式",
		// yt-dlp cannot read the header after a byte order mark.
		"\ufeff# Netscape HTTP Cookie File\n": "BOM",
	} {
		if err := checkCookiesFile(writeCookies(t, content)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", content, err, want)
		}
	}
	if err := checkCookiesFile(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Error("a missing file was accepted")
	}
}

func TestYtDlpRunnerForCookies(t *testing.T) {
	runner, err := ytDlpRunnerFor(appSettings{YtDlpPath: "/usr/bin/yt-dlp"})
	if err != nil || runner.cookies != "" {
		t.Errorf("without cookies: %+v, %v", runner, err)
	}
	cookies := writeCookies(t, netscapeCookies)
	runner, err = ytDlpRunnerFor(appSettings{YtDlpPath: "/usr/bin/yt-dlp", YtDlpCookiesPath: cookies})
	if err != nil || runner.cookies != cookies || runner.path != "/usr/bin/yt-dlp" {
		t.Errorf("with cookies: %+v, %v", runner, err)
	}
	// A file removed since it was chosen stops the run with advice.
	if _, err := ytDlpRunnerFor(appSettings{YtDlpPath: "/usr/bin/yt-dlp", YtDlpCookiesPath: filepath.Join(t.TempDir(), "gone.txt")}); err == nil || !strings.Contains(err.Error(), "「設定」で選び直してください") {
		t.Errorf("missing cookies: %v", err)
	}
}

func TestSettingsPageChoosesCookies(t *testing.T) {
	settings := appSettings{UseYtDlp: true, YtDlpPath: "/usr/local/bin/yt-dlp"}
	saved := 0
	page := &settingsPage{settings: &settings, onChange: func() error { saved++; return nil }}
	if got := page.cookiesText(); got != "クッキー：なし" {
		t.Errorf("without cookies = %q", got)
	}

	page.chooseCookies(func(string) (string, error) { return "", zenity.ErrCanceled })
	if settings.YtDlpCookiesPath != "" || saved != 0 || page.cookieErrorMessage != "" {
		t.Errorf("after cancelling: %+v, saved %d, error %q", settings, saved, page.cookieErrorMessage)
	}

	json := writeCookies(t, `[{"domain": ".youtube.com"}]`)
	page.chooseCookies(func(string) (string, error) { return json, nil })
	if settings.YtDlpCookiesPath != "" || saved != 0 || !strings.Contains(page.cookieErrorMessage, "JSON") {
		t.Errorf("after a JSON file: %+v, saved %d, error %q", settings, saved, page.cookieErrorMessage)
	}

	good := writeCookies(t, netscapeCookies)
	var offered string
	page.chooseCookies(func(current string) (string, error) { offered = current; return good, nil })
	if settings.YtDlpCookiesPath != good || saved != 1 || page.cookieErrorMessage != "" || offered != "" {
		t.Errorf("after a cookies.txt: %+v, saved %d, error %q", settings, saved, page.cookieErrorMessage)
	}
	if got := page.cookiesText(); got != "クッキー："+good {
		t.Errorf("with cookies = %q", got)
	}
	// Choosing again starts from the file in use.
	page.chooseCookies(func(current string) (string, error) { offered = current; return "", zenity.ErrCanceled })
	if offered != good || settings.YtDlpCookiesPath != good {
		t.Errorf("offered %q, kept %q", offered, settings.YtDlpCookiesPath)
	}
}

func TestSettingsPageClearsCookies(t *testing.T) {
	settings := appSettings{UseYtDlp: true, YtDlpPath: "/usr/local/bin/yt-dlp", YtDlpCookiesPath: "/Users/me/cookies.txt"}
	saved := 0
	page := &settingsPage{settings: &settings, onChange: func() error { saved++; return nil }, cookieErrorMessage: "old error"}
	page.clearCookies()
	if settings.YtDlpCookiesPath != "" || saved != 1 || page.cookieErrorMessage != "" {
		t.Errorf("after clearing: %+v, saved %d, error %q", settings, saved, page.cookieErrorMessage)
	}
	if got := page.cookiesText(); got != "クッキー：なし" {
		t.Errorf("cookies = %q", got)
	}
	// Nothing to clear saves nothing.
	page.clearCookies()
	if saved != 1 {
		t.Errorf("saved %d times", saved)
	}
}
