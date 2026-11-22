package main

import (
	"testing"
	"time"

	"github.com/aethiopicuschan/video2dvd"
)

func TestDVDAudioStreams(t *testing.T) {
	audio := func(codec string, channels int) video2dvd.TrackInfo {
		return video2dvd.TrackInfo{Kind: video2dvd.TrackKindAudio, Codec: codec, Channels: channels}
	}
	video := video2dvd.TrackInfo{Kind: video2dvd.TrackKindVideo, Codec: "avc1"}
	for _, test := range []struct {
		tracks []video2dvd.TrackInfo
		want   int
	}{
		{nil, 0},
		{[]video2dvd.TrackInfo{video, audio("mp4a", 2)}, 1},
		{[]video2dvd.TrackInfo{video, audio("mp4a", 2), audio("mp4a", 2), {Kind: video2dvd.TrackKindSubtitle, Codec: "tx3g"}}, 2},
		// Multichannel AAC is skipped; AC-3 and Opus are downmixed.
		{[]video2dvd.TrackInfo{video, audio("mp4a", 6), audio("ac-3", 6), audio("opus", 6)}, 2},
		{[]video2dvd.TrackInfo{video, audio("mp3", 2)}, 0},
		{[]video2dvd.TrackInfo{video, audio("opus", 2), audio("opus", 2), audio("opus", 2), audio("opus", 2), audio("opus", 2), audio("opus", 2), audio("opus", 2), audio("opus", 2), audio("opus", 2)}, 8},
	} {
		if got := dvdAudioStreams(video2dvd.MP4Info{Tracks: test.tracks}); got != test.want {
			t.Errorf("dvdAudioStreams(%d tracks) = %d, want %d", len(test.tracks), got, test.want)
		}
	}
}

func TestDVDISOSizeEstimate(t *testing.T) {
	files := []dvdVideoFile{
		{path: "/videos/a.mp4", duration: 25*time.Minute + 39*time.Second, audioStreams: 2},
		{path: "/videos/b.webm", duration: 3 * time.Minute, audioStreams: 1},
		{path: "/videos/c.mp4"}, // not probed yet
	}
	got, ok := dvdISOSizeEstimate([]string{"/videos/a.mp4", "/videos/b.webm", "/videos/b.webm"}, files)
	want, err := video2dvd.EstimateDVDBytes([]video2dvd.TitleEstimate{
		{Duration: files[0].duration, AudioStreams: 2},
		{Duration: files[1].duration, AudioStreams: 1},
		{Duration: files[1].duration, AudioStreams: 1},
	}, dvdExportOptions(""))
	if err != nil || !ok || got != want {
		t.Errorf("estimate = %d, %t; want video2dvd's %d (%v)", got, ok, want, err)
	}
	if _, ok := dvdISOSizeEstimate([]string{"/videos/a.mp4", "/videos/c.mp4"}, files); ok {
		t.Error("a video of unknown length was estimated")
	}
	if _, ok := dvdISOSizeEstimate(nil, files); ok {
		t.Error("an empty queue was estimated")
	}
}
