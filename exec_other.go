//go:build !windows

package main

import "os/exec"

// hideConsoleWindow does nothing outside Windows.
func hideConsoleWindow(*exec.Cmd) {}
