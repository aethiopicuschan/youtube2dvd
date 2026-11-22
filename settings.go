package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/ncruces/zenity"
)

// appSettings holds the choices the user made on the settings page.
type appSettings struct {
	// UseYtDlp downloads with the yt-dlp installed by the user instead of
	// the built-in downloader.
	UseYtDlp bool `json:"use_yt_dlp"`
	// YtDlpPath is the yt-dlp executable to run.
	YtDlpPath string `json:"yt_dlp_path"`
	// YtDlpCookiesPath is a cookies.txt the user chose, passed to yt-dlp
	// with --cookies; empty passes none.
	YtDlpCookiesPath string `json:"yt_dlp_cookies_path,omitempty"`
}

// normalized returns the settings with yt-dlp turned off when no executable
// is set: without a path there is nothing to run, and downloads would only
// fail.
func (s appSettings) normalized() appSettings {
	s.YtDlpPath = strings.TrimSpace(s.YtDlpPath)
	if s.YtDlpPath == "" {
		s.UseYtDlp = false
	}
	return s
}

func settingsFilePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "youtube2dvd", "settings.json"), nil
}

// usableSettings prepares settings read from the file for use: a yt-dlp
// that was moved or reinstalled elsewhere since is looked for again, and
// turned off when it is gone.
func usableSettings(settings appSettings, find func() string) appSettings {
	if settings.UseYtDlp && !isRegularFile(settings.YtDlpPath) {
		settings.YtDlpPath = find()
	}
	return settings.normalized()
}

// resetSettingsFile empties the settings file, which reads as the defaults.
func resetSettingsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, nil, 0o600)
}

// ensureSettingsFile writes settings to path unless the file is there
// already, so that it can be opened before anything was saved.
func ensureSettingsFile(path string, settings appSettings) error {
	if isRegularFile(path) {
		return nil
	}
	return saveSettings(path, settings)
}

// loadSettings reads the settings file. A missing or empty file gives the
// defaults; resetting the settings empties the file.
func loadSettings(path string) (appSettings, error) {
	var settings appSettings
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	// An editor may save the file with a byte order mark, which JSON does
	// not allow.
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	if len(bytes.TrimSpace(data)) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return appSettings{}, err
	}
	return settings, nil
}

func saveSettings(path string, settings appSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// findYtDlp looks for a yt-dlp the user installed: on PATH, and in the usual
// install locations, because an app started from the desktop often does not
// inherit the PATH of a terminal. It returns "" when there is none.
func findYtDlp() string {
	if path, err := exec.LookPath("yt-dlp"); err == nil {
		return path
	}
	for _, candidate := range ytDlpCandidatePaths(runtime.GOOS, os.Getenv, os.UserHomeDir) {
		if isRegularFile(candidate) {
			return candidate
		}
	}
	return ""
}

func isRegularFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func ytDlpCandidatePaths(goos string, getenv func(string) string, homeDir func() (string, error)) []string {
	home, _ := homeDir()
	var candidates []string
	// The executables yt-dlp publishes on its releases page are often left
	// in the Downloads folder.
	downloads := ""
	if home != "" {
		downloads = filepath.Join(home, "Downloads")
	}
	switch goos {
	case "windows":
		if local := getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates,
				filepath.Join(local, "Microsoft", "WinGet", "Links", "yt-dlp.exe"),
				filepath.Join(local, "Programs", "yt-dlp", "yt-dlp.exe"),
			)
		}
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "scoop", "shims", "yt-dlp.exe"))
		}
		if programData := getenv("ProgramData"); programData != "" {
			candidates = append(candidates, filepath.Join(programData, "chocolatey", "bin", "yt-dlp.exe"))
		}
		if downloads != "" {
			candidates = append(candidates, filepath.Join(downloads, "yt-dlp.exe"))
		}
	case "darwin":
		candidates = append(candidates, "/opt/homebrew/bin/yt-dlp", "/usr/local/bin/yt-dlp", "/opt/local/bin/yt-dlp")
		if home != "" {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "yt-dlp"), filepath.Join(downloads, "yt-dlp_macos"))
		}
	default:
		candidates = append(candidates, "/usr/local/bin/yt-dlp", "/usr/bin/yt-dlp", "/snap/bin/yt-dlp")
		if home != "" {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "yt-dlp"), filepath.Join(downloads, "yt-dlp_linux"))
		}
	}
	return candidates
}

// chooseCookiesFile asks the user for a cookies.txt with a file dialog,
// opened in the folder of current or else in Downloads. It returns
// zenity.ErrCanceled when the user closes the dialog.
func chooseCookiesFile(current string) (string, error) {
	options := []zenity.Option{
		zenity.Title("cookies.txt を選択"),
		zenity.FileFilters{
			{Name: "クッキーのファイル (*.txt)", Patterns: []string{"*.txt"}, CaseFold: true},
			{Name: "すべてのファイル", Patterns: []string{"*"}},
		},
	}
	directory := filepath.Dir(current)
	if current == "" || !isDirectory(directory) {
		directory = ""
		if home, err := os.UserHomeDir(); err == nil {
			directory = filepath.Join(home, "Downloads")
		}
	}
	if directory != "" && isDirectory(directory) {
		// A trailing separator makes it the folder the dialog opens in.
		options = append(options, zenity.Filename(directory+string(filepath.Separator)))
	}
	path, err := zenity.SelectFile(options...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(path), nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// netscapeCookiesHeader is the first line yt-dlp requires of a cookies file,
// as Python's MozillaCookieJar does.
var netscapeCookiesHeader = regexp.MustCompile(`#( Netscape)? HTTP Cookie File`)

// checkCookiesFile reports why yt-dlp could not read path as cookies, or nil.
func checkCookiesFile(path string) error {
	if !isRegularFile(path) {
		return errors.New("ファイルが見つかりません")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("ファイルを開けません")
	}
	defer file.Close()
	first, err := bufio.NewReader(io.LimitReader(file, 64<<10)).ReadString('\n')
	if err != nil && err != io.EOF {
		return errors.New("ファイルを読み取れません")
	}
	if strings.HasPrefix(first, "\ufeff") {
		// yt-dlp reads the file without dropping it, and then refuses the
		// header after it.
		return errors.New("ファイルの先頭にBOMが付いています。BOMなしのUTF-8で保存し直してください")
	}
	if netscapeCookiesHeader.MatchString(first) {
		return nil
	}
	if trimmed := strings.TrimSpace(first); strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		return errors.New("このファイルはJSON形式です。Netscape形式の cookies.txt を選んでください")
	}
	return errors.New("このファイルはNetscape形式の cookies.txt ではありません")
}
