package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kkdai/youtube/v2"
	"golang.org/x/text/language"
)

// This file holds what is particular to YouTube captions: which tracks a
// video has, which of them are generated automatically, how to fetch them,
// and how to undo the rolling display of automatic captions.

// captionTrack is a caption track of a YouTube video.
type captionTrack struct {
	// ID tells the track apart for the downloader that listed it: the
	// vssId with the built-in downloader, the subtitle key with yt-dlp.
	ID string
	// Language is the BCP 47 tag of the captions, such as "ja".
	Language string
	// Name is the name the uploader gave the track, which tells tracks of
	// the same language apart, such as "English (Plain Text)". It is empty
	// for the plain track of a language.
	Name string
	// Auto is set for captions YouTube generated from the speech.
	Auto bool
}

// label is how the track is offered to the user.
func (t captionTrack) label() string {
	label := subtitleLanguageName(t.Language)
	if t.Name != "" {
		label += "（" + t.Name + "）"
	}
	if t.Auto {
		label += "（自動生成）"
	}
	return label
}

// videoCaptions are the caption tracks a video offers: the uploader's, in
// one or more languages, and the automatically generated one, if any.
type videoCaptions struct {
	Tracks []captionTrack
}

func (c videoCaptions) any() bool {
	return len(c.Tracks) > 0
}

// find returns the track with id.
func (c videoCaptions) find(id string) (captionTrack, bool) {
	for _, track := range c.Tracks {
		if track.ID == id {
			return track, true
		}
	}
	return captionTrack{}, false
}

// requestedCaption is a caption track to embed. Default marks the track
// players show when they show subtitles at all.
type requestedCaption struct {
	Track   captionTrack
	Default bool
}

// youTubeLanguageTag turns a YouTube caption language code into a BCP 47
// tag: the "-orig" yt-dlp adds to the original automatic track goes, and
// the obsolete codes YouTube still uses are replaced.
func youTubeLanguageTag(code string) string {
	code = strings.TrimSuffix(code, "-orig")
	primary, rest, _ := strings.Cut(code, "-")
	switch primary {
	case "iw":
		primary = "he"
	case "in":
		primary = "id"
	case "ji":
		primary = "yi"
	}
	if rest != "" {
		return primary + "-" + rest
	}
	return primary
}

// captionURLDetails reads the language and the track name from the URL of a
// caption track. The IDs of the tracks do not tell them: a second track of
// a language has an ID such as ".en.t3oy0ra6tek", which yt-dlp turns into
// the key "en-t3oy0ra6tek", and the name is only in the URL. Either is
// empty when the URL does not have it.
func captionURLDetails(address string) (code, name string) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", ""
	}
	query := parsed.Query()
	name = strings.TrimSpace(query.Get("name"))
	// Some names are wrapped in parentheses, such as "(Texte Plein)"; the
	// label adds its own.
	for _, pair := range [][2]string{{"(", ")"}, {"（", "）"}} {
		if inner, ok := strings.CutPrefix(name, pair[0]); ok {
			if inner, ok := strings.CutSuffix(inner, pair[1]); ok && !strings.ContainsAny(inner, "()（）") {
				name = strings.TrimSpace(inner)
			}
		}
	}
	return query.Get("lang"), name
}

// newCaptionTrack makes a caption track from what a downloader lists: its
// ID, the language code from the ID and the URL of the track.
func newCaptionTrack(id, idCode, address string, auto bool) captionTrack {
	track := captionTrack{ID: id, Language: youTubeLanguageTag(idCode), Auto: auto}
	code, name := captionURLDetails(address)
	if code != "" {
		track.Language = youTubeLanguageTag(code)
	} else if _, err := language.Parse(track.Language); err != nil {
		// Without the URL, a key such as "en-t3oy0ra6tek" still names
		// its language first.
		track.Language, _, _ = strings.Cut(track.Language, "-")
	}
	if !auto {
		track.Name = name
	}
	return track
}

// numberSameCaptionLabels numbers tracks that would still be offered with
// the same label, as when a URL does not name its track, so that the user
// can tell them apart.
func numberSameCaptionLabels(tracks []captionTrack) {
	count := make(map[string]int)
	for _, track := range tracks {
		count[track.label()]++
	}
	seen := make(map[string]int)
	for i := range tracks {
		label := tracks[i].label()
		if count[label] < 2 {
			continue
		}
		seen[label]++
		if n := seen[label]; n > 1 {
			suffix := strconv.Itoa(n)
			if tracks[i].Name != "" {
				suffix = tracks[i].Name + " " + suffix
			}
			tracks[i].Name = suffix
		}
	}
}

// sortCaptionTracks puts Japanese first and the other languages after it
// in the order of their names.
func sortCaptionTracks(tracks []captionTrack) {
	japanese := func(track captionTrack) bool {
		primary, _, _ := strings.Cut(track.Language, "-")
		return primary == "ja"
	}
	sort.SliceStable(tracks, func(i, j int) bool {
		if japanese(tracks[i]) != japanese(tracks[j]) {
			return japanese(tracks[i])
		}
		return tracks[i].label() < tracks[j].label()
	})
}

// builtinCaptions lists the caption tracks the player response of the
// built-in downloader carries. Kind "asr" marks the automatic track; only
// its spoken language is listed, not machine translations.
func builtinCaptions(tracks []youtube.CaptionTrack) videoCaptions {
	var captions videoCaptions
	haveAuto := false
	for _, track := range tracks {
		if track.VssID == "" || track.LanguageCode == "" || track.BaseURL == "" {
			continue
		}
		caption := newCaptionTrack(track.VssID, track.LanguageCode, track.BaseURL, track.Kind == "asr")
		if caption.Auto {
			if haveAuto {
				continue
			}
			haveAuto = true
		}
		captions.Tracks = append(captions.Tracks, caption)
	}
	sortCaptionTracks(captions.Tracks)
	numberSameCaptionLabels(captions.Tracks)
	return captions
}

// ytDlpSubtitle is one format of a subtitle in "yt-dlp -J" output.
type ytDlpSubtitle struct {
	Ext      string `json:"ext"`
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
}

// ytDlpCaptions lists the caption tracks in "yt-dlp -J" output. The
// uploader's are in "subtitles", apart from the "live_chat" entry yt-dlp
// adds for chat replays. Of "automatic_captions", which lists the automatic
// track translated into many languages, only the original, whose key ends
// in "-orig", is used. Entries served only over HLS are left out: they
// mirror uploader captions rather than being automatic ones.
func ytDlpCaptions(subtitles, automatic map[string][]ytDlpSubtitle) videoCaptions {
	vtt := func(formats []ytDlpSubtitle) (ytDlpSubtitle, bool) {
		for _, format := range formats {
			if format.Ext == "vtt" && !strings.HasPrefix(format.Protocol, "m3u8") {
				return format, true
			}
		}
		return ytDlpSubtitle{}, false
	}
	var captions videoCaptions
	// Keys are visited in order, so that tracks numbered for having the
	// same label get the same numbers every time.
	for _, key := range slices.Sorted(maps.Keys(subtitles)) {
		format, ok := vtt(subtitles[key])
		if key == "live_chat" || !ok {
			continue
		}
		captions.Tracks = append(captions.Tracks, newCaptionTrack(key, key, format.URL, false))
	}
	for _, key := range slices.Sorted(maps.Keys(automatic)) {
		if format, ok := vtt(automatic[key]); ok && strings.HasSuffix(key, "-orig") {
			captions.Tracks = append(captions.Tracks, newCaptionTrack(key, key, format.URL, true))
			break
		}
	}
	sortCaptionTracks(captions.Tracks)
	numberSameCaptionLabels(captions.Tracks)
	return captions
}

// youTubeCaptionCues turns a downloaded WebVTT caption track into cues.
// Automatic captions are written for a rolling two-line display with word
// timing; they are reduced to one line per cue.
func youTubeCaptionCues(vtt []byte, auto bool) ([]subtitleCue, error) {
	cues, err := parseWebVTT(bytes.NewReader(vtt))
	if err != nil {
		return nil, err
	}
	if auto && hasYouTubeWordTiming(cues) {
		return cleanYouTubeAutoCaptions(cues), nil
	}
	return plainCues(cues), nil
}

func hasYouTubeWordTiming(cues []vttCue) bool {
	for _, cue := range cues {
		for _, line := range cue.Lines {
			if strings.Contains(line, "<c>") {
				return true
			}
		}
	}
	return false
}

// cleanYouTubeAutoCaptions undoes the rolling display of automatic
// captions. Each cue repeats the line before and adds a new last line with
// word timing tags, and a 10 ms cue that only repeats the finished line
// follows it. Keeping the new last line of each cue, and joining a line
// with its repeat, gives one cue per line.
func cleanYouTubeAutoCaptions(cues []vttCue) []subtitleCue {
	var out []subtitleCue
	for _, cue := range cues {
		if len(cue.Lines) == 0 {
			continue
		}
		text := plainCueText(cue.Lines[len(cue.Lines)-1])
		if text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Text == text && cue.Start <= out[n-1].End+50*time.Millisecond {
			out[n-1].End = max(out[n-1].End, cue.End)
			continue
		}
		out = append(out, subtitleCue{Start: cue.Start, End: cue.End, Text: text})
	}
	return out
}

// fetchBuiltinCaptionVTT downloads a caption track of video, found again
// in the video's current player response because track URLs expire.
func fetchBuiltinCaptionVTT(ctx context.Context, video *youtube.Video, id string) ([]byte, error) {
	for _, track := range video.CaptionTracks {
		if track.VssID != id {
			continue
		}
		address, err := url.Parse(track.BaseURL)
		if err != nil {
			return nil, err
		}
		query := address.Query()
		query.Set("fmt", "vtt")
		address.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
		if err != nil {
			return nil, err
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("字幕の取得に失敗しました（HTTP %d）", response.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(body)) == 0 {
			return nil, errors.New("字幕が空でした")
		}
		return body, nil
	}
	return nil, errors.New("選択した字幕が見つかりません")
}

// ytDlpSubtitleArgs asks yt-dlp for the given caption tracks and no media.
// yt-dlp writes each to outputBase.<key>.vtt. Uploader tracks come first
// when a key is both an uploader and an automatic one, which the "-orig"
// keys of automatic tracks never are.
func ytDlpSubtitleArgs(tracks []captionTrack, outputBase, videoURL string) []string {
	args := append(ytDlpCommonArgs(), "-f", ytDlpAnyFormat, "--skip-download")
	var keys []string
	uploader, auto := false, false
	for _, track := range tracks {
		keys = append(keys, regexp.QuoteMeta(track.ID))
		if track.Auto {
			auto = true
		} else {
			uploader = true
		}
	}
	if uploader {
		args = append(args, "--write-subs")
	}
	if auto {
		args = append(args, "--write-auto-subs")
	}
	outputTemplate := strings.ReplaceAll(outputBase, "%", "%%")
	return append(args,
		// --sub-langs takes regular expressions.
		"--sub-langs", "^("+strings.Join(keys, "|")+")$",
		"--sub-format", "vtt",
		"-o", outputTemplate,
		"--", videoURL,
	)
}

// fetchYtDlpCaptions downloads caption tracks with one run of yt-dlp into
// workDir and returns a function that gives the WebVTT of each.
func fetchYtDlpCaptions(ctx context.Context, runner ytDlpRunner, workDir string, tracks []captionTrack, videoURL string) func(captionTrack) ([]byte, error) {
	outputBase := filepath.Join(workDir, "captions")
	_, runErr := runner.Run(ctx, ytDlpSubtitleArgs(tracks, outputBase, videoURL), func(string) {})
	return func(track captionTrack) ([]byte, error) {
		if runErr != nil {
			return nil, runErr
		}
		// yt-dlp exits successfully without writing anything when a track
		// is not there.
		vtt, err := os.ReadFile(outputBase + "." + track.ID + ".vtt")
		if err != nil {
			return nil, errors.New("yt-dlpが字幕を保存しませんでした")
		}
		return vtt, nil
	}
}

// addCaptions embeds the requested caption tracks in the downloaded video
// at mediaPath, replacing it. fetch downloads one track as WebVTT. Tracks
// that cannot be fetched or read are left out; the warning names them. When
// none is left, or embedding fails, the video stays as it was.
func addCaptions(ctx context.Context, mediaPath string, requested []requestedCaption, fetch func(captionTrack) ([]byte, error)) string {
	var tracks []subtitleTrack
	var failed []string
	var lastErr error
	for _, request := range requested {
		if ctx.Err() != nil {
			return ""
		}
		cues, err := func() ([]subtitleCue, error) {
			vtt, err := fetch(request.Track)
			if err != nil {
				return nil, err
			}
			return youTubeCaptionCues(vtt, request.Track.Auto)
		}()
		if err != nil {
			failed = append(failed, request.Track.label())
			lastErr = err
			continue
		}
		tracks = append(tracks, subtitleTrack{Language: request.Track.Language, Name: request.Track.label(), Default: request.Default, Cues: cues})
	}
	if len(tracks) > 0 {
		subtitled := mediaPath + ".subtitled" + filepath.Ext(mediaPath)
		err := embedSubtitles(ctx, mediaPath, subtitled, tracks)
		if err == nil {
			err = os.Rename(subtitled, mediaPath)
		}
		if err != nil {
			_ = os.Remove(subtitled)
			if ctx.Err() != nil {
				return ""
			}
			return captionWarning(nil, err)
		}
	}
	if len(failed) == 0 {
		return ""
	}
	if len(tracks) == 0 {
		return captionWarning(nil, lastErr)
	}
	return captionWarning(failed, lastErr)
}

// captionWarning is shown after a download whose captions could not all be
// added; the video itself was saved. failed names the tracks left out, or
// is nil when none could be added.
func captionWarning(failed []string, err error) string {
	reason := ""
	if err != nil {
		reason = "（" + strings.TrimSpace(err.Error()) + "）"
	}
	if failed == nil {
		return "字幕を追加できなかったため、字幕なしで保存しました" + reason + "。"
	}
	return strings.Join(failed, "、") + "の字幕を追加できませんでした" + reason + "。"
}
