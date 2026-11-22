package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kkdai/youtube/v2"
)

func loadYtDlpFixture(t *testing.T) ytDlpInfo {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ytdlp_info.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := parseYtDlpInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func ytDlpQualityLabels(qualities []downloadQuality) []string {
	labels := make([]string, 0, len(qualities))
	for _, quality := range qualities {
		labels = append(labels, quality.label)
	}
	return labels
}

func TestYtDlpQualitiesFromFixture(t *testing.T) {
	info := loadYtDlpFixture(t)
	qualities := ytDlpQualities(info)

	// Nothing above 480p; 360p only as the progressive MP4; HLS and
	// video-only MP4 streams are not offered.
	want := []string{"480p (WebM)", "360p (MP4)", "240p (WebM)", "144p (WebM)"}
	if got := ytDlpQualityLabels(qualities); !slices.Equal(got, want) {
		t.Fatalf("labels = %q, want %q", got, want)
	}

	combined := qualities[0]
	if combined.ytDlpFormat != "244" || combined.ytDlpAudioFormat != "251" || combined.extension != ".webm" {
		t.Errorf("480p = %+v, want video 244 with audio 251 as .webm", combined)
	}
	if combined.videoSize != 2400000 || combined.audioSize != 300000 {
		t.Errorf("480p sizes = %d + %d, want 2400000 + 300000", combined.videoSize, combined.audioSize)
	}
	progressive := qualities[1]
	if progressive.ytDlpFormat != "18" || progressive.ytDlpAudioFormat != "" || progressive.extension != ".mp4" {
		t.Errorf("360p = %+v, want format 18 without separate audio as .mp4", progressive)
	}
	if qualities[2].videoSize != 900000 {
		t.Errorf("240p size = %d, want the approximate size 900000", qualities[2].videoSize)
	}

	seenItags := make(map[int]bool)
	for _, quality := range qualities {
		if quality.itag == 0 || seenItags[quality.itag] {
			t.Errorf("quality %q has itag %d, want a unique non-zero value", quality.label, quality.itag)
		}
		seenItags[quality.itag] = true
		if quality.title != "テスト動画: サンプル/01" {
			t.Errorf("quality %q has title %q", quality.label, quality.title)
		}
		if quality.height > 480 {
			t.Errorf("quality %q is taller than 480", quality.label)
		}
	}
}

func TestYtDlpQualitiesWithoutWebMAudio(t *testing.T) {
	info := loadYtDlpFixture(t)
	info.Formats = slices.DeleteFunc(info.Formats, func(format ytDlpFormatInfo) bool {
		return format.Ext == "webm" && !format.hasVideo()
	})

	want := []string{"360p (MP4)"}
	if got := ytDlpQualityLabels(ytDlpQualities(info)); !slices.Equal(got, want) {
		t.Fatalf("labels = %q, want %q", got, want)
	}
}

func TestYtDlpQualitiesPrefersProgressiveAtSameHeight(t *testing.T) {
	info := ytDlpInfo{Formats: []ytDlpFormatInfo{
		{FormatID: "243", Ext: "webm", VideoCodec: "vp9", AudioCodec: "none", Height: 360},
		{FormatID: "251", Ext: "webm", VideoCodec: "none", AudioCodec: "opus", AudioBitrate: 130},
		{FormatID: "18", Ext: "mp4", Protocol: "https", VideoCodec: "avc1", AudioCodec: "mp4a", Height: 360},
	}}

	qualities := ytDlpQualities(info)
	if len(qualities) != 1 || qualities[0].ytDlpFormat != "18" || qualities[0].ytDlpAudioFormat != "" {
		t.Fatalf("qualities = %+v, want only the progressive format 18", qualities)
	}
}

func TestYtDlpQualitiesNoneAvailable(t *testing.T) {
	info := ytDlpInfo{Formats: []ytDlpFormatInfo{
		{FormatID: "22", Ext: "mp4", Protocol: "https", VideoCodec: "avc1", AudioCodec: "mp4a", Height: 720},
		{FormatID: "248", Ext: "webm", VideoCodec: "vp9", AudioCodec: "none", Height: 1080},
		{FormatID: "251", Ext: "webm", VideoCodec: "none", AudioCodec: "opus", AudioBitrate: 130},
	}}
	if qualities := ytDlpQualities(info); len(qualities) != 0 {
		t.Fatalf("qualities = %q, want none", ytDlpQualityLabels(qualities))
	}
}

func TestYtDlpThumbnailURL(t *testing.T) {
	info := loadYtDlpFixture(t)
	if got, want := ytDlpThumbnailURL(info), "https://example.invalid/vi/exampleid01/hqdefault.jpg?sqp=abc"; got != want {
		t.Errorf("thumbnail = %q, want the largest JPEG %q", got, want)
	}
	if got := ytDlpThumbnailURL(ytDlpInfo{Thumbnail: "https://example.invalid/a.webp"}); got != "" {
		t.Errorf("thumbnail = %q, want none for WebP", got)
	}
}

func TestParseYtDlpProgress(t *testing.T) {
	tests := []struct {
		line string
		want downloadProgress
		ok   bool
	}{
		{line: "youtube2dvd-progress:1024/4096/NA", want: downloadProgress{downloaded: 1024, total: 4096}, ok: true},
		{line: "youtube2dvd-progress:1024/NA/8192.0", want: downloadProgress{downloaded: 1024, total: 8192}, ok: true},
		{line: "youtube2dvd-progress:1024/NA/NA", want: downloadProgress{downloaded: 1024}, ok: true},
		{line: "  youtube2dvd-progress:10/20/30\r", want: downloadProgress{downloaded: 10, total: 20}, ok: true},
		{line: "youtube2dvd-progress:NA/4096/4096"},
		{line: "youtube2dvd-progress:-5/4096/4096"},
		{line: "youtube2dvd-progress:1024/4096"},
		{line: "youtube2dvd-progress:1/2/3/4"},
		{line: "youtube2dvd-progress:"},
		{line: "[download] 12.3% of 4.00MiB at 1.00MiB/s ETA 00:03"},
		{line: "[youtube] exampleid01: Downloading webpage"},
		{line: ""},
	}
	for _, test := range tests {
		got, ok := parseYtDlpProgress(test.line)
		if ok != test.ok || got != test.want {
			t.Errorf("parseYtDlpProgress(%q) = %+v, %t; want %+v, %t", test.line, got, ok, test.want, test.ok)
		}
	}
}

// Options that change how yt-dlp reaches the site or signs in belong to the
// user's own yt-dlp configuration; the app must never pass them.
var forbiddenYtDlpOptions = []string{
	"--cookies",
	"--cookies-from-browser",
	"--extractor-args",
	"--user-agent",
	"--add-header",
	"--add-headers",
	"--impersonate",
	"--username",
	"--password",
	"--netrc",
	"--proxy",
	"--geo-bypass",
	"--ignore-config",
	"--no-config",
	"--exec",
}

func assertYtDlpArgs(t *testing.T, args []string, videoURL string) {
	t.Helper()
	separator := slices.Index(args, "--")
	if separator < 0 || separator != len(args)-2 || args[len(args)-1] != videoURL {
		t.Errorf("args = %q, want them to end with \"--\" and the URL", args)
	}
	for _, required := range []string{"--no-playlist", "--newline", "--no-color"} {
		if index := slices.Index(args, required); index < 0 || index > separator {
			t.Errorf("args = %q, want %s before \"--\"", args, required)
		}
	}
	for _, arg := range args[:max(separator, 0)] {
		name, _, _ := strings.Cut(arg, "=")
		if slices.Contains(forbiddenYtDlpOptions, name) {
			t.Errorf("args = %q contain the forbidden option %s", args, name)
		}
	}
}

func TestYtDlpArgs(t *testing.T) {
	for _, videoURL := range []string{
		"https://www.youtube.com/watch?v=exampleid01",
		"--exec=touch /tmp/pwned",
		"-o",
	} {
		assertYtDlpArgs(t, ytDlpInfoArgs(videoURL), videoURL)
		if !slices.Contains(ytDlpInfoArgs(videoURL), "-J") {
			t.Errorf("info args for %q lack -J", videoURL)
		}

		args := ytDlpDownloadArgs("244", "/tmp/work/video.webm", videoURL)
		assertYtDlpArgs(t, args, videoURL)
		for _, pair := range [][2]string{
			{"-f", "244"},
			{"-o", "/tmp/work/video.webm"},
			{"--progress-template", ytDlpProgressTemplate},
		} {
			index := slices.Index(args, pair[0])
			if index < 0 || index+1 >= len(args) || args[index+1] != pair[1] {
				t.Errorf("args = %q, want %s %s", args, pair[0], pair[1])
			}
		}
	}
}

func TestYtDlpDownloadArgsEscapeOutputTemplate(t *testing.T) {
	args := ytDlpDownloadArgs("18", "/home/100%user/video.mp4", "https://www.youtube.com/watch?v=exampleid01")
	index := slices.Index(args, "-o")
	if index < 0 || args[index+1] != "/home/100%%user/video.mp4" {
		t.Errorf("args = %q, want the %% in the output path doubled", args)
	}
}

// fakeYtDlp stands in for yt-dlp: a download writes the requested output
// file and reports progress lines.
type fakeYtDlp struct {
	calls [][]string
	// sizes gives the bytes written per format; a missing format fails.
	sizes map[string]int
	// failFormat makes the download of this format fail with failErr.
	failFormat string
	failErr    error
	// skipOutput makes downloads succeed without writing a file.
	skipOutput bool
	// captions is written by a subtitle-only run, as yt-dlp names it;
	// nil writes nothing, as yt-dlp does when the track is missing.
	captions []byte
	// forbidOnce makes the first download of these formats fail as YouTube
	// refusing it does.
	forbidOnce map[string]bool
}

func (f *fakeYtDlp) Run(_ context.Context, args []string, onLine func(string)) ([]byte, error) {
	f.calls = append(f.calls, slices.Clone(args))
	if slices.Contains(args, "--skip-download") {
		if f.captions == nil {
			return nil, nil
		}
		base := strings.ReplaceAll(args[slices.Index(args, "-o")+1], "%%", "%")
		keys := strings.TrimSuffix(strings.TrimPrefix(args[slices.Index(args, "--sub-langs")+1], "^("), ")$")
		for key := range strings.SplitSeq(strings.ReplaceAll(keys, `\`, ""), "|") {
			if err := os.WriteFile(base+"."+key+".vtt", f.captions, 0o600); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	format := args[slices.Index(args, "-f")+1]
	output := strings.ReplaceAll(args[slices.Index(args, "-o")+1], "%%", "%")
	if format == f.failFormat {
		// A failed download leaves a partial file behind.
		if err := os.WriteFile(output+".part", []byte("partial"), 0o600); err != nil {
			return nil, err
		}
		return nil, f.failErr
	}
	if f.forbidOnce[format] {
		delete(f.forbidOnce, format)
		return nil, &ytDlpExitError{stderr: "ERROR: unable to download video data: HTTP Error 403: Forbidden"}
	}
	size, ok := f.sizes[format]
	if !ok {
		return nil, &ytDlpExitError{stderr: "ERROR: requested format is not available"}
	}
	if onLine != nil {
		onLine("[download] Destination: " + output)
		onLine(ytDlpProgressPrefix + "0/NA/999")
		onLine(ytDlpProgressPrefix + itoa(size/2) + "/" + itoa(size) + "/NA")
		onLine(ytDlpProgressPrefix + itoa(size) + "/" + itoa(size) + "/NA")
	}
	if f.skipOutput {
		return nil, nil
	}
	return nil, os.WriteFile(output, make([]byte, size), 0o600)
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func assertOnlyFiles(t *testing.T, directory string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("files in the download folder = %q, want %q", got, want)
	}
}

func failingMux(t *testing.T) streamMuxer {
	return func(context.Context, string, []audioStream, string) error {
		t.Error("mux was called for a single stream")
		return nil
	}
}

const testVideoURL = "https://www.youtube.com/watch?v=exampleid01"

func TestDownloadWithYtDlpProgressive(t *testing.T) {
	directory := t.TempDir()
	runner := &fakeYtDlp{sizes: map[string]int{"18": 2000}}
	quality := downloadQuality{title: `テスト: 動画/01`, extension: ".mp4", ytDlpFormat: "18", videoSize: 2000}
	var progresses []downloadProgress

	path, _, err := downloadWithYtDlp(context.Background(), runner, directory, testVideoURL, quality, nil, nil, failingMux(t), func(progress downloadProgress) {
		progresses = append(progresses, progress)
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(directory, "テスト_ 動画_01.mp4"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 2000 {
		t.Errorf("saved file: %v, %v; want 2000 bytes", info, err)
	}
	assertOnlyFiles(t, directory, "テスト_ 動画_01.mp4")
	if len(runner.calls) != 1 {
		t.Fatalf("yt-dlp ran %d times, want once", len(runner.calls))
	}
	assertYtDlpArgs(t, runner.calls[0], testVideoURL)
	want := []downloadProgress{{downloaded: 0, total: 999}, {downloaded: 1000, total: 2000}, {downloaded: 2000, total: 2000}}
	if !slices.Equal(progresses, want) {
		t.Errorf("progress = %+v, want %+v", progresses, want)
	}
}

func TestDownloadWithYtDlpCombinesStreams(t *testing.T) {
	directory := t.TempDir()
	runner := &fakeYtDlp{sizes: map[string]int{"244": 3000, "251": 1000}}
	quality := downloadQuality{title: "combined", extension: ".webm", ytDlpFormat: "244", ytDlpAudioFormat: "251", videoSize: 3000, audioSize: 1000}
	var progresses []downloadProgress
	muxed := false
	mux := func(_ context.Context, videoPath string, audio []audioStream, outputPath string) error {
		if len(audio) != 1 || !audio[0].Default {
			t.Errorf("audio streams = %+v, want one default stream", audio)
		}
		audioPath := audio[0].Path
		muxed = true
		for _, input := range []string{videoPath, audioPath} {
			if _, err := os.Stat(input); err != nil {
				t.Errorf("mux input is missing: %v", err)
			}
		}
		return os.WriteFile(outputPath, []byte("combined"), 0o600)
	}

	path, _, err := downloadWithYtDlp(context.Background(), runner, directory, testVideoURL, quality, nil, nil, mux, func(progress downloadProgress) {
		progresses = append(progresses, progress)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !muxed {
		t.Error("mux was not called")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "combined" {
		t.Errorf("saved file = %q, %v; want the muxed output", data, err)
	}
	assertOnlyFiles(t, directory, "combined.webm")
	if len(runner.calls) != 2 {
		t.Fatalf("yt-dlp ran %d times, want twice", len(runner.calls))
	}
	// One download from 0 to the total of both streams, never backwards.
	var previous int64
	for _, progress := range progresses {
		if progress.total != 4000 || progress.downloaded < previous || progress.downloaded > 4000 {
			t.Fatalf("progress = %+v, want steady progress out of 4000", progresses)
		}
		previous = progress.downloaded
	}
	if previous != 4000 {
		t.Errorf("last progress = %d, want 4000", previous)
	}
}

func TestDownloadWithYtDlpFailuresLeaveNoFiles(t *testing.T) {
	exitErr := &ytDlpExitError{stderr: "WARNING: something\nERROR: [youtube] exampleid01: Video unavailable\n"}
	tests := []struct {
		name    string
		runner  *fakeYtDlp
		quality downloadQuality
		mux     streamMuxer
		check   func(t *testing.T, err error)
	}{
		{
			name:    "yt-dlp fails",
			runner:  &fakeYtDlp{failFormat: "18", failErr: exitErr},
			quality: downloadQuality{title: "video", extension: ".mp4", ytDlpFormat: "18"},
			check: func(t *testing.T, err error) {
				if !strings.Contains(err.Error(), "Video unavailable") {
					t.Errorf("error = %q, want the end of yt-dlp's error output", err)
				}
			},
		},
		{
			name:    "audio download fails after video",
			runner:  &fakeYtDlp{sizes: map[string]int{"244": 3000}, failFormat: "251", failErr: exitErr},
			quality: downloadQuality{title: "video", extension: ".webm", ytDlpFormat: "244", ytDlpAudioFormat: "251"},
			check:   func(*testing.T, error) {},
		},
		{
			name:    "cancelled",
			runner:  &fakeYtDlp{failFormat: "18", failErr: context.Canceled},
			quality: downloadQuality{title: "video", extension: ".mp4", ytDlpFormat: "18"},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error = %v, want context.Canceled", err)
				}
			},
		},
		{
			name:    "yt-dlp cannot start",
			runner:  &fakeYtDlp{failFormat: "18", failErr: &ytDlpStartError{err: exec.ErrNotFound}},
			quality: downloadQuality{title: "video", extension: ".mp4", ytDlpFormat: "18"},
			check: func(t *testing.T, err error) {
				if !strings.Contains(err.Error(), "「設定」") {
					t.Errorf("error = %q, want it to point to the settings page", err)
				}
			},
		},
		{
			name:    "no output file",
			runner:  &fakeYtDlp{sizes: map[string]int{"18": 10}, skipOutput: true},
			quality: downloadQuality{title: "video", extension: ".mp4", ytDlpFormat: "18"},
			check:   func(*testing.T, error) {},
		},
		{
			name:    "mux fails",
			runner:  &fakeYtDlp{sizes: map[string]int{"244": 3000, "251": 1000}},
			quality: downloadQuality{title: "video", extension: ".webm", ytDlpFormat: "244", ytDlpAudioFormat: "251"},
			mux: func(_ context.Context, _ string, _ []audioStream, outputPath string) error {
				_ = os.WriteFile(outputPath, []byte("half"), 0o600)
				return errors.New("broken stream")
			},
			check: func(t *testing.T, err error) {
				if !strings.Contains(err.Error(), "結合できませんでした") {
					t.Errorf("error = %q, want the mux failure", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			mux := test.mux
			if mux == nil {
				mux = func(context.Context, string, []audioStream, string) error { return nil }
			}
			path, _, err := downloadWithYtDlp(context.Background(), test.runner, directory, testVideoURL, test.quality, nil, nil, mux, nil)
			if err == nil {
				t.Fatalf("download succeeded with %q, want an error", path)
			}
			test.check(t, err)
			assertOnlyFiles(t, directory)
		})
	}
}

func TestExplainBotCheck(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("動画情報を取得できませんでした: %w", youtube.ErrBotCheck),
		&ytDlpExitError{stderr: "ERROR: [youtube] EOxarwd3eTs: Sign in to confirm you’re not a bot. This helps protect our community.", err: errors.New("exit status 1")},
	} {
		if got := explainBotCheck(err); !errors.Is(got, errYouTubeBotCheck) {
			t.Errorf("explainBotCheck(%v) = %v", err, got)
		}
	}
	other := &ytDlpExitError{stderr: "ERROR: Video unavailable", err: errors.New("exit status 1")}
	if got := explainBotCheck(other); got != other {
		t.Errorf("another error was replaced: %v", got)
	}
	if explainBotCheck(nil) != nil {
		t.Error("nil became an error")
	}
}

func TestYtDlpErrorMessages(t *testing.T) {
	exit := &ytDlpExitError{stderr: "one\n\ntwo\nthree\nERROR: four\n", err: errors.New("exit status 1")}
	if got, want := exit.Error(), "yt-dlpがエラーで終了しました: two / three / ERROR: four"; got != want {
		t.Errorf("exit error = %q, want %q", got, want)
	}
	silent := &ytDlpExitError{err: errors.New("exit status 2")}
	if got := silent.Error(); !strings.Contains(got, "exit status 2") {
		t.Errorf("exit error without output = %q, want the exit status", got)
	}
	for reason, err := range map[string]error{
		"ファイルが見つかりません": os.ErrNotExist,
		"実行する権限がありません": os.ErrPermission,
	} {
		if got := (&ytDlpStartError{err: err}).Error(); !strings.Contains(got, reason) || !strings.Contains(got, "「設定」") {
			t.Errorf("start error = %q, want %q and a pointer to the settings page", got, reason)
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	if settings, err := loadSettings(path); err != nil || settings != (appSettings{}) {
		t.Fatalf("missing file: %+v, %v; want the defaults", settings, err)
	}
	want := appSettings{UseYtDlp: true, YtDlpPath: "/opt/homebrew/bin/yt-dlp"}
	if err := saveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err != nil || got != want {
		t.Fatalf("loaded %+v, %v; want %+v", got, err, want)
	}
	assertOnlyFiles(t, filepath.Dir(path), "settings.json")

	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if settings, err := loadSettings(path); err == nil || settings != (appSettings{}) {
		t.Errorf("broken file: %+v, %v; want the defaults and an error", settings, err)
	}
}

func TestYtDlpCandidatePaths(t *testing.T) {
	home := func() (string, error) { return "/home/user", nil }
	env := func(name string) string {
		return map[string]string{"LOCALAPPDATA": `C:\Users\user\AppData\Local`}[name]
	}
	darwin := ytDlpCandidatePaths("darwin", env, home)
	for _, want := range []string{"/opt/homebrew/bin/yt-dlp", "/usr/local/bin/yt-dlp"} {
		if !slices.Contains(darwin, want) {
			t.Errorf("darwin candidates = %q, want %q among them", darwin, want)
		}
	}
	windows := ytDlpCandidatePaths("windows", env, home)
	if !slices.ContainsFunc(windows, func(path string) bool {
		return strings.Contains(path, "WinGet") && strings.HasSuffix(path, "yt-dlp.exe")
	}) {
		t.Errorf("windows candidates = %q, want the WinGet links folder among them", windows)
	}
	// The executables of the releases page, left where they were saved.
	if !slices.Contains(windows, filepath.Join("/home/user", "Downloads", "yt-dlp.exe")) {
		t.Errorf("windows candidates = %q, want the Downloads folder among them", windows)
	}
	if !slices.Contains(darwin, "/home/user/Downloads/yt-dlp_macos") {
		t.Errorf("darwin candidates = %q, want yt-dlp_macos in Downloads among them", darwin)
	}
}

func TestSettingsNormalizedTurnsYtDlpOffWithoutPath(t *testing.T) {
	tests := []struct {
		in, want appSettings
	}{
		{in: appSettings{UseYtDlp: true, YtDlpPath: ""}, want: appSettings{UseYtDlp: false, YtDlpPath: ""}},
		{in: appSettings{UseYtDlp: true, YtDlpPath: "   "}, want: appSettings{UseYtDlp: false, YtDlpPath: ""}},
		{in: appSettings{UseYtDlp: true, YtDlpPath: " /opt/homebrew/bin/yt-dlp "}, want: appSettings{UseYtDlp: true, YtDlpPath: "/opt/homebrew/bin/yt-dlp"}},
		{in: appSettings{UseYtDlp: false, YtDlpPath: "/usr/local/bin/yt-dlp"}, want: appSettings{UseYtDlp: false, YtDlpPath: "/usr/local/bin/yt-dlp"}},
	}
	for _, test := range tests {
		if got := test.in.normalized(); got != test.want {
			t.Errorf("%+v.normalized() = %+v, want %+v", test.in, got, test.want)
		}
	}
}
