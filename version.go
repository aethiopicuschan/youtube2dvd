package main

import "runtime/debug"

func init() {
	bi, ok := debug.ReadBuildInfo()
	if ok {
		version = bi.Main.Version
	}
}
