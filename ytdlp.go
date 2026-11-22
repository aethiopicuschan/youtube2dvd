package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kkdai/youtube/v2"
)

// This file runs the yt-dlp that the user installed and enabled on the
// settings page. The app only tells yt-dlp which format to fetch and where to
// put it; everything about how yt-dlp talks to the site is left to yt-dlp and
// the user's own yt-dlp configuration.

const (
	// ytDlpMaxHeight is the tallest picture worth downloading: DVD-Video is
	// 720x480, so anything larger would only be scaled down again.
	ytDlpMaxHeight = 480

	ytDlpProgressPrefix   = "youtube2dvd-progress:"
	ytDlpProgressTemplate = "download:" + ytDlpProgressPrefix + "%(progress.downloaded_bytes)s/%(progress.total_bytes)s/%(progress.total_bytes_estimate)s"
)

// ytDlpRunner runs yt-dlp with the given arguments. With a nil onLine it
// returns what yt-dlp wrote to the standard output; otherwise it passes each
// line of the standard output to onLine as it arrives and returns nothing.
type ytDlpRunner interface {
	Run(ctx context.Context, args []string, onLine func(line string)) ([]byte, error)
}

// execYtDlpRunner runs the yt-dlp executable at path.
type execYtDlpRunner struct {
	path string
}

func (r execYtDlpRunner) Run(ctx context.Context, args []string, onLine func(line string)) ([]byte, error) {
	path := strings.TrimSpace(r.path)
	if path == "" {
		return nil, &ytDlpStartError{err: exec.ErrNotFound}
	}
	// The arguments go to yt-dlp as they are, without a shell in between.
	cmd := exec.CommandContext(ctx, path, args...)
	configureYtDlpProcess(cmd)
	cmd.WaitDelay = 5 * time.Second
	stderr := &tailBuffer{limit: 8 << 10}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &ytDlpStartError{err: err}
	}
	if err := cmd.Start(); err != nil {
		return nil, &ytDlpStartError{err: err}
	}

	var output []byte
	var readErr error
	if onLine == nil {
		output, readErr = io.ReadAll(stdout)
	} else {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
		readErr = scanner.Err()
	}
	if readErr != nil {
		// Let yt-dlp finish writing so that Wait does not block on the pipe.
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if waitErr != nil {
		return nil, &ytDlpExitError{stderr: stderr.String(), err: waitErr}
	}
	if readErr != nil {
		return nil, readErr
	}
	return output, nil
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	limit int
	data  []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append(b.data[:0], b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	return string(b.data)
}

// ytDlpStartError reports that yt-dlp could not be started at all.
type ytDlpStartError struct {
	err error
}

func (e *ytDlpStartError) Error() string {
	reason := e.err.Error()
	switch {
	case errors.Is(e.err, exec.ErrNotFound), errors.Is(e.err, os.ErrNotExist):
		reason = "ファイルが見つかりません"
	case errors.Is(e.err, os.ErrPermission):
		reason = "実行する権限がありません"
	}
	return "yt-dlpを実行できません（" + reason + "）。「設定」でyt-dlpのパスを確認してください。"
}

func (e *ytDlpStartError) Unwrap() error {
	return e.err
}

// ytDlpExitError reports that yt-dlp ran and ended with an error.
type ytDlpExitError struct {
	stderr string
	err    error
}

func (e *ytDlpExitError) Error() string {
	const maxLines = 3
	var lines []string
	for line := range strings.SplitSeq(e.stderr, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "yt-dlpがエラーで終了しました: " + e.err.Error()
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return "yt-dlpがエラーで終了しました: " + strings.Join(lines, " / ")
}

func (e *ytDlpExitError) Unwrap() error {
	return e.err
}

// ytDlpCommonArgs are passed on every run that works on a video.
func ytDlpCommonArgs() []string {
	return []string{"--no-playlist", "--newline", "--no-color"}
}

// ytDlpInfoArgs asks for the description of one video as JSON. The URL comes
// after "--" so that it is never read as an option.
func ytDlpInfoArgs(videoURL string) []string {
	return append(ytDlpCommonArgs(), "-f", ytDlpAnyFormat, "-J", "--", videoURL)
}

// ytDlpAnyFormat is a format choice that any video has. Runs that do not
// download the video give it all the same: it only replaces a -f in the
// user's yt-dlp configuration, with which yt-dlp would fail when that format
// is missing.
const ytDlpAnyFormat = "b/bv*/ba"

// ytDlpDownloadArgs asks for one format of one video, written to outputPath.
func ytDlpDownloadArgs(formatID, outputPath, videoURL string) []string {
	// "%" starts a field in an output template.
	outputTemplate := strings.ReplaceAll(outputPath, "%", "%%")
	return append(ytDlpCommonArgs(),
		"-f", formatID,
		"-o", outputTemplate,
		"--progress-template", ytDlpProgressTemplate,
		"--", videoURL,
	)
}

// ytDlpVersion returns the version that yt-dlp reports.
func ytDlpVersion(ctx context.Context, runner ytDlpRunner) (string, error) {
	output, err := runner.Run(ctx, []string{"--version"}, nil)
	if err != nil {
		return "", err
	}
	version, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	version = strings.TrimSpace(version)
	if version == "" {
		return "", errors.New("yt-dlpからバージョンを取得できませんでした。")
	}
	return version, nil
}

// ytDlpInfo is the part of "yt-dlp -J" output that the app reads.
type ytDlpInfo struct {
	Title             string                     `json:"title"`
	Thumbnail         string                     `json:"thumbnail"`
	Thumbnails        []ytDlpThumbnail           `json:"thumbnails"`
	Formats           []ytDlpFormatInfo          `json:"formats"`
	Subtitles         map[string][]ytDlpSubtitle `json:"subtitles"`
	AutomaticCaptions map[string][]ytDlpSubtitle `json:"automatic_captions"`
}

type ytDlpThumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type ytDlpFormatInfo struct {
	FormatID       string  `json:"format_id"`
	FormatNote     string  `json:"format_note"`
	URL            string  `json:"url"`
	Ext            string  `json:"ext"`
	Protocol       string  `json:"protocol"`
	VideoCodec     string  `json:"vcodec"`
	AudioCodec     string  `json:"acodec"`
	Language       string  `json:"language"`
	Height         int     `json:"height"`
	AudioBitrate   float64 `json:"abr"`
	TotalBitrate   float64 `json:"tbr"`
	Filesize       float64 `json:"filesize"`
	FilesizeApprox float64 `json:"filesize_approx"`
	// LanguagePreference is highest for the audio track of the language
	// the video was made in, and next for the one YouTube plays by default.
	LanguagePreference *float64 `json:"language_preference"`
}

func (f ytDlpFormatInfo) hasVideo() bool {
	return f.VideoCodec != "" && f.VideoCodec != "none"
}

func (f ytDlpFormatInfo) hasAudio() bool {
	return f.AudioCodec != "" && f.AudioCodec != "none"
}

func (f ytDlpFormatInfo) size() int64 {
	size := f.Filesize
	if size <= 0 {
		size = f.FilesizeApprox
	}
	if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
		return 0
	}
	return int64(size)
}

func (f ytDlpFormatInfo) audioRate() float64 {
	if f.AudioBitrate > 0 {
		return f.AudioBitrate
	}
	return f.TotalBitrate
}

func parseYtDlpInfo(data []byte) (ytDlpInfo, error) {
	var info ytDlpInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return ytDlpInfo{}, fmt.Errorf("yt-dlpの出力を読み取れませんでした: %w", err)
	}
	return info, nil
}

// ytDlpQualities lists what to offer for one video, best first: MP4 streams
// that carry both video and audio, and WebM video streams to combine with the
// best WebM audio stream. Nothing taller than ytDlpMaxHeight is offered, and
// a height that is available as a single stream is not offered again as a
// combination, which has more ways to fail.
func ytDlpQualities(info ytDlpInfo) []downloadQuality {
	qualities := make([]downloadQuality, 0, len(info.Formats))
	seen := make(map[string]bool)
	singleStreamHeights := make(map[int]bool)
	addQuality := func(format ytDlpFormatInfo, audio *ytDlpFormatInfo) {
		if format.FormatID == "" || format.Height <= 0 || format.Height > ytDlpMaxHeight {
			return
		}
		if audio != nil && singleStreamHeights[format.Height] {
			return
		}
		container, extension := "MP4", ".mp4"
		if audio != nil {
			container, extension = "WebM", ".webm"
		}
		key := fmt.Sprintf("%d-%s", format.Height, container)
		if seen[key] {
			return
		}
		seen[key] = true
		quality := downloadQuality{
			label:       fmt.Sprintf("%dp (%s)", format.Height, container),
			extension:   extension,
			height:      format.Height,
			title:       info.Title,
			ytDlpFormat: format.FormatID,
			videoSize:   format.size(),
		}
		if audio == nil {
			singleStreamHeights[format.Height] = true
		} else {
			quality.ytDlpAudioFormat = audio.FormatID
			quality.audioSize = audio.size()
		}
		qualities = append(qualities, quality)
	}

	for _, format := range info.Formats {
		// HLS streams are MPEG-TS that only becomes a real MP4 after a pass
		// through ffmpeg, which the app does not require.
		if format.hasVideo() && format.hasAudio() && format.Ext == "mp4" && !strings.HasPrefix(format.Protocol, "m3u8") {
			addQuality(format, nil)
		}
	}
	// A dubbed video has an audio stream per language: the one to use
	// unless asked for others is that of the language the video was made in,
	// without compressed dynamic range.
	var bestAudio *ytDlpFormatInfo
	for i := range info.Formats {
		format := &info.Formats[i]
		if format.FormatID == "" || format.hasVideo() || !format.hasAudio() || format.Ext != "webm" {
			continue
		}
		if bestAudio == nil || ytDlpPreferredAudio(format, bestAudio) {
			bestAudio = format
		}
	}
	if bestAudio != nil {
		for _, format := range info.Formats {
			if format.hasVideo() && !format.hasAudio() && format.Ext == "webm" {
				addQuality(format, bestAudio)
			}
		}
	}

	sort.SliceStable(qualities, func(i, j int) bool {
		if qualities[i].height != qualities[j].height {
			return qualities[i].height > qualities[j].height
		}
		return qualities[i].extension < qualities[j].extension
	})
	// The quality selector identifies its items by itag; yt-dlp formats have
	// no such number, so give each one its position in the list.
	for i := range qualities {
		qualities[i].itag = i + 1
	}
	return qualities
}

// ytDlpPreferredAudio reports whether format is a better audio stream to
// use by default than current.
func ytDlpPreferredAudio(format, current *ytDlpFormatInfo) bool {
	preference := func(f *ytDlpFormatInfo) float64 {
		if f.LanguagePreference == nil {
			return 0
		}
		return *f.LanguagePreference
	}
	if a, b := preference(format), preference(current); a != b {
		return a > b
	}
	_, formatDRC := youTubeAudioTags(format.URL)
	_, currentDRC := youTubeAudioTags(current.URL)
	formatDRC = formatDRC || strings.HasSuffix(format.FormatID, "-drc")
	currentDRC = currentDRC || strings.HasSuffix(current.FormatID, "-drc")
	if formatDRC != currentDRC {
		return currentDRC
	}
	return format.audioRate() > current.audioRate()
}

// ytDlpThumbnailURL picks the largest thumbnail the app can decode.
func ytDlpThumbnailURL(info ytDlpInfo) string {
	decodable := func(url string) bool {
		path, _, _ := strings.Cut(url, "?")
		path = strings.ToLower(path)
		return strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") || strings.HasSuffix(path, ".png")
	}
	best := ""
	bestArea := -1
	for _, thumbnail := range info.Thumbnails {
		if !decodable(thumbnail.URL) {
			continue
		}
		if area := thumbnail.Width * thumbnail.Height; area > bestArea {
			best, bestArea = thumbnail.URL, area
		}
	}
	if best == "" && decodable(info.Thumbnail) {
		best = info.Thumbnail
	}
	return best
}

func getYtDlpDetails(ctx context.Context, runner ytDlpRunner, videoURL string) (videoInfo, error) {
	output, err := runner.Run(ctx, ytDlpInfoArgs(videoURL), nil)
	if err != nil {
		return videoInfo{}, err
	}
	info, err := parseYtDlpInfo(output)
	if err != nil {
		return videoInfo{}, err
	}
	var thumbnail image.Image
	if url := ytDlpThumbnailURL(info); url != "" {
		thumbnail = fetchThumbnail(ctx, url)
	}
	details := videoInfo{title: info.Title, thumbnail: thumbnail, captions: ytDlpCaptions(info.Subtitles, info.AutomaticCaptions), audio: ytDlpAudio(info.Formats)}
	qualities := ytDlpQualities(info)
	if len(qualities) == 0 {
		return details, fmt.Errorf("音声付きで取得できる動画形式が見つかりません")
	}
	details.qualities = qualities
	return details, nil
}

// parseYtDlpProgress reads one line written for ytDlpProgressTemplate. Other
// lines, and lines without a usable byte count, are reported as not ok. The
// total is zero when yt-dlp knows neither the size nor an estimate.
func parseYtDlpProgress(line string) (downloadProgress, bool) {
	fields, found := strings.CutPrefix(strings.TrimSpace(line), ytDlpProgressPrefix)
	if !found {
		return downloadProgress{}, false
	}
	parts := strings.Split(fields, "/")
	if len(parts) != 3 {
		return downloadProgress{}, false
	}
	parse := func(value string) (int64, bool) {
		number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(number) || number < 0 || number > math.MaxInt64/2 {
			return 0, false
		}
		return int64(number), true
	}
	downloaded, ok := parse(parts[0])
	if !ok {
		return downloadProgress{}, false
	}
	total, ok := parse(parts[1])
	if !ok || total == 0 {
		total, _ = parse(parts[2])
	}
	return downloadProgress{downloaded: downloaded, total: total}, true
}

// audioStream is a downloaded audio stream to combine with a video, and
// how to describe its track. Default marks the track to play; the muxers put
// the streams in the order given, the default one first.
type audioStream struct {
	Path     string
	Language string
	Name     string
	Default  bool
}

// streamMuxer combines a downloaded video stream with audio streams into
// outputPath. Only the picture of the video stream is used.
type streamMuxer func(ctx context.Context, videoPath string, audio []audioStream, outputPath string) error

// ytDlpRetryDelay is how long to wait before asking yt-dlp again for a
// stream YouTube refused.
var ytDlpRetryDelay = 5 * time.Second

// isYtDlpForbidden reports whether yt-dlp failed because YouTube answered
// with HTTP 403.
func isYtDlpForbidden(err error) bool {
	var exit *ytDlpExitError
	return errors.As(err, &exit) && strings.Contains(exit.stderr, "HTTP Error 403")
}

// ytDlpAudioFetch is an audio stream to download for a requested track.
type ytDlpAudioFetch struct {
	choice ytDlpAudioChoice
	stream audioStream
}

// ytDlpAudioFetches lists the audio streams to download for the requested
// tracks in the container of quality, and the names of the tracks that are
// not available in it. The first stream is the default one.
func ytDlpAudioFetches(quality downloadQuality, audio []requestedAudio) ([]ytDlpAudioFetch, []string) {
	var fetches []ytDlpAudioFetch
	var failed []string
	for _, request := range audio {
		choice, err := ytDlpAudioFormat(request.Track, quality.extension)
		if err != nil {
			failed = append(failed, request.Track.label())
			continue
		}
		fetches = append(fetches, ytDlpAudioFetch{choice: choice, stream: audioStream{
			Language: request.Track.Language,
			Name:     request.Track.label(),
		}})
	}
	if len(fetches) > 0 {
		// When the default track is not available, the next one plays.
		fetches[0].stream.Default = true
	}
	return fetches, failed
}

// downloadWithYtDlp downloads quality of the video into directory with the
// requested audio tracks, and embeds the requested caption tracks. It
// returns the path of the saved file and a warning naming the tracks that
// could not be added: the video is saved without them. Everything is first
// written to a temporary directory inside directory, which is removed
// however the download ends.
func downloadWithYtDlp(ctx context.Context, runner ytDlpRunner, directory, videoURL string, quality downloadQuality, audio []requestedAudio, captions []requestedCaption, mux streamMuxer, onProgress func(downloadProgress)) (string, string, error) {
	if quality.ytDlpFormat == "" {
		return "", "", fmt.Errorf("選択した画質は現在利用できません。画質を選び直してください")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", fmt.Errorf("ダウンロードフォルダを作成できません: %w", err)
	}
	workDir, err := os.MkdirTemp(directory, ".youtube2dvd-ytdlp-")
	if err != nil {
		return "", "", fmt.Errorf("一時フォルダを作成できません: %w", err)
	}
	defer os.RemoveAll(workDir)

	// fetch downloads one format. Progress is reported as offset plus the
	// bytes of this format, out of total; a zero total means the size that
	// yt-dlp reports for this format alone.
	fetch := func(formatID, name string, offset, total int64) (string, int64, error) {
		path := filepath.Join(workDir, name)
		run := func() error {
			_, err := runner.Run(ctx, ytDlpDownloadArgs(formatID, path, videoURL), func(line string) {
				progress, ok := parseYtDlpProgress(line)
				if !ok || onProgress == nil {
					return
				}
				progress.downloaded += offset
				if total > 0 {
					progress.total = total
					progress.downloaded = min(progress.downloaded, total)
				}
				onProgress(progress)
			})
			return err
		}
		err := run()
		if isYtDlpForbidden(err) {
			// YouTube refuses a stream now and then, more likely the more
			// streams a download has; it usually serves it a moment later.
			timer := time.NewTimer(ytDlpRetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", 0, ctx.Err()
			case <-timer.C:
			}
			_ = os.Remove(path)
			err = run()
		}
		if err != nil {
			return "", 0, err
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", 0, fmt.Errorf("yt-dlpが保存したファイルが見つかりません。yt-dlpの設定ファイルで出力を変更している場合は見直してください")
		}
		return path, info.Size(), nil
	}

	// The streams to download: the video, and the audio to combine with it,
	// if any. A single-file MP4 brings the audio YouTube plays by default;
	// other tracks are downloaded on their own and take its place. A WebM
	// quality always takes the requested tracks: its own audio stream is the
	// original language, which need not be the default one.
	type part struct {
		formatID, name string
		size           int64
		stream         *audioStream
	}
	parts := []part{{formatID: quality.ytDlpFormat, name: "video" + quality.extension, size: quality.videoSize}}
	var audioFailed []string
	if len(audio) > 0 && (quality.ytDlpAudioFormat != "" || !onlyYouTubeDefaultAudio(audio)) {
		var fetches []ytDlpAudioFetch
		fetches, audioFailed = ytDlpAudioFetches(quality, audio)
		for i, f := range fetches {
			ext := ".m4a"
			if quality.extension == ".webm" {
				ext = ".webm"
			}
			parts = append(parts, part{formatID: f.choice.FormatID, name: fmt.Sprintf("audio-%d%s", i+1, ext), size: f.choice.Size, stream: &f.stream})
		}
	}
	if len(parts) == 1 && quality.ytDlpAudioFormat != "" {
		parts = append(parts, part{formatID: quality.ytDlpAudioFormat, name: "audio.webm", size: quality.audioSize, stream: &audioStream{Default: true}})
	}

	// Report several streams as one download. Without every size, or for a
	// single stream, each reports the progress yt-dlp sees instead.
	var total int64
	if len(parts) > 1 {
		for _, p := range parts {
			if p.size <= 0 {
				total = 0
				break
			}
			total += p.size
		}
	}
	var downloadedPath string
	var streams []audioStream
	var offset, fetched int64
	for _, p := range parts {
		path, size, err := fetch(p.formatID, p.name, offset, total)
		if err != nil {
			return "", "", err
		}
		fetched += size
		if total > 0 {
			offset += p.size
		}
		if p.stream == nil {
			downloadedPath = path
			continue
		}
		stream := *p.stream
		stream.Path = path
		streams = append(streams, stream)
	}
	if len(streams) > 0 {
		if onProgress != nil {
			onProgress(downloadProgress{downloaded: fetched, total: fetched})
		}
		combinedPath := filepath.Join(workDir, "combined"+quality.extension)
		if err := mux(ctx, downloadedPath, streams, combinedPath); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", "", ctxErr
			}
			return "", "", fmt.Errorf("動画と音声を結合できませんでした: %w", err)
		}
		downloadedPath = combinedPath
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	var captionWarning string
	if len(captions) > 0 {
		tracks := make([]captionTrack, 0, len(captions))
		for _, caption := range captions {
			tracks = append(tracks, caption.Track)
		}
		captionWarning = addCaptions(ctx, downloadedPath, captions, fetchYtDlpCaptions(ctx, runner, workDir, tracks, videoURL))
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
	}

	outputPath := uniqueDownloadPath(directory, safeDownloadName(quality.title)+quality.extension)
	if err := os.Rename(downloadedPath, outputPath); err != nil {
		return "", "", fmt.Errorf("保存先へ移動できません: %w", err)
	}
	return outputPath, joinWarnings(audioWarning(audioFailed), captionWarning), nil
}

// videoDetails looks up a video with the downloader chosen in settings. When
// yt-dlp is chosen and cannot run, the lookup fails; it does not fall back to
// the built-in downloader.
func videoDetails(ctx context.Context, settings appSettings, videoURL string) (videoInfo, error) {
	if settings.UseYtDlp {
		info, err := getYtDlpDetails(ctx, execYtDlpRunner{path: settings.YtDlpPath}, videoURL)
		return info, explainBotCheck(err)
	}
	info, err := getYouTubeDetails(ctx, videoURL)
	return info, explainBotCheck(err)
}

// errYouTubeBotCheck reports that YouTube asked to sign in to confirm that
// the request does not come from a bot. YouTube does so for a while after
// many requests from one network, so the user is asked to wait.
var errYouTubeBotCheck = errors.New("YouTubeから、ボットではないことの確認を求められたため、動画を取得できませんでした。しばらく時間をおいてから、もう一度お試しください。")

// explainBotCheck replaces the bot check error of either downloader with
// errYouTubeBotCheck, and returns other errors as they are.
func explainBotCheck(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, youtube.ErrBotCheck) {
		return errYouTubeBotCheck
	}
	var exit *ytDlpExitError
	if errors.As(err, &exit) && strings.Contains(exit.stderr, "not a bot") {
		return errYouTubeBotCheck
	}
	return err
}

// downloadVideo downloads quality, the requested audio tracks and the
// requested captions with the downloader that listed them. See
// downloadWithYtDlp for the results.
func downloadVideo(ctx context.Context, settings appSettings, videoURL string, quality downloadQuality, audio []requestedAudio, captions []requestedCaption, onProgress func(downloadProgress)) (string, string, error) {
	if quality.ytDlpFormat == "" {
		// The built-in downloader offers no choice of audio.
		path, warning, err := downloadYouTubeVideo(ctx, videoURL, quality, captions, onProgress)
		return path, warning, explainBotCheck(err)
	}
	directory, err := downloadDirectory()
	if err != nil {
		return "", "", err
	}
	path, warning, err := downloadWithYtDlp(ctx, execYtDlpRunner{path: settings.YtDlpPath}, directory, videoURL, quality, audio, captions, muxerFor(quality.extension), onProgress)
	return path, warning, explainBotCheck(err)
}
