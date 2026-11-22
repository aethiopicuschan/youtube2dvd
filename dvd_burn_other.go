//go:build !darwin

package main

import (
	stdcontext "context"
	"fmt"
)

// runDrutil exists only on macOS.
func runDrutil(stdcontext.Context, []string, dvdExportProgressReporter) error {
	return fmt.Errorf("このOSでのDVD書き込みには対応していません")
}
