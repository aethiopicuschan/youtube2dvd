//go:build windows

package main

import (
	"os/exec"
	"strconv"
)

// configureYtDlpProcess keeps yt-dlp from opening a console window, and makes
// a cancelled yt-dlp end together with the processes it started: the
// stand-alone yt-dlp.exe runs the actual program as a child process.
func configureYtDlpProcess(cmd *exec.Cmd) {
	hideConsoleWindow(cmd)
	cmd.Cancel = func() error {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideConsoleWindow(kill)
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
