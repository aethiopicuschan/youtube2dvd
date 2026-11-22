package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aethiopicuschan/video2dvd"
	"github.com/gravity-zero/mkvgo/mkv"
	"github.com/gravity-zero/mkvgo/mkv/reader"
	"github.com/gravity-zero/mkvgo/mkv/writer"
	"github.com/gravity-zero/mkvgo/mp4"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// This file handles subtitles in general: reading WebVTT, tidying cues and
// embedding them in an MP4 or WebM file. Nothing here knows about YouTube;
// see captions_youtube.go for that.

// vttCue is one cue of a WebVTT file as written, with its payload lines
// still holding any markup.
type vttCue struct {
	Start, End time.Duration
	Lines      []string
}

// subtitleCue is a cue ready to be embedded: plain text, possibly on
// several lines.
type subtitleCue struct {
	Start, End time.Duration
	Text       string
}

var vttTimingLine = regexp.MustCompile(`^((?:\d+:)?\d{2}:\d{2}\.\d{3})\s+-->\s+((?:\d+:)?\d{2}:\d{2}\.\d{3})`)

func parseVTTTimestamp(value string) (time.Duration, bool) {
	parts := strings.Split(value, ":")
	var hours, minutes int
	var seconds string
	switch len(parts) {
	case 3:
		var err error
		if hours, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		if minutes, err = strconv.Atoi(parts[1]); err != nil {
			return 0, false
		}
		seconds = parts[2]
	case 2:
		var err error
		if minutes, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		seconds = parts[1]
	default:
		return 0, false
	}
	whole, fraction, found := strings.Cut(seconds, ".")
	secs, err := strconv.Atoi(whole)
	if err != nil || !found || len(fraction) != 3 {
		return 0, false
	}
	millis, err := strconv.Atoi(fraction)
	if err != nil {
		return 0, false
	}
	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute +
		time.Duration(secs)*time.Second + time.Duration(millis)*time.Millisecond, true
}

// parseWebVTT reads the cues of a WebVTT file. The header and NOTE, STYLE
// and REGION blocks are skipped, and cue settings are ignored. Only an empty
// line ends a cue: a line holding a single space is part of the payload.
func parseWebVTT(r io.Reader) ([]vttCue, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var cues []vttCue
	var current *vttCue
	skipping := true // the header runs to the first empty line
	first := true
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if first {
			first = false
			line = strings.TrimPrefix(line, "\ufeff")
			if !strings.HasPrefix(line, "WEBVTT") {
				return nil, errors.New("WebVTT形式の字幕ではありません")
			}
		}
		if line == "" {
			if current != nil {
				cues = append(cues, *current)
				current = nil
			}
			skipping = false
			continue
		}
		if skipping {
			continue
		}
		if current == nil {
			if match := vttTimingLine.FindStringSubmatch(line); match != nil {
				start, okStart := parseVTTTimestamp(match[1])
				end, okEnd := parseVTTTimestamp(match[2])
				if okStart && okEnd {
					current = &vttCue{Start: start, End: end}
				}
				continue
			}
			if strings.HasPrefix(line, "NOTE") || strings.HasPrefix(line, "STYLE") || strings.HasPrefix(line, "REGION") {
				skipping = true
			}
			// Anything else before the timing line is a cue identifier.
			continue
		}
		current.Lines = append(current.Lines, line)
	}
	if current != nil {
		cues = append(cues, *current)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cues, nil
}

var vttMarkup = regexp.MustCompile(`<[^>]*>`)

// plainCueText removes WebVTT markup and character references from a line.
func plainCueText(line string) string {
	return strings.TrimSpace(html.UnescapeString(vttMarkup.ReplaceAllString(line, "")))
}

// plainCues turns WebVTT cues into plain-text cues, keeping every line.
func plainCues(cues []vttCue) []subtitleCue {
	out := make([]subtitleCue, 0, len(cues))
	for _, cue := range cues {
		var lines []string
		for _, line := range cue.Lines {
			if text := plainCueText(line); text != "" {
				lines = append(lines, text)
			}
		}
		if len(lines) == 0 {
			continue
		}
		out = append(out, subtitleCue{Start: cue.Start, End: cue.End, Text: strings.Join(lines, "\n")})
	}
	return out
}

// tidyCues orders the cues, ends each one when the next begins, and keeps
// them within the length of the video, so that no two cues are shown at
// once and none outlasts the picture. A zero length keeps every cue.
func tidyCues(cues []subtitleCue, length time.Duration) []subtitleCue {
	sorted := append([]subtitleCue(nil), cues...)
	// Insertion sort keeps cues with equal starts in their order.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Start < sorted[j-1].Start; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	out := sorted[:0]
	for i, cue := range sorted {
		if i+1 < len(sorted) && sorted[i+1].Start < cue.End {
			cue.End = sorted[i+1].Start
		}
		if length > 0 {
			if cue.Start >= length {
				break
			}
			cue.End = min(cue.End, length)
		}
		if cue.End > cue.Start && strings.TrimSpace(cue.Text) != "" {
			out = append(out, cue)
		}
	}
	return out
}

func formatSRTTimestamp(d time.Duration) string {
	ms := d.Milliseconds()
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000)
}

// writeSRT writes the cues as SubRip text. MP4 timed text drops anything
// between angle brackets, so literal ones become their full-width forms.
func writeSRT(w io.Writer, cues []subtitleCue) error {
	brackets := strings.NewReplacer("<", "＜", ">", "＞")
	for i, cue := range cues {
		if _, err := fmt.Fprintf(w, "%d\n%s --> %s\n%s\n\n", i+1, formatSRTTimestamp(cue.Start), formatSRTTimestamp(cue.End), brackets.Replace(cue.Text)); err != nil {
			return err
		}
	}
	return nil
}

// subtitleTrack describes the subtitle track to embed.
type subtitleTrack struct {
	// Language is a BCP 47 tag such as "ja" or "pt-BR".
	Language string
	// Name is the track name shown by players, such as "日本語".
	Name string
	// Default marks the track players pick when they show subtitles.
	Default bool
	Cues    []subtitleCue
}

// subtitleLanguageName returns the Japanese name of a BCP 47 language tag,
// or the tag itself when it is not known.
func subtitleLanguageName(tag string) string {
	parsed, err := language.Parse(tag)
	if err != nil {
		return tag
	}
	if name := display.Japanese.Tags().Name(parsed); name != "" {
		return name
	}
	return tag
}

// matroskaLanguage returns the ISO 639-2 code Matroska expects next to the
// BCP 47 tag. Matroska uses the bibliographic codes where they differ.
func matroskaLanguage(tag string) string {
	parsed, err := language.Parse(tag)
	if err != nil {
		return "und"
	}
	base, _ := parsed.Base()
	code := base.ISO3()
	if bibliographic, ok := iso6392Bibliographic[code]; ok {
		return bibliographic
	}
	if code == "" {
		return "und"
	}
	return code
}

var iso6392Bibliographic = map[string]string{
	"bod": "tib", "ces": "cze", "cym": "wel", "deu": "ger", "ell": "gre",
	"eus": "baq", "fas": "per", "fra": "fre", "hye": "arm", "isl": "ice",
	"kat": "geo", "mkd": "mac", "mri": "mao", "msa": "may", "mya": "bur",
	"nld": "dut", "ron": "rum", "slk": "slo", "sqi": "alb", "zho": "chi",
}

// mediaLength returns how long the picture of a video file lasts. The video
// track is preferred, because a container's own duration can be stretched
// by other tracks.
func mediaLength(path string) (time.Duration, error) {
	media, err := video2dvd.ProbeMedia(path)
	if err != nil {
		return 0, err
	}
	var length time.Duration
	for _, track := range media.Tracks {
		if track.Kind == video2dvd.TrackKindVideo && track.Duration > length {
			length = track.Duration
		}
	}
	if length <= 0 {
		length = media.Duration
	}
	return length, nil
}

// embedSubtitles writes srcPath to dstPath with tracks added as subtitle
// tracks: MP4 timed text for an MP4, WebVTT for a WebM. The audio and video
// are copied as they are. The cues are kept within the video's length.
func embedSubtitles(ctx context.Context, srcPath, dstPath string, tracks []subtitleTrack) error {
	length, err := mediaLength(srcPath)
	if err != nil {
		return fmt.Errorf("動画の長さを確認できません: %w", err)
	}
	var kept []subtitleTrack
	for _, track := range tracks {
		track.Cues = tidyCues(track.Cues, length)
		if len(track.Cues) > 0 {
			kept = append(kept, track)
		}
	}
	if len(kept) == 0 {
		return errors.New("字幕に表示できる内容がありません")
	}
	switch strings.ToLower(filepath.Ext(srcPath)) {
	case ".mp4":
		return embedMP4Subtitles(ctx, srcPath, dstPath, kept)
	case ".webm":
		return embedWebMSubtitles(ctx, srcPath, dstPath, kept)
	default:
		return fmt.Errorf("この形式の動画には字幕を埋め込めません: %s", filepath.Ext(srcPath))
	}
}

// embedMP4Subtitles adds the tracks one at a time, since mkvgo adds one
// timed-text track per pass, and then marks the default one in place.
func embedMP4Subtitles(ctx context.Context, srcPath, dstPath string, tracks []subtitleTrack) error {
	srt, err := os.CreateTemp(filepath.Dir(dstPath), ".youtube2dvd-subtitles-*.srt")
	if err != nil {
		return err
	}
	srtPath := srt.Name()
	_ = srt.Close()
	defer os.Remove(srtPath)
	// Each pass reads the previous one's output.
	passes := []string{dstPath + ".pass1" + filepath.Ext(dstPath), dstPath + ".pass2" + filepath.Ext(dstPath)}
	defer func() {
		for _, pass := range passes {
			_ = os.Remove(pass)
		}
	}()
	source := srcPath
	for i, track := range tracks {
		out := passes[i%2]
		if i == len(tracks)-1 {
			out = dstPath
		}
		if err := writeSRTFile(srtPath, track.Cues); err != nil {
			return err
		}
		if err := mp4.MergeSubtitle(ctx, source, srtPath, out, track.Language, track.Name); err != nil {
			_ = os.Remove(dstPath)
			return err
		}
		source = out
	}
	// The timed-text tracks were added after the source's tracks, in order.
	err = mp4.EditInPlace(ctx, dstPath, func(c *mkv.Container) {
		first := len(c.Tracks) - len(tracks)
		for i, track := range tracks {
			c.Tracks[first+i].IsDefault = track.Default
			c.Tracks[first+i].DefaultPresent = true
		}
	})
	if err != nil {
		_ = os.Remove(dstPath)
		return err
	}
	return nil
}

func writeSRTFile(path string, cues []subtitleCue) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := writeSRT(file, cues); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// embedWebMSubtitles copies a WebM file block by block, adding a WebVTT
// track (codec D_WEBVTT/SUBTITLES, the one WebM allows) for each subtitle
// track in the same pass.
func embedWebMSubtitles(ctx context.Context, srcPath, dstPath string, subtitleTracks []subtitleTrack) (err error) {
	container, err := reader.Open(ctx, srcPath, reader.WithoutAttachmentData())
	if err != nil {
		return err
	}
	var lastID uint64
	for _, existing := range container.Tracks {
		lastID = max(lastID, existing.ID)
	}
	tracks := append([]mkv.Track(nil), container.Tracks...)
	var cueBlocks []mkv.Block
	for i, track := range subtitleTracks {
		id := lastID + uint64(i) + 1
		tracks = append(tracks, mkv.Track{
			ID:             id,
			UID:            id,
			Type:           mkv.SubtitleTrack,
			Codec:          "D_WEBVTT/SUBTITLES",
			Language:       matroskaLanguage(track.Language),
			LanguageBCP47:  track.Language,
			Name:           track.Name,
			IsDefault:      track.Default,
			DefaultPresent: true,
		})
		cueBlocks = append(cueBlocks, webVTTBlocks(id, track.Cues)...)
	}
	withSubtitles := *container
	withSubtitles.Tracks = tracks
	if err := mkv.ValidateWebM(&withSubtitles); err != nil {
		return err
	}
	// The blocks of all subtitle tracks go out in time order.
	sort.SliceStable(cueBlocks, func(i, j int) bool { return cueBlocks[i].Timecode < cueBlocks[j].Timecode })

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	scale := container.Info.TimecodeScale
	if scale == 0 {
		scale = 1_000_000
	}
	blocks, err := reader.NewBlockReader(src, scale)
	if err != nil {
		return err
	}
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dst.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(dstPath)
		}
	}()
	mkvWriter := writer.NewMKVWriter(dst)
	if err := mkvWriter.WriteStartWebM(mkv.WebMDocTypeVersion(&withSubtitles)); err != nil {
		return err
	}
	meta := *container
	meta.Chapters, meta.Attachments, meta.Tags = nil, nil, nil
	meta.Info.SegmentUID, meta.Info.PrevUID, meta.Info.NextUID = nil, nil, nil
	if err := mkvWriter.WriteMetadata(&meta, tracks, container.DurationMs); err != nil {
		return err
	}

	// Clusters of about a second keep block offsets well inside their
	// 16-bit range.
	const clusterSpanMs = 1000
	var cluster []mkv.Block
	clusterStart := int64(-1)
	flush := func() error {
		if len(cluster) == 0 {
			return nil
		}
		err := mkvWriter.WriteClusterWithCues(clusterStart, scale, cluster)
		cluster, clusterStart = cluster[:0], -1
		return err
	}
	add := func(block mkv.Block) error {
		if clusterStart >= 0 && (block.Timecode-clusterStart >= clusterSpanMs || block.Timecode < clusterStart) {
			if err := flush(); err != nil {
				return err
			}
		}
		if clusterStart < 0 {
			clusterStart = block.Timecode
		}
		cluster = append(cluster, block)
		return nil
	}
	next := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		block, err := blocks.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		for next < len(cueBlocks) && cueBlocks[next].Timecode <= block.Timecode {
			if err := add(cueBlocks[next]); err != nil {
				return err
			}
			next++
		}
		if err := add(block); err != nil {
			return err
		}
	}
	for ; next < len(cueBlocks); next++ {
		if err := add(cueBlocks[next]); err != nil {
			return err
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return mkvWriter.Finalize()
}

// webVTTBlocks turns cues into the blocks of a WebM WebVTT track. A block
// holds the cue identifier, the cue settings and the payload, each ending
// with a line feed; both of the first are left empty.
func webVTTBlocks(trackNumber uint64, cues []subtitleCue) []mkv.Block {
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	blocks := make([]mkv.Block, 0, len(cues))
	for _, cue := range cues {
		blocks = append(blocks, mkv.Block{
			TrackNumber: trackNumber,
			Timecode:    cue.Start.Milliseconds(),
			Duration:    (cue.End - cue.Start).Milliseconds(),
			Keyframe:    true,
			Data:        []byte("\n\n" + escape.Replace(cue.Text)),
		})
	}
	return blocks
}
