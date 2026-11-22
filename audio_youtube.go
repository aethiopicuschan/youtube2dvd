package main

import (
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/kkdai/youtube/v2"
)

// This file holds what is particular to the audio tracks of YouTube videos:
// a video dubbed into other languages offers one audio track per language,
// each in several formats, and the app can put several of them in the saved
// file.

// audioTrack is an audio track of a YouTube video.
type audioTrack struct {
	// ID tells the track apart: its language and kind, such as
	// "ja.dubbed".
	ID string
	// Language is the BCP 47 tag of the audio, such as "ja".
	Language string
	// Kind is how YouTube describes the track: "original" for the language
	// the video was made in, "dubbed", "dubbed-auto" for a dub YouTube
	// generated, or "descriptive" for audio description.
	Kind string
	// YouTubeDefault is set for the track YouTube plays unless asked for
	// another. A video's single-file formats carry this track.
	YouTubeDefault bool
	// Number tells apart tracks that would otherwise have the same label;
	// it is zero for most.
	Number int

	// The yt-dlp formats to download the track in, for MP4 and WebM files.
	ytDlpM4A  ytDlpAudioChoice
	ytDlpWebM ytDlpAudioChoice
}

// ytDlpAudioChoice is the yt-dlp format chosen for an audio track in one
// container, and its size in bytes where known. An empty ID means there is
// none.
type ytDlpAudioChoice struct {
	FormatID string
	Size     int64
}

// label is how the track is offered to the user.
func (t audioTrack) label() string {
	label := subtitleLanguageName(t.Language)
	switch t.Kind {
	case "original":
		label += "（オリジナル）"
	case "dubbed-auto":
		label += "（自動吹き替え）"
	case "descriptive":
		label += "（音声解説）"
	}
	if t.Number > 0 {
		label += "（" + strconv.Itoa(t.Number) + "）"
	}
	return label
}

// videoAudio are the audio tracks a video offers. It is empty for a video
// with a single audio track, which leaves nothing to choose.
type videoAudio struct {
	Tracks []audioTrack
}

func (a videoAudio) find(id string) (audioTrack, bool) {
	for _, track := range a.Tracks {
		if track.ID == id {
			return track, true
		}
	}
	return audioTrack{}, false
}

// defaultID is the track to offer first: the one YouTube plays by default,
// or else the original.
func (a videoAudio) defaultID() string {
	for _, track := range a.Tracks {
		if track.YouTubeDefault {
			return track.ID
		}
	}
	for _, track := range a.Tracks {
		if track.Kind == "original" {
			return track.ID
		}
	}
	if len(a.Tracks) > 0 {
		return a.Tracks[0].ID
	}
	return ""
}

// requestedAudio is an audio track to put in the saved file. Default marks
// the track players, and the DVD, play; it comes first.
type requestedAudio struct {
	Track   audioTrack
	Default bool
}

// youTubeAudioTags reads what the xtags parameter of a stream URL says
// about its audio: the kind of track and whether its dynamic range is
// compressed. A DRC stream sounds flatter and is only used when there is no
// other.
func youTubeAudioTags(address string) (kind string, drc bool) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", false
	}
	for tag := range strings.SplitSeq(parsed.Query().Get("xtags"), ":") {
		key, value, _ := strings.Cut(tag, "=")
		switch key {
		case "acont":
			kind = value
		case "drc":
			drc = value == "1"
		}
	}
	return kind, drc
}

// youTubeAudioKind is the kind of track a display name such as "English
// (US) original" describes, for when the URL does not say.
func youTubeAudioKind(displayName string) string {
	name := strings.ToLower(displayName)
	switch {
	case strings.Contains(name, "descriptive"):
		return "descriptive"
	case strings.Contains(name, "original"):
		return "original"
	}
	return ""
}

// finishAudioTracks sorts the tracks of a video and numbers those that
// would have the same label. A video with fewer than two tracks offers no
// choice.
func finishAudioTracks(tracks []audioTrack) videoAudio {
	if len(tracks) < 2 {
		return videoAudio{}
	}
	japanese := func(track audioTrack) bool {
		primary, _, _ := strings.Cut(track.Language, "-")
		return primary == "ja"
	}
	// The default track first, then Japanese, then the others by name.
	sort.SliceStable(tracks, func(i, j int) bool {
		if tracks[i].YouTubeDefault != tracks[j].YouTubeDefault {
			return tracks[i].YouTubeDefault
		}
		if japanese(tracks[i]) != japanese(tracks[j]) {
			return japanese(tracks[i])
		}
		return tracks[i].label() < tracks[j].label()
	})
	count := make(map[string]int)
	for _, track := range tracks {
		count[track.label()]++
	}
	seen := make(map[string]int)
	for i := range tracks {
		label := tracks[i].label()
		if count[label] > 1 {
			seen[label]++
			if seen[label] > 1 {
				tracks[i].Number = seen[label]
			}
		}
	}
	return videoAudio{Tracks: tracks}
}

// builtinHasOtherAudio reports whether a video has audio in more than one
// language. The built-in downloader cannot fetch the separate audio streams
// that carry them, which YouTube refuses to its client, so it only tells.
func builtinHasOtherAudio(formats youtube.FormatList) bool {
	first := ""
	for _, format := range formats {
		if format.AudioTrack == nil || format.AudioTrack.ID == "" {
			continue
		}
		if first == "" {
			first = format.AudioTrack.ID
		} else if format.AudioTrack.ID != first {
			return true
		}
	}
	return false
}

// errAudioUnavailable reports that an audio track cannot be downloaded in
// the container of the chosen quality.
var errAudioUnavailable = errors.New("この形式では取得できません")

// ytDlpAudio lists the audio tracks in "yt-dlp -J" output, with the best
// MP4 and WebM format of each. yt-dlp lists a format per track and quality,
// with the track's language, and the track's name in the format note.
func ytDlpAudio(formats []ytDlpFormatInfo) videoAudio {
	type candidate struct {
		track         audioTrack
		m4a, webm     *ytDlpFormatInfo
		m4aDRC, wbDRC bool
	}
	var order []string
	candidates := make(map[string]*candidate)
	better := func(current *ytDlpFormatInfo, currentDRC bool, format *ytDlpFormatInfo, drc bool) bool {
		if current == nil {
			return true
		}
		if currentDRC != drc {
			return currentDRC
		}
		return format.audioRate() > current.audioRate()
	}
	for i := range formats {
		format := &formats[i]
		if format.FormatID == "" || format.hasVideo() || !format.hasAudio() || format.Language == "" {
			continue
		}
		if strings.HasPrefix(format.Protocol, "m3u8") || (format.Ext != "m4a" && format.Ext != "webm") {
			continue
		}
		kind, drc := youTubeAudioTags(format.URL)
		name, _, _ := strings.Cut(format.FormatNote, ", ")
		if kind == "" {
			kind = youTubeAudioKind(name)
		}
		drc = drc || strings.HasSuffix(format.FormatID, "-drc")
		// yt-dlp marks audio description with a "-desc" language.
		language, descriptive := strings.CutSuffix(format.Language, "-desc")
		if descriptive {
			kind = "descriptive"
		}
		id := language + "." + kind
		c := candidates[id]
		if c == nil {
			c = &candidate{track: audioTrack{ID: id, Language: youTubeLanguageTag(language), Kind: kind}}
			candidates[id] = c
			order = append(order, id)
		}
		if strings.Contains(name, "(default)") {
			c.track.YouTubeDefault = true
		}
		if format.Ext == "m4a" {
			if better(c.m4a, c.m4aDRC, format, drc) {
				c.m4a, c.m4aDRC = format, drc
			}
		} else if better(c.webm, c.wbDRC, format, drc) {
			c.webm, c.wbDRC = format, drc
		}
	}
	tracks := make([]audioTrack, 0, len(order))
	for _, id := range order {
		c := candidates[id]
		if c.m4a != nil {
			c.track.ytDlpM4A = ytDlpAudioChoice{FormatID: c.m4a.FormatID, Size: c.m4a.size()}
		}
		if c.webm != nil {
			c.track.ytDlpWebM = ytDlpAudioChoice{FormatID: c.webm.FormatID, Size: c.webm.size()}
		}
		tracks = append(tracks, c.track)
	}
	return finishAudioTracks(tracks)
}

// ytDlpAudioFormat is the format to download track in for a file with
// extension.
func ytDlpAudioFormat(track audioTrack, extension string) (ytDlpAudioChoice, error) {
	choice := track.ytDlpM4A
	if extension == ".webm" {
		choice = track.ytDlpWebM
	}
	if choice.FormatID == "" {
		return ytDlpAudioChoice{}, errAudioUnavailable
	}
	return choice, nil
}

// onlyYouTubeDefaultAudio reports whether requested asks for nothing but
// the track YouTube plays by default, which the single-file formats already
// carry.
func onlyYouTubeDefaultAudio(requested []requestedAudio) bool {
	return len(requested) == 0 || (len(requested) == 1 && requested[0].Track.YouTubeDefault)
}

// audioWarning is shown after a download that left out audio tracks that
// could not be had in the chosen format.
func audioWarning(failed []string) string {
	if len(failed) == 0 {
		return ""
	}
	return strings.Join(failed, "、") + "の音声はこの画質の形式では取得できないため、入れませんでした。"
}

// joinWarnings puts the warnings of a download together.
func joinWarnings(warnings ...string) string {
	var parts []string
	for _, warning := range warnings {
		if warning != "" {
			parts = append(parts, warning)
		}
	}
	return strings.Join(parts, "\n")
}
