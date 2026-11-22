package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aethiopicuschan/video2dvd"
	"github.com/gravity-zero/mkvgo/matroska"
	"github.com/gravity-zero/mkvgo/mkv"
)

// videoDetailsCache holds what was read from each video, by path. Reading
// runs in the background; changed tells the app to show what arrived.
type videoDetailsCache struct {
	mu      sync.Mutex
	details map[string]videoFileDetails
	changed atomic.Bool
}

// videoDetailsRetryAfter is how long a failed read is shown before the file
// is read again. A read can fail only for a while, as when another program
// still holds a video that was just saved, or a drive is away for a moment.
const videoDetailsRetryAfter = 5 * time.Second

// videoFileDetails is what was read from a file, and the size and time of the
// file it was read from, so that a changed file is read again. reading is
// set while it is being read; a file is read once at a time, however often
// it changes meanwhile. failedAt is when a read failed.
type videoFileDetails struct {
	size     int64
	modTime  time.Time
	media    *video2dvd.MP4Info
	err      error
	failedAt time.Time
	reading  bool
}

// describe tells the length, picture, audio and subtitles of the video at
// path, and true; or, while that is not known, why, and false. It starts
// reading the video when that has not been done.
func (c *videoDetailsCache) describe(path string) (string, bool) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".mp4" && extension != ".webm" {
		return "この形式の動画の詳しい情報は表示できません。", false
	}
	stat, err := os.Stat(path)
	if err != nil {
		return "ファイルを確認できません：" + err.Error(), false
	}
	details, ok := c.lookup(path, stat)
	switch {
	case !ok:
		return "読み込んでいます…", false
	case details.err != nil:
		return "情報を読み取れませんでした：" + details.err.Error(), false
	}
	return videoDetailsText(*details.media), true
}

// lookup returns what was read from path, and starts reading it when it was
// not read from the file as it is now.
func (c *videoDetailsCache) lookup(path string, stat os.FileInfo) (videoFileDetails, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.details == nil {
		c.details = make(map[string]videoFileDetails)
	}
	details, ok := c.details[path]
	current := ok && details.size == stat.Size() && details.modTime.Equal(stat.ModTime()) &&
		(details.err == nil || time.Since(details.failedAt) < videoDetailsRetryAfter)
	if current && !details.reading {
		return details, true
	}
	if ok && details.reading {
		// Wait for the read that runs; the file is checked again after it.
		return videoFileDetails{}, false
	}
	c.details[path] = videoFileDetails{size: stat.Size(), modTime: stat.ModTime(), reading: true}
	go func() {
		media, err := readVideoDetails(path)
		c.mu.Lock()
		read := videoFileDetails{size: stat.Size(), modTime: stat.ModTime(), err: err}
		if err == nil {
			read.media = &media
		} else {
			read.failedAt = time.Now()
		}
		c.details[path] = read
		c.mu.Unlock()
		c.changed.Store(true)
	}()
	return videoFileDetails{}, false
}

// readVideoDetails reads the tracks of a video. For WebM it also works out
// which default marks count, as video2dvd does when it converts the file.
func readVideoDetails(path string) (video2dvd.MP4Info, error) {
	media, err := video2dvd.ProbeMedia(path)
	if err != nil {
		return media, err
	}
	if strings.EqualFold(filepath.Ext(path), ".webm") {
		if container, err := matroska.OpenMeta(context.Background(), path); err == nil {
			applyWebMDefaultMarks(&media, container.Tracks)
		}
	}
	return media, nil
}

// applyWebMDefaultMarks keeps the default mark of a WebM track only where
// video2dvd counts it. Matroska makes a track without a FlagDefault element
// a default one, but video2dvd follows such an implied mark only when
// another track of the same kind has FlagDefault 0: tools that write no
// FlagDefault at all would have every track marked. tracks are the file's
// tracks as mkvgo reads them, which tell an implied mark from a written one;
// the tracks of each kind are in the same order in both.
func applyWebMDefaultMarks(media *video2dvd.MP4Info, tracks []mkv.Track) {
	kinds := map[video2dvd.TrackKind]mkv.TrackType{
		video2dvd.TrackKindVideo:    mkv.VideoTrack,
		video2dvd.TrackKindAudio:    mkv.AudioTrack,
		video2dvd.TrackKindSubtitle: mkv.SubtitleTrack,
	}
	for kind, trackType := range kinds {
		var ofKind []mkv.Track
		explicitNonDefault := false
		for _, track := range tracks {
			if track.Type == trackType {
				ofKind = append(ofKind, track)
				explicitNonDefault = explicitNonDefault || (track.DefaultPresent && !track.IsDefault)
			}
		}
		n := 0
		for i := range media.Tracks {
			if media.Tracks[i].Kind != kind {
				continue
			}
			if n < len(ofKind) && !ofKind[n].DefaultPresent && !explicitNonDefault {
				media.Tracks[i].Default = false
			}
			n++
		}
	}
}

// videoDetailsText describes the length, picture, audio and subtitles of a
// video, one line each and one line per track.
func videoDetailsText(media video2dvd.MP4Info) string {
	var video, audio, subtitles []video2dvd.TrackInfo
	for _, track := range media.Tracks {
		switch track.Kind {
		case video2dvd.TrackKindVideo:
			video = append(video, track)
		case video2dvd.TrackKindAudio:
			audio = append(audio, track)
		case video2dvd.TrackKindSubtitle:
			subtitles = append(subtitles, track)
		}
	}
	var b strings.Builder
	duration := dvdMediaDuration(media)
	if duration > 0 {
		b.WriteString("再生時間：" + formatPlayTime(duration) + "\n")
	} else {
		b.WriteString("再生時間：不明\n")
	}
	for _, track := range video {
		parts := []string{codecLabel(track.Codec)}
		if track.Width > 0 && track.Height > 0 {
			parts = append(parts, fmt.Sprintf("%d×%d", track.Width, track.Height))
		}
		if track.DefaultDuration > 0 {
			parts = append(parts, fmt.Sprintf("%.4g fps", float64(time.Second)/float64(track.DefaultDuration)))
		}
		b.WriteString("映像：" + strings.Join(parts, "　") + "\n")
	}

	b.WriteString(fmt.Sprintf("\n音声（%d）\n", len(audio)))
	if len(audio) == 0 {
		b.WriteString("　なし\n")
	}
	for i, track := range audio {
		parts := []string{codecLabel(track.Codec)}
		if channels := channelsLabel(track.Channels); channels != "" {
			parts = append(parts, channels)
		}
		if track.SampleRate > 0 {
			parts = append(parts, fmt.Sprintf("%g kHz", float64(track.SampleRate)/1000))
		}
		b.WriteString(fmt.Sprintf("　%d. %s　%s\n", i+1, trackTitle(track), strings.Join(parts, "　")))
	}

	b.WriteString(fmt.Sprintf("\n字幕（%d）\n", len(subtitles)))
	if len(subtitles) == 0 {
		b.WriteString("　なし\n")
	}
	for i, track := range subtitles {
		b.WriteString(fmt.Sprintf("　%d. %s　%s\n", i+1, trackTitle(track), codecLabel(track.Codec)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// trackTitle names a track by its language, its name where that says more,
// and whether it is the default one. A name that starts with the language,
// as the app names tracks ("英語（オリジナル）"), stands for both.
func trackTitle(track video2dvd.TrackInfo) string {
	title := "言語不明"
	if track.Language != "" && track.Language != "und" {
		title = subtitleLanguageName(track.Language)
	}
	if name := strings.TrimSpace(track.Name); name != "" && !isHandlerName(name) {
		switch {
		case strings.HasPrefix(name, title):
			title = name
		default:
			title += "「" + name + "」"
		}
	}
	if track.Default {
		title += "（規定）"
	}
	return title
}

// isHandlerName reports whether name is what a muxer writes into an MP4
// track of its own accord, such as "SoundHandler" or YouTube's "ISO Media
// file produced by Google Inc.", rather than a name someone gave the track.
func isHandlerName(name string) bool {
	return strings.Contains(name, "Handler") ||
		strings.HasPrefix(name, "ISO Media file produced by") ||
		strings.HasPrefix(name, "Core Media ")
}

// codecLabel is the common name of a codec as video2dvd reports it.
func codecLabel(codec string) string {
	switch codec {
	case "avc1", "h264":
		return "H.264"
	case "vp9":
		return "VP9"
	case "vp8":
		return "VP8"
	case "av1", "av01":
		return "AV1"
	case "mp4a":
		return "AAC"
	case "opus":
		return "Opus"
	case "ac-3":
		return "AC-3"
	case "tx3g":
		return "MP4 テキスト"
	case "D_WEBVTT/SUBTITLES", "S_TEXT/WEBVTT":
		return "WebVTT"
	case "S_TEXT/UTF8":
		return "SubRip"
	}
	return codec
}

// channelsLabel describes a number of audio channels.
func channelsLabel(channels int) string {
	switch channels {
	case 0:
		return ""
	case 1:
		return "モノラル"
	case 2:
		return "ステレオ"
	case 6:
		return "5.1ch"
	case 8:
		return "7.1ch"
	}
	return fmt.Sprintf("%dch", channels)
}

// formatPlayTime writes a length as h:mm:ss, or m:ss under an hour.
func formatPlayTime(d time.Duration) string {
	seconds := int64((d + time.Second/2) / time.Second)
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
