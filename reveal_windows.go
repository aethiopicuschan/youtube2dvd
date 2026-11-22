//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// revealInFileManager opens the folder of path in Explorer with the file
// selected.
func revealInFileManager(path string) error {
	cmd := exec.Command("explorer")
	// Explorer reads "/select," and the quoted path as one argument, which
	// Go's quoting of a whole argument with spaces would break. Explorer is
	// a window of its own, so it is not hidden like console programs.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + path + `"`}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Explorer exits with status 1 even when it opened the folder.
	go func() { _ = cmd.Wait() }()
	return nil
}
