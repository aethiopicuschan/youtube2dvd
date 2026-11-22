package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aethiopicuschan/video2dvd"
	"github.com/gravity-zero/mkvgo/mkv"
)

func TestTrackTitle(t *testing.T) {
	for _, test := range []struct {
		track video2dvd.TrackInfo
		want  string
	}{
		{video2dvd.TrackInfo{Language: "ja", Default: true}, "日本語（規定）"},
		{video2dvd.TrackInfo{Language: "jpn"}, "日本語"},
		{video2dvd.TrackInfo{Language: "und"}, "言語不明"},
		{video2dvd.TrackInfo{}, "言語不明"},
		// The app names tracks after their language.
		{video2dvd.TrackInfo{Language: "en-US", Name: "アメリカ英語（オリジナル）"}, "アメリカ英語（オリジナル）"},
		{video2dvd.TrackInfo{Language: "en", Name: "コメンタリー"}, "英語「コメンタリー」"},
		// Names a muxer writes of its own accord say nothing.
		{video2dvd.TrackInfo{Language: "en", Name: "ISO Media file produced by Google Inc. Created on: 12/28/2025."}, "英語"},
		{video2dvd.TrackInfo{Language: "en", Name: "SoundHandler"}, "英語"},
		{video2dvd.TrackInfo{Language: "en", Name: "Core Media Audio", Default: true}, "英語（規定）"},
	} {
		if got := trackTitle(test.track); got != test.want {
			t.Errorf("trackTitle(%+v) = %q, want %q", test.track, got, test.want)
		}
	}
}

func TestFormatPlayTime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                                      "0:00",
		212*time.Second + 834*time.Millisecond: "3:33",
		25*time.Minute + 41*time.Second:        "25:41",
		time.Hour + 2*time.Minute + 3*time.Second: "1:02:03",
	} {
		if got := formatPlayTime(d); got != want {
			t.Errorf("formatPlayTime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestVideoDetailsText(t *testing.T) {
	media := video2dvd.MP4Info{
		Duration: 25*time.Minute + 41*time.Second,
		Tracks: []video2dvd.TrackInfo{
			{Kind: video2dvd.TrackKindVideo, Codec: "vp9", Width: 256, Height: 144, DefaultDuration: time.Second * 1001 / 24000},
			{Kind: video2dvd.TrackKindAudio, Codec: "opus", Channels: 2, SampleRate: 48000, Language: "ja", Name: "日本語", Default: true},
			{Kind: video2dvd.TrackKindAudio, Codec: "mp4a", Channels: 6, SampleRate: 44100, Language: "en-US", Name: "アメリカ英語（オリジナル）"},
			{Kind: video2dvd.TrackKindSubtitle, Codec: "D_WEBVTT/SUBTITLES", Language: "ja", Name: "日本語", Default: true},
		},
	}
	want := `再生時間：25:41
映像：VP9　256×144　23.98 fps

音声（2）
　1. 日本語（規定）　Opus　ステレオ　48 kHz
　2. アメリカ英語（オリジナル）　AAC　5.1ch　44.1 kHz

字幕（1）
　1. 日本語（規定）　WebVTT`
	if got := videoDetailsText(media); got != want {
		t.Errorf("videoDetailsText =\n%s\nwant\n%s", got, want)
	}

	// Without tracks of a kind, the list says so.
	got := videoDetailsText(video2dvd.MP4Info{Tracks: []video2dvd.TrackInfo{{Kind: video2dvd.TrackKindVideo, Codec: "avc1", Width: 640, Height: 360, Duration: time.Minute}}})
	if !strings.Contains(got, "再生時間：1:00") || !strings.Contains(got, "音声（0）\n　なし") || !strings.Contains(got, "字幕（0）\n　なし") {
		t.Errorf("video alone =\n%s", got)
	}
}

func TestApplyWebMDefaultMarks(t *testing.T) {
	track := func(kind video2dvd.TrackKind) video2dvd.TrackInfo {
		return video2dvd.TrackInfo{Kind: kind, Default: true}
	}
	implied := func(typ mkv.TrackType) mkv.Track { return mkv.Track{Type: typ, IsDefault: true} }
	written := func(typ mkv.TrackType, isDefault bool) mkv.Track {
		return mkv.Track{Type: typ, IsDefault: isDefault, DefaultPresent: true}
	}
	media := video2dvd.MP4Info{Tracks: []video2dvd.TrackInfo{
		track(video2dvd.TrackKindVideo),
		track(video2dvd.TrackKindAudio), {Kind: video2dvd.TrackKindAudio},
		track(video2dvd.TrackKindSubtitle),
	}}
	applyWebMDefaultMarks(&media, []mkv.Track{
		implied(mkv.VideoTrack),
		// The app writes the default audio with no element, the others 0.
		implied(mkv.AudioTrack), written(mkv.AudioTrack, false),
		// A lone subtitle track without the element.
		implied(mkv.SubtitleTrack),
	})
	got := []bool{media.Tracks[0].Default, media.Tracks[1].Default, media.Tracks[2].Default, media.Tracks[3].Default}
	// The implied marks count only beside an explicit 0, as in video2dvd.
	if want := []bool{false, true, false, false}; !slices.Equal(got, want) {
		t.Errorf("defaults = %v, want %v", got, want)
	}

	// A written FlagDefault 1 always counts.
	media = video2dvd.MP4Info{Tracks: []video2dvd.TrackInfo{track(video2dvd.TrackKindSubtitle)}}
	applyWebMDefaultMarks(&media, []mkv.Track{written(mkv.SubtitleTrack, true)})
	if !media.Tracks[0].Default {
		t.Error("a written default mark was dropped")
	}
}

func TestVideoDetailsCacheReadsOnceAtATime(t *testing.T) {
	var cache videoDetailsCache
	if text, ok := cache.describe(filepath.Join(t.TempDir(), "old.mkv")); ok || !strings.Contains(text, "表示できません") {
		t.Errorf("MKV: %q, %t", text, ok)
	}
	if text, ok := cache.describe(filepath.Join(t.TempDir(), "gone.mp4")); ok || !strings.Contains(text, "確認できません") {
		t.Errorf("missing file: %q, %t", text, ok)
	}

	path := filepath.Join(t.TempDir(), "broken.mp4")
	if err := os.WriteFile(path, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if text, ok := cache.describe(path); ok || text != "読み込んでいます…" {
		t.Errorf("first look: %q, %t", text, ok)
	}
	// While the read runs, looking again starts no other.
	cache.mu.Lock()
	reading := cache.details[path].reading
	cache.mu.Unlock()
	if !reading {
		t.Fatal("no read was started")
	}
	cache.describe(path)
	deadline := time.Now().Add(10 * time.Second)
	for !cache.changed.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the read did not end")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if text, ok := cache.describe(path); ok || !strings.Contains(text, "読み取れませんでした") {
		t.Errorf("after reading: %q, %t", text, ok)
	}

	// A failed read is tried again after a while, though the file is the same.
	cache.mu.Lock()
	failed := cache.details[path]
	failed.failedAt = failed.failedAt.Add(-videoDetailsRetryAfter)
	cache.details[path] = failed
	cache.mu.Unlock()
	if text, ok := cache.describe(path); ok || text != "読み込んでいます…" {
		t.Errorf("after the failure aged: %q, %t", text, ok)
	}
}
