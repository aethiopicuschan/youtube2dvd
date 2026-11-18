package main

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/gravity-zero/mkvgo/matroska"
	"github.com/gravity-zero/mkvgo/mkv"
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

func newYouTubeClient(visionOS bool) youtube.Client {
	if !visionOS {
		return youtube.Client{}
	}
	return youtube.Client{HTTPClient: &http.Client{
		Transport: visionOSContextTransport{base: http.DefaultTransport},
	}}
}

func visionOSYouTubeClientInfo() youtube.ClientInfo {
	return youtube.ClientInfo{
		Name:        "VISIONOS",
		Version:     "1.02",
		Key:         youtube.EmbeddedClient.Key,
		DeviceModel: "RealityDevice17,1",
		UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15",
	}
}

type visionOSContextTransport struct {
	base http.RoundTripper
}

func (t visionOSContextTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.URL.Host != "www.youtube.com" || request.URL.Path != "/youtubei/v1/player" || request.Body == nil {
		return t.base.RoundTrip(request)
	}

	body, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var requestContext map[string]json.RawMessage
	if err := json.Unmarshal(payload["context"], &requestContext); err != nil {
		return nil, err
	}
	var clientContext map[string]json.RawMessage
	if err := json.Unmarshal(requestContext["client"], &clientContext); err != nil {
		return nil, err
	}
	for key, value := range map[string]string{
		"clientName":    "VISIONOS",
		"clientVersion": "1.02",
		"deviceMake":    "Apple",
		"deviceModel":   "RealityDevice17,1",
		"osName":        "visionOS",
		"osVersion":     "26.5.23O471",
		"userAgent":     "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15",
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		clientContext[key] = encoded
	}
	encodedClient, err := json.Marshal(clientContext)
	if err != nil {
		return nil, err
	}
	requestContext["client"] = encodedClient
	encodedContext, err := json.Marshal(requestContext)
	if err != nil {
		return nil, err
	}
	payload["context"] = encodedContext
	encodedBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	forwarded := request.Clone(request.Context())
	forwarded.Body = io.NopCloser(bytes.NewReader(encodedBody))
	forwarded.ContentLength = int64(len(encodedBody))
	forwarded.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(encodedBody)), nil
	}
	forwarded.Header = request.Header.Clone()
	forwarded.Header.Del("Content-Length")
	forwarded.Header.Set("X-YouTube-Client-Name", "101")
	return t.base.RoundTrip(forwarded)
}

func downloadYouTubeVideo(ctx context.Context, videoURL string, quality downloadQuality, onProgress func(downloadProgress)) (string, error) {
	clientInfo := youtube.AndroidClient
	if quality.visionOSClient {
		clientInfo = visionOSYouTubeClientInfo()
	}
	var outputPath string
	err := withYouTubeClient(clientInfo, func() error {
		var err error
		outputPath, err = downloadYouTubeVideoWithCurrentClient(ctx, videoURL, quality, onProgress)
		return err
	})
	return outputPath, err
}

func downloadYouTubeVideoWithCurrentClient(ctx context.Context, videoURL string, quality downloadQuality, onProgress func(downloadProgress)) (string, error) {
	client := newYouTubeClient(quality.visionOSClient)
	video, err := client.GetVideoContext(ctx, videoURL)
	if err != nil {
		return "", fmt.Errorf("動画情報を取得できませんでした: %w", err)
	}
	var format *youtube.Format
	for i := range video.Formats {
		if video.Formats[i].ItagNo == quality.itag {
			format = &video.Formats[i]
			break
		}
	}
	if format == nil {
		return "", fmt.Errorf("選択した画質は現在利用できません。画質を選び直してください")
	}
	ext := quality.extension
	if ext == "" {
		ext = downloadExtension(format)
	}

	directory, err := downloadDirectory()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("ダウンロードフォルダを作成できません: %w", err)
	}
	name := safeDownloadName(video.Title) + ext
	outputPath := uniqueDownloadPath(directory, name)

	tmp, err := os.CreateTemp(directory, ".youtube-download-*.part")
	if err != nil {
		return "", fmt.Errorf("一時ファイルを作成できません: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("一時ファイルを閉じられません: %w", err)
	}
	defer os.Remove(tmpPath)
	if quality.audioItag == 0 {
		if err := downloadFormatToPath(ctx, client, video, format, tmpPath, 0, 0, onProgress); err != nil {
			return "", err
		}
	} else {
		var audioFormat *youtube.Format
		for i := range video.Formats {
			if video.Formats[i].ItagNo == quality.audioItag {
				audioFormat = &video.Formats[i]
				break
			}
		}
		if audioFormat == nil {
			return "", fmt.Errorf("選択した音声形式は現在利用できません。確認し直してください")
		}
		totalSize := quality.videoSize + quality.audioSize
		videoPath, videoSize, err := downloadFormatToTemp(ctx, directory, client, video, format, 0, totalSize, onProgress)
		if err != nil {
			return "", err
		}
		defer os.Remove(videoPath)
		audioPath, audioSize, err := downloadFormatToTemp(ctx, directory, client, video, audioFormat, quality.videoSize, totalSize, onProgress)
		if err != nil {
			return "", err
		}
		defer os.Remove(audioPath)
		if onProgress != nil {
			onProgress(downloadProgress{downloaded: videoSize, total: videoSize + audioSize})
		}
		if err := muxDownloadedStreams(ctx, videoPath, audioPath, tmpPath, ext, format.MimeType); err != nil {
			return "", fmt.Errorf("動画と音声を結合できませんでした: %w", err)
		}
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return "", fmt.Errorf("保存先へ移動できません: %w", err)
	}
	return outputPath, nil
}

func downloadableFormats(formats youtube.FormatList) youtube.FormatList {
	return formats.Type("video/mp4").WithAudioChannels()
}

func bestAudioFormat(formats youtube.FormatList, mimeType string) *youtube.Format {
	audio := formats.Type(mimeType)
	downloadable := make(youtube.FormatList, 0, len(audio))
	for _, format := range audio {
		if format.URL != "" || format.Cipher != "" {
			downloadable = append(downloadable, format)
		}
	}
	if len(downloadable) == 0 {
		return nil
	}
	downloadable.Sort()
	return &downloadable[0]
}

func downloadFormatToTemp(ctx context.Context, directory string, client youtube.Client, video *youtube.Video, format *youtube.Format, offset, total int64, onProgress func(downloadProgress)) (string, int64, error) {
	tmp, err := os.CreateTemp(directory, ".youtube-stream-*.part")
	if err != nil {
		return "", 0, fmt.Errorf("一時ストリームファイルを作成できません: %w", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", 0, fmt.Errorf("一時ストリームファイルを閉じられません: %w", err)
	}
	if err := downloadFormatToPath(ctx, client, video, format, path, offset, total, onProgress); err != nil {
		_ = os.Remove(path)
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return "", 0, err
	}
	return path, info.Size(), nil
}

func downloadFormatToPath(ctx context.Context, client youtube.Client, video *youtube.Video, format *youtube.Format, path string, offset, total int64, onProgress func(downloadProgress)) error {
	for attempt := range 2 {
		err := downloadFormatAttempt(ctx, client, video, format, path, offset, total, onProgress)
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

func downloadFormatAttempt(ctx context.Context, client youtube.Client, video *youtube.Video, format *youtube.Format, path string, offset, total int64, onProgress func(downloadProgress)) error {
	stream, size, err := client.GetStreamContext(ctx, video, format)
	if err != nil {
		return fmt.Errorf("動画ストリームを取得できませんでした: %w", err)
	}
	defer stream.Close()
	if total == 0 {
		total = size
	}
	if onProgress != nil {
		onProgress(downloadProgress{downloaded: offset, total: total})
	}
	dst, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("一時動画ファイルを作成できません: %w", err)
	}
	progressStream := &downloadProgressWriter{writer: dst, total: total, offset: offset, onProgress: onProgress}
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
	offset     int64
	onProgress func(downloadProgress)
}

func (w *downloadProgressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.written += int64(n)
	if w.onProgress != nil {
		w.onProgress(downloadProgress{downloaded: w.offset + w.written, total: w.total})
	}
	return n, err
}

type downloadQuality struct {
	itag           int
	audioItag      int
	label          string
	extension      string
	height         int
	videoSize      int64
	audioSize      int64
	visionOSClient bool
}

type qualityLookupResult struct {
	requestID uint64
	videoURL  string
	title     string
	thumbnail image.Image
	qualities []downloadQuality
	err       error
}

func getYouTubeDetails(ctx context.Context, videoURL string) (string, image.Image, []downloadQuality, error) {
	var video *youtube.Video
	err := withYouTubeClient(youtube.AndroidClient, func() error {
		client := newYouTubeClient(false)
		var err error
		video, err = client.GetVideoContext(ctx, videoURL)
		return err
	})
	if err != nil {
		return "", nil, nil, fmt.Errorf("動画情報を取得できませんでした: %w", err)
	}
	var thumbnail image.Image
	if len(video.Thumbnails) > 0 {
		thumb := video.Thumbnails[0]
		for _, candidate := range video.Thumbnails[1:] {
			if uint64(candidate.Width)*uint64(candidate.Height) > uint64(thumb.Width)*uint64(thumb.Height) {
				thumb = candidate
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, thumb.URL, nil)
		if err == nil {
			response, requestErr := http.DefaultClient.Do(req)
			if requestErr == nil {
				defer response.Body.Close()
				if response.StatusCode == http.StatusOK {
					thumbnail, _, _ = image.Decode(response.Body)
				}
			}
		}
	}
	qualities := make([]downloadQuality, 0, len(video.Formats))
	seen := make(map[string]bool)
	addQuality := func(format youtube.Format, formats youtube.FormatList, audioItag int, visionOSClient bool) {
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
			itag:           format.ItagNo,
			audioItag:      audioItag,
			label:          fmt.Sprintf("%s (%s)", label, container),
			extension:      extension,
			height:         format.Height,
			videoSize:      format.ContentLength,
			visionOSClient: visionOSClient,
		})
		if audioItag != 0 {
			for _, audioFormat := range formats {
				if audioFormat.ItagNo == audioItag {
					qualities[len(qualities)-1].audioSize = audioFormat.ContentLength
					break
				}
			}
		}
	}
	for _, format := range downloadableFormats(video.Formats) {
		addQuality(format, video.Formats, 0, false)
	}
	// Android often exposes only the 360p MP4 progressive stream. The visionOS
	// profile also exposes direct WebM URLs for adaptive high-resolution streams.
	var visionOSVideo *youtube.Video
	visionOSErr := withYouTubeClient(visionOSYouTubeClientInfo(), func() error {
		client := newYouTubeClient(true)
		var err error
		visionOSVideo, err = client.GetVideoContext(ctx, videoURL)
		return err
	})
	if visionOSErr != nil && ctx.Err() != nil {
		return video.Title, thumbnail, qualities, ctx.Err()
	}
	if visionOSErr == nil && visionOSVideo != nil {
		visionOSFormats := visionOSVideo.Formats
		for _, format := range visionOSFormats.Type("video/webm").WithAudioChannels() {
			addQuality(format, visionOSFormats, 0, true)
		}
		if audio := bestAudioFormat(visionOSFormats, "audio/webm"); audio != nil {
			for _, format := range visionOSFormats.Type("video/webm").AudioChannels(0) {
				addQuality(format, visionOSFormats, audio.ItagNo, true)
			}
		}
	}
	sort.SliceStable(qualities, func(i, j int) bool {
		if qualities[i].height != qualities[j].height {
			return qualities[i].height > qualities[j].height
		}
		return qualities[i].extension < qualities[j].extension
	})
	if len(qualities) == 0 {
		return video.Title, thumbnail, nil, fmt.Errorf("音声と合わせて取得できる動画形式が見つかりません")
	}
	return video.Title, thumbnail, qualities, nil
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
