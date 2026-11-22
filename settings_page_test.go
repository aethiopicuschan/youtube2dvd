package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lookUpWith runs a search on page with lookup and hands its result over, as
// Tick does.
func lookUpWith(t *testing.T, page *settingsPage, lookup func(context.Context) (string, string, error)) {
	t.Helper()
	page.lookup = lookup
	page.turnOn()
	if !page.searching || page.settings.UseYtDlp {
		t.Fatalf("while searching: searching=%t, use=%t; want yt-dlp not yet in use", page.searching, page.settings.UseYtDlp)
	}
	if text, _ := page.statusText(); text != "yt-dlp を探しています…" {
		t.Errorf("status while searching = %q", text)
	}
	page.handle(<-page.lookupResults)
}

func TestSettingsPageTurnsOnFoundYtDlp(t *testing.T) {
	settings := appSettings{}
	saved := 0
	page := &settingsPage{settings: &settings, onChange: func() error { saved++; return nil }}
	lookUpWith(t, page, func(context.Context) (string, string, error) {
		return "/opt/homebrew/bin/yt-dlp", "2026.10.26", nil
	})
	if !settings.UseYtDlp || settings.YtDlpPath != "/opt/homebrew/bin/yt-dlp" || saved != 1 {
		t.Errorf("settings = %+v, saved %d times", settings, saved)
	}
	if text, _ := page.statusText(); text != "使用する yt-dlp：/opt/homebrew/bin/yt-dlp\nバージョン：2026.10.26" {
		t.Errorf("status = %q", text)
	}

	page.turnOff()
	if settings.UseYtDlp || saved != 2 {
		t.Errorf("after turning off: %+v, saved %d times", settings, saved)
	}
	if text, _ := page.statusText(); text != "" {
		t.Errorf("status when off = %q", text)
	}
}

func TestSettingsPageLeavesMissingYtDlpOff(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"not found", errYtDlpNotFound, "yt-dlp が見つかりませんでした"},
		{"does not run", &ytDlpStartError{err: errors.New("exec format error")}, "yt-dlpを実行できません"},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := appSettings{}
			page := &settingsPage{settings: &settings}
			lookUpWith(t, page, func(context.Context) (string, string, error) {
				return "/usr/local/bin/yt-dlp", "", test.err
			})
			if settings.UseYtDlp || page.searching {
				t.Errorf("settings = %+v, searching=%t; want yt-dlp off", settings, page.searching)
			}
			if text, _ := page.statusText(); !strings.HasPrefix(text, test.want) {
				t.Errorf("status = %q, want %q first", text, test.want)
			}
		})
	}
}

func TestSettingsPageTurnsOffYtDlpThatStoppedRunning(t *testing.T) {
	// yt-dlp was turned on in an earlier session and no longer runs.
	settings := appSettings{UseYtDlp: true, YtDlpPath: "/usr/local/bin/yt-dlp"}
	saved := 0
	page := &settingsPage{settings: &settings, onChange: func() error { saved++; return nil }}
	page.start(func(context.Context) (string, string, error) {
		return "/usr/local/bin/yt-dlp", "", &ytDlpStartError{err: errors.New("no such file")}
	})
	page.handle(<-page.lookupResults)
	if settings.UseYtDlp || saved != 1 {
		t.Errorf("settings = %+v, saved %d times", settings, saved)
	}
}

func TestSettingsPageHoldsResultWhileBusy(t *testing.T) {
	// The search ends while a download started meanwhile is running: the
	// setting must not change under it.
	settings := appSettings{}
	saved := 0
	page := &settingsPage{settings: &settings, onChange: func() error { saved++; return nil }}
	page.lookup = func(context.Context) (string, string, error) { return "/usr/local/bin/yt-dlp", "2026.10.26", nil }
	page.turnOn()
	result := <-page.lookupResults
	page.lookupResults <- result // as if it had just arrived

	page.poll(true)
	if settings.UseYtDlp || saved != 0 || !page.searching {
		t.Fatalf("while busy: %+v, saved %d times, searching=%t", settings, saved, page.searching)
	}
	page.poll(false)
	if !settings.UseYtDlp || settings.YtDlpPath != "/usr/local/bin/yt-dlp" || saved != 1 || page.searching {
		t.Errorf("after: %+v, saved %d times, searching=%t", settings, saved, page.searching)
	}
}

func TestSettingsPageChecksAgainWhenOpened(t *testing.T) {
	settings := appSettings{UseYtDlp: true, YtDlpPath: "/usr/local/bin/yt-dlp"}
	page := &settingsPage{settings: &settings}
	page.start(checkVersion(settings.YtDlpPath))
	page.opened() // a check is running: nothing to redo
	page.handle(ytDlpLookupResult{path: settings.YtDlpPath, version: "2026.10.26"})
	if !page.versionChecked {
		t.Fatal("the check was not recorded")
	}
	page.opened()
	if page.versionChecked {
		t.Error("opening the page again does not check again")
	}
}

func TestSettingsPageClearsSaveError(t *testing.T) {
	settings := appSettings{}
	fail := true
	page := &settingsPage{settings: &settings, onChange: func() error {
		if fail {
			return errors.New("disk full")
		}
		return nil
	}}
	page.save()
	if text, _ := page.statusText(); !strings.HasPrefix(text, "設定を保存できませんでした") {
		t.Errorf("status = %q", text)
	}
	fail = false
	page.save()
	if text, _ := page.statusText(); text != "" {
		t.Errorf("status after saving = %q", text)
	}
}

func TestUsableSettings(t *testing.T) {
	present := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(present, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	found := func() string { return "/found/yt-dlp" }
	none := func() string { return "" }
	for _, test := range []struct {
		in   appSettings
		find func() string
		want appSettings
	}{
		{appSettings{UseYtDlp: true, YtDlpPath: present}, none, appSettings{UseYtDlp: true, YtDlpPath: present}},
		{appSettings{UseYtDlp: true, YtDlpPath: "/gone/yt-dlp"}, found, appSettings{UseYtDlp: true, YtDlpPath: "/found/yt-dlp"}},
		{appSettings{UseYtDlp: true, YtDlpPath: "/gone/yt-dlp"}, none, appSettings{}},
		{appSettings{YtDlpPath: "/gone/yt-dlp"}, found, appSettings{YtDlpPath: "/gone/yt-dlp"}},
	} {
		if got := usableSettings(test.in, test.find); got != test.want {
			t.Errorf("usableSettings(%+v) = %+v, want %+v", test.in, got, test.want)
		}
	}
}
