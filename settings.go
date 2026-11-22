package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// appSettings holds the choices the user made on the settings page.
type appSettings struct {
	// UseYtDlp downloads with the yt-dlp installed by the user instead of
	// the built-in downloader.
	UseYtDlp bool `json:"use_yt_dlp"`
	// YtDlpPath is the yt-dlp executable to run.
	YtDlpPath string `json:"yt_dlp_path"`
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
