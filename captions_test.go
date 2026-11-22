package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kkdai/youtube/v2"
)

func ms(n int64) time.Duration { return time.Duration(n) * time.Millisecond }

const uploaderVTT = "\ufeffWEBVTT\nKind: captions\nLanguage: ja\n\nNOTE a comment\nthat spans lines\n\n1\n00:00:01.000 --> 00:00:03.500 align:middle\n一行目 &amp; <i>強調</i>\n二行目\n\n00:03.500 --> 00:05.000\n次の字幕\n\n"

// A shortened example in the format of YouTube's automatic captions: each
// cue repeats the line before and adds a new line with word timing, and a
// 10 ms cue repeats the finished line with a single space below it.
const autoVTT = `WEBVTT
Kind: captions
Language: ja

00:00:00.400 --> 00:00:02.000 align:start position:0%
 
みな<00:00:00.900><c>さん</c><00:00:01.200><c>こんにちは</c>

00:00:02.000 --> 00:00:02.010 align:start position:0%
みなさんこんにちは
 

00:00:02.010 --> 00:00:04.000 align:start position:0%
みなさんこんにちは
今日<00:00:02.500><c>は</c><00:00:03.000><c>晴れ</c>

00:00:04.000 --> 00:00:04.010 align:start position:0%
今日は晴れ
 

00:00:04.010 --> 00:00:06.000 align:start position:0%
今日は晴れ
では<00:00:05.000><c>また</c>
`

func TestParseWebVTT(t *testing.T) {
	cues, err := parseWebVTT(strings.NewReader(uploaderVTT))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("cues = %+v, want 2", cues)
	}
	if cues[0].Start != ms(1000) || cues[0].End != ms(3500) || !slices.Equal(cues[0].Lines, []string{"一行目 &amp; <i>強調</i>", "二行目"}) {
		t.Errorf("first cue = %+v", cues[0])
	}
	if cues[1].Start != ms(3500) || cues[1].End != ms(5000) {
		t.Errorf("short timestamps were not read: %+v", cues[1])
	}
	plain := plainCues(cues)
	if plain[0].Text != "一行目 & 強調\n二行目" {
		t.Errorf("plain text = %q", plain[0].Text)
	}

	if _, err := parseWebVTT(strings.NewReader("1\n00:00:01,000 --> 00:00:02,000\nSRT\n")); err == nil {
		t.Error("an SRT file was read as WebVTT")
	}
}

func TestYouTubeCaptionCuesCleansAutomaticCaptions(t *testing.T) {
	cues, err := youTubeCaptionCues([]byte(autoVTT), true)
	if err != nil {
		t.Fatal(err)
	}
	want := []subtitleCue{
		{Start: ms(400), End: ms(2000), Text: "みなさんこんにちは"},
		{Start: ms(2010), End: ms(4000), Text: "今日は晴れ"},
		{Start: ms(4010), End: ms(6000), Text: "ではまた"},
	}
	if !slices.Equal(cues, want) {
		t.Errorf("cues = %+v\nwant %+v", cues, want)
	}

	// Uploader captions are taken as written, even when marked automatic
	// but without word timing.
	cues, err = youTubeCaptionCues([]byte(uploaderVTT), true)
	if err != nil || len(cues) != 2 || cues[0].Text != "一行目 & 強調\n二行目" {
		t.Errorf("cues = %+v, %v", cues, err)
	}
}

func TestTidyCues(t *testing.T) {
	cues := []subtitleCue{
		{Start: ms(5000), End: ms(9000), Text: "c"},
		{Start: ms(0), End: ms(3000), Text: "a"},
		{Start: ms(2000), End: ms(4000), Text: "b"},
		{Start: ms(4500), End: ms(4500), Text: "empty"},
		{Start: ms(6000), End: ms(7000), Text: "  "},
		{Start: ms(12000), End: ms(13000), Text: "after the end"},
	}
	got := tidyCues(cues, 10*time.Second)
	want := []subtitleCue{
		{Start: ms(0), End: ms(2000), Text: "a"},
		{Start: ms(2000), End: ms(4000), Text: "b"},
		{Start: ms(5000), End: ms(6000), Text: "c"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("tidyCues = %+v\nwant %+v", got, want)
	}
	clamped := tidyCues([]subtitleCue{{Start: ms(8000), End: ms(15000), Text: "long"}}, 10*time.Second)
	if len(clamped) != 1 || clamped[0].End != 10*time.Second {
		t.Errorf("cue was not kept within the video: %+v", clamped)
	}
}

func TestWriteSRT(t *testing.T) {
	var out bytes.Buffer
	err := writeSRT(&out, []subtitleCue{
		{Start: ms(1500), End: ms(3_723_004), Text: "<記号> & 二行\n目"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "1\n00:00:01,500 --> 01:02:03,004\n＜記号＞ & 二行\n目\n\n"
	if out.String() != want {
		t.Errorf("SRT = %q, want %q", out.String(), want)
	}
}

func captionIDs(captions videoCaptions) []string {
	var ids []string
	for _, caption := range captions.Tracks {
		ids = append(ids, caption.ID)
	}
	return ids
}

func TestBuiltinCaptions(t *testing.T) {
	track := func(vss, code, kind string) youtube.CaptionTrack {
		return youtube.CaptionTrack{BaseURL: "https://example.invalid/timedtext", VssID: vss, LanguageCode: code, Kind: kind}
	}
	captions := builtinCaptions([]youtube.CaptionTrack{
		track(".en", "en", ""), track("a.en", "en", "asr"), track("a.fr", "fr", "asr"), track(".ja", "ja", ""), track(".iw", "iw", ""), {VssID: ".xx"},
	})
	// Japanese first, then by name; the automatic track is a candidate
	// too, but only the first one listed.
	if got, want := captionIDs(captions), []string{".ja", ".iw", ".en", "a.en"}; !slices.Equal(got, want) {
		t.Errorf("tracks = %q, want %q", got, want)
	}
	if track, ok := captions.find(".iw"); !ok || track.Language != "he" || track.label() != "ヘブライ語" {
		t.Errorf("iw track = %+v, %t", track, ok)
	}
	if track, ok := captions.find("a.en"); !ok || !track.Auto || track.label() != "英語（自動生成）" {
		t.Errorf("automatic track = %+v, %t", track, ok)
	}
	if none := builtinCaptions(nil); none.any() {
		t.Errorf("captions for a video without tracks = %+v", none)
	}
}

func TestYtDlpCaptions(t *testing.T) {
	vtt := []ytDlpSubtitle{{Ext: "json3"}, {Ext: "vtt"}}
	hlsOnly := []ytDlpSubtitle{{Ext: "vtt", Protocol: "m3u8_native"}}
	captions := ytDlpCaptions(
		map[string][]ytDlpSubtitle{"ja": vtt, "en": vtt, "live_chat": {{Ext: "json", Protocol: "youtube_live_chat_replay"}}, "de": hlsOnly},
		map[string][]ytDlpSubtitle{"en-orig": vtt, "ja": vtt, "fr": vtt},
	)
	// Uploader tracks plus the original automatic track; translated
	// automatic tracks, chat replays and HLS-only entries are left out.
	if got, want := captionIDs(captions), []string{"ja", "en", "en-orig"}; !slices.Equal(got, want) {
		t.Errorf("tracks = %q, want %q", got, want)
	}
	if track, _ := captions.find("en-orig"); !track.Auto || track.Language != "en" {
		t.Errorf("automatic track = %+v", track)
	}
	// Automatic entries served only over HLS mirror uploader captions.
	if mirrored := ytDlpCaptions(nil, map[string][]ytDlpSubtitle{"en": hlsOnly}); mirrored.any() {
		t.Errorf("HLS-only automatic entry was offered: %+v", mirrored)
	}
}

// A second track of a language has its language and name only in its URL,
// as a video with "English" and "English (Plain Text)" tracks lists them.
func TestNamedCaptionTracks(t *testing.T) {
	const base = "https://www.youtube.com/api/timedtext?v=x"
	builtin := builtinCaptions([]youtube.CaptionTrack{
		{VssID: ".en", LanguageCode: "en", BaseURL: base + "&lang=en"},
		{VssID: ".en.t3oy0ra6tek", LanguageCode: "en", BaseURL: base + "&lang=en&name=English+(Plain+Text)"},
		{VssID: ".fr.N9WPsFwite0", LanguageCode: "fr", BaseURL: base + "&lang=fr&name=(Texte+Plein)"},
		{VssID: ".ko.nCmV_HIkQHQ", LanguageCode: "ko", BaseURL: base + "&lang=ko&name=%EC%A0%9C%EC%A3%BC%EC%96%B4"},
	})
	vtt := func(query string) []ytDlpSubtitle {
		return []ytDlpSubtitle{{Ext: "vtt", URL: base + query}}
	}
	ytDlp := ytDlpCaptions(map[string][]ytDlpSubtitle{
		"en":             vtt("&lang=en"),
		"en-t3oy0ra6tek": vtt("&lang=en&name=English+(Plain+Text)"),
		"fr-N9WPsFwite0": vtt("&lang=fr&name=(Texte+Plein)"),
		"ko-nCmV_HIkQHQ": vtt("&lang=ko&name=%EC%A0%9C%EC%A3%BC%EC%96%B4"),
	}, nil)
	want := []string{"フランス語（Texte Plein）", "英語", "英語（English (Plain Text)）", "韓国語（제주어）"}
	for _, captions := range []videoCaptions{builtin, ytDlp} {
		var labels []string
		for _, track := range captions.Tracks {
			labels = append(labels, track.label())
			if strings.Contains(track.Language, "-") {
				t.Errorf("language of %q = %q", track.ID, track.Language)
			}
		}
		if !slices.Equal(labels, want) {
			t.Errorf("labels = %q, want %q", labels, want)
		}
	}

	// Without URLs, the language still comes from the key, and tracks
	// of the same language are numbered.
	bare := ytDlpCaptions(map[string][]ytDlpSubtitle{
		"en":             {{Ext: "vtt"}},
		"en-t3oy0ra6tek": {{Ext: "vtt"}},
		"en-nP7-2PuUl7o": {{Ext: "vtt"}},
		"pt-BR":          {{Ext: "vtt"}},
	}, nil)
	var labels []string
	for _, track := range bare.Tracks {
		labels = append(labels, track.label())
	}
	if want := []string{"ポルトガル語 (ブラジル)", "英語", "英語（2）", "英語（3）"}; !slices.Equal(labels, want) {
		t.Errorf("labels without URLs = %q, want %q", labels, want)
	}
}

func TestCaptionLanguages(t *testing.T) {
	for code, want := range map[string]string{"ja-orig": "ja", "iw": "he", "in": "id", "zh-Hans": "zh-Hans", "pt-BR": "pt-BR"} {
		if got := youTubeLanguageTag(code); got != want {
			t.Errorf("youTubeLanguageTag(%q) = %q, want %q", code, got, want)
		}
	}
	for tag, want := range map[string]string{"ja": "jpn", "en-US": "eng", "de": "ger", "zh-Hans": "chi", "fr": "fre", "not a tag": "und"} {
		if got := matroskaLanguage(tag); got != want {
			t.Errorf("matroskaLanguage(%q) = %q, want %q", tag, got, want)
		}
	}
	if got := subtitleLanguageName("ja"); got != "日本語" {
		t.Errorf("subtitleLanguageName(ja) = %q", got)
	}
}

func TestYtDlpSubtitleArgs(t *testing.T) {
	uploader := ytDlpSubtitleArgs([]captionTrack{{ID: "zh-Hans", Language: "zh-Hans"}, {ID: "en", Language: "en"}}, "/tmp/work/captions", testVideoURL)
	assertYtDlpArgs(t, uploader, testVideoURL)
	for _, want := range [][2]string{{"--sub-langs", `^(zh-Hans|en)$`}, {"--sub-format", "vtt"}, {"-o", "/tmp/work/captions"}} {
		if index := slices.Index(uploader, want[0]); index < 0 || uploader[index+1] != want[1] {
			t.Errorf("args = %q, want %s %s", uploader, want[0], want[1])
		}
	}
	if !slices.Contains(uploader, "--skip-download") || !slices.Contains(uploader, "--write-subs") || slices.Contains(uploader, "--write-auto-subs") {
		t.Errorf("uploader args = %q", uploader)
	}
	both := ytDlpSubtitleArgs([]captionTrack{{ID: "ja", Language: "ja"}, {ID: "ja-orig", Language: "ja", Auto: true}}, "/tmp/work/captions", testVideoURL)
	assertYtDlpArgs(t, both, testVideoURL)
	if !slices.Contains(both, "--write-subs") || !slices.Contains(both, "--write-auto-subs") {
		t.Errorf("args for uploader and automatic tracks = %q", both)
	}
	if index := slices.Index(both, "--sub-langs"); both[index+1] != `^(ja|ja-orig)$` {
		t.Errorf("--sub-langs = %q", both[index+1])
	}
}

func TestCaptionWarning(t *testing.T) {
	if got := captionWarning(nil, errors.New("HTTP 403")); got != "字幕を追加できなかったため、字幕なしで保存しました（HTTP 403）。" {
		t.Errorf("warning = %q", got)
	}
	if got := captionWarning([]string{"英語", "ドイツ語"}, errors.New("空")); got != "英語、ドイツ語の字幕を追加できませんでした（空）。" {
		t.Errorf("warning = %q", got)
	}
}

func TestDownloadWithYtDlpKeepsVideoWhenCaptionsFail(t *testing.T) {
	captions := []requestedCaption{{Track: captionTrack{ID: "ja", Language: "ja"}, Default: true}, {Track: captionTrack{ID: "en", Language: "en"}}}
	for name, runner := range map[string]*fakeYtDlp{
		"tracks missing": {sizes: map[string]int{"18": 2000}},
		// The fake media cannot be read, so embedding fails after fetching.
		"embedding fails": {sizes: map[string]int{"18": 2000}, captions: []byte(uploaderVTT)},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			quality := downloadQuality{title: "video", extension: ".mp4", ytDlpFormat: "18", videoSize: 2000}
			path, warning, err := downloadWithYtDlp(context.Background(), runner, directory, testVideoURL, quality, captions, failingMux(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(warning, "字幕なしで保存しました") {
				t.Errorf("warning = %q", warning)
			}
			if info, err := os.Stat(path); err != nil || info.Size() != 2000 {
				t.Errorf("saved video: %v, %v", info, err)
			}
			assertOnlyFiles(t, directory, filepath.Base(path))
			// One run for the video and one for all the captions.
			if len(runner.calls) != 2 || !slices.Contains(runner.calls[1], "--skip-download") {
				t.Errorf("yt-dlp calls = %q, want the video and then the captions", runner.calls)
			}
		})
	}
}
