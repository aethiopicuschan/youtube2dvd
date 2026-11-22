package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
}

func settingsFilePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "youtube2dvd", "settings.json"), nil
}

// loadSettings reads the settings file. A missing file gives the defaults.
func loadSettings(path string) (appSettings, error) {
	var settings appSettings
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
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
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func ytDlpCandidatePaths(goos string, getenv func(string) string, homeDir func() (string, error)) []string {
	home, _ := homeDir()
	var candidates []string
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
	case "darwin":
		candidates = append(candidates, "/opt/homebrew/bin/yt-dlp", "/usr/local/bin/yt-dlp")
		if home != "" {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "yt-dlp"))
		}
	default:
		candidates = append(candidates, "/usr/local/bin/yt-dlp", "/usr/bin/yt-dlp")
		if home != "" {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "yt-dlp"))
		}
	}
	return candidates
}

// chooseYtDlpPath asks the user for the yt-dlp executable with a file
// dialog. It returns zenity.ErrCanceled when the user closes the dialog.
func chooseYtDlpPath() (string, error) {
	options := []zenity.Option{zenity.Title("yt-dlp の実行ファイルを選択")}
	if directory := ytDlpDialogDirectory(runtime.GOOS, os.Getenv, os.UserHomeDir, isDirectory); directory != "" {
		// A trailing separator makes it the folder the dialog opens in.
		options = append(options, zenity.Filename(directory+string(filepath.Separator)))
	}
	if runtime.GOOS == "windows" {
		options = append(options, zenity.FileFilters{{Name: "実行ファイル (*.exe)", Patterns: []string{"*.exe"}, CaseFold: true}})
	}
	path, err := zenity.SelectFile(options...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(path), nil
}

// ytDlpDialogDirectory returns the folder to open the file dialog in: the
// first usual install location of yt-dlp that exists, or else the Downloads
// folder, where a downloaded yt-dlp executable is often left.
func ytDlpDialogDirectory(goos string, getenv func(string) string, homeDir func() (string, error), exists func(string) bool) string {
	for _, candidate := range ytDlpCandidatePaths(goos, getenv, homeDir) {
		if directory := filepath.Dir(candidate); exists(directory) {
			return directory
		}
	}
	home, err := homeDir()
	if err != nil || home == "" {
		return ""
	}
	if downloads := filepath.Join(home, "Downloads"); exists(downloads) {
		return downloads
	}
	return home
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
