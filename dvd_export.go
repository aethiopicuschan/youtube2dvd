package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aethiopicuschan/video2dvd"
)

type dvdExportResult struct {
	path        string
	err         error
	burned      bool
	burnFailure bool
}

type dvdExportProgressUpdate struct {
	message         string
	convertedBytes  int64
	estimatedBytes  int64
	hasProgress     bool
	progressPercent int
	hasPercent      bool
}

type dvdExportProgressReporter func(dvdExportProgressUpdate)

type dvdExportETAEstimator struct {
	startedAt      time.Time
	lastProgressAt time.Time
	smoothedAt     time.Time
	lastBytes      int64
	totalBytes     int64
	bytesPerSecond float64
	remainingSecs  float64
}

func (e *dvdExportETAEstimator) Reset() {
	*e = dvdExportETAEstimator{}
}

func (e *dvdExportETAEstimator) Observe(now time.Time, convertedBytes, estimatedBytes int64) {
	if convertedBytes < 0 || estimatedBytes <= 0 {
		return
	}
	if e.startedAt.IsZero() {
		e.startedAt = now
		e.lastProgressAt = now
		e.lastBytes = convertedBytes
		e.totalBytes = estimatedBytes
		return
	}
	e.totalBytes = estimatedBytes
	delta := convertedBytes - e.lastBytes
	if delta <= 0 {
		if delta < 0 {
			e.lastBytes = convertedBytes
			e.lastProgressAt = now
			e.bytesPerSecond = 0
			e.remainingSecs = 0
			e.smoothedAt = time.Time{}
		}
		return
	}

	elapsed := now.Sub(e.lastProgressAt).Seconds()
	if elapsed < 0.2 {
		return
	}
	observedRate := float64(delta) / elapsed
	if e.bytesPerSecond == 0 {
		e.bytesPerSecond = observedRate
	} else {
		const rateSmoothingWindow = 10.0
		alpha := 1 - math.Exp(-math.Min(elapsed, 2)/rateSmoothingWindow)
		e.bytesPerSecond += alpha * (observedRate - e.bytesPerSecond)
	}
	e.lastBytes = convertedBytes
	e.lastProgressAt = now
}

func (e *dvdExportETAEstimator) Label(now time.Time) string {
	if e.startedAt.IsZero() || e.totalBytes <= 0 || e.bytesPerSecond <= 0 || now.Sub(e.startedAt) < 5*time.Second {
		return ""
	}
	if float64(e.lastBytes)/float64(e.totalBytes) < 0.05 {
		return ""
	}
	if now.Sub(e.lastProgressAt) > 10*time.Second {
		return dvdExportETASecondsLabel(e.remainingSecs)
	}
	predictedBytes := e.lastBytes + int64(e.bytesPerSecond*now.Sub(e.lastProgressAt).Seconds())
	if predictedBytes >= e.totalBytes {
		return "残り時間の目安：まもなく完了"
	}
	rawRemaining := float64(e.totalBytes-predictedBytes) / e.bytesPerSecond
	if e.remainingSecs <= 0 || e.smoothedAt.IsZero() {
		e.remainingSecs = rawRemaining
	} else {
		elapsed := now.Sub(e.smoothedAt).Seconds()
		const etaSmoothingWindow = 6.0
		alpha := 1 - math.Exp(-elapsed/etaSmoothingWindow)
		e.remainingSecs += alpha * (rawRemaining - e.remainingSecs)
	}
	e.smoothedAt = now
	return dvdExportETASecondsLabel(e.remainingSecs)
}

func dvdExportETASecondsLabel(remaining float64) string {
	if remaining <= 0 {
		return ""
	}
	step := 5.0
	if remaining >= 3600 {
		step = 60
	} else if remaining >= 600 {
		step = 30
	} else if remaining >= 60 {
		step = 10
	}
	seconds := int(math.Ceil(remaining/step) * step)
	if seconds >= 3600 {
		return fmt.Sprintf("残り時間の目安：約%d時間%d分", seconds/3600, (seconds%3600)/60)
	}
	if seconds >= 60 {
		if seconds%60 == 0 {
			return fmt.Sprintf("残り時間の目安：約%d分", seconds/60)
		}
		return fmt.Sprintf("残り時間の目安：約%d分%d秒", seconds/60, seconds%60)
	}
	return fmt.Sprintf("残り時間の目安：約%d秒", seconds)
}

type dvdExportEncoder struct {
	encoder        *video2dvd.InProcessEncoder
	titleNames     []string
	progress       dvdExportProgressReporter
	count          int
	total          int
	estimate       int64
	completedBytes int64
	mu             sync.Mutex
}

func (e *dvdExportEncoder) report(message string, convertedBytes int64) {
	if e.progress == nil {
		return
	}
	if e.estimate > 0 {
		message = fmt.Sprintf("%s（変換済み %s / ISO概算 %s）", message, formatBytes(convertedBytes), formatBytes(e.estimate))
	} else {
		message = fmt.Sprintf("%s（変換済み %s / ISO概算を計算できません）", message, formatBytes(convertedBytes))
	}
	e.progress(dvdExportProgressUpdate{
		message:        message,
		convertedBytes: convertedBytes,
		estimatedBytes: e.estimate,
		hasProgress:    true,
	})
}

func (e *dvdExportEncoder) currentConvertedBytes(workDir string) int64 {
	e.mu.Lock()
	completed := e.completedBytes
	e.mu.Unlock()
	if workDir == "" {
		return completed
	}
	return completed + dvdExportGeneratedBytes(workDir)
}

func (e *dvdExportEncoder) Encode(ctx context.Context, inputPath, workDir string, profile video2dvd.Profile) (video2dvd.EncodedTitle, error) {
	return e.encode(workDir, inputPath, func() (video2dvd.EncodedTitle, error) {
		return e.encoder.Encode(ctx, inputPath, workDir, profile)
	})
}

func (e *dvdExportEncoder) EncodeStreams(ctx context.Context, input video2dvd.MediaInput, workDir string, profile video2dvd.Profile) (video2dvd.EncodedTitle, error) {
	return e.encode(workDir, input.VideoPath, func() (video2dvd.EncodedTitle, error) {
		return e.encoder.EncodeStreams(ctx, input, workDir, profile)
	})
}

func (e *dvdExportEncoder) encode(workDir, inputPath string, encode func() (video2dvd.EncodedTitle, error)) (video2dvd.EncodedTitle, error) {
	e.mu.Lock()
	e.count++
	index := e.count
	// The converter calls the encoder once per input, in queue order.
	titleName := dvdMenuVideoName("", index)
	if index <= len(e.titleNames) {
		titleName = e.titleNames[index-1]
	}
	e.mu.Unlock()
	videoLabel := fmt.Sprintf("動画 %d/%d", index, e.total)
	e.report(videoLabel+" を変換しています…", e.currentConvertedBytes(workDir))

	stop := make(chan struct{})
	monitorDone := make(chan struct{})
	monitorStopped := false
	stopMonitor := func() {
		if monitorStopped {
			return
		}
		close(stop)
		<-monitorDone
		monitorStopped = true
	}
	defer stopMonitor()
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		var lastSize int64 = -1
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				size := e.currentConvertedBytes(workDir)
				if size == lastSize {
					continue
				}
				lastSize = size
				e.report(videoLabel+" を変換しています…", size)
			}
		}
	}()
	title, err := encode()
	stopMonitor()
	if err == nil {
		// Without a name the converter would label the title after the input
		// path, which is a temporary "title-NN" link for a video queued twice.
		title.Name = titleName
		e.mu.Lock()
		e.completedBytes += dvdExportGeneratedBytes(workDir)
		completed := e.completedBytes
		e.mu.Unlock()
		e.report(videoLabel+" の変換が完了しました。", completed)
	}
	return title, err
}

// dvdMenuVideoName returns the label of a queued video in the DVD title menu:
// its file name without the extension. index is the one-based queue position,
// used when the file name leaves nothing to show.
func dvdMenuVideoName(path string, index int) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	// The menu draws one "?" for every character it has no glyph for, so drop
	// the invisible ones and keep the rest as written.
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Variation_Selector, r):
			return -1
		case unicode.IsControl(r):
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return fmt.Sprintf("動画 %d", index)
	}
	return name
}

type dvdExportAuthorer struct {
	authorer video2dvd.Authorer
	encoder  *dvdExportEncoder
}

func (a *dvdExportAuthorer) Author(ctx context.Context, titles []video2dvd.EncodedTitle, outputDir string, profile video2dvd.Profile) error {
	a.encoder.report("DVD-Videoのメニューと映像データを作成しています…", a.encoder.currentConvertedBytes(""))
	return a.authorer.Author(ctx, titles, outputDir, profile)
}

func dvdExportGeneratedBytes(workDir string) int64 {
	var total int64
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != "audio.lpcm" && strings.ToLower(filepath.Ext(name)) != ".m2v" {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

const (
	// Upper bound: video2dvd codes video at 7.5 Mbit/s or less, next to
	// 1.536 Mbit/s LPCM audio and about 3% of pack overhead.
	dvdISOEstimateBytesPerSecond int64 = 1_170_000
	dvdISOEstimateBaseOverhead   int64 = 2 << 20
	dvdISOEstimateTitleOverhead  int64 = 1 << 20
)

func dvdISOSizeEstimate(queue []string, files []dvdVideoFile) (int64, bool) {
	if len(queue) == 0 {
		return 0, false
	}
	byPath := make(map[string]time.Duration, len(files))
	for _, file := range files {
		byPath[file.path] = file.duration
	}

	const maxInt64 = int64(^uint64(0) >> 1)
	var totalDuration int64
	for _, path := range queue {
		duration, ok := byPath[path]
		if !ok || duration <= 0 || int64(duration) > maxInt64-totalDuration {
			return 0, false
		}
		totalDuration += int64(duration)
	}

	seconds := totalDuration / int64(time.Second)
	remainder := totalDuration % int64(time.Second)
	overhead := dvdISOEstimateBaseOverhead + int64(len(queue))*dvdISOEstimateTitleOverhead
	if seconds > (maxInt64-overhead)/dvdISOEstimateBytesPerSecond {
		return 0, false
	}
	estimate := seconds * dvdISOEstimateBytesPerSecond
	fraction := (remainder*dvdISOEstimateBytesPerSecond + int64(time.Second) - 1) / int64(time.Second)
	if fraction > maxInt64-estimate-overhead {
		return 0, false
	}
	estimate += fraction + overhead
	const sectorSize int64 = 2048
	if estimate > maxInt64-(sectorSize-1) {
		return 0, false
	}
	estimate = (estimate + sectorSize - 1) / sectorSize * sectorSize
	return estimate, true
}

func dvdISOSizeEstimateFromInputs(inputs []string) int64 {
	files := make([]dvdVideoFile, 0, len(inputs))
	for _, path := range inputs {
		media, err := video2dvd.ProbeMedia(path)
		if err != nil {
			return 0
		}
		duration := media.Duration
		if duration <= 0 {
			for _, track := range media.Tracks {
				if track.Kind == video2dvd.TrackKindVideo && track.Duration > duration {
					duration = track.Duration
				}
			}
		}
		files = append(files, dvdVideoFile{path: path, duration: duration})
	}
	estimate, ok := dvdISOSizeEstimate(inputs, files)
	if !ok {
		return 0
	}
	return estimate
}

func dvdExportFailureMessage(err error) string {
	if err == nil {
		return "ISOファイルの作成に失敗しました。"
	}

	switch {
	case errors.Is(err, video2dvd.ErrDiscCapacity):
		return "ISOファイルの作成に失敗しました。\n原因：動画の合計がDVD 1枚に収まりません。キューの動画を減らしてください。"
	case strings.Contains(err.Error(), "mono or stereo only"):
		return "ISOファイルの作成に失敗しました。\n原因：音声が3チャンネル以上の動画には対応していません（モノラルまたはステレオのみ）。"
	case strings.Contains(err.Error(), "none are supported by the built-in decoder"):
		return "ISOファイルの作成に失敗しました。\n原因：対応していない形式の音声が含まれています（MP4はAAC-LC/AC-3、WebMはOpusに対応）。"
	}

	cause := err
	for errors.Unwrap(cause) != nil {
		cause = errors.Unwrap(cause)
	}
	message := strings.TrimSpace(cause.Error())
	var pathError *os.PathError
	if errors.As(cause, &pathError) {
		message = fmt.Sprintf("%s %s: %v", pathError.Op, filepath.Base(pathError.Path), pathError.Err)
	}
	message = strings.TrimPrefix(message, "video2dvd: ")
	const maxErrorRunes = 120
	if utf8.RuneCountInString(message) > maxErrorRunes {
		runes := []rune(message)
		message = string(runes[:maxErrorRunes]) + "…"
	}
	return "ISOファイルの作成に失敗しました。\n原因：" + message
}

func exportDVDISO(ctx context.Context, queue []string, estimatedBytes int64, menuTitle, outputPath string, progress dvdExportProgressReporter) (finalPath string, exportErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			finalPath = ""
			exportErr = fmt.Errorf("DVD/ISO変換中に予期しないエラーが発生しました: %v", recovered)
		}
	}()
	if ctx == nil {
		return "", fmt.Errorf("DVD/ISO変換のコンテキストがありません")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(queue) == 0 {
		return "", fmt.Errorf("キューに動画がありません")
	}
	if len(queue) > 99 {
		return "", fmt.Errorf("DVD-Videoに登録できる動画は99件までです")
	}

	if strings.TrimSpace(outputPath) == "" {
		return "", fmt.Errorf("ISOファイルの保存先が指定されていません")
	}
	outputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return "", fmt.Errorf("ISOファイルの保存先を確認できません: %w", err)
	}

	inputs := make([]string, 0, len(queue))
	titleNames := make([]string, 0, len(queue))
	usedInputs := make(map[string]struct{}, len(queue))
	var workingDir string
	defer func() {
		if workingDir != "" {
			_ = os.RemoveAll(workingDir)
		}
	}()
	ensureWorkingDir := func() error {
		if workingDir != "" {
			return nil
		}
		var err error
		workingDir, err = os.MkdirTemp("", "youtube2dvd-export-inputs-")
		return err
	}
	for index, path := range queue {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		titleNames = append(titleNames, dvdMenuVideoName(path, index+1))
		extension := strings.ToLower(filepath.Ext(path))
		if extension != ".mp4" && extension != ".webm" {
			return "", fmt.Errorf("ISO作成に対応している動画形式はMP4とWebMです: %s", filepath.Base(path))
		}
		canonicalPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("動画ファイルを確認できません: %s: %w", filepath.Base(path), err)
		}
		inputPath := canonicalPath
		if _, duplicate := usedInputs[inputPath]; duplicate {
			if err := ensureWorkingDir(); err != nil {
				return "", fmt.Errorf("一時フォルダを作成できません: %w", err)
			}
			duplicatePath := filepath.Join(workingDir, fmt.Sprintf("title-%02d%s", index+1, extension))
			if err := os.Link(inputPath, duplicatePath); err != nil {
				if err := copyDVDExportInput(inputPath, duplicatePath); err != nil {
					return "", fmt.Errorf("重複した動画を準備できません: %w", err)
				}
			}
			inputPath = duplicatePath
		}
		usedInputs[inputPath] = struct{}{}
		inputs = append(inputs, inputPath)
	}
	if estimatedBytes <= 0 {
		estimatedBytes = dvdISOSizeEstimateFromInputs(inputs)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	stageDir, err := os.MkdirTemp(filepath.Dir(outputPath), ".youtube2dvd-export-")
	if err != nil {
		return "", fmt.Errorf("ISO作成用の一時フォルダを作成できません: %w", err)
	}
	defer os.RemoveAll(stageDir)

	encoder := &dvdExportEncoder{
		encoder:    video2dvd.NewInProcessEncoder(),
		titleNames: titleNames,
		progress:   progress,
		total:      len(inputs),
		estimate:   estimatedBytes,
	}
	converter, err := video2dvd.New(
		encoder,
		&dvdExportAuthorer{authorer: video2dvd.NewInProcessAuthorer(), encoder: encoder},
	)
	if err != nil {
		return "", fmt.Errorf("DVD変換器を準備できません: %w", err)
	}
	discPath := filepath.Join(stageDir, "disc")
	options := video2dvd.Options{
		Standard:  video2dvd.VideoStandardNTSC,
		Aspect:    video2dvd.AspectRatio16x9,
		MenuTitle: menuTitle,
	}
	if strings.TrimSpace(options.MenuTitle) == "" {
		options.MenuTitle = "YouTube to DVD"
	}
	if progress != nil {
		encoder.report("動画を確認しています…", 0)
	}
	if err := converter.Convert(ctx, inputs, discPath, options); err != nil {
		return "", err
	}
	stagedISO := filepath.Join(stageDir, "youtube2dvd.iso")
	if progress != nil {
		encoder.report("ISOイメージを書き出しています…", encoder.currentConvertedBytes(""))
	}
	imageOptions := video2dvd.DVDImageOptions{
		VolumeID: dvdISOVolumeID(menuTitle),
	}
	if err := video2dvd.WriteDVDISOWithOptions(ctx, filepath.Join(discPath, "VIDEO_TS"), stagedISO, imageOptions); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := replaceISOFile(stagedISO, outputPath); err != nil {
		return "", fmt.Errorf("完成したISOファイルを保存できません: %w", err)
	}
	return outputPath, nil
}

func dvdISOVolumeID(title string) string {
	const maxVolumeIDBytes = 32
	var volumeID strings.Builder
	separatorPending := false
	for _, r := range strings.ToUpper(strings.TrimSpace(title)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			if separatorPending && volumeID.Len() > 0 {
				volumeID.WriteByte('_')
			}
			separatorPending = false
			if volumeID.Len() < maxVolumeIDBytes {
				volumeID.WriteRune(r)
			}
			if volumeID.Len() >= maxVolumeIDBytes {
				break
			}
			continue
		}
		separatorPending = volumeID.Len() > 0
	}
	result := strings.Trim(volumeID.String(), "_")
	if result == "" {
		return "YOUTUBE2DVD"
	}
	return result
}

func replaceISOFile(stagedPath, outputPath string) error {
	info, err := os.Lstat(outputPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if info.IsDir() || (!info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0) {
			return fmt.Errorf("保存先にファイル以外が存在します")
		}
	}

	backupDir := ""
	if err == nil {
		backupDir, err = os.MkdirTemp(filepath.Dir(outputPath), ".youtube2dvd-backup-")
		if err != nil {
			return fmt.Errorf("既存ファイルを保護できません: %w", err)
		}
		backupPath := filepath.Join(backupDir, filepath.Base(outputPath))
		if err := os.Rename(outputPath, backupPath); err != nil {
			_ = os.RemoveAll(backupDir)
			return err
		}

		if err := os.Rename(stagedPath, outputPath); err != nil {
			restoreErr := os.Rename(backupPath, outputPath)
			_ = os.RemoveAll(backupDir)
			if restoreErr != nil {
				return fmt.Errorf("保存に失敗し、既存ファイルも復元できませんでした: %w (復元エラー: %v)", err, restoreErr)
			}
			return err
		}
		_ = os.RemoveAll(backupDir)
		return nil
	}

	return os.Rename(stagedPath, outputPath)
}

func copyDVDExportInput(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		_ = os.Remove(target)
		return err
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(target)
		return err
	}
	return nil
}
