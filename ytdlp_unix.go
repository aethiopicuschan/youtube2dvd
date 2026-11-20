//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// configureYtDlpProcess makes a cancelled yt-dlp end together with any
// process it started, by running it in its own process group.
func configureYtDlpProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
