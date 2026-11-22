package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kkdai/youtube/v2"
)

var youtubeClientMu sync.Mutex

// kkdai/youtube exposes its selected Innertube client as a package global.
// Keep profile changes serialized for metadata lookup and stream downloads.
func withYouTubeClient(info youtube.ClientInfo, action func() error) error {
	youtubeClientMu.Lock()
	defer youtubeClientMu.Unlock()

	previous := youtube.DefaultClient
	youtube.DefaultClient = info
	defer func() { youtube.DefaultClient = previous }()
	return action()
}

// downloadYouTubeVideo downloads quality of the video and embeds the
// requested caption tracks. It returns the saved path and, when captions
// could not be added, a warning: the video is saved without them.
func downloadYouTubeVideo(ctx context.Context, videoURL string, quality downloadQuality, captions []requestedCaption, onProgress func(downloadProgress)) (string, string, error) {
	var outputPath, warning string
	err := withYouTubeClient(youtube.AndroidClient, func() error {
		var err error
		outputPath, warning, err = downloadYouTubeVideoWithCurrentClient(ctx, videoURL, quality, captions, onProgress)
		return err
	})
	return outputPath, warning, err
}

func downloadYouTubeVideoWithCurrentClient(ctx context.Context, videoURL string, quality downloadQuality, captions []requestedCaption, onProgress func(downloadProgress)) (string, string, error) {
	client := youtube.Client{}
	video, err := client.GetVideoContext(ctx, videoURL)
	if err != nil {
		return "", "", fmt.Errorf("動画情報を取得できませんでした: %w", err)
	}
	var format *youtube.Format
	for i := range video.Formats {
		if video.Formats[i].ItagNo == quality.itag {
			format = &video.Formats[i]
			break
		}
	}
	if format == nil {
		return "", "", fmt.Errorf("選択した画質は現在利用できません。画質を選び直してください")
	}
	ext := quality.extension
	if ext == "" {
		ext = downloadExtension(format)
	}

	directory, err := downloadDirectory()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", fmt.Errorf("ダウンロードフォルダを作成できません: %w", err)
	}
	name := safeDownloadName(video.Title) + ext
	outputPath := uniqueDownloadPath(directory, name)

	// The extension tells the subtitle step which container it holds.
	tmp, err := os.CreateTemp(directory, ".youtube-download-*.part"+ext)
	if err != nil {
		return "", "", fmt.Errorf("一時ファイルを作成できません: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", fmt.Errorf("一時ファイルを閉じられません: %w", err)
	}
	defer os.Remove(tmpPath)
	if err := downloadFormatToPath(ctx, client, video, format, tmpPath, onProgress); err != nil {
		return "", "", err
	}
	var warning string
	if len(captions) > 0 {
		warning = addCaptions(ctx, tmpPath, captions, func(track captionTrack) ([]byte, error) {
			return fetchBuiltinCaptionVTT(ctx, video, track.ID)
		})
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return "", "", fmt.Errorf("保存先へ移動できません: %w", err)
	}
	return outputPath, warning, nil
}

func downloadableFormats(formats youtube.FormatList) youtube.FormatList {
	return formats.Type("video/mp4").WithAudioChannels()
}

func downloadFormatToPath(ctx context.Context, client youtube.Client, video *youtube.Video, format *youtube.Format, path string, onProgress func(downloadProgress)) error {
	for attempt := range 2 {
		err := downloadFormatAttempt(ctx, client, video, format, path, onProgress)
		if err == nil {
			return nil
		}
		if attempt > 0 || !isYouTubeForbidden(err) {
			return err
		}

		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func downloadFormatAttempt(ctx context.Context, client youtube.Client, video *youtube.Video, format *youtube.Format, path string, onProgress func(downloadProgress)) error {
	stream, total, err := client.GetStreamContext(ctx, video, format)
	if err != nil {
		return fmt.Errorf("動画ストリームを取得できませんでした: %w", err)
	}
	defer stream.Close()
	if onProgress != nil {
		onProgress(downloadProgress{total: total})
	}
	dst, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("一時動画ファイルを作成できません: %w", err)
	}
	progressStream := &downloadProgressWriter{writer: dst, total: total, onProgress: onProgress}
	_, copyErr := io.Copy(progressStream, stream)
	closeErr := dst.Close()
	if copyErr != nil {
		return fmt.Errorf("動画ストリームを保存できませんでした: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("動画ファイルを閉じられません: %w", closeErr)
	}
	return nil
}

func isYouTubeForbidden(err error) bool {
	var status youtube.ErrUnexpectedStatusCode
	return errors.As(err, &status) && status == youtube.ErrUnexpectedStatusCode(http.StatusForbidden)
}

func downloadExtension(format *youtube.Format) string {
	if strings.Contains(format.MimeType, "video/webm") {
		return ".webm"
	}
	return ".mp4"
}

type downloadProgressWriter struct {
	writer     io.Writer
	written    int64
	total      int64
	onProgress func(downloadProgress)
}

func (w *downloadProgressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.written += int64(n)
	if w.onProgress != nil {
		w.onProgress(downloadProgress{downloaded: w.written, total: w.total})
	}
	return n, err
}

type downloadQuality struct {
	itag      int
	label     string
	extension string
	height    int

	// The rest is set for a quality listed by yt-dlp: the title to save the
	// video under, the format to download, the separate audio format to
	// combine with it if any, and their sizes in bytes where known.
	title            string
	ytDlpFormat      string
	ytDlpAudioFormat string
	videoSize        int64
	audioSize        int64
}

// videoInfo is what the app shows about a video before downloading it.
type videoInfo struct {
	title     string
	thumbnail image.Image
	qualities []downloadQuality
	captions  videoCaptions
	audio     videoAudio
	// otherAudio is set when the video has audio in other languages that
	// the downloader cannot fetch, so that the user can be told.
	otherAudio bool
}

type qualityLookupResult struct {
	requestID uint64
	videoURL  string
	info      videoInfo
	err       error
}

func getYouTubeDetails(ctx context.Context, videoURL string) (videoInfo, error) {
	var video *youtube.Video
	err := withYouTubeClient(youtube.AndroidClient, func() error {
		client := youtube.Client{}
		var err error
		video, err = client.GetVideoContext(ctx, videoURL)
		return err
	})
	if err != nil {
		return videoInfo{}, fmt.Errorf("動画情報を取得できませんでした: %w", err)
	}
	var thumbnail image.Image
	if len(video.Thumbnails) > 0 {
		thumb := video.Thumbnails[0]
		for _, candidate := range video.Thumbnails[1:] {
			if uint64(candidate.Width)*uint64(candidate.Height) > uint64(thumb.Width)*uint64(thumb.Height) {
				thumb = candidate
			}
		}
		thumbnail = fetchThumbnail(ctx, thumb.URL)
	}
	qualities := make([]downloadQuality, 0, len(video.Formats))
	seen := make(map[string]bool)
	addQuality := func(format youtube.Format) {
		if format.URL == "" && format.Cipher == "" {
			return
		}
		container := "MP4"
		extension := downloadExtension(&format)
		if extension == ".webm" {
			container = "WebM"
		}
		label := format.QualityLabel
		if label == "" && format.Height > 0 {
			label = fmt.Sprintf("%dp", format.Height)
		}
		key := fmt.Sprintf("%d-%s", format.Height, container)
		if label == "" || seen[key] {
			return
		}
		seen[key] = true
		qualities = append(qualities, downloadQuality{
			itag:      format.ItagNo,
			label:     fmt.Sprintf("%s (%s)", label, container),
			extension: extension,
			height:    format.Height,
		})
	}
	for _, format := range downloadableFormats(video.Formats) {
		addQuality(format)
	}
	sort.SliceStable(qualities, func(i, j int) bool {
		if qualities[i].height != qualities[j].height {
			return qualities[i].height > qualities[j].height
		}
		return qualities[i].extension < qualities[j].extension
	})
	info := videoInfo{title: video.Title, thumbnail: thumbnail, captions: builtinCaptions(video.CaptionTracks), otherAudio: builtinHasOtherAudio(video.Formats)}
	if len(qualities) == 0 {
		return info, fmt.Errorf("音声付きで取得できる動画形式が見つかりません")
	}
	info.qualities = qualities
	return info, nil
}

// fetchThumbnail downloads and decodes a thumbnail. It returns nil when the
// image cannot be fetched or is in a format the app does not decode.
func fetchThumbnail(ctx context.Context, url string) image.Image {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	thumbnail, _, _ := image.Decode(response.Body)
	return thumbnail
}

func downloadDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリを取得できません: %w", err)
	}
	downloadsDir := filepath.Join(home, "Downloads")
	if runtime.GOOS == "linux" {
		configured := false
		if dir := os.Getenv("XDG_DOWNLOAD_DIR"); dir != "" {
			dir = strings.TrimSpace(dir)
			dir = strings.Trim(dir, `"`)
			dir = strings.ReplaceAll(dir, "$HOME", home)
			if filepath.IsAbs(dir) {
				downloadsDir = filepath.Clean(dir)
				configured = true
			}
		}
		if !configured {
			configPath := filepath.Join(home, ".config", "user-dirs.dirs")
			if data, err := os.ReadFile(configPath); err == nil {
				for line := range strings.SplitSeq(string(data), "\n") {
					key, value, ok := strings.Cut(line, "=")
					if ok && strings.TrimSpace(key) == "XDG_DOWNLOAD_DIR" {
						value = strings.Trim(strings.TrimSpace(value), `"`)
						value = strings.ReplaceAll(value, "$HOME", home)
						if filepath.IsAbs(value) {
							downloadsDir = filepath.Clean(value)
							break
						}
					}
				}
			}
		}
	}
	return filepath.Join(downloadsDir, "youtube2dvd"), nil
}

func safeDownloadName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\\|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	name = strings.Trim(strings.TrimSpace(b.String()), ".")
	if name == "" || name == "." || name == ".." {
		return "youtube-video"
	}
	return name
}

func uniqueDownloadPath(directory, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidateName := name
		if i > 0 {
			candidateName = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		candidate := filepath.Join(directory, candidateName)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
