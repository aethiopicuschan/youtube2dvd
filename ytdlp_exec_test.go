//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// writeFakeYtDlp writes a shell script to run in place of yt-dlp.
func writeFakeYtDlp(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecYtDlpRunnerOutputAndLines(t *testing.T) {
	path := writeFakeYtDlp(t, `echo "first"; echo "second $1"; echo "warning" >&2`)
	runner := execYtDlpRunner{path: path}

	output, err := runner.Run(context.Background(), []string{"--version"}, nil)
	if err != nil || string(output) != "first\nsecond --version\n" {
		t.Errorf("output = %q, %v", output, err)
	}

	var lines []string
	output, err = runner.Run(context.Background(), []string{"-J"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil || output != nil || !slices.Equal(lines, []string{"first", "second -J"}) {
		t.Errorf("lines = %q, output = %q, %v", lines, output, err)
	}

	if version, err := ytDlpVersion(context.Background(), execYtDlpRunner{path: writeFakeYtDlp(t, `echo 2026.11.01`)}); err != nil || version != "2026.11.01" {
		t.Errorf("version = %q, %v", version, err)
	}
}

func TestInfoAndSubtitleRunsChooseAFormat(t *testing.T) {
	// A -f in the user's yt-dlp configuration would make yt-dlp fail where
	// that format is missing; runs that download nothing give their own.
	for name, args := range map[string][]string{
		"info":      ytDlpInfoArgs(testVideoURL),
		"subtitles": ytDlpSubtitleArgs([]captionTrack{{ID: "ja"}}, "/tmp/captions", testVideoURL),
	} {
		if f := slices.Index(args, "-f"); f < 0 || args[f+1] != ytDlpAnyFormat {
			t.Errorf("%s arguments = %q, want -f %s", name, args, ytDlpAnyFormat)
		}
	}
}

func TestExecYtDlpRunnerPassesCookies(t *testing.T) {
	path := writeFakeYtDlp(t, `for arg in "$@"; do echo "$arg"; done`)
	for _, test := range []struct {
		cookies string
		want    []string
	}{
		{"", nil},
		{"/Users/me/My Cookies/cookies.txt", []string{"--cookies", "/Users/me/My Cookies/cookies.txt"}},
	} {
		output, err := execYtDlpRunner{path: path, cookies: test.cookies}.Run(context.Background(), ytDlpInfoArgs(testVideoURL), nil)
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
		got := args[:min(2, len(args))]
		if test.want == nil {
			if slices.Contains(args, "--cookies") {
				t.Errorf("args = %q, want no cookies", args)
			}
		} else if !slices.Equal(got, test.want) {
			t.Errorf("args = %q, want %q first", args, test.want)
		}
		if args[len(args)-2] != "--" || args[len(args)-1] != testVideoURL {
			t.Errorf("args = %q, want the URL last", args)
		}
	}
}

func TestExecYtDlpRunnerPassesArgumentsWithoutShell(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	path := writeFakeYtDlp(t, `for arg in "$@"; do echo "$arg"; done`)
	hostile := "$(touch " + marker + "); touch " + marker + " #"

	output, err := execYtDlpRunner{path: path}.Run(context.Background(), ytDlpInfoArgs(hostile), nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if lines[len(lines)-1] != hostile || lines[len(lines)-2] != "--" {
		t.Errorf("yt-dlp received %q, want the URL unchanged after \"--\"", lines)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the URL was interpreted by a shell")
	}
}

func TestExecYtDlpRunnerErrors(t *testing.T) {
	failing := writeFakeYtDlp(t, `echo "WARNING: slow" >&2; echo "ERROR: Video unavailable" >&2; exit 1`)
	_, err := execYtDlpRunner{path: failing}.Run(context.Background(), nil, nil)
	var exitErr *ytDlpExitError
	if !errors.As(err, &exitErr) || !strings.Contains(err.Error(), "ERROR: Video unavailable") {
		t.Errorf("error = %v, want the end of the error output", err)
	}

	for name, path := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "no-such-yt-dlp"),
		"empty":     "",
		"directory": t.TempDir(),
	} {
		_, err := execYtDlpRunner{path: path}.Run(context.Background(), []string{"--version"}, nil)
		var startErr *ytDlpStartError
		if !errors.As(err, &startErr) {
			t.Errorf("%s path: error = %v, want a start error", name, err)
		}
	}

	notExecutable := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = execYtDlpRunner{path: notExecutable}.Run(context.Background(), []string{"--version"}, nil)
	if err == nil || !strings.Contains(err.Error(), "実行する権限がありません") {
		t.Errorf("error = %v, want a permission error", err)
	}
}

func TestExecYtDlpRunnerCancelKillsProcessTree(t *testing.T) {
	directory := t.TempDir()
	started := filepath.Join(directory, "started")
	survived := filepath.Join(directory, "survived")
	// The child stands for the process a stand-alone yt-dlp starts.
	path := writeFakeYtDlp(t, `(sleep 2; touch "`+survived+`") &
touch "`+started+`"
echo "running"
sleep 30`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := execYtDlpRunner{path: path}.Run(ctx, nil, func(string) {})
		done <- err
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake yt-dlp did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	time.Sleep(3 * time.Second)
	if _, err := os.Stat(survived); err == nil {
		t.Error("a process started by yt-dlp kept running after cancellation")
	}
}
