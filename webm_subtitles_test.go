package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aethiopicuschan/video2dvd"
	"github.com/gravity-zero/mkvgo/matroska"
	"github.com/gravity-zero/mkvgo/mkv"
)

// A WebM subtitle track marked as default says so with FlagDefault 1, so
// that video2dvd shows it from the start even when it is the only one.
func TestEmbedWebMSubtitlesWritesDefaultFlag(t *testing.T) {
	cues := []subtitleCue{{Start: ms(100), End: ms(900), Text: "字幕"}}
	for _, test := range []struct {
		name   string
		tracks []subtitleTrack
	}{
		{"one default", []subtitleTrack{{Language: "ja", Name: "日本語", Default: true, Cues: cues}}},
		{"default and other", []subtitleTrack{
			{Language: "ja", Name: "日本語", Default: true, Cues: cues},
			{Language: "en", Name: "英語", Cues: cues},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", "short.webm"))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			srcPath := filepath.Join(dir, "in.webm")
			if err := os.WriteFile(srcPath, src, 0o600); err != nil {
				t.Fatal(err)
			}
			dstPath := filepath.Join(dir, "out.webm")
			if err := embedSubtitles(context.Background(), srcPath, dstPath, test.tracks); err != nil {
				t.Fatal(err)
			}

			container, err := matroska.OpenMeta(context.Background(), dstPath)
			if err != nil {
				t.Fatal(err)
			}
			var subtitles []mkv.Track
			for _, track := range container.Tracks {
				if track.Type == mkv.SubtitleTrack {
					subtitles = append(subtitles, track)
				}
			}
			if len(subtitles) != len(test.tracks) {
				t.Fatalf("%d subtitle tracks, want %d", len(subtitles), len(test.tracks))
			}
			for i, track := range subtitles {
				if !track.DefaultPresent || track.IsDefault != test.tracks[i].Default {
					t.Errorf("track %d: FlagDefault present %t, default %t; want present, default %t", i, track.DefaultPresent, track.IsDefault, test.tracks[i].Default)
				}
			}

			// The marks as video2dvd counts them.
			media, err := readVideoDetails(dstPath)
			if err != nil {
				t.Fatal(err)
			}
			i := 0
			for _, track := range media.Tracks {
				if track.Kind != video2dvd.TrackKindSubtitle {
					continue
				}
				if track.Default != test.tracks[i].Default {
					t.Errorf("track %d: video2dvd default %t, want %t", i, track.Default, test.tracks[i].Default)
				}
				i++
			}
		})
	}
}
