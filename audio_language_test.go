package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gravity-zero/mkvgo/mkv"
	"github.com/gravity-zero/mkvgo/mp4"
	"github.com/kkdai/youtube/v2"
)

func TestBuiltinAudioLanguage(t *testing.T) {
	withTrack := func(id string, isDefault bool) youtube.Format {
		f := youtube.Format{ItagNo: 140}
		f.AudioTrack = &struct {
			DisplayName    string `json:"displayName"`
			ID             string `json:"id"`
			AudioIsDefault bool   `json:"audioIsDefault"`
		}{ID: id, AudioIsDefault: isDefault}
		return f
	}
	asr := youtube.CaptionTrack{VssID: "a.ja", LanguageCode: "ja", Kind: "asr"}
	uploaded := youtube.CaptionTrack{VssID: ".en", LanguageCode: "en"}
	for _, test := range []struct {
		name  string
		video youtube.Video
		want  string
	}{
		{"dubbed", youtube.Video{Formats: youtube.FormatList{{ItagNo: 18}, withTrack("ja.3", false), withTrack("en-US.4", true)}, CaptionTracks: []youtube.CaptionTrack{asr}}, "en-US"},
		{"automatic captions", youtube.Video{Formats: youtube.FormatList{{ItagNo: 18}}, CaptionTracks: []youtube.CaptionTrack{uploaded, asr}}, "ja"},
		{"obsolete code", youtube.Video{CaptionTracks: []youtube.CaptionTrack{{LanguageCode: "iw", Kind: "asr"}}}, "he"},
		{"unknown", youtube.Video{Formats: youtube.FormatList{{ItagNo: 18}}, CaptionTracks: []youtube.CaptionTrack{uploaded}}, ""},
	} {
		if got := builtinAudioLanguage(&test.video); got != test.want {
			t.Errorf("%s: %q, want %q", test.name, got, test.want)
		}
	}
}

func TestYtDlpAudioLanguage(t *testing.T) {
	info := ytDlpInfo{Language: "ja"}
	for _, test := range []struct {
		format ytDlpFormatInfo
		info   ytDlpInfo
		want   string
	}{
		{ytDlpFormatInfo{Language: "en-US"}, info, "en-US"},
		{ytDlpFormatInfo{}, info, "ja"},
		{ytDlpFormatInfo{Language: "en-desc"}, info, "en"},
		{ytDlpFormatInfo{}, ytDlpInfo{}, ""},
	} {
		if got := ytDlpAudioLanguage(test.format, test.info); got != test.want {
			t.Errorf("ytDlpAudioLanguage(%+v, %+v) = %q, want %q", test.format, test.info, got, test.want)
		}
	}
	// Each quality remembers the language of the audio it brings.
	qualities := ytDlpQualities(ytDlpInfo{Title: "dubbed", Language: "en", Formats: dubbedFormats()})
	for _, quality := range qualities {
		want := "en" // the single-file MP4 says "en"
		if quality.extension == ".webm" {
			want = "en-US" // the original WebM audio says "en-US"
		}
		if quality.audioLanguage != want {
			t.Errorf("%s: audio language %q, want %q", quality.label, quality.audioLanguage, want)
		}
	}
}

func TestSetMP4AudioLanguage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "no_language.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	languages := func() map[mkv.TrackType][2]string {
		c, _, err := mp4.OpenMeta(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		got := map[mkv.TrackType][2]string{}
		for _, track := range c.Tracks {
			got[track.Type] = [2]string{track.Language, track.LanguageBCP47}
		}
		return got
	}

	if err := setMP4AudioLanguage(context.Background(), path, ""); err != nil {
		t.Fatal(err)
	}
	if got := languages()[mkv.AudioTrack]; got[0] != "" && got[0] != "und" {
		t.Errorf("an empty language set the audio to %q", got)
	}

	if err := setMP4AudioLanguage(context.Background(), path, "en-US"); err != nil {
		t.Fatal(err)
	}
	got := languages()
	if audio := got[mkv.AudioTrack]; audio[0] != "eng" || audio[1] != "en-US" {
		t.Errorf("audio language = %q, want eng and en-US", audio)
	}
	if video := got[mkv.VideoTrack]; video[0] != "" && video[0] != "und" {
		t.Errorf("the video track became %q", video)
	}

	// What the file says is replaced: YouTube's single-file MP4 says
	// English for audio in any language.
	if err := setMP4AudioLanguage(context.Background(), path, "ja"); err != nil {
		t.Fatal(err)
	}
	if audio := languages()[mkv.AudioTrack]; audio[0] != "jpn" || audio[1] != "ja" {
		t.Errorf("audio language = %q, want jpn and ja", audio)
	}
}
