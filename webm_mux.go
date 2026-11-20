package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gravity-zero/mkvgo/matroska"
	"github.com/gravity-zero/mkvgo/mkv"
)

// muxDownloadedStreams combines a WebM video stream and a WebM audio stream
// that were downloaded separately into one WebM file.
func muxDownloadedStreams(ctx context.Context, videoPath, audioPath, outputPath, extension, videoMIME string) error {
	if extension != ".webm" || !strings.Contains(videoMIME, "video/webm") {
		return fmt.Errorf("WebM以外のストリームです")
	}
	videoMeta, err := matroska.OpenMeta(ctx, videoPath)
	if err != nil {
		return fmt.Errorf("WebM映像ストリームを読み取れません: %w", err)
	}
	audioMeta, err := matroska.OpenMeta(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("WebM音声ストリームを読み取れません: %w", err)
	}
	videoID, err := firstTrackID(videoMeta.Tracks, mkv.VideoTrack)
	if err != nil {
		return err
	}
	audioID, err := firstTrackID(audioMeta.Tracks, mkv.AudioTrack)
	if err != nil {
		return err
	}
	mkvPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".mux.mkv"
	defer os.Remove(mkvPath)
	if err := matroska.Mux(ctx, mkv.MuxOptions{OutputPath: mkvPath, Tracks: []mkv.TrackInput{
		{SourcePath: videoPath, TrackID: videoID, IsDefault: true},
		{SourcePath: audioPath, TrackID: audioID, IsDefault: true},
	}}); err != nil {
		return err
	}
	return matroska.RemuxToWebM(ctx, mkvPath, outputPath)
}

func firstTrackID(tracks []mkv.Track, trackType mkv.TrackType) (uint64, error) {
	for _, track := range tracks {
		if track.Type == trackType {
			return track.ID, nil
		}
	}
	return 0, fmt.Errorf("結合する%sトラックがありません", trackType)
}
