package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gravity-zero/mkvgo/matroska"
	"github.com/gravity-zero/mkvgo/mkv"
	"github.com/gravity-zero/mkvgo/mp4"
)

// muxerFor returns the muxer for files with extension.
func muxerFor(extension string) streamMuxer {
	if extension == ".webm" {
		return muxWebMStreams
	}
	return muxMP4Streams
}

// muxWebMStreams combines the picture of a WebM video stream with WebM audio
// streams that were downloaded separately into one WebM file.
func muxWebMStreams(ctx context.Context, videoPath string, audio []audioStream, outputPath string) error {
	if len(audio) == 0 {
		return fmt.Errorf("結合する音声がありません")
	}
	videoMeta, err := matroska.OpenMeta(ctx, videoPath)
	if err != nil {
		return fmt.Errorf("WebM映像ストリームを読み取れません: %w", err)
	}
	videoID, err := firstTrackID(videoMeta.Tracks, mkv.VideoTrack)
	if err != nil {
		return err
	}
	tracks := []mkv.TrackInput{{SourcePath: videoPath, TrackID: videoID, IsDefault: true}}
	for _, stream := range audio {
		audioMeta, err := matroska.OpenMeta(ctx, stream.Path)
		if err != nil {
			return fmt.Errorf("WebM音声ストリームを読み取れません: %w", err)
		}
		audioID, err := firstTrackID(audioMeta.Tracks, mkv.AudioTrack)
		if err != nil {
			return err
		}
		tracks = append(tracks, mkv.TrackInput{SourcePath: stream.Path, TrackID: audioID, Language: stream.Language, Name: stream.Name, IsDefault: stream.Default})
	}
	mkvPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".mux.mkv"
	defer os.Remove(mkvPath)
	if err := matroska.Mux(ctx, mkv.MuxOptions{OutputPath: mkvPath, Tracks: tracks}); err != nil {
		return err
	}
	return matroska.RemuxToWebM(ctx, mkvPath, outputPath)
}

// muxMP4Streams combines the picture of an MP4 video, which may carry audio
// of its own, with MP4 audio streams that were downloaded separately into
// one MP4 file. YouTube serves such audio as fragmented MP4, which is first
// rewritten as a plain one.
func muxMP4Streams(ctx context.Context, videoPath string, audio []audioStream, outputPath string) error {
	if len(audio) == 0 {
		return fmt.Errorf("結合する音声がありません")
	}
	videoMeta, _, err := mp4.OpenMeta(ctx, videoPath)
	if err != nil {
		return fmt.Errorf("MP4映像を読み取れません: %w", err)
	}
	videoID, err := firstTrackID(videoMeta.Tracks, mkv.VideoTrack)
	if err != nil {
		return err
	}
	tracks := []mkv.TrackInput{{SourcePath: videoPath, TrackID: videoID, IsDefault: true}}
	for i, stream := range audio {
		base := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + fmt.Sprintf(".audio-%d", i+1)
		mkvPath, plainPath := base+".mkv", base+".mp4"
		defer os.Remove(mkvPath)
		defer os.Remove(plainPath)
		if err := mp4.RemuxFromMP4(ctx, stream.Path, mkvPath); err != nil {
			return fmt.Errorf("MP4音声を読み取れません: %w", err)
		}
		if err := mp4.RemuxToMP4(ctx, mkvPath, plainPath); err != nil {
			return fmt.Errorf("MP4音声を書き直せません: %w", err)
		}
		audioMeta, _, err := mp4.OpenMeta(ctx, plainPath)
		if err != nil {
			return fmt.Errorf("MP4音声を読み取れません: %w", err)
		}
		audioID, err := firstTrackID(audioMeta.Tracks, mkv.AudioTrack)
		if err != nil {
			return err
		}
		tracks = append(tracks, mkv.TrackInput{SourcePath: plainPath, TrackID: audioID, Language: stream.Language, Name: stream.Name, IsDefault: stream.Default})
	}
	return mp4.Mux(ctx, mkv.MuxOptions{OutputPath: outputPath, Tracks: tracks})
}

func firstTrackID(tracks []mkv.Track, trackType mkv.TrackType) (uint64, error) {
	for _, track := range tracks {
		if track.Type == trackType {
			return track.ID, nil
		}
	}
	return 0, fmt.Errorf("結合する%sトラックがありません", trackType)
}
