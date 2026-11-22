package main

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kkdai/youtube/v2"
)

const testStreamURL = "https://rr1---sn-example.googlevideo.com/videoplayback?itag=251"

func TestYouTubeAudioTags(t *testing.T) {
	for address, want := range map[string]struct {
		kind string
		drc  bool
	}{
		testStreamURL + "&xtags=acont%3Doriginal%3Alang%3Den-US":        {"original", false},
		testStreamURL + "&xtags=acont%3Doriginal%3Adrc%3D1%3Alang%3Den": {"original", true},
		testStreamURL + "&xtags=acont%3Ddubbed-auto%3Alang%3Dja":        {"dubbed-auto", false},
		testStreamURL:   {"", false},
		"::not a url::": {"", false},
	} {
		kind, drc := youTubeAudioTags(address)
		if kind != want.kind || drc != want.drc {
			t.Errorf("youTubeAudioTags(%q) = %q, %t; want %q, %t", address, kind, drc, want.kind, want.drc)
		}
	}
}

// dubbedFormats are formats as "yt-dlp -J" lists them for a video made in
// English and dubbed into Japanese and Spanish.
func dubbedFormats() []ytDlpFormatInfo {
	preference := func(value float64) *float64 { return &value }
	audio := func(id, ext, language, note, xtags string, abr float64, pref float64) ytDlpFormatInfo {
		return ytDlpFormatInfo{
			FormatID: id, Ext: ext, Protocol: "https", VideoCodec: "none", AudioCodec: "opus",
			Language: language, FormatNote: note, AudioBitrate: abr, Filesize: abr * 1000,
			URL: testStreamURL + "&xtags=" + xtags, LanguagePreference: preference(pref),
		}
	}
	return []ytDlpFormatInfo{
		{FormatID: "18", Ext: "mp4", Protocol: "https", VideoCodec: "avc1", AudioCodec: "mp4a", Language: "en", Height: 360, Filesize: 9000},
		{FormatID: "244", Ext: "webm", Protocol: "https", VideoCodec: "vp9", AudioCodec: "none", Height: 480, Filesize: 8000},
		audio("251-drc", "webm", "en-US", "English (US) original (default), medium, DRC", "acont%3Doriginal%3Adrc%3D1", 140, 10),
		audio("251-0", "webm", "ja", "Japanese, medium", "acont%3Ddubbed", 150, -1),
		audio("251-1", "webm", "en-US", "English (US) original (default), medium", "acont%3Doriginal", 130, 10),
		audio("250-1", "webm", "en-US", "English (US) original (default), low", "acont%3Doriginal", 70, 10),
		audio("140-0", "m4a", "ja", "Japanese, medium", "acont%3Ddubbed", 129, -1),
		audio("140-1", "m4a", "en-US", "English (US) original (default), medium", "acont%3Doriginal", 129, 10),
		// Spanish is only served as WebM.
		audio("251-2", "webm", "es", "Spanish, medium", "acont%3Ddubbed-auto", 120, -1),
		audio("251-3", "webm", "en-desc", "English descriptive, medium", "", 110, -10),
		// HLS audio is left out.
		{FormatID: "233", Ext: "mp4", Protocol: "m3u8_native", VideoCodec: "none", AudioCodec: "mp4a", Language: "fr"},
	}
}

func TestYtDlpAudio(t *testing.T) {
	audio := ytDlpAudio(dubbedFormats())
	var ids, labels []string
	for _, track := range audio.Tracks {
		ids = append(ids, track.ID)
		labels = append(labels, track.label())
	}
	// The default first, then Japanese, then by name.
	if want := []string{"en-US.original", "ja.dubbed", "es.dubbed-auto", "en.descriptive"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %q, want %q", ids, want)
	}
	if want := []string{"アメリカ英語（オリジナル）", "日本語", "スペイン語（自動吹き替え）", "英語（音声解説）"}; !slices.Equal(labels, want) {
		t.Errorf("labels = %q, want %q", labels, want)
	}
	if got := audio.defaultID(); got != "en-US.original" {
		t.Errorf("default = %q", got)
	}
	original, _ := audio.find("en-US.original")
	// The best stream without compressed dynamic range.
	if original.ytDlpWebM.FormatID != "251-1" || original.ytDlpM4A.FormatID != "140-1" || !original.YouTubeDefault {
		t.Errorf("original track = %+v", original)
	}
	spanish, _ := audio.find("es.dubbed-auto")
	if _, err := ytDlpAudioFormat(spanish, ".mp4"); err == nil {
		t.Error("a WebM-only track was offered for MP4")
	}
	if choice, err := ytDlpAudioFormat(spanish, ".webm"); err != nil || choice.FormatID != "251-2" || choice.Size != 120000 {
		t.Errorf("Spanish WebM = %+v, %v", choice, err)
	}

	// A video in one language offers no choice.
	var single []ytDlpFormatInfo
	for _, format := range dubbedFormats() {
		if format.Language == "en-US" || format.hasVideo() {
			single = append(single, format)
		}
	}
	if got := ytDlpAudio(single); len(got.Tracks) != 0 {
		t.Errorf("single-language video offered %+v", got.Tracks)
	}
}

func TestYtDlpQualitiesUseOriginalAudio(t *testing.T) {
	// The Japanese dub has the highest bit rate, but the default WebM
	// audio is the original, without compressed dynamic range.
	qualities := ytDlpQualities(ytDlpInfo{Title: "dubbed", Formats: dubbedFormats()})
	for _, quality := range qualities {
		if quality.extension == ".webm" && quality.ytDlpAudioFormat != "251-1" {
			t.Errorf("%s uses audio %q, want 251-1", quality.label, quality.ytDlpAudioFormat)
		}
	}
}

func TestBuiltinHasOtherAudio(t *testing.T) {
	format := func(id string) youtube.Format {
		f := youtube.Format{MimeType: "audio/mp4"}
		if id != "" {
			f.AudioTrack = &struct {
				DisplayName    string `json:"displayName"`
				ID             string `json:"id"`
				AudioIsDefault bool   `json:"audioIsDefault"`
			}{ID: id}
		}
		return f
	}
	if !builtinHasOtherAudio(youtube.FormatList{format(""), format("en-US.4"), format("en-US.4"), format("ja.3")}) {
		t.Error("a dubbed video was not noticed")
	}
	if builtinHasOtherAudio(youtube.FormatList{format(""), format("en-US.4"), format("en-US.4")}) {
		t.Error("a video in one language was taken as dubbed")
	}
}

func TestDownloadWithYtDlpAudioTracks(t *testing.T) {
	audio := ytDlpAudio(dubbedFormats())
	japanese, _ := audio.find("ja.dubbed")
	original, _ := audio.find("en-US.original")
	spanish, _ := audio.find("es.dubbed-auto")
	requested := []requestedAudio{{Track: japanese, Default: true}, {Track: spanish}, {Track: original}}

	for _, test := range []struct {
		name       string
		quality    downloadQuality
		wantFormat []string
		wantNames  []string
		warning    string
	}{
		{
			name:       "MP4",
			quality:    downloadQuality{title: "dubbed", extension: ".mp4", ytDlpFormat: "18", videoSize: 9000},
			wantFormat: []string{"18", "140-0", "140-1"},
			wantNames:  []string{"日本語", "アメリカ英語（オリジナル）"},
			warning:    "スペイン語（自動吹き替え）の音声はこの画質の形式では取得できないため、入れませんでした。",
		},
		{
			name:       "WebM",
			quality:    downloadQuality{title: "dubbed", extension: ".webm", ytDlpFormat: "244", ytDlpAudioFormat: "251-1", videoSize: 8000, audioSize: 130000},
			wantFormat: []string{"244", "251-0", "251-2", "251-1"},
			wantNames:  []string{"日本語", "スペイン語（自動吹き替え）", "アメリカ英語（オリジナル）"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			runner := &fakeYtDlp{sizes: map[string]int{"18": 9000, "244": 8000, "140-0": 129000, "140-1": 129000, "251-0": 150000, "251-1": 130000, "251-2": 120000}}
			var streams []audioStream
			mux := func(_ context.Context, videoPath string, audio []audioStream, outputPath string) error {
				streams = audio
				for _, stream := range audio {
					if _, err := os.Stat(stream.Path); err != nil {
						t.Errorf("audio stream is missing: %v", err)
					}
				}
				return os.WriteFile(outputPath, []byte("muxed"), 0o600)
			}
			var last downloadProgress
			path, warning, err := downloadWithYtDlp(context.Background(), runner, directory, testVideoURL, test.quality, requested, nil, mux, func(progress downloadProgress) {
				if progress.total > 0 && progress.downloaded > progress.total {
					t.Errorf("progress %+v passes its total", progress)
				}
				last = progress
			})
			if err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "muxed" {
				t.Errorf("saved file = %q, %v; want the muxed output", data, err)
			}
			var formats []string
			for _, call := range runner.calls {
				formats = append(formats, call[slices.Index(call, "-f")+1])
			}
			if !slices.Equal(formats, test.wantFormat) {
				t.Errorf("formats = %q, want %q", formats, test.wantFormat)
			}
			var names []string
			for i, stream := range streams {
				names = append(names, stream.Name)
				if stream.Default != (i == 0) {
					t.Errorf("stream %d default = %t", i, stream.Default)
				}
			}
			if !slices.Equal(names, test.wantNames) {
				t.Errorf("streams = %q, want %q", names, test.wantNames)
			}
			if streams[0].Language != "ja" {
				t.Errorf("default stream language = %q", streams[0].Language)
			}
			if warning != test.warning {
				t.Errorf("warning = %q, want %q", warning, test.warning)
			}
			if last.downloaded != last.total || last.total == 0 {
				t.Errorf("last progress = %+v", last)
			}
			assertOnlyFiles(t, directory, "dubbed"+test.quality.extension)
		})
	}
}

func TestDownloadWithYtDlpDefaultAudioOnly(t *testing.T) {
	// Asking for nothing but the track YouTube plays by default keeps the
	// single-file MP4 as it is.
	original, _ := ytDlpAudio(dubbedFormats()).find("en-US.original")
	directory := t.TempDir()
	runner := &fakeYtDlp{sizes: map[string]int{"18": 9000}}
	quality := downloadQuality{title: "dubbed", extension: ".mp4", ytDlpFormat: "18", videoSize: 9000}
	_, _, err := downloadWithYtDlp(context.Background(), runner, directory, testVideoURL, quality, []requestedAudio{{Track: original, Default: true}}, nil, failingMux(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Errorf("yt-dlp ran %d times, want once", len(runner.calls))
	}
}

func TestDownloadWithYtDlpDubbedDefaultAudioInWebM(t *testing.T) {
	// YouTube may play a dub by default. A WebM quality's own audio stream
	// is the original, so the requested track is downloaded instead.
	formats := dubbedFormats()
	for i := range formats {
		switch {
		case formats[i].Language == "ja":
			formats[i].FormatNote = "Japanese (default), medium"
		case strings.HasPrefix(formats[i].Language, "en-US"):
			formats[i].FormatNote = strings.Replace(formats[i].FormatNote, " (default)", "", 1)
		}
	}
	audio := ytDlpAudio(formats)
	if got := audio.defaultID(); got != "ja.dubbed" {
		t.Fatalf("default = %q, want the dub YouTube plays", got)
	}
	japanese, _ := audio.find("ja.dubbed")
	qualities := ytDlpQualities(ytDlpInfo{Title: "dubbed", Formats: formats})
	var webm downloadQuality
	for _, quality := range qualities {
		if quality.extension == ".webm" {
			webm = quality
		}
	}
	if webm.ytDlpAudioFormat != "251-1" {
		t.Fatalf("WebM audio = %q, want the original", webm.ytDlpAudioFormat)
	}
	runner := &fakeYtDlp{sizes: map[string]int{"244": 8000, "251-0": 150000, "251-1": 130000}}
	var streams []audioStream
	mux := func(_ context.Context, _ string, audio []audioStream, outputPath string) error {
		streams = audio
		return os.WriteFile(outputPath, nil, 0o600)
	}
	_, _, err := downloadWithYtDlp(context.Background(), runner, t.TempDir(), testVideoURL, webm, []requestedAudio{{Track: japanese, Default: true}}, nil, mux, nil)
	if err != nil {
		t.Fatal(err)
	}
	var formatsRun []string
	for _, call := range runner.calls {
		formatsRun = append(formatsRun, call[slices.Index(call, "-f")+1])
	}
	if !slices.Equal(formatsRun, []string{"244", "251-0"}) {
		t.Errorf("formats = %q, want the video and the Japanese audio", formatsRun)
	}
	if len(streams) != 1 || streams[0].Language != "ja" || !streams[0].Default {
		t.Errorf("streams = %+v", streams)
	}
}

func TestDownloadWithYtDlpUnavailableDefaultAudio(t *testing.T) {
	// When the default track is not in the chosen container, the next
	// requested one plays.
	audio := ytDlpAudio(dubbedFormats())
	spanish, _ := audio.find("es.dubbed-auto")
	japanese, _ := audio.find("ja.dubbed")
	directory := t.TempDir()
	runner := &fakeYtDlp{sizes: map[string]int{"18": 9000, "140-0": 100}}
	quality := downloadQuality{title: "dubbed", extension: ".mp4", ytDlpFormat: "18"}
	var streams []audioStream
	mux := func(_ context.Context, _ string, audio []audioStream, outputPath string) error {
		streams = audio
		return os.WriteFile(outputPath, nil, 0o600)
	}
	_, warning, err := downloadWithYtDlp(context.Background(), runner, directory, testVideoURL, quality, []requestedAudio{{Track: spanish, Default: true}, {Track: japanese}}, nil, mux, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 1 || !streams[0].Default || streams[0].Language != "ja" {
		t.Errorf("streams = %+v, want Japanese as the default", streams)
	}
	if !strings.HasPrefix(warning, "スペイン語（自動吹き替え）の音声は") {
		t.Errorf("warning = %q", warning)
	}

	// With none of the requested tracks available, the single-file MP4 is
	// kept and the warning names them all.
	runner = &fakeYtDlp{sizes: map[string]int{"18": 9000}}
	_, warning, err = downloadWithYtDlp(context.Background(), runner, t.TempDir(), testVideoURL, quality, []requestedAudio{{Track: spanish, Default: true}}, nil, failingMux(t), nil)
	if err != nil || !strings.Contains(warning, "スペイン語") {
		t.Errorf("warning = %q, %v", warning, err)
	}
}

func TestDownloadWithYtDlpRetriesForbiddenStream(t *testing.T) {
	previous := ytDlpRetryDelay
	ytDlpRetryDelay = time.Millisecond
	defer func() { ytDlpRetryDelay = previous }()

	directory := t.TempDir()
	runner := &fakeYtDlp{sizes: map[string]int{"18": 2000}, forbidOnce: map[string]bool{"18": true}}
	quality := downloadQuality{title: "video", extension: ".mp4", ytDlpFormat: "18"}
	if _, _, err := downloadWithYtDlp(context.Background(), runner, directory, testVideoURL, quality, nil, nil, failingMux(t), nil); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Errorf("yt-dlp ran %d times, want twice", len(runner.calls))
	}

	// A second refusal is reported.
	runner = &fakeYtDlp{sizes: map[string]int{}, forbidOnce: map[string]bool{"18": true}}
	if _, _, err := downloadWithYtDlp(context.Background(), runner, t.TempDir(), testVideoURL, quality, nil, nil, failingMux(t), nil); err == nil {
		t.Error("the download succeeded without the stream")
	}
}

func TestOnlyYouTubeDefaultAudio(t *testing.T) {
	original := audioTrack{ID: "en.original", YouTubeDefault: true}
	dub := audioTrack{ID: "ja.dubbed"}
	for _, test := range []struct {
		requested []requestedAudio
		want      bool
	}{
		{nil, true},
		{[]requestedAudio{{Track: original, Default: true}}, true},
		{[]requestedAudio{{Track: dub, Default: true}}, false},
		{[]requestedAudio{{Track: original, Default: true}, {Track: dub}}, false},
	} {
		if got := onlyYouTubeDefaultAudio(test.requested); got != test.want {
			t.Errorf("onlyYouTubeDefaultAudio(%+v) = %t", test.requested, got)
		}
	}
	if got := joinWarnings("", "a", "", "b"); got != "a\nb" {
		t.Errorf("joinWarnings = %q", got)
	}
}
