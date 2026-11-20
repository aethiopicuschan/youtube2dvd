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
	return append(ytDlpCommonArgs(), "-J", "--", videoURL)
}

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
	Title      string            `json:"title"`
	Thumbnail  string            `json:"thumbnail"`
	Thumbnails []ytDlpThumbnail  `json:"thumbnails"`
	Formats    []ytDlpFormatInfo `json:"formats"`
}

type ytDlpThumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type ytDlpFormatInfo struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	Protocol       string  `json:"protocol"`
	VideoCodec     string  `json:"vcodec"`
	AudioCodec     string  `json:"acodec"`
	Height         int     `json:"height"`
	AudioBitrate   float64 `json:"abr"`
	TotalBitrate   float64 `json:"tbr"`
	Filesize       float64 `json:"filesize"`
	FilesizeApprox float64 `json:"filesize_approx"`
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
	var bestAudio *ytDlpFormatInfo
	for i := range info.Formats {
		format := &info.Formats[i]
		if format.FormatID == "" || format.hasVideo() || !format.hasAudio() || format.Ext != "webm" {
			continue
		}
		if bestAudio == nil || format.audioRate() > bestAudio.audioRate() {
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

func getYtDlpDetails(ctx context.Context, runner ytDlpRunner, videoURL string) (string, image.Image, []downloadQuality, error) {
	output, err := runner.Run(ctx, ytDlpInfoArgs(videoURL), nil)
	if err != nil {
		return "", nil, nil, err
	}
	info, err := parseYtDlpInfo(output)
	if err != nil {
		return "", nil, nil, err
	}
	var thumbnail image.Image
	if url := ytDlpThumbnailURL(info); url != "" {
		thumbnail = fetchThumbnail(ctx, url)
	}
	qualities := ytDlpQualities(info)
	if len(qualities) == 0 {
		return info.Title, thumbnail, nil, fmt.Errorf("音声付きで取得できる動画形式が見つかりません")
	}
	return info.Title, thumbnail, qualities, nil
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

// streamMuxer combines separately downloaded video and audio into outputPath.
type streamMuxer func(ctx context.Context, videoPath, audioPath, outputPath string) error

// downloadWithYtDlp downloads quality of the video into directory and returns
// the path of the saved file. Everything is first written to a temporary
// directory inside directory, which is removed however the download ends.
func downloadWithYtDlp(ctx context.Context, runner ytDlpRunner, directory, videoURL string, quality downloadQuality, mux streamMuxer, onProgress func(downloadProgress)) (string, error) {
	if quality.ytDlpFormat == "" {
		return "", fmt.Errorf("選択した画質は現在利用できません。画質を選び直してください")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("ダウンロードフォルダを作成できません: %w", err)
	}
	workDir, err := os.MkdirTemp(directory, ".youtube2dvd-ytdlp-")
	if err != nil {
		return "", fmt.Errorf("一時フォルダを作成できません: %w", err)
	}
	defer os.RemoveAll(workDir)

	// fetch downloads one format. Progress is reported as offset plus the
	// bytes of this format, out of total; a zero total means the size that
	// yt-dlp reports for this format alone.
	fetch := func(formatID, name string, offset, total int64) (string, int64, error) {
		path := filepath.Join(workDir, name)
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
		if err != nil {
			return "", 0, err
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", 0, fmt.Errorf("yt-dlpが保存したファイルが見つかりません。yt-dlpの設定ファイルで出力を変更している場合は見直してください")
		}
		return path, info.Size(), nil
	}

	var downloadedPath string
	if quality.ytDlpAudioFormat == "" {
		downloadedPath, _, err = fetch(quality.ytDlpFormat, "video"+quality.extension, 0, 0)
		if err != nil {
			return "", err
		}
	} else {
		// Report both streams as one download. Without both sizes each
		// stream reports its own progress instead.
		var total int64
		if quality.videoSize > 0 && quality.audioSize > 0 {
			total = quality.videoSize + quality.audioSize
		}
		videoPath, videoSize, err := fetch(quality.ytDlpFormat, "video.webm", 0, total)
		if err != nil {
			return "", err
		}
		var audioOffset int64
		if total > 0 {
			audioOffset = quality.videoSize
		}
		audioPath, audioSize, err := fetch(quality.ytDlpAudioFormat, "audio.webm", audioOffset, total)
		if err != nil {
			return "", err
		}
		if onProgress != nil {
			onProgress(downloadProgress{downloaded: videoSize + audioSize, total: videoSize + audioSize})
		}
		downloadedPath = filepath.Join(workDir, "combined.webm")
		if err := mux(ctx, videoPath, audioPath, downloadedPath); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			return "", fmt.Errorf("動画と音声を結合できませんでした: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	outputPath := uniqueDownloadPath(directory, safeDownloadName(quality.title)+quality.extension)
	if err := os.Rename(downloadedPath, outputPath); err != nil {
		return "", fmt.Errorf("保存先へ移動できません: %w", err)
	}
	return outputPath, nil
}

// videoDetails looks up a video with the downloader chosen in settings. When
// yt-dlp is chosen and cannot run, the lookup fails; it does not fall back to
// the built-in downloader.
func videoDetails(ctx context.Context, settings appSettings, videoURL string) (string, image.Image, []downloadQuality, error) {
	if settings.UseYtDlp {
		return getYtDlpDetails(ctx, execYtDlpRunner{path: settings.YtDlpPath}, videoURL)
	}
	return getYouTubeDetails(ctx, videoURL)
}

// downloadVideo downloads quality with the downloader that listed it.
func downloadVideo(ctx context.Context, settings appSettings, videoURL string, quality downloadQuality, onProgress func(downloadProgress)) (string, error) {
	if quality.ytDlpFormat == "" {
		return downloadYouTubeVideo(ctx, videoURL, quality, onProgress)
	}
	directory, err := downloadDirectory()
	if err != nil {
		return "", err
	}
	mux := func(ctx context.Context, videoPath, audioPath, outputPath string) error {
		return muxDownloadedStreams(ctx, videoPath, audioPath, outputPath, ".webm", "video/webm")
	}
	return downloadWithYtDlp(ctx, execYtDlpRunner{path: settings.YtDlpPath}, directory, videoURL, quality, mux, onProgress)
}
