package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
