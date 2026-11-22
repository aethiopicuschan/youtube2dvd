package main

import (
	"bytes"
	stdcontext "context"
	"embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aethiopicuschan/video2dvd"
	"github.com/aethiopicuschan/youtube2dvd/internal/licenses"
	_ "github.com/ebitengine/hideconsole"
	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
	_ "github.com/guigui-gui/guigui/basicwidget/cjkfont"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Set to the current Git tag in release builds with -ldflags.
var version = "v0.0.0-dev"

//go:embed help/*.txt manual/*.txt
var embeddedHelp embed.FS

//go:embed icon/icon.png
var embeddedAppIcon []byte

type root struct {
	guigui.DefaultWidget
	background           basicwidget.Background
	sidebar              basicwidget.Panel
	home                 sidebarMenuItem
	downloads            sidebarMenuItem
	dvdWriting           sidebarMenuItem
	settingsMenu         sidebarMenuItem
	manualMenu           sidebarMenuItem
	nav                  sidebarMenuItem
	divider              basicwidget.Divider
	about                basicwidget.Text
	title                basicwidget.Text
	body                 basicwidget.Text
	dvdVideoHeading      basicwidget.Text
	dvdVideoList         basicwidget.List[string]
	dvdQueueHeading      basicwidget.Text
	dvdQueueEstimate     basicwidget.Text
	dvdQueueList         basicwidget.List[string]
	dvdActionButton      basicwidget.Button
	dvdActionTooltip     basicwidget.TooltipArea
	dvdWriteDialog       dvdWriteDialog
	dvdProgressDialog    dvdProgressDialog
	dvdExportStatus      basicwidget.Text
	urlLabel             basicwidget.Text
	urlInput             basicwidget.TextInput
	urlHint              basicwidget.Text
	urlNotice            basicwidget.Text
	downloadMethod       basicwidget.Text
	downloadMethodCookie basicwidget.Image
	confirmButton        basicwidget.Button
	urlError             basicwidget.Text
	urlErrorMessage      string
	videoTitle           basicwidget.Text
	thumbnail            basicwidget.Image
	qualityLabel         basicwidget.Text
	qualitySelect        basicwidget.Select[int]
	qualityStatus        basicwidget.Text
	downloadButton       basicwidget.Button
	downloadProgressBar  basicwidget.ProgressBar
	downloadProgressText basicwidget.Text
	internetCard         environmentCard
	driveCard            environmentCard
	environmentDivider   basicwidget.Background
	environmentNotice    basicwidget.Text
	helpDivider          basicwidget.Divider
	environmentHelp      basicwidget.Text
	helpPrevButton       basicwidget.Button
	helpNextButton       basicwidget.Button
	helpPageIndicator    basicwidget.Text
	list                 basicwidget.List[int]
	rows                 []licenseRow
	licenseText          basicwidget.Text
	manualPanel          basicwidget.Panel
	manualPageContent    guigui.WidgetWithPadding[*basicwidget.Text]
	manualContent        string
	showLicenses         bool
	showManual           bool
	showSettings         bool
	settingsPage         settingsPage
	settings             appSettings
	settingsPath         string
	showDownloads        bool
	showDVDWriting       bool
	dvdVideoFiles        []dvdVideoFile
	selectedDVDVideo     string
	// videoDetails reads the saved videos for the popup of the 書き込み
	// page; dvdInfoPath is the video the popup shows.
	videoDetails         videoDetailsCache
	dvdInfoPopup         videoInfoPopup
	dvdInfoPath          string
	dvdVideoListMessage  string
	dvdWriteQueue        []string
	dvdDurationResults   chan dvdDurationResult
	dvdDurationRequestID uint64
	dvdExportResults     chan dvdExportResult
	dvdExportProgresses  chan dvdExportProgressUpdate
	dvdExportInFlight    bool
	dvdExportIsBurn      bool
	dvdExportCanceling   bool
	dvdExportMessage     string
	dvdExportProgress    int
	dvdExportETA         string
	dvdExportConverting  bool
	dvdExportETAEstimate dvdExportETAEstimator
	dvdExportCancel      stdcontext.CancelFunc
	dvdExportDone        chan struct{}
	lastDVDVideoClick    string
	lastDVDVideoClickAt  time.Time
	lastDVDQueueClick    string
	lastDVDQueueClickAt  time.Time
	helpPages            []string
	currentHelpPage      int
	helpAutoAdvanceAt    time.Time
	packages             []licenses.Package
	selected             int
	checks               <-chan environmentCheck
	downloadResults      chan downloadResult
	downloadProgresses   chan downloadProgress
	downloadStatus       string
	// downloadWarning is the part of downloadStatus that warns about
	// captions that could not be added.
	downloadWarning      string
	downloadedBytes      int64
	totalBytes           int64
	downloadFailed       bool
	downloadInFlight     bool
	cancelDownload       stdcontext.CancelFunc
	qualityLookupResults chan qualityLookupResult
	qualityOptions       []downloadQuality
	videoCaptions        videoCaptions
	// selectedCaptionID is the caption track to embed as the default one,
	// or "" for none; additionalCaptionIDs are the other tracks to embed.
	selectedCaptionID       string
	additionalCaptionIDs    map[string]bool
	captionLabel            basicwidget.Text
	captionSelect           basicwidget.Select[string]
	captionNone             basicwidget.Text
	additionalCaptionLabel  basicwidget.Text
	additionalCaptionButton basicwidget.Button
	additionalCaptionText   basicwidget.Text
	captionPicker           trackPickerDialog
	videoAudio              videoAudio
	// videoOtherAudio is set for a video with audio in other languages that
	// the downloader in use cannot fetch.
	videoOtherAudio bool
	audioNote       basicwidget.Text
	// selectedAudioID is the audio track to play by default, and
	// additionalAudioIDs are the other tracks to put in the file.
	selectedAudioID       string
	additionalAudioIDs    map[string]bool
	audioLabel            basicwidget.Text
	audioSelect           basicwidget.Select[string]
	additionalAudioLabel  basicwidget.Text
	additionalAudioButton basicwidget.Button
	additionalAudioText   basicwidget.Text
	audioPicker           trackPickerDialog
	selectedQualityItag   int
	qualityLookupURL      string
	confirmedVideoURL     string
	qualityLookupAt       time.Time
	qualityRequestID      uint64
	qualityLoading        bool
	qualityError          string
	cancelQualityLookup   stdcontext.CancelFunc
	internetState         string
	driveState            string
	discState             string
	dvdDrives             []dvdDrive
}

type downloadResult struct {
	path string
	// warning tells what went wrong with the captions of a saved video.
	warning string
	err     error
}

type downloadProgress struct {
	downloaded int64
	total      int64
}

type dvdDurationResult struct {
	requestID    uint64
	path         string
	duration     time.Duration
	audioStreams int
}

type dvdVideoFile struct {
	path     string
	name     string
	size     int64
	duration time.Duration
	// audioStreams is the number of audio streams the file gives a DVD
	// title, known together with duration.
	audioStreams int
}

var dvdVideoExtensions = map[string]struct{}{
	".avi": {}, ".m4v": {}, ".mkv": {}, ".mov": {}, ".mp4": {}, ".mpeg": {}, ".mpg": {}, ".webm": {}, ".wmv": {},
}

func (r *root) refreshDVDVideoFiles() {
	previousSelection := r.selectedDVDVideo
	r.dvdVideoFiles = nil
	r.selectedDVDVideo = ""
	directory, err := downloadDirectory()
	if err != nil {
		r.dvdVideoListMessage = "ダウンロードフォルダを確認できません: " + err.Error()
		return
	}
	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		r.dvdVideoListMessage = "動画ファイルを読み込めませんでした: " + err.Error()
		return
	}
	for _, entry := range entries {
		// Hidden files are the app's own work in progress, such as a
		// download being written.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, ok := dvdVideoExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		file := dvdVideoFile{path: path, name: entry.Name(), size: info.Size()}
		r.dvdVideoFiles = append(r.dvdVideoFiles, file)
		if path == previousSelection {
			r.selectedDVDVideo = path
		}
	}
	available := make(map[string]struct{}, len(r.dvdVideoFiles))
	for _, file := range r.dvdVideoFiles {
		available[file.path] = struct{}{}
	}
	queue := r.dvdWriteQueue[:0]
	for _, path := range r.dvdWriteQueue {
		if _, ok := available[path]; ok {
			queue = append(queue, path)
		}
	}
	r.dvdWriteQueue = queue
	r.dvdDurationRequestID++
	requestID := r.dvdDurationRequestID
	pathsToProbe := make([]string, 0, len(r.dvdVideoFiles))
	for _, file := range r.dvdVideoFiles {
		extension := strings.ToLower(filepath.Ext(file.path))
		if extension == ".mp4" || extension == ".webm" {
			pathsToProbe = append(pathsToProbe, file.path)
		}
	}
	if len(pathsToProbe) > 0 && r.dvdDurationResults != nil {
		go func() {
			for _, path := range pathsToProbe {
				media, err := video2dvd.ProbeMedia(path)
				if err != nil {
					continue
				}
				if duration := dvdMediaDuration(media); duration > 0 {
					r.dvdDurationResults <- dvdDurationResult{requestID: requestID, path: path, duration: duration, audioStreams: dvdAudioStreams(media)}
				}
			}
		}()
	}
	if len(r.dvdVideoFiles) == 0 {
		r.dvdVideoListMessage = "動画ファイルがありません。ダウンロードするとここに表示されます。"
		return
	}
	r.dvdVideoListMessage = "DVDに書き込む動画を選択してください。"
}

func (r *root) handleDVDVideoClick(path string) {
	if r.dvdExportInFlight {
		return
	}
	now := time.Now()
	if r.lastDVDVideoClick == path && now.Sub(r.lastDVDVideoClickAt) <= 500*time.Millisecond {
		r.dvdWriteQueue = append(r.dvdWriteQueue, path)
		r.lastDVDVideoClick = ""
		r.dvdInfoPopup.Close()
		guigui.RequestRebuild()
		return
	}
	r.selectedDVDVideo = path
	r.lastDVDVideoClick = path
	r.lastDVDVideoClickAt = now
	// A click on a video shows what it holds next to it.
	r.dvdInfoPath = path
	text, _ := r.videoDetails.describe(path)
	r.dvdInfoPopup.ShowAt(image.Pt(ebiten.CursorPosition()), text)
	guigui.RequestRebuild()
}

func (r *root) handleDVDQueueClick(path string) {
	if r.dvdExportInFlight {
		return
	}
	now := time.Now()
	if r.lastDVDQueueClick == path && now.Sub(r.lastDVDQueueClickAt) <= 500*time.Millisecond {
		for i, queuedPath := range r.dvdWriteQueue {
			if queuedPath == path {
				r.dvdWriteQueue = append(r.dvdWriteQueue[:i], r.dvdWriteQueue[i+1:]...)
				break
			}
		}
		r.lastDVDQueueClick = ""
		guigui.RequestRebuild()
		return
	}
	r.lastDVDQueueClick = path
	r.lastDVDQueueClickAt = now
}

func (r *root) downloadDownloadedComplete() bool {
	return r.totalBytes > 0 && r.downloadedBytes >= r.totalBytes
}

// showDownloadProgressText reports whether the progress text is shown: from
// the moment a download starts, and after a download that completed.
func (r *root) showDownloadProgressText() bool {
	return r.downloadInFlight || (!r.downloadFailed && r.downloadDownloadedComplete())
}

// downloadProgressLabel describes the progress of the download. Until the
// size is known, which takes a moment after the download starts, it shows
// what has arrived so far.
func (r *root) downloadProgressLabel() string {
	if r.totalBytes > 0 {
		percent := r.downloadedBytes * 100 / r.totalBytes
		return fmt.Sprintf("%d%%  (%s / %s)", percent, formatBytes(r.downloadedBytes), formatBytes(r.totalBytes))
	}
	if r.downloadedBytes > 0 {
		return formatBytes(r.downloadedBytes)
	}
	return "0%"
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func (r *root) dvdQueueEstimateText() string {
	if len(r.dvdWriteQueue) == 0 {
		return "ISO概算: キューは空です"
	}
	estimate, ok := dvdISOSizeEstimate(r.dvdWriteQueue, r.dvdVideoFiles)
	if !ok {
		return "ISO概算: 動画の長さが不明"
	}
	capacity := video2dvd.DVD5CapacityBytes
	if estimate > capacity {
		return fmt.Sprintf("ISO上限目安 %s > DVD5 %s", formatBytes(estimate), formatBytes(capacity))
	}
	return fmt.Sprintf("ISO上限目安 %s / DVD5 %s", formatBytes(estimate), formatBytes(capacity))
}

type environmentCard struct {
	guigui.DefaultWidget
	background basicwidget.Background
	heading    basicwidget.Text
	value      basicwidget.Text
	label      string
	status     string
	// detail, if any, follows the status on the same line in its own tone.
	detail     string
	tone       color.Color
	detailTone color.Color
}

func (c *environmentCard) Build(context *guigui.Context, adder *guigui.ChildAdder) error {
	c.background.SetTintColor(color.RGBA{R: 250, G: 250, B: 250, A: 255})
	c.heading.SetValue(c.label)
	value := c.status
	if c.detail != "" {
		value += "：" + c.detail
	}
	c.value.SetValue(value)
	c.heading.SetSelectable(false)
	c.value.SetSelectable(false)
	// A long status wraps onto a second line in a narrow window.
	c.value.SetMultiline(true)
	c.value.SetWrapMode(basicwidget.WrapModeNormal)
	var style basicwidget.TextStyles
	style.SetColorInRange(0, len(c.status), c.tone)
	if c.detail != "" {
		style.SetColorInRange(len(c.status), len(value), c.detailTone)
	}
	style.SetWeightInRange(0, len(value), 600)
	c.value.SetOverrideStyles(&style, false)
	adder.AddWidget(&c.background)
	adder.AddWidget(&c.heading)
	adder.AddWidget(&c.value)
	return nil
}

func (c *environmentCard) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	layouter.LayoutWidget(&c.background, b)
	layouter.LayoutWidget(&c.heading, image.Rect(b.Min.X+u, b.Min.Y+u/6, b.Max.X-u, b.Min.Y+u))
	layouter.LayoutWidget(&c.value, image.Rect(b.Min.X+u, b.Min.Y+u, b.Max.X, b.Max.Y-u/6))
}

type environmentCheck struct {
	internet string
	drive    string
	disc     string
	drives   []dvdDrive
}

type sidebarMenuItem struct {
	guigui.DefaultWidget
	background     basicwidget.Background
	tooltip        basicwidget.TooltipArea
	text           basicwidget.Text
	label          string
	disabledReason string
	hovered        bool
	enabled        bool
	activate       func()
}

func (m *sidebarMenuItem) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	m.text.SetValue(m.label)
	m.text.SetSelectable(false)
	m.text.SetHorizontalAlign(basicwidget.HorizontalAlignCenter)
	m.text.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	var textStyle basicwidget.TextStyles
	if !m.enabled {
		textStyle.SetColorInRange(0, len(m.label), color.RGBA{R: 145, G: 145, B: 145, A: 255})
	} else if m.hovered {
		textStyle.SetColorInRange(0, len(m.label), color.RGBA{R: 30, G: 100, B: 220, A: 255})
		textStyle.SetUnderlineInRange(0, len(m.label), true)
	}
	m.text.SetOverrideStyles(&textStyle, false)
	if !m.enabled {
		m.tooltip.SetText(m.disabledReason)
		adder.AddWidget(&m.tooltip)
	}
	if m.enabled && m.hovered {
		m.background.SetTintColor(color.RGBA{R: 215, G: 225, B: 242, A: 255})
		adder.AddWidget(&m.background)
	}
	adder.AddWidget(&m.text)
	return nil
}

func (m *sidebarMenuItem) Layout(_ *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	if !m.enabled {
		layouter.LayoutWidget(&m.tooltip, bounds.Bounds())
	}
	if m.enabled && m.hovered {
		layouter.LayoutWidget(&m.background, bounds.Bounds())
	}
	layouter.LayoutWidget(&m.text, bounds.Bounds().Inset(12))
}

func (m *sidebarMenuItem) Tick(_ *guigui.Context, bounds *guigui.WidgetBounds) error {
	hovered := m.enabled && bounds.IsHitAtCursor()
	if m.hovered != hovered {
		m.hovered = hovered
		guigui.RequestRebuild()
	}
	return nil
}

func (m *sidebarMenuItem) HandlePointingInput(_ *guigui.Context, bounds *guigui.WidgetBounds) guigui.HandleInputResult {
	if m.enabled && bounds.IsHitAtCursor() && inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) {
		if m.activate != nil {
			m.activate()
		}
		return guigui.HandleInputByWidget(m)
	}
	return guigui.HandleInputResult{}
}

func (m *sidebarMenuItem) CursorShape(_ *guigui.Context, bounds *guigui.WidgetBounds) (ebiten.CursorShapeType, bool) {
	if m.enabled && bounds.IsHitAtCursor() {
		return ebiten.CursorShapePointer, true
	}
	return 0, true
}

type licenseRow struct {
	guigui.DefaultWidget
	text         basicwidget.Text
	value        string
	licenseStart int
	onClick      func()
}

type dvdVideoRow struct {
	guigui.DefaultWidget
	nameText basicwidget.Text
	sizeText basicwidget.Text
	name     string
	size     string
	onClick  func()
}

func (r *dvdVideoRow) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	r.nameText.SetValue(r.name)
	r.nameText.SetSelectable(false)
	r.nameText.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	r.sizeText.SetValue(r.size)
	r.sizeText.SetSelectable(false)
	r.sizeText.SetHorizontalAlign(basicwidget.HorizontalAlignEnd)
	r.sizeText.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	adder.AddWidget(&r.nameText)
	adder.AddWidget(&r.sizeText)
	return nil
}

func (r *dvdVideoRow) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	sizeWidth := 5 * u
	layouter.LayoutWidget(&r.nameText, image.Rect(b.Min.X+u/2, b.Min.Y, b.Max.X-sizeWidth-u/2, b.Max.Y))
	layouter.LayoutWidget(&r.sizeText, image.Rect(b.Max.X-sizeWidth, b.Min.Y, b.Max.X-u/2, b.Max.Y))
	colorType := basicwidget.ListItemColorTypeDefault
	if value, ok := context.Env(r, basicwidget.EnvKeyListItemColorType); ok {
		if itemColorType, ok := value.(basicwidget.ListItemColorType); ok {
			colorType = itemColorType
		}
	}
	var nameStyle, sizeStyle basicwidget.TextStyles
	nameStyle.SetColorInRange(0, len(r.name), colorType.TextColor(context))
	sizeStyle.SetColorInRange(0, len(r.size), colorType.TextColor(context))
	r.nameText.SetOverrideStyles(&nameStyle, false)
	r.sizeText.SetOverrideStyles(&sizeStyle, false)
}

func (r *dvdVideoRow) Measure(context *guigui.Context, constraints guigui.Constraints) image.Point {
	nameSize := r.nameText.Measure(context, constraints)
	sizeSize := r.sizeText.Measure(context, constraints)
	return image.Pt(nameSize.X+sizeSize.X+basicwidget.UnitSize(context), max(nameSize.Y, sizeSize.Y, basicwidget.UnitSize(context)))
}

func (r *dvdVideoRow) HandlePointingInput(_ *guigui.Context, bounds *guigui.WidgetBounds) guigui.HandleInputResult {
	if bounds.IsHitAtCursor() && inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) && r.onClick != nil {
		r.onClick()
	}
	return guigui.HandleInputResult{}
}

func (r *dvdVideoRow) CursorShape(_ *guigui.Context, bounds *guigui.WidgetBounds) (ebiten.CursorShapeType, bool) {
	if r.onClick != nil && bounds.IsHitAtCursor() {
		return ebiten.CursorShapePointer, true
	}
	return 0, true
}

func (r *licenseRow) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	r.text.SetValue(r.value)
	r.text.SetSelectable(false)
	var licenseStyle basicwidget.TextStyles
	licenseStyle.SetColorInRange(0, len(r.value)-r.licenseStart, color.RGBA{R: 105, G: 75, B: 165, A: 255})
	licenseStyle.SetWeightInRange(0, len(r.value)-r.licenseStart, 600)
	r.text.SetOverrideStylesInRange(&licenseStyle, r.licenseStart, len(r.value), false)
	r.text.SetHotspotRanges([]basicwidget.TextRange{{StartInBytes: 0, EndInBytes: len(r.value)}})
	r.text.OnHotspotUp(func(_ *guigui.Context, _ basicwidget.TextRange) {
		if r.onClick != nil {
			r.onClick()
		}
	})
	adder.AddWidget(&r.text)
	return nil
}

func (r *licenseRow) Layout(_ *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	layouter.LayoutWidget(&r.text, bounds.Bounds())
}

func (r *licenseRow) Measure(context *guigui.Context, constraints guigui.Constraints) image.Point {
	return r.text.Measure(context, constraints)
}

func (r *root) Build(context *guigui.Context, adder *guigui.ChildAdder) error {
	progressTitle := "ISOファイルを保存しています"
	if r.dvdExportIsBurn {
		progressTitle = "DVDに書き込んでいます"
	}
	r.dvdProgressDialog.title = progressTitle
	r.dvdProgressDialog.message = r.dvdExportMessage
	r.dvdProgressDialog.progress = r.dvdExportProgress
	r.dvdProgressDialog.eta = r.dvdExportETA
	r.dvdProgressDialog.canceling = r.dvdExportCanceling
	r.dvdProgressDialog.onCancel = r.cancelDVDOperation
	r.dvdProgressDialog.SetOpen(r.dvdExportInFlight)

	r.home.label = "ホーム"
	r.home.enabled = !r.dvdExportInFlight
	r.home.activate = func() {
		if r.dvdExportInFlight {
			return
		}
		r.showDownloads = false
		r.showDVDWriting = false
		r.showLicenses = false
		r.showSettings = false
		r.showManual = false
		guigui.RequestRebuild()
	}
	r.downloads.label = "ダウンロード"
	r.downloads.enabled = r.internetState == "接続あり" && !r.dvdExportInFlight
	var disabledReasons []string
	if r.internetState == "確認中…" {
		disabledReasons = append(disabledReasons, "環境を確認しています")
	} else {
		if r.internetState != "接続あり" {
			disabledReasons = append(disabledReasons, "インターネット接続がありません")
		}
	}
	r.downloads.disabledReason = strings.Join(disabledReasons, "\n")
	r.downloads.activate = func() {
		if r.dvdExportInFlight {
			return
		}
		r.showDownloads = true
		r.showDVDWriting = false
		r.showLicenses = false
		r.showSettings = false
		r.showManual = false
		guigui.RequestRebuild()
	}
	r.dvdWriting.label = "書き込み"
	r.dvdWriting.enabled = !r.dvdExportInFlight
	r.dvdWriting.disabledReason = ""
	r.dvdWriting.activate = func() {
		if r.dvdExportInFlight {
			return
		}
		r.refreshDVDVideoFiles()
		r.showDVDWriting = true
		r.showDownloads = false
		r.showLicenses = false
		r.showSettings = false
		r.showManual = false
		guigui.RequestRebuild()
	}
	r.settingsMenu.label = "設定"
	r.settingsMenu.enabled = !r.dvdExportInFlight
	r.settingsMenu.activate = func() {
		if r.dvdExportInFlight {
			return
		}
		r.showSettings = true
		r.settingsPage.opened()
		r.showDownloads = false
		r.showDVDWriting = false
		r.showLicenses = false
		r.showManual = false
		guigui.RequestRebuild()
	}
	r.manualMenu.label = "取扱説明書"
	r.manualMenu.enabled = !r.dvdExportInFlight
	r.manualMenu.activate = func() {
		if r.dvdExportInFlight {
			return
		}
		r.showManual = true
		r.showDownloads = false
		r.showDVDWriting = false
		r.showLicenses = false
		r.showSettings = false
		guigui.RequestRebuild()
	}
	r.nav.label = "Licenses"
	r.nav.enabled = !r.dvdExportInFlight
	const mitLabel = "MIT License"
	const appLabel = "YouTube To DVD "
	aboutText := appLabel + version + "\nMIT License\nCopyright (c) 2026 Chan Aethiopicus"
	const authorLabel = "Chan Aethiopicus"
	appStart := strings.Index(aboutText, appLabel)
	appEnd := appStart + len(appLabel)
	mitStart := strings.Index(aboutText, mitLabel)
	authorStart := strings.Index(aboutText, authorLabel)
	r.about.SetValue(aboutText)
	r.about.SetWrapMode(basicwidget.WrapModeNormal)
	r.about.SetMultiline(true)
	r.about.SetHorizontalAlign(basicwidget.HorizontalAlignCenter)
	r.about.SetVerticalAlign(basicwidget.VerticalAlignTop)
	r.about.SetHotspotRanges([]basicwidget.TextRange{
		{StartInBytes: appStart, EndInBytes: appEnd},
		{StartInBytes: mitStart, EndInBytes: mitStart + len(mitLabel)},
		{StartInBytes: authorStart, EndInBytes: authorStart + len(authorLabel)},
	})
	var mitLinkStyle basicwidget.TextStyles
	for _, link := range [][2]int{{appStart, appEnd}, {mitStart, mitStart + len(mitLabel)}, {authorStart, authorStart + len(authorLabel)}} {
		mitLinkStyle.SetColorInRange(link[0], link[1], color.RGBA{R: 30, G: 100, B: 220, A: 255})
		mitLinkStyle.SetUnderlineInRange(link[0], link[1], true)
	}
	r.about.SetOverrideStyles(&mitLinkStyle, false)
	r.about.OnHotspotUp(func(_ *guigui.Context, textRange basicwidget.TextRange) {
		if r.dvdExportInFlight {
			return
		}
		target := ""
		switch textRange.StartInBytes {
		case appStart:
			target = "https://github.com/aethiopicuschan/youtube2dvd"
		case mitStart:
			target = "https://github.com/aethiopicuschan/youtube2dvd/blob/main/LICENSE"
		case authorStart:
			target = "https://github.com/aethiopicuschan"
		}
		if target != "" {
			openURL(target)
		}
	})
	r.nav.activate = func() {
		if r.dvdExportInFlight {
			return
		}
		r.showLicenses = true
		r.showSettings = false
		r.showDownloads = false
		r.showDVDWriting = false
		r.showManual = false
		guigui.RequestRebuild()
	}
	if !r.showDVDWriting || r.dvdExportInFlight {
		r.dvdInfoPopup.Close()
	} else if r.dvdInfoPath != "" {
		text, _ := r.videoDetails.describe(r.dvdInfoPath)
		r.dvdInfoPopup.SetText(text)
	}
	if r.showSettings {
		r.title.SetValue("設定")
		r.settingsPage.settings = &r.settings
		r.settingsPage.busy = r.downloadInFlight || r.qualityLoading
		r.settingsPage.onChange = func() error {
			// Qualities listed by the other downloader cannot be used.
			r.clearConfirmedVideo()
			if r.settingsPath == "" {
				return nil
			}
			return saveSettings(r.settingsPath, r.settings)
		}
		r.settingsPage.openFile, r.settingsPage.reloadFile, r.settingsPage.resetFile = nil, nil, nil
		if r.settingsPath != "" {
			r.settingsPage.openFile = r.openSettingsFolder
			r.settingsPage.reloadFile = r.reloadSettings
			r.settingsPage.resetFile = r.resetSettings
		}
	} else if r.showLicenses {
		r.title.SetValue("Third-party licenses")
		items := make([]basicwidget.ListItem[int], 0, len(r.packages)+1)
		for i, pkg := range r.packages {
			row := &r.rows[i]
			row.value = pkg.Name + "  ·  " + pkg.License
			row.licenseStart = len(pkg.Name + "  ·  ")
			row.onClick = func() {
				if r.selected == i {
					r.selected = -1
				} else {
					r.selected = i
					r.list.ForceSetScrollPosition(basicwidget.ListScrollPosition{})
				}
				guigui.RequestRebuild()
			}
			items = append(items, basicwidget.ListItem[int]{Content: row, Value: i})
			if i == r.selected {
				const label = "License URL: "
				value := label + pkg.LicenseURL + "\n\n" + pkg.Text
				r.licenseText.SetValue(value)
				r.licenseText.SetWrapMode(basicwidget.WrapModeNormal)
				r.licenseText.SetMultiline(true)
				r.licenseText.SetSelectable(true)
				var linkStyles basicwidget.TextStyles
				linkStyles.SetColorInRange(0, len(pkg.LicenseURL), color.RGBA{R: 30, G: 100, B: 220, A: 255})
				linkStyles.SetUnderlineInRange(0, len(pkg.LicenseURL), true)
				r.licenseText.SetOverrideStylesInRange(&linkStyles, len(label), len(label)+len(pkg.LicenseURL), false)
				r.licenseText.SetHotspotRanges([]basicwidget.TextRange{{StartInBytes: len(label), EndInBytes: len(label) + len(pkg.LicenseURL)}})
				r.licenseText.OnHotspotUp(func(_ *guigui.Context, _ basicwidget.TextRange) {
					openURL(pkg.LicenseURL)
				})
				items = append(items, basicwidget.ListItem[int]{Content: &r.licenseText, Value: -1, IndentLevel: 1, Unselectable: true})
			}
		}
		r.list.SetItems(items)
	} else if r.showManual {
		r.title.SetValue("取扱説明書")
		r.manualPanel.SetStyle(basicwidget.PanelStyleDefault)
		r.manualPanel.SetContentConstraints(basicwidget.PanelContentConstraintsFixedWidth)
		r.manualPageContent.SetPadding(guigui.Padding{Start: basicwidget.UnitSize(context) / 2, Top: basicwidget.UnitSize(context) / 2, End: basicwidget.UnitSize(context) / 2, Bottom: basicwidget.UnitSize(context) / 2})
		r.manualPanel.SetContent(&r.manualPageContent)
		manualText := r.manualPageContent.Widget()
		manualText.SetValue(r.manualContent)
		manualText.SetMultiline(true)
		manualText.SetWrapMode(basicwidget.WrapModeNormal)
		manualText.SetSelectable(true)
		manualText.SetFontSize(float64(basicwidget.UnitSize(context)) * 0.62)
		manualText.SetLineHeight(float64(basicwidget.UnitSize(context)) * 0.78)
		var manualStyle basicwidget.TextStyles
		for _, heading := range []string{
			"はじめに",
			"ご利用にあたって",
			"1. 動画を保存する",
			"2. DVDにまとめる動画を選ぶ",
			"3. DVDに書き込む、またはISOファイルとして保存する",
			"4. 困ったときは",
		} {
			if start := strings.Index(r.manualContent, heading); start >= 0 {
				manualStyle.SetWeightInRange(start, start+len(heading), 600)
				manualStyle.SetColorInRange(start, start+len(heading), color.RGBA{R: 55, G: 90, B: 145, A: 255})
			}
		}
		manualText.SetOverrideStyles(&manualStyle, false)
	} else if r.showDownloads {
		r.title.SetValue("ダウンロード")
		r.urlLabel.SetValue("YouTube動画URL")
		r.urlInput.SetPlaceholder("https://www.youtube.com/watch?v=...")
		hint := "ヒント：ブラウザで保存したい動画を開きます。\n画面上部のURL（ページの住所）をコピーし、ここに貼り付けて「確認」を押してください。"
		r.urlHint.SetValue(hint)
		r.urlHint.SetWrapMode(basicwidget.WrapModeNormal)
		r.urlHint.SetMultiline(true)
		r.urlHint.SetFontSize(float64(basicwidget.UnitSize(context)) * 0.65)
		r.urlHint.SetLineHeight(float64(basicwidget.UnitSize(context)) * 0.7)
		r.urlHint.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		r.urlHint.SetSelectable(false)
		var hintStyle basicwidget.TextStyles
		hintStyle.SetColorInRange(0, len(hint), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		hintStyle.SetColorInRange(0, len("ヒント："), color.RGBA{R: 55, G: 90, B: 145, A: 255})
		hintStyle.SetWeightInRange(0, len("ヒント："), 600)
		r.urlHint.SetOverrideStyles(&hintStyle, false)
		const urlNotice = "※ この機能の利用はYouTube利用規約に抵触します。権利者の許諾のない動画の保存・配布は著作権侵害となる場合があります。"
		r.urlNotice.SetValue(urlNotice)
		r.urlNotice.SetWrapMode(basicwidget.WrapModeNormal)
		r.urlNotice.SetMultiline(true)
		r.urlNotice.SetFontSize(float64(basicwidget.UnitSize(context)) * 0.45)
		r.urlNotice.SetLineHeight(float64(basicwidget.UnitSize(context)) * 0.6)
		r.urlNotice.SetVerticalAlign(basicwidget.VerticalAlignTop)
		r.urlNotice.SetSelectable(false)
		var urlNoticeStyle basicwidget.TextStyles
		urlNoticeStyle.SetColorInRange(0, len(urlNotice), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		r.urlNotice.SetOverrideStyles(&urlNoticeStyle, false)
		downloadMethod := "取得方法：内蔵（最大 360p 程度）"
		if r.settings.UseYtDlp {
			downloadMethod = "取得方法：yt-dlp"
			if r.showsCookieMethod() {
				// The cookies.txt chosen on the settings page goes with it,
				// shown by a cookie after the text.
				downloadMethod += " with"
			}
		}
		r.downloadMethod.SetValue(downloadMethod)
		r.downloadMethod.SetSelectable(false)
		r.downloadMethod.SetHorizontalAlign(basicwidget.HorizontalAlignEnd)
		r.downloadMethod.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		var downloadMethodStyle basicwidget.TextStyles
		downloadMethodStyle.SetColorInRange(0, len(downloadMethod), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		r.downloadMethod.SetOverrideStyles(&downloadMethodStyle, false)
		r.downloadMethodCookie.SetImage(cookieIcon())
		r.urlInput.OnValueChanged(func(_ *guigui.Context, value string, _ bool) {
			r.urlErrorMessage = validateYouTubeURL(value)
			if strings.TrimSpace(value) != r.confirmedVideoURL {
				r.clearConfirmedVideo()
			}
			guigui.RequestRebuild()
		})
		videoURL := strings.TrimSpace(r.urlInput.Value())
		r.confirmButton.SetText("確認")
		// The video cannot change while it downloads.
		context.SetEnabled(&r.urlInput, !r.downloadInFlight)
		context.SetEnabled(&r.confirmButton, videoURL != "" && validateYouTubeURL(videoURL) == "" && r.internetState == "接続あり" && !r.qualityLoading && !r.downloadInFlight)
		r.confirmButton.OnDown(func(_ *guigui.Context) {
			videoURL := strings.TrimSpace(r.urlInput.Value())
			if videoURL == "" || validateYouTubeURL(videoURL) != "" || r.internetState != "接続あり" || r.qualityLoading || r.downloadInFlight {
				return
			}
			r.queueQualityLookup(videoURL)
			guigui.RequestRebuild()
		})
		r.urlError.SetValue(r.urlErrorMessage)
		var urlErrorStyle basicwidget.TextStyles
		urlErrorStyle.SetColorInRange(0, len(r.urlErrorMessage), color.RGBA{R: 190, G: 35, B: 35, A: 255})
		r.urlError.SetOverrideStyles(&urlErrorStyle, false)
		r.qualityLabel.SetValue("画質")
		r.qualityLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		qualityItems := make([]basicwidget.SelectItem[int], 0, len(r.qualityOptions))
		for _, quality := range r.qualityOptions {
			qualityItems = append(qualityItems, basicwidget.SelectItem[int]{Text: quality.label, Value: quality.itag})
		}
		r.qualitySelect.SetItems(qualityItems)
		r.qualitySelect.OnItemSelected(func(_ *guigui.Context, index int) {
			if r.downloadInFlight {
				return
			}
			if item, ok := r.qualitySelect.ItemByIndex(index); ok {
				r.selectedQualityItag = item.Value
				guigui.RequestRebuild()
			}
		})
		if r.selectedQualityItag != 0 {
			r.qualitySelect.SelectItemByValue(r.selectedQualityItag)
		}
		// The quality, like the captions, cannot change while it downloads.
		context.SetEnabled(&r.qualitySelect, len(qualityItems) > 0 && !r.downloadInFlight)
		r.buildAudioRows(context)
		r.buildAudioPicker()
		r.buildCaptionRows(context)
		r.buildCaptionPicker()
		qualityStatus := r.qualityError
		if r.qualityLoading {
			qualityStatus = "動画の情報を取得しています…"
		} else if qualityStatus == "" && videoURL != "" && validateYouTubeURL(videoURL) == "" && r.internetState != "接続あり" {
			qualityStatus = "画質の取得にはインターネット接続が必要です。"
		}
		r.qualityStatus.SetValue(qualityStatus)
		r.qualityStatus.SetSelectable(false)
		// An error can take more than one line, and its advice comes last.
		r.qualityStatus.SetMultiline(true)
		r.qualityStatus.SetWrapMode(basicwidget.WrapModeNormal)
		var qualityStatusStyle basicwidget.TextStyles
		if r.qualityError != "" {
			qualityStatusStyle.SetColorInRange(0, len(qualityStatus), color.RGBA{R: 190, G: 35, B: 35, A: 255})
		} else {
			qualityStatusStyle.SetColorInRange(0, len(qualityStatus), color.RGBA{R: 100, G: 100, B: 100, A: 255})
		}
		r.qualityStatus.SetOverrideStyles(&qualityStatusStyle, false)
		r.downloadButton.SetText("ダウンロード")
		context.SetEnabled(&r.downloadButton, videoURL != "" && videoURL == r.confirmedVideoURL && validateYouTubeURL(videoURL) == "" && r.internetState == "接続あり" && len(r.qualityOptions) > 0 && r.selectedQualityItag != 0 && !r.downloadInFlight)
		r.downloadProgressBar.SetMinimumValueInt64(0)
		if r.totalBytes > 0 {
			r.downloadProgressBar.SetMaximumValueInt64(r.totalBytes)
			r.downloadProgressBar.SetValueInt64(r.downloadedBytes)
		} else {
			r.downloadProgressBar.SetMaximumValueInt64(1)
			r.downloadProgressBar.SetValueInt64(0)
		}
		if r.showDownloadProgressText() {
			r.downloadProgressText.SetValue(r.downloadProgressLabel())
			r.downloadProgressText.SetHorizontalAlign(basicwidget.HorizontalAlignStart)
			r.downloadProgressText.SetSelectable(false)
		}
		r.downloadButton.OnDown(func(_ *guigui.Context) {
			videoURL := strings.TrimSpace(r.urlInput.Value())
			quality, qualityOK := r.selectedDownloadQuality()
			if validateYouTubeURL(videoURL) != "" || videoURL != r.confirmedVideoURL || r.internetState != "接続あり" || !qualityOK || r.downloadInFlight {
				return
			}
			ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
			r.cancelDownload = cancel
			r.downloadInFlight = true
			r.downloadFailed = false
			r.downloadStatus = "動画をダウンロードしています…"
			r.downloadWarning = ""
			r.downloadedBytes = 0
			r.totalBytes = 0
			settings := r.settings
			audio := r.chosenAudio()
			captions := r.chosenCaptions()
			go func() {
				path, warning, err := downloadVideo(ctx, settings, videoURL, quality, audio, captions, func(progress downloadProgress) {
					select {
					case r.downloadProgresses <- progress:
					default:
						select {
						case <-r.downloadProgresses:
						default:
						}
						select {
						case r.downloadProgresses <- progress:
						default:
						}
					}
				})
				r.downloadResults <- downloadResult{path: path, warning: warning, err: err}
			}()
			guigui.RequestRebuild()
		})
		status := r.downloadStatus
		if status == "" {
			status = downloadPageStatus(r.internetState)
		}
		r.body.SetValue(status)
		r.body.SetMultiline(true)
		r.body.SetWrapMode(basicwidget.WrapModeNormal)
		r.body.SetSelectable(false)
		var style basicwidget.TextStyles
		status = r.body.Value()
		if r.downloadStatus == "" && r.internetState == "接続あり" {
			style.SetColorInRange(0, len(status), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		} else if strings.Contains(status, "確認しています") {
			style.SetColorInRange(0, len(status), color.RGBA{R: 140, G: 100, B: 35, A: 255})
		} else if r.downloadFailed || r.internetState != "接続あり" {
			style.SetColorInRange(0, len(status), color.RGBA{R: 175, G: 45, B: 45, A: 255})
			style.SetWeightInRange(0, len(status), 600)
		} else {
			style.SetColorInRange(0, len(status), color.RGBA{R: 35, G: 120, B: 65, A: 255})
			if r.downloadWarning != "" && strings.HasSuffix(status, r.downloadWarning) {
				style.SetColorInRange(len(status)-len(r.downloadWarning), len(status), color.RGBA{R: 150, G: 100, B: 25, A: 255})
			}
		}
		r.body.SetOverrideStyles(&style, false)
	} else if r.showDVDWriting {
		r.title.SetValue("書き込み")
		r.dvdVideoHeading.SetValue("ダウンロード済みの動画（ダブルクリックでキューに追加）")
		r.dvdVideoHeading.SetSelectable(false)
		r.dvdQueueHeading.SetValue("書き込む動画のキュー")
		r.dvdQueueHeading.SetSelectable(false)
		r.dvdQueueHeading.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		r.dvdQueueEstimate.SetValue(r.dvdQueueEstimateText())
		r.dvdQueueEstimate.SetSelectable(false)
		r.dvdQueueEstimate.SetHorizontalAlign(basicwidget.HorizontalAlignEnd)
		r.dvdQueueEstimate.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		r.dvdQueueEstimate.SetFontSize(float64(basicwidget.UnitSize(context)) * 0.45)
		r.dvdQueueEstimate.SetWrapMode(basicwidget.WrapModeNone)
		var estimateStyle basicwidget.TextStyles
		if estimate, ok := dvdISOSizeEstimate(r.dvdWriteQueue, r.dvdVideoFiles); ok && estimate > video2dvd.DVD5CapacityBytes {
			estimateStyle.SetColorInRange(0, len(r.dvdQueueEstimate.Value()), color.RGBA{R: 185, G: 45, B: 45, A: 255})
		} else {
			estimateStyle.SetColorInRange(0, len(r.dvdQueueEstimate.Value()), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
		r.dvdQueueEstimate.SetOverrideStyles(&estimateStyle, false)
		videoItems := make([]basicwidget.ListItem[string], 0, max(1, len(r.dvdVideoFiles)))
		if len(r.dvdVideoFiles) == 0 {
			videoItems = append(videoItems, basicwidget.ListItem[string]{Text: r.dvdVideoListMessage, Unselectable: true})
		} else {
			for _, file := range r.dvdVideoFiles {
				row := &dvdVideoRow{
					name: file.name,
					size: formatBytes(file.size),
					onClick: func() {
						r.handleDVDVideoClick(file.path)
					},
				}
				videoItems = append(videoItems, basicwidget.ListItem[string]{Content: row, Value: file.path})
			}
		}
		r.dvdVideoList.SetStyle(basicwidget.ListStyleNormal)
		r.dvdVideoList.SetStripeVisible(true)
		r.dvdVideoList.SetItems(videoItems)
		r.dvdVideoList.OnItemSelected(func(_ *guigui.Context, index int) {
			if r.dvdExportInFlight {
				return
			}
			if item, ok := r.dvdVideoList.ItemByIndex(index); ok && item.Value != "" {
				r.selectedDVDVideo = item.Value
				guigui.RequestRebuild()
			}
		})
		if r.selectedDVDVideo != "" {
			r.dvdVideoList.SelectItemByValue(r.selectedDVDVideo)
		}
		context.SetEnabled(&r.dvdVideoList, !r.dvdExportInFlight)
		queueItems := make([]basicwidget.ListItem[string], 0, max(1, len(r.dvdWriteQueue)))
		for _, path := range r.dvdWriteQueue {
			for _, file := range r.dvdVideoFiles {
				if file.path == path {
					row := &dvdVideoRow{
						name: file.name,
						size: formatBytes(file.size),
						onClick: func() {
							r.handleDVDQueueClick(path)
						},
					}
					queueItems = append(queueItems, basicwidget.ListItem[string]{Content: row, Value: path})
					break
				}
			}
		}
		if len(queueItems) == 0 {
			queueItems = append(queueItems, basicwidget.ListItem[string]{Text: "ダブルクリックした動画がここに追加されます。", Unselectable: true})
		}
		r.dvdQueueList.SetStyle(basicwidget.ListStyleNormal)
		r.dvdQueueList.SetStripeVisible(true)
		r.dvdQueueList.SetItems(queueItems)
		context.SetEnabled(&r.dvdQueueList, !r.dvdExportInFlight)
		r.dvdActionButton.SetText("")
		r.dvdActionButton.SetIcon(dvdActionIcon(context))
		tooltipText := "書き込み方法を選択"
		if len(r.dvdWriteQueue) == 0 {
			tooltipText = "書き込みキューに動画を追加してください"
		} else if r.dvdExportInFlight {
			tooltipText = "DVD-Videoを作成しています"
		}
		r.dvdActionTooltip.SetText(tooltipText)
		context.SetEnabled(&r.dvdActionButton, len(r.dvdWriteQueue) > 0 && !r.dvdExportInFlight)
		dialogQueue := make([]dvdVideoFile, 0, len(r.dvdWriteQueue))
		for _, path := range r.dvdWriteQueue {
			for _, file := range r.dvdVideoFiles {
				if file.path == path {
					dialogQueue = append(dialogQueue, file)
					break
				}
			}
		}
		r.dvdWriteDialog.SetQueue(dialogQueue, r.dvdQueueEstimateText())
		r.dvdWriteDialog.driveState = r.driveState
		r.dvdWriteDialog.discState = r.discState
		r.dvdWriteDialog.drives = r.dvdDrives
		r.dvdWriteDialog.busy = r.dvdExportInFlight
		r.dvdWriteDialog.onSaveISO = r.startDVDExport
		r.dvdWriteDialog.onBurnDVD = r.startDVDBurn
		r.dvdActionButton.OnDown(func(_ *guigui.Context) {
			if len(r.dvdWriteQueue) == 0 || r.dvdExportInFlight {
				return
			}
			r.dvdWriteDialog.SetOpen(true)
			guigui.RequestRebuild()
		})
		exportStatus := r.dvdExportMessage
		if r.dvdExportETA != "" {
			exportStatus += "\n" + r.dvdExportETA
		}
		r.dvdExportStatus.SetValue(exportStatus)
		r.dvdExportStatus.SetWrapMode(basicwidget.WrapModeNormal)
		r.dvdExportStatus.SetMultiline(true)
		r.dvdExportStatus.SetSelectable(true)
		r.dvdExportStatus.SetFontSize(float64(basicwidget.UnitSize(context)) * 0.65)
		r.dvdExportStatus.SetLineHeight(float64(basicwidget.UnitSize(context)) * 0.75)
		var exportStatusStyle basicwidget.TextStyles
		if r.dvdExportInFlight {
			exportStatusStyle.SetColorInRange(0, len(exportStatus), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		} else if strings.Contains(r.dvdExportMessage, "失敗") {
			exportStatusStyle.SetColorInRange(0, len(r.dvdExportMessage), color.RGBA{R: 185, G: 45, B: 45, A: 255})
		} else if r.dvdExportMessage != "" {
			exportStatusStyle.SetColorInRange(0, len(r.dvdExportMessage), color.RGBA{R: 35, G: 120, B: 65, A: 255})
		}
		r.dvdExportStatus.SetOverrideStyles(&exportStatusStyle, false)
	} else {
		r.title.SetValue("YouTube to DVD")
		r.internetCard.label = "インターネット接続"
		r.internetCard.status = r.internetState
		r.internetCard.tone = statusColor(r.internetState)
		r.driveCard.label = "DVDドライブ"
		r.driveCard.status = dvdDriveStatusLabel(r.driveState, r.dvdDrives)
		r.driveCard.tone = statusColor(r.driveState)
		r.driveCard.detail = ""
		if r.driveState == "検出済み" {
			r.driveCard.detail = r.discState
		}
		r.driveCard.detailTone = statusColor(r.discState)
		r.environmentDivider.SetTintColor(color.RGBA{R: 185, G: 185, B: 185, A: 255})
		r.environmentNotice.SetValue(environmentNotice(r.internetState, r.driveState, r.discState, dvdDrivesHint(r.dvdDrives)))
		r.environmentNotice.SetMultiline(true)
		r.environmentNotice.SetWrapMode(basicwidget.WrapModeNormal)
		if len(r.helpPages) > 0 {
			if r.currentHelpPage < 0 {
				r.currentHelpPage = 0
			}
			if r.currentHelpPage >= len(r.helpPages) {
				r.currentHelpPage = len(r.helpPages) - 1
			}
			r.environmentHelp.SetValue(r.helpPages[r.currentHelpPage])
		}
		r.environmentHelp.SetMultiline(true)
		r.environmentHelp.SetWrapMode(basicwidget.WrapModeNormal)
		r.environmentHelp.SetSelectable(false)
		var helpStyle basicwidget.TextStyles
		helpText := r.environmentHelp.Value()
		helpStyle.SetColorInRange(0, len(helpText), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		helpTitleEnd := strings.IndexByte(helpText, '\n')
		if helpTitleEnd >= 0 {
			helpStyle.SetWeightInRange(0, helpTitleEnd, 600)
		}
		r.environmentHelp.SetOverrideStyles(&helpStyle, false)
		r.helpPrevButton.SetText("前へ")
		r.helpPrevButton.OnDown(func(_ *guigui.Context) {
			r.turnHelpPage(-1)
		})
		r.helpNextButton.SetText("次へ")
		r.helpNextButton.OnDown(func(_ *guigui.Context) {
			r.turnHelpPage(1)
		})
		indicator := fmt.Sprintf("%d / %d", r.currentHelpPage+1, len(r.helpPages))
		r.helpPageIndicator.SetValue(indicator)
		r.helpPageIndicator.SetSelectable(false)
		r.helpPageIndicator.SetHorizontalAlign(basicwidget.HorizontalAlignCenter)
		r.helpPageIndicator.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		var indicatorStyle basicwidget.TextStyles
		indicatorStyle.SetColorInRange(0, len(indicator), color.RGBA{R: 90, G: 90, B: 90, A: 255})
		r.helpPageIndicator.SetOverrideStyles(&indicatorStyle, false)
		var noticeStyle basicwidget.TextStyles
		noticeText := r.environmentNotice.Value()
		if r.internetState == "確認中…" || r.driveState == "確認中…" || r.discState == dvdMediaChecking {
			noticeStyle.SetColorInRange(0, len(noticeText), color.RGBA{R: 140, G: 100, B: 35, A: 255})
		} else if r.internetState != "接続あり" && r.driveState != "検出済み" {
			conclusionEnd := strings.IndexByte(noticeText, '\n')
			if conclusionEnd < 0 {
				conclusionEnd = len(noticeText)
			}
			noticeStyle.SetColorInRange(0, conclusionEnd, color.RGBA{R: 175, G: 45, B: 45, A: 255})
			noticeStyle.SetWeightInRange(0, conclusionEnd, 600)
			if conclusionEnd < len(noticeText) {
				noticeStyle.SetColorInRange(conclusionEnd+1, len(noticeText), color.RGBA{R: 80, G: 80, B: 80, A: 255})
			}
		} else if r.internetState != "接続あり" || r.driveState != "検出済み" || r.discState != dvdMediaWritable {
			conclusionEnd := strings.IndexByte(noticeText, '\n')
			if conclusionEnd < 0 {
				conclusionEnd = len(noticeText)
			}
			noticeStyle.SetColorInRange(0, conclusionEnd, color.RGBA{R: 150, G: 100, B: 25, A: 255})
			noticeStyle.SetWeightInRange(0, conclusionEnd, 600)
			if conclusionEnd < len(noticeText) {
				noticeStyle.SetColorInRange(conclusionEnd+1, len(noticeText), color.RGBA{R: 80, G: 80, B: 80, A: 255})
			}
		} else {
			noticeStyle.SetColorInRange(0, len(noticeText), color.RGBA{R: 35, G: 120, B: 65, A: 255})
		}
		r.environmentNotice.SetOverrideStyles(&noticeStyle, false)
	}
	adder.AddWidget(&r.background)
	r.sidebar.SetStyle(basicwidget.PanelStyleSide)
	r.sidebar.SetBorders(basicwidget.PanelBorders{End: true})
	adder.AddWidget(&r.sidebar)
	adder.AddWidget(&r.home)
	adder.AddWidget(&r.downloads)
	adder.AddWidget(&r.dvdWriting)
	adder.AddWidget(&r.settingsMenu)
	adder.AddWidget(&r.manualMenu)
	adder.AddWidget(&r.nav)
	adder.AddWidget(&r.divider)
	adder.AddWidget(&r.about)
	adder.AddWidget(&r.dvdWriteDialog)
	adder.AddWidget(&r.dvdProgressDialog)
	adder.AddWidget(&r.audioPicker)
	adder.AddWidget(&r.captionPicker)
	adder.AddWidget(&r.dvdInfoPopup)
	adder.AddWidget(&r.title)
	if r.showSettings {
		adder.AddWidget(&r.settingsPage)
	} else if r.showLicenses {
		adder.AddWidget(&r.list)
	} else if r.showManual {
		adder.AddWidget(&r.manualPanel)
	} else if r.showDownloads {
		if r.body.Value() != "" {
			adder.AddWidget(&r.body)
		}
		adder.AddWidget(&r.urlLabel)
		adder.AddWidget(&r.urlNotice)
		adder.AddWidget(&r.downloadMethod)
		if r.showsCookieMethod() {
			adder.AddWidget(&r.downloadMethodCookie)
		}
		adder.AddWidget(&r.urlInput)
		adder.AddWidget(&r.confirmButton)
		if r.urlErrorMessage != "" {
			adder.AddWidget(&r.urlError)
		} else if r.confirmedVideoURL == strings.TrimSpace(r.urlInput.Value()) {
			adder.AddWidget(&r.videoTitle)
			if r.thumbnail.HasImage() {
				adder.AddWidget(&r.thumbnail)
			}
			if len(r.qualityOptions) > 0 {
				adder.AddWidget(&r.qualityLabel)
				adder.AddWidget(&r.qualitySelect)
				adder.AddWidget(&r.downloadButton)
				r.addAudioRows(adder)
				r.addCaptionRows(adder)
			} else if r.qualityLoading || r.qualityError != "" || (strings.TrimSpace(r.urlInput.Value()) != "" && validateYouTubeURL(r.urlInput.Value()) == "" && r.internetState != "接続あり") {
				adder.AddWidget(&r.qualityStatus)
			}
		} else if r.qualityLoading || r.qualityError != "" || (strings.TrimSpace(r.urlInput.Value()) != "" && validateYouTubeURL(r.urlInput.Value()) == "" && r.internetState != "接続あり") {
			adder.AddWidget(&r.qualityStatus)
		}
		if r.downloadInFlight {
			adder.AddWidget(&r.downloadProgressBar)
		}
		if r.showDownloadProgressText() {
			adder.AddWidget(&r.downloadProgressText)
		}
		if !r.videoReadyToDownload() {
			adder.AddWidget(&r.urlHint)
		}
	} else if r.showDVDWriting {
		adder.AddWidget(&r.dvdVideoHeading)
		adder.AddWidget(&r.dvdVideoList)
		adder.AddWidget(&r.dvdQueueHeading)
		adder.AddWidget(&r.dvdQueueEstimate)
		adder.AddWidget(&r.dvdQueueList)
		adder.AddWidget(&r.dvdActionButton)
		adder.AddWidget(&r.dvdActionTooltip)
		if r.dvdExportMessage != "" && !r.dvdExportInFlight {
			adder.AddWidget(&r.dvdExportStatus)
		}
	} else {
		if r.showDownloads {
			adder.AddWidget(&r.body)
		} else if !r.showManual {
			adder.AddWidget(&r.internetCard)
			adder.AddWidget(&r.driveCard)
			adder.AddWidget(&r.environmentDivider)
			adder.AddWidget(&r.environmentNotice)
			adder.AddWidget(&r.helpDivider)
			adder.AddWidget(&r.environmentHelp)
			if len(r.helpPages) > 1 {
				adder.AddWidget(&r.helpPrevButton)
				adder.AddWidget(&r.helpPageIndicator)
				adder.AddWidget(&r.helpNextButton)
			}
		}
	}
	return nil
}

func (r *root) startDVDExport(menuTitle, outputPath string) {
	r.startDVDOperation(menuTitle, outputPath, false, dvdDrive{})
}

func (r *root) startDVDBurn(menuTitle string, drive dvdDrive) {
	r.startDVDOperation(menuTitle, "", true, drive)
}

// startDVDOperation saves the queue as an ISO file at outputPath or, when
// burn is set, burns it to drive.
func (r *root) startDVDOperation(menuTitle, outputPath string, burn bool, drive dvdDrive) {
	if len(r.dvdWriteQueue) == 0 || r.dvdExportInFlight || (!burn && strings.TrimSpace(outputPath) == "") {
		return
	}
	if _, writable := findDVDDrive(writableDVDDrives(r.dvdDrives), drive); burn && !writable {
		return
	}
	queue := append([]string(nil), r.dvdWriteQueue...)
	estimatedBytes, _ := dvdISOSizeEstimate(queue, r.dvdVideoFiles)
	r.dvdExportInFlight = true
	r.dvdExportIsBurn = burn
	r.dvdExportCanceling = false
	r.dvdExportMessage = "動画を確認しています…"
	if burn {
		r.dvdExportMessage = "DVDディスクを再確認しています…"
	}
	r.dvdExportProgress = 0
	r.dvdExportETA = ""
	r.dvdExportConverting = false
	r.dvdExportETAEstimate.Reset()
	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	done := make(chan struct{})
	r.dvdExportCancel = cancel
	r.dvdExportDone = done
	r.dvdProgressDialog.SetOpen(true)
	progress := func(update dvdExportProgressUpdate) {
		select {
		case r.dvdExportProgresses <- update:
		default:
			select {
			case <-r.dvdExportProgresses:
			default:
			}
			select {
			case r.dvdExportProgresses <- update:
			default:
			}
		}
	}
	go func() {
		defer close(done)
		if burn {
			// Check the drive before spending minutes on the conversion.
			current, found := findDVDDrive(listDVDDrives(), drive)
			if !found || current.State != dvdMediaWritable {
				r.dvdExportResults <- dvdExportResult{err: fmt.Errorf("書き込み先のドライブ（%s）のディスクが書き込み可能ではありません。ドライブとディスクを確認してください", drive.Name), burnFailure: true}
				return
			}
		}
		if burn {
			progress(dvdExportProgressUpdate{message: "DVD-Videoを作成しています…"})
			var err error
			outputPath, err = os.MkdirTemp("", "youtube2dvd-burn-")
			if err != nil {
				r.dvdExportResults <- dvdExportResult{err: fmt.Errorf("書き込み用の一時フォルダーを作成できません: %w", err), burnFailure: true}
				return
			}
			defer os.RemoveAll(outputPath)
			outputPath = filepath.Join(outputPath, "youtube2dvd.iso")
		}
		path, err := exportDVDISO(ctx, queue, estimatedBytes, menuTitle, outputPath, progress)
		burnFailure := false
		if err == nil && burn {
			err = dvdBurnISO(ctx, path, drive, progress)
			burnFailure = err != nil
		}
		r.dvdExportResults <- dvdExportResult{path: path, err: err, burned: burn && err == nil, burnFailure: burnFailure}
	}()
	guigui.RequestRebuild()
}

func (r *root) cancelDVDOperation() {
	if !r.dvdExportInFlight || r.dvdExportCanceling || r.dvdExportCancel == nil {
		return
	}
	r.dvdExportCanceling = true
	r.dvdExportCancel()
	guigui.RequestRebuild()
}

func (r *root) stopDVDExport() {
	if r.dvdExportCancel != nil {
		r.dvdExportCancel()
		r.dvdExportCancel = nil
	}
	if r.dvdExportDone != nil {
		<-r.dvdExportDone
		r.dvdExportDone = nil
	}
}

func statusColor(status string) color.Color {
	switch status {
	case "接続あり", "検出済み", dvdMediaWritable:
		return color.RGBA{R: 35, G: 135, B: 75, A: 255}
	case "確認中…", dvdMediaUnknown, dvdMediaInsertedUnknown:
		return color.RGBA{R: 175, G: 115, B: 20, A: 255}
	default:
		return color.RGBA{R: 185, G: 55, B: 55, A: 255}
	}
}

const (
	environmentCheckInterval = 30 * time.Second
	// environmentRetryInterval is how soon a failed internet check is
	// repeated, so that a single slow connection right after start does not
	// show the app as offline for a whole environmentCheckInterval.
	environmentRetryInterval = 5 * time.Second
	// environmentQuickRetries is how many failed checks in a row are
	// repeated soon. After that the connection is probably really down, and
	// checking every few seconds would only keep PowerShell or drutil busy.
	environmentQuickRetries = 3
	// internetCheckTimeout is how long the internet check waits for a
	// connection to YouTube.
	internetCheckTimeout = 10 * time.Second
)

// nextEnvironmentCheckDelay returns how long to wait before the next
// environment check, given the result of the last one and how many checks
// in a row before it found no internet connection.
func nextEnvironmentCheckDelay(result environmentCheck, failuresBefore int) (delay time.Duration, failures int) {
	if result.internet == "接続あり" {
		return environmentCheckInterval, 0
	}
	failures = failuresBefore + 1
	if failures <= environmentQuickRetries {
		return environmentRetryInterval, failures
	}
	return environmentCheckInterval, failures
}

const helpAutoAdvanceInterval = 15 * time.Second

func monitorEnvironment(checkResults chan environmentCheck) {
	failures := 0
	for {
		result := checkEnvironment()
		select {
		case checkResults <- result:
		default:
			select {
			case <-checkResults:
			default:
			}
			select {
			case checkResults <- result:
			default:
			}
		}
		var delay time.Duration
		delay, failures = nextEnvironmentCheckDelay(result, failures)
		time.Sleep(delay)
	}
}

// environmentNotice describes what can be done in the current environment.
// discHint explains what keeps the drives from burning, if anything does.
func environmentNotice(internet, drive, disc, discHint string) string {
	if internet == "確認中…" || drive == "確認中…" || disc == dvdMediaChecking {
		return "必要な環境を確認しています…"
	}
	internetAvailable := internet == "接続あり"
	driveAvailable := drive == "検出済み"
	switch {
	case !internetAvailable && !driveAvailable:
		return "一部の機能を利用できません。\nインターネット接続がないため、YouTube動画をダウンロードできません。\nDVDドライブも未検出です。手元の動画ファイルからISOファイルを作成できます。"
	case !internetAvailable:
		message := "一部の機能を利用できません。\nインターネット接続がないため、YouTube動画をダウンロードできません。\n手元の動画ファイルからISOファイルを作成できます。"
		if disc != dvdMediaWritable {
			message += "\n" + discHint
		}
		return message
	case !driveAvailable:
		return "一部の機能を利用できません。\nDVDドライブが未検出のため、ディスクの状態を確認できません。\nYouTube動画のダウンロードとISOファイルの作成はできます。"
	case disc != dvdMediaWritable:
		return "一部の機能を利用できません。\n" + discHint + "\nYouTube動画をダウンロードし、ISOファイルを作成できます。"
	default:
		if message := dvdBurnBackendMessage(); message != "" {
			return "一部の機能を利用できません。\n" + message + "\nYouTube動画をダウンロードし、ISOファイルを作成できます。"
		}
		return "インターネット接続と書き込み可能なDVDディスクを確認しました。\nYouTube動画をダウンロードし、DVDへの書き込みとISOファイルの作成ができます。"
	}
}

func dvdDiscEnvironmentHint(disc string) string {
	switch disc {
	case dvdMediaNoDisc:
		return "DVDディスクが挿入されていないため、DVDへの書き込みはできません。"
	case dvdMediaReadOnly:
		return "挿入されたディスクは書き込みできないため、書き込み可能なDVDを入れてください。"
	case dvdMediaInsertedUnknown:
		return "ディスクは挿入されていますが、書き込み可否を確認できませんでした。ディスクとドライブを確認してください。"
	case dvdMediaDriveAbsent:
		return "DVDドライブが未検出のため、ディスクの状態を確認できません。"
	case dvdMediaDriveCannotWrite:
		return "接続されているドライブはDVDの書き込みに対応していないため、DVDへの書き込みはできません。書き込み対応のドライブを接続してください。"
	default:
		return "DVDディスクの書き込み可否を確認できませんでした。ディスクとドライブを確認してください。"
	}
}

// turnHelpPage shows the help page delta pages away, wrapping around, and
// restarts the wait for the automatic advance.
func (r *root) turnHelpPage(delta int) {
	count := len(r.helpPages)
	if count < 2 {
		return
	}
	r.currentHelpPage = ((r.currentHelpPage+delta)%count + count) % count
	r.helpAutoAdvanceAt = time.Now().Add(helpAutoAdvanceInterval)
	guigui.RequestRebuild()
}

func loadHelpPages() ([]string, error) {
	paths, err := fs.Glob(embeddedHelp, "help/*.txt")
	if err != nil {
		return nil, fmt.Errorf("ヘルプファイルを検索できません: %w", err)
	}
	pages := make([]string, 0, len(paths))
	for _, path := range paths {
		contents, err := fs.ReadFile(embeddedHelp, path)
		if err != nil {
			return nil, fmt.Errorf("%s を読み込めません: %w", path, err)
		}
		pages = append(pages, strings.TrimSpace(string(contents)))
	}
	return pages, nil
}

func downloadPageStatus(internet string) (stat string) {
	if internet == "確認中…" {
		stat = "インターネット接続を確認しています…"
	}
	if internet != "接続あり" {
		stat = "現在の環境ではダウンロードを開始できません。\nインターネット接続がないため、YouTube動画を取得できません。"
	}
	return stat
}

func downloadFailureStatus(err error) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	if strings.Contains(message, "unexpected status code: 403") {
		message = "YouTubeから動画データの取得を拒否されました（HTTP 403）。別の動画や画質を試してください。複数の動画で続く場合は、時間を置くか別のネットワークで再試行してください。"
	}
	const maxRunes = 180
	messageRunes := []rune(message)
	if len(messageRunes) > maxRunes {
		message = string(messageRunes[:maxRunes]) + "…"
	}
	return "ダウンロードに失敗しました。\n" + message
}

func validateYouTubeURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return "https:// から始まる完全な URL を入力してください。"
	}
	if u.User != nil || u.Port() != "" {
		return "YouTube の動画 URL を入力してください。"
	}
	host := strings.ToLower(u.Hostname())
	var videoID string
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path == "/watch" {
			videoID = u.Query().Get("v")
		} else {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "live" || parts[0] == "embed") {
				videoID = parts[1]
			}
		}
	case "youtu.be", "www.youtu.be":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 1 {
			videoID = parts[0]
		}
	}
	if len(videoID) != 11 {
		return "有効な YouTube 動画 URL（通常動画・Shorts・ライブ）を入力してください。"
	}
	for _, c := range videoID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "動画 URL の ID が正しくありません。"
		}
	}
	return ""
}

func (r *root) Tick(_ *guigui.Context, _ *guigui.WidgetBounds) error {
	updated := false
	r.settingsPage.poll(r.downloadInFlight || r.qualityLoading)
	if r.videoDetails.changed.Swap(false) {
		// A video was read; the pages showing it say what it holds.
		updated = true
	}
	if r.dvdExportInFlight && r.dvdExportConverting && !r.dvdExportCanceling {
		eta := r.dvdExportETAEstimate.Label(time.Now())
		if eta != r.dvdExportETA {
			r.dvdExportETA = eta
			updated = true
		}
	}
	if r.showDownloads || r.showDVDWriting || r.showLicenses || r.showManual || r.showSettings || len(r.helpPages) < 2 {
		r.helpAutoAdvanceAt = time.Time{}
	} else {
		now := time.Now()
		if r.helpAutoAdvanceAt.IsZero() {
			r.helpAutoAdvanceAt = now.Add(helpAutoAdvanceInterval)
		} else if !now.Before(r.helpAutoAdvanceAt) {
			r.currentHelpPage = (r.currentHelpPage + 1) % len(r.helpPages)
			r.helpAutoAdvanceAt = now.Add(helpAutoAdvanceInterval)
			updated = true
		}
	}
	if r.downloadProgresses != nil {
		select {
		case progress := <-r.downloadProgresses:
			r.downloadedBytes = progress.downloaded
			r.totalBytes = progress.total
			updated = true
		default:
		}
	}
	if r.checks != nil {
		select {
		case result := <-r.checks:
			r.internetState = result.internet
			r.driveState = result.drive
			r.discState = result.disc
			r.dvdDrives = result.drives
			if result.internet != "接続あり" && r.qualityLoading {
				if r.cancelQualityLookup != nil {
					r.cancelQualityLookup()
					r.cancelQualityLookup = nil
				}
				r.qualityRequestID++
				r.qualityLoading = false
				r.qualityLookupAt = time.Time{}
			}
			if r.downloadInFlight && result.internet != "接続あり" && r.cancelDownload != nil {
				r.cancelDownload()
				r.downloadFailed = true
				r.downloadStatus = "インターネット接続が切れたため、ダウンロードを中止しました。"
				r.cancelDownload = nil
			}
			updated = true
		default:
		}
	}
	if r.qualityLoading && !r.qualityLookupAt.IsZero() && !time.Now().Before(r.qualityLookupAt) {
		requestID := r.qualityRequestID
		videoURL := r.qualityLookupURL
		r.qualityLookupAt = time.Time{}
		settings := r.settings
		timeout := 30 * time.Second
		if settings.UseYtDlp {
			// Starting yt-dlp alone can take several seconds.
			timeout = 90 * time.Second
		}
		ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
		r.cancelQualityLookup = cancel
		go func() {
			info, err := videoDetails(ctx, settings, videoURL)
			r.qualityLookupResults <- qualityLookupResult{requestID: requestID, videoURL: videoURL, info: info, err: err}
		}()
		updated = true
	}
	if r.qualityLookupResults != nil {
	qualityResultsLoop:
		for {
			select {
			case result := <-r.qualityLookupResults:
				if result.requestID != r.qualityRequestID || result.videoURL != r.qualityLookupURL {
					continue
				}
				if r.cancelQualityLookup != nil {
					r.cancelQualityLookup()
					r.cancelQualityLookup = nil
				}
				r.qualityLoading = false
				if result.info.title != "" {
					r.confirmedVideoURL = result.videoURL
					r.videoTitle.SetValue(result.info.title)
					if result.info.thumbnail != nil {
						r.thumbnail.SetImage(ebiten.NewImageFromImage(result.info.thumbnail))
					}
				}
				r.videoCaptions = result.info.captions
				r.selectedCaptionID = ""
				r.additionalCaptionIDs = nil
				r.videoAudio = result.info.audio
				r.videoOtherAudio = result.info.otherAudio
				r.selectedAudioID = result.info.audio.defaultID()
				r.additionalAudioIDs = nil
				if result.err != nil || len(result.info.qualities) == 0 {
					r.qualityOptions = nil
					r.selectedQualityItag = 0
					if result.err != nil {
						r.qualityError = result.err.Error()
					} else {
						r.qualityError = "取得できる画質がありません。"
					}
				} else {
					r.qualityOptions = result.info.qualities
					r.selectedQualityItag = result.info.qualities[0].itag
					r.qualityError = ""
					r.confirmedVideoURL = result.videoURL
				}
				updated = true
			default:
				break qualityResultsLoop
			}
		}
	}
	if r.downloadResults != nil {
		select {
		case result := <-r.downloadResults:
			if r.cancelDownload != nil {
				r.cancelDownload()
				r.cancelDownload = nil
			}
			r.downloadInFlight = false
			if result.err != nil {
				r.downloadFailed = true
				if errors.Is(result.err, stdcontext.Canceled) && r.downloadStatus != "" {
					// Keep the more useful status set when the connection dropped.
				} else {
					r.downloadStatus = downloadFailureStatus(result.err)
				}
			} else {
				r.downloadFailed = false
				r.downloadStatus = "ダウンロードが完了しました。\n保存先：" + result.path
				r.downloadWarning = result.warning
				if result.warning != "" {
					r.downloadStatus += "\n" + result.warning
				}
			}
			updated = true
		default:
		}
	}
	if r.dvdDurationResults != nil {
	dvdDurationResultsLoop:
		for {
			select {
			case result := <-r.dvdDurationResults:
				if result.requestID != r.dvdDurationRequestID {
					continue
				}
				for i := range r.dvdVideoFiles {
					if r.dvdVideoFiles[i].path == result.path {
						r.dvdVideoFiles[i].duration = result.duration
						r.dvdVideoFiles[i].audioStreams = result.audioStreams
						updated = true
						break
					}
				}
			default:
				break dvdDurationResultsLoop
			}
		}
	}
	if r.dvdExportProgresses != nil {
	dvdExportProgressLoop:
		for {
			select {
			case progress := <-r.dvdExportProgresses:
				if r.dvdExportInFlight && !r.dvdExportCanceling {
					if progress.message != "" {
						r.dvdExportMessage = progress.message
						if strings.Contains(progress.message, "を変換しています…") {
							r.dvdExportConverting = true
						} else {
							r.dvdExportConverting = false
							r.dvdExportETA = ""
							if strings.Contains(progress.message, "DVD-Videoのメニュー") || strings.Contains(progress.message, "ISOイメージを書き出しています") {
								r.dvdExportETAEstimate.Reset()
							}
						}
					}
					if progress.hasPercent {
						r.dvdExportProgress = min(99, max(0, progress.progressPercent))
					}
					if progress.hasProgress {
						if r.dvdExportConverting {
							r.dvdExportETAEstimate.Observe(time.Now(), progress.convertedBytes, progress.estimatedBytes)
						}
						if progress.estimatedBytes > 0 {
							percent := int(float64(progress.convertedBytes) / float64(progress.estimatedBytes) * 100)
							r.dvdExportProgress = min(99, max(0, percent))
						} else {
							r.dvdExportProgress = 0
						}
					}
					updated = true
				}
			default:
				break dvdExportProgressLoop
			}
		}
	}
	if r.dvdExportResults != nil {
		select {
		case result := <-r.dvdExportResults:
			wasCanceled := r.dvdExportCanceling && result.err != nil
			r.dvdExportInFlight = false
			r.dvdExportCanceling = false
			r.dvdExportConverting = false
			r.dvdExportETA = ""
			r.dvdExportETAEstimate.Reset()
			r.dvdProgressDialog.SetOpen(false)
			if r.dvdExportCancel != nil {
				r.dvdExportCancel()
				r.dvdExportCancel = nil
			}
			if wasCanceled || errors.Is(result.err, stdcontext.Canceled) {
				r.dvdExportMessage = "処理をキャンセルしました。"
				r.dvdExportProgress = 0
			} else if result.err != nil {
				if result.burnFailure {
					r.dvdExportMessage = dvdBurnFailureMessage(result.err)
				} else {
					r.dvdExportMessage = dvdExportFailureMessage(result.err)
				}
			} else if result.burned {
				r.dvdExportMessage = "DVDへの書き込みが完了しました。"
				r.dvdExportProgress = 100
			} else {
				r.dvdExportMessage = "ISOファイルを書き出しました。\n保存先：" + result.path
				r.dvdExportProgress = 100
			}
			updated = true
		default:
		}
	}
	if updated {
		guigui.RequestRebuild()
	}
	return nil
}

// additionalCaptionCandidates are the tracks that can be added besides the
// default one.
func (r *root) additionalCaptionCandidates() []captionTrack {
	var tracks []captionTrack
	for _, track := range r.videoCaptions.Tracks {
		if track.ID != r.selectedCaptionID {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

// chosenAdditionalCaptions are the additional tracks the user checked, in
// the order of the track list. There are none without a default track.
func (r *root) chosenAdditionalCaptions() []captionTrack {
	if !r.showAdditionalCaptions() {
		return nil
	}
	var tracks []captionTrack
	for _, track := range r.additionalCaptionCandidates() {
		if r.additionalCaptionIDs[track.ID] {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

// showAdditionalCaptions reports whether the 追加の字幕 row is shown: only
// when a default track is chosen, and another track is left to add beside
// it.
func (r *root) showAdditionalCaptions() bool {
	if _, ok := r.videoCaptions.find(r.selectedCaptionID); !ok {
		return false
	}
	return len(r.videoCaptions.Tracks) > 1
}

// buildCaptionRows sets up the 字幕 row, which picks the default track, and
// the 追加の字幕 row, which picks any number of other tracks.
func (r *root) buildCaptionRows(context *guigui.Context) {
	gray := color.RGBA{R: 120, G: 120, B: 120, A: 255}
	r.captionLabel.SetValue("字幕")
	r.captionLabel.SetSelectable(false)
	r.captionLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	tracks := r.videoCaptions.Tracks
	if len(tracks) == 0 {
		note := "この動画には字幕がありません"
		r.captionNone.SetValue(note)
		r.captionNone.SetSelectable(false)
		r.captionNone.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		var noteStyle basicwidget.TextStyles
		noteStyle.SetColorInRange(0, len(note), gray)
		r.captionNone.SetOverrideStyles(&noteStyle, false)
		return
	}
	items := make([]basicwidget.SelectItem[string], 0, len(tracks)+1)
	items = append(items, basicwidget.SelectItem[string]{Text: "なし", Value: ""})
	for _, track := range tracks {
		items = append(items, basicwidget.SelectItem[string]{Text: track.label(), Value: track.ID})
	}
	r.captionSelect.SetItems(items)
	r.captionSelect.SelectItemByValue(r.selectedCaptionID)
	r.captionSelect.OnItemSelected(func(_ *guigui.Context, index int) {
		item, ok := r.captionSelect.ItemByIndex(index)
		if !ok || item.Value == r.selectedCaptionID {
			return
		}
		r.selectedCaptionID = item.Value
		// The default track is not also an additional one.
		delete(r.additionalCaptionIDs, item.Value)
		guigui.RequestRebuild()
	})
	context.SetEnabled(&r.captionSelect, !r.downloadInFlight)

	if !r.showAdditionalCaptions() {
		return
	}
	r.additionalCaptionLabel.SetValue("追加の字幕")
	r.additionalCaptionLabel.SetSelectable(false)
	r.additionalCaptionLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	r.additionalCaptionButton.SetText("選択")
	context.SetEnabled(&r.additionalCaptionButton, !r.downloadInFlight)
	r.additionalCaptionButton.OnDown(func(_ *guigui.Context) {
		if r.downloadInFlight {
			return
		}
		r.captionPicker.SetOpen(true)
		guigui.RequestRebuild()
	})
	var labels []string
	for _, track := range r.chosenAdditionalCaptions() {
		labels = append(labels, track.label())
	}
	summary := additionalTracksSummary(labels)
	r.additionalCaptionText.SetValue(summary)
	r.additionalCaptionText.SetSelectable(false)
	r.additionalCaptionText.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	var summaryStyle basicwidget.TextStyles
	if summary == "なし" {
		summaryStyle.SetColorInRange(0, len(summary), gray)
	}
	r.additionalCaptionText.SetOverrideStyles(&summaryStyle, false)
}

// buildCaptionPicker sets up the popup that picks the additional tracks.
func (r *root) buildCaptionPicker() {
	r.captionPicker.heading = "追加の字幕"
	r.captionPicker.choices = r.captionPicker.choices[:0]
	for _, track := range r.additionalCaptionCandidates() {
		r.captionPicker.choices = append(r.captionPicker.choices, trackChoice{ID: track.ID, Label: track.label()})
	}
	r.captionPicker.selected = r.additionalCaptionIDs
	r.captionPicker.onToggle = func(id string, on bool) {
		if r.downloadInFlight {
			return
		}
		if r.additionalCaptionIDs == nil {
			r.additionalCaptionIDs = make(map[string]bool)
		}
		if on {
			r.additionalCaptionIDs[id] = true
		} else {
			delete(r.additionalCaptionIDs, id)
		}
	}
}

// addCaptionRows adds the widgets of the caption rows that apply.
func (r *root) addCaptionRows(adder *guigui.ChildAdder) {
	adder.AddWidget(&r.captionLabel)
	if len(r.videoCaptions.Tracks) == 0 {
		adder.AddWidget(&r.captionNone)
		return
	}
	adder.AddWidget(&r.captionSelect)
	if r.showAdditionalCaptions() {
		adder.AddWidget(&r.additionalCaptionLabel)
		adder.AddWidget(&r.additionalCaptionButton)
		adder.AddWidget(&r.additionalCaptionText)
	}
}

// additionalAudioCandidates are the audio tracks that can be added besides
// the default one.
func (r *root) additionalAudioCandidates() []audioTrack {
	var tracks []audioTrack
	for _, track := range r.videoAudio.Tracks {
		if track.ID != r.selectedAudioID {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

// chosenAdditionalAudio are the additional audio tracks the user checked,
// in the order of the track list.
func (r *root) chosenAdditionalAudio() []audioTrack {
	var tracks []audioTrack
	for _, track := range r.additionalAudioCandidates() {
		if r.additionalAudioIDs[track.ID] {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

// showAudioRows reports whether the 音声 rows are shown: only for a video
// that has audio in more than one language. The rest have nothing to choose.
func (r *root) showAudioRows() bool {
	return len(r.videoAudio.Tracks) > 1
}

// showAudioNote reports whether the 音声 row only tells that the video has
// audio in other languages, which the built-in downloader cannot fetch.
func (r *root) showAudioNote() bool {
	return !r.showAudioRows() && r.videoOtherAudio
}

// buildAudioRows sets up the 音声 row, which picks the track to play by
// default, and the 追加の音声 row, which picks any number of other tracks.
func (r *root) buildAudioRows(context *guigui.Context) {
	r.audioLabel.SetValue("音声")
	r.audioLabel.SetSelectable(false)
	r.audioLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	if r.showAudioNote() {
		note := "他の言語の音声は、「設定」でyt-dlpを使うと選べます"
		r.audioNote.SetValue(note)
		r.audioNote.SetSelectable(false)
		r.audioNote.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
		var noteStyle basicwidget.TextStyles
		noteStyle.SetColorInRange(0, len(note), color.RGBA{R: 120, G: 120, B: 120, A: 255})
		r.audioNote.SetOverrideStyles(&noteStyle, false)
		return
	}
	if !r.showAudioRows() {
		return
	}
	items := make([]basicwidget.SelectItem[string], 0, len(r.videoAudio.Tracks))
	for _, track := range r.videoAudio.Tracks {
		items = append(items, basicwidget.SelectItem[string]{Text: track.label(), Value: track.ID})
	}
	r.audioSelect.SetItems(items)
	r.audioSelect.SelectItemByValue(r.selectedAudioID)
	r.audioSelect.OnItemSelected(func(_ *guigui.Context, index int) {
		item, ok := r.audioSelect.ItemByIndex(index)
		if !ok || item.Value == r.selectedAudioID || r.downloadInFlight {
			return
		}
		r.selectedAudioID = item.Value
		// The default track is not also an additional one.
		delete(r.additionalAudioIDs, item.Value)
		guigui.RequestRebuild()
	})
	context.SetEnabled(&r.audioSelect, !r.downloadInFlight)

	r.additionalAudioLabel.SetValue("追加の音声")
	r.additionalAudioLabel.SetSelectable(false)
	r.additionalAudioLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	r.additionalAudioButton.SetText("選択")
	context.SetEnabled(&r.additionalAudioButton, !r.downloadInFlight)
	r.additionalAudioButton.OnDown(func(_ *guigui.Context) {
		if r.downloadInFlight {
			return
		}
		r.audioPicker.SetOpen(true)
		guigui.RequestRebuild()
	})
	var labels []string
	for _, track := range r.chosenAdditionalAudio() {
		labels = append(labels, track.label())
	}
	summary := additionalTracksSummary(labels)
	r.additionalAudioText.SetValue(summary)
	r.additionalAudioText.SetSelectable(false)
	r.additionalAudioText.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	var summaryStyle basicwidget.TextStyles
	if summary == "なし" {
		summaryStyle.SetColorInRange(0, len(summary), color.RGBA{R: 120, G: 120, B: 120, A: 255})
	}
	r.additionalAudioText.SetOverrideStyles(&summaryStyle, false)
}

// buildAudioPicker sets up the popup that picks the additional audio
// tracks.
func (r *root) buildAudioPicker() {
	r.audioPicker.heading = "追加の音声"
	r.audioPicker.choices = r.audioPicker.choices[:0]
	for _, track := range r.additionalAudioCandidates() {
		r.audioPicker.choices = append(r.audioPicker.choices, trackChoice{ID: track.ID, Label: track.label()})
	}
	r.audioPicker.selected = r.additionalAudioIDs
	r.audioPicker.onToggle = func(id string, on bool) {
		if r.downloadInFlight {
			return
		}
		if r.additionalAudioIDs == nil {
			r.additionalAudioIDs = make(map[string]bool)
		}
		if on {
			r.additionalAudioIDs[id] = true
		} else {
			delete(r.additionalAudioIDs, id)
		}
	}
}

// addAudioRows adds the widgets of the audio rows, when they are shown.
func (r *root) addAudioRows(adder *guigui.ChildAdder) {
	if r.showAudioNote() {
		adder.AddWidget(&r.audioLabel)
		adder.AddWidget(&r.audioNote)
		return
	}
	if !r.showAudioRows() {
		return
	}
	adder.AddWidget(&r.audioLabel)
	adder.AddWidget(&r.audioSelect)
	adder.AddWidget(&r.additionalAudioLabel)
	adder.AddWidget(&r.additionalAudioButton)
	adder.AddWidget(&r.additionalAudioText)
}

// layoutAudioRows lays out the audio rows from top, when they are shown, and
// returns the top of the row after them.
func (r *root) layoutAudioRows(context *guigui.Context, layouter *guigui.ChildLayouter, content image.Rectangle, top int) int {
	u := basicwidget.UnitSize(context)
	left, right := downloadRowColumns(context, content)
	if r.showAudioNote() {
		layouter.LayoutWidget(&r.audioLabel, image.Rect(content.Min.X, top, left, top+u))
		layouter.LayoutWidget(&r.audioNote, image.Rect(left, top, content.Max.X, top+u))
		return top + 5*u/4
	}
	if !r.showAudioRows() {
		return top
	}
	layouter.LayoutWidget(&r.audioLabel, image.Rect(content.Min.X, top, left, top+u))
	layouter.LayoutWidget(&r.audioSelect, image.Rect(left, top, right, top+u))
	top += 5 * u / 4
	layouter.LayoutWidget(&r.additionalAudioLabel, image.Rect(content.Min.X, top, left, top+u))
	buttonWidth := 3 * u
	layouter.LayoutWidget(&r.additionalAudioButton, image.Rect(left, top, left+buttonWidth, top+u))
	layouter.LayoutWidget(&r.additionalAudioText, image.Rect(left+buttonWidth+u/2, top, content.Max.X, top+u))
	return top + 5*u/4
}

// chosenAudio returns the audio tracks to put in the file: the default one
// first, then the additional ones. It is empty for a video with a single
// audio track.
func (r *root) chosenAudio() []requestedAudio {
	if !r.showAudioRows() {
		return nil
	}
	var audio []requestedAudio
	if track, ok := r.videoAudio.find(r.selectedAudioID); ok {
		audio = append(audio, requestedAudio{Track: track, Default: true})
	}
	for _, track := range r.chosenAdditionalAudio() {
		audio = append(audio, requestedAudio{Track: track})
	}
	return audio
}

// The rows below a video share a label column, so that their controls line
// up; the download button sits at the right end, below 確認.
func downloadRowColumns(context *guigui.Context, content image.Rectangle) (left, right int) {
	u := basicwidget.UnitSize(context)
	return content.Min.X + 4*u, content.Max.X - 5*u
}

// layoutQualityStatus lays out the status shown in place of the qualities
// at top, as tall as its wrapped lines.
func (r *root) layoutQualityStatus(context *guigui.Context, layouter *guigui.ChildLayouter, content image.Rectangle, top int) {
	u := basicwidget.UnitSize(context)
	height := max(u, r.qualityStatus.Measure(context, guigui.FixedWidthConstraints(content.Dx())).Y)
	layouter.LayoutWidget(&r.qualityStatus, image.Rect(content.Min.X, top, content.Max.X, top+height))
}

// layoutQualityRow lays out the 画質 row and the download button at top.
func (r *root) layoutQualityRow(context *guigui.Context, layouter *guigui.ChildLayouter, content image.Rectangle, top int) {
	u := basicwidget.UnitSize(context)
	left, right := downloadRowColumns(context, content)
	layouter.LayoutWidget(&r.qualityLabel, image.Rect(content.Min.X, top, left, top+u))
	layouter.LayoutWidget(&r.qualitySelect, image.Rect(left, top, right, top+u))
	layouter.LayoutWidget(&r.downloadButton, image.Rect(content.Max.X-4*u, top, content.Max.X, top+u))
}

// layoutCaptionRows lays out the caption rows from top and returns the top
// of the last one.
func (r *root) layoutCaptionRows(context *guigui.Context, layouter *guigui.ChildLayouter, content image.Rectangle, top int) int {
	u := basicwidget.UnitSize(context)
	left, right := downloadRowColumns(context, content)
	layouter.LayoutWidget(&r.captionLabel, image.Rect(content.Min.X, top, left, top+u))
	if len(r.videoCaptions.Tracks) == 0 {
		layouter.LayoutWidget(&r.captionNone, image.Rect(left, top, content.Max.X, top+u))
		return top
	}
	layouter.LayoutWidget(&r.captionSelect, image.Rect(left, top, right, top+u))
	if !r.showAdditionalCaptions() {
		return top
	}
	top += 5 * u / 4
	layouter.LayoutWidget(&r.additionalCaptionLabel, image.Rect(content.Min.X, top, left, top+u))
	buttonWidth := 3 * u
	layouter.LayoutWidget(&r.additionalCaptionButton, image.Rect(left, top, left+buttonWidth, top+u))
	layouter.LayoutWidget(&r.additionalCaptionText, image.Rect(left+buttonWidth+u/2, top, content.Max.X, top+u))
	return top
}

// chosenCaptions returns the caption tracks to embed: the default one first,
// then the additional ones.
func (r *root) chosenCaptions() []requestedCaption {
	var captions []requestedCaption
	if track, ok := r.videoCaptions.find(r.selectedCaptionID); ok {
		captions = append(captions, requestedCaption{Track: track, Default: true})
	}
	for _, track := range r.chosenAdditionalCaptions() {
		captions = append(captions, requestedCaption{Track: track})
	}
	return captions
}

// openSettingsFolder shows the settings file in the file manager, writing
// it first when nothing was saved yet so that there is a file to show.
func (r *root) openSettingsFolder() error {
	if err := ensureSettingsFile(r.settingsPath, r.settings); err != nil {
		return err
	}
	return revealInFileManager(r.settingsPath)
}

// errSettingsInUse reports that the settings cannot be replaced now: a
// lookup or download is using them.
var errSettingsInUse = errors.New("動画の情報の取得中やダウンロード中は変更できません")

// reloadSettings reads the settings file again and uses what it says, as
// at startup.
func (r *root) reloadSettings() error {
	if r.downloadInFlight || r.qualityLoading {
		return errSettingsInUse
	}
	loaded, err := loadSettings(r.settingsPath)
	if err != nil {
		return err
	}
	r.settings = usableSettings(loaded, findYtDlp)
	// Qualities listed with the settings before cannot be used.
	r.clearConfirmedVideo()
	r.settingsPage.reloaded()
	return nil
}

// resetSettings empties the settings file and reloads it, which gives the
// defaults.
func (r *root) resetSettings() error {
	if r.downloadInFlight || r.qualityLoading {
		return errSettingsInUse
	}
	if err := resetSettingsFile(r.settingsPath); err != nil {
		return err
	}
	return r.reloadSettings()
}

// showsCookieMethod reports whether the download page says that yt-dlp is
// used with the cookies.txt chosen on the settings page.
func (r *root) showsCookieMethod() bool {
	return r.settings.UseYtDlp && r.settings.YtDlpCookiesPath != ""
}

// videoReadyToDownload reports whether the download page shows a confirmed
// video with qualities to choose from.
func (r *root) videoReadyToDownload() bool {
	return r.urlErrorMessage == "" && r.confirmedVideoURL != "" && r.confirmedVideoURL == strings.TrimSpace(r.urlInput.Value()) && len(r.qualityOptions) > 0
}

func (r *root) selectedDownloadQuality() (downloadQuality, bool) {
	for _, quality := range r.qualityOptions {
		if quality.itag == r.selectedQualityItag {
			return quality, true
		}
	}
	return downloadQuality{}, false
}

func (r *root) queueQualityLookup(videoURL string) {
	if r.cancelQualityLookup != nil {
		r.cancelQualityLookup()
		r.cancelQualityLookup = nil
	}
	r.qualityRequestID++
	r.qualityLookupURL = videoURL
	r.qualityLookupAt = time.Time{}
	r.qualityOptions = nil
	r.selectedQualityItag = 0
	r.videoCaptions = videoCaptions{}
	r.selectedCaptionID = ""
	r.additionalCaptionIDs = nil
	r.videoAudio = videoAudio{}
	r.videoOtherAudio = false
	r.selectedAudioID = ""
	r.additionalAudioIDs = nil
	r.confirmedVideoURL = ""
	r.videoTitle.SetValue("")
	r.thumbnail.SetImage(nil)
	r.qualityError = ""
	r.qualityLoading = false
	if videoURL == "" || validateYouTubeURL(videoURL) != "" || r.internetState != "接続あり" {
		return
	}
	r.qualityLoading = true
	r.qualityLookupAt = time.Now().Add(500 * time.Millisecond)
}

func (r *root) clearConfirmedVideo() {
	r.qualityRequestID++
	if r.cancelQualityLookup != nil {
		r.cancelQualityLookup()
		r.cancelQualityLookup = nil
	}
	if !r.downloadInFlight {
		r.downloadStatus = ""
		r.downloadWarning = ""
		r.downloadFailed = false
		r.downloadedBytes = 0
		r.totalBytes = 0
	}
	r.qualityOptions = nil
	r.selectedQualityItag = 0
	r.videoCaptions = videoCaptions{}
	r.selectedCaptionID = ""
	r.additionalCaptionIDs = nil
	r.videoAudio = videoAudio{}
	r.videoOtherAudio = false
	r.selectedAudioID = ""
	r.additionalAudioIDs = nil
	r.confirmedVideoURL = ""
	r.qualityError = ""
	r.qualityLoading = false
	r.qualityLookupAt = time.Time{}
	r.videoTitle.SetValue("")
	r.thumbnail.SetImage(nil)
}

func checkEnvironment() environmentCheck {
	result := environmentCheck{internet: "未接続"}
	conn, err := (&net.Dialer{Timeout: internetCheckTimeout}).Dial("tcp", "youtube.com:443")
	if err == nil {
		_ = conn.Close()
		result.internet = "接続あり"
	}
	result.drives = listDVDDrives()
	result.drive, result.disc = summarizeDVDDrives(result.drives)
	return result
}

// discBurningReportHasDVDWriter reports whether the output of
// "system_profiler SPDiscBurningDataType" lists a drive that writes DVDs.
// A drive has a line such as "DVD-Write: -R, -R DL, -RW, +R, +R DL, +RW",
// naming the media it writes; a drive that only reads has no such line or
// says "No". Without any drive the report is empty.
func discBurningReportHasDVDWriter(report string) bool {
	for line := range strings.SplitSeq(report, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "DVD-Write") {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" && !strings.EqualFold(value, "No") {
			return true
		}
	}
	return false
}

func (r *root) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	layouter.LayoutWidget(&r.background, b)
	layouter.LayoutWidget(&r.dvdWriteDialog, b)
	layouter.LayoutWidget(&r.dvdProgressDialog, b)
	layouter.LayoutWidget(&r.audioPicker, b)
	layouter.LayoutWidget(&r.captionPicker, b)
	layouter.LayoutWidget(&r.dvdInfoPopup, b)
	sidebarWidth := 9 * u
	layouter.LayoutWidget(&r.sidebar, image.Rect(b.Min.X, b.Min.Y, b.Min.X+sidebarWidth, b.Max.Y))
	layouter.LayoutWidget(&r.home, image.Rect(b.Min.X, b.Min.Y, b.Min.X+sidebarWidth, b.Min.Y+2*u))
	layouter.LayoutWidget(&r.downloads, image.Rect(b.Min.X, b.Min.Y+2*u, b.Min.X+sidebarWidth, b.Min.Y+4*u))
	layouter.LayoutWidget(&r.dvdWriting, image.Rect(b.Min.X, b.Min.Y+4*u, b.Min.X+sidebarWidth, b.Min.Y+6*u))
	navTop := b.Max.Y - 7*u
	layouter.LayoutWidget(&r.settingsMenu, image.Rect(b.Min.X, navTop-4*u, b.Min.X+sidebarWidth, navTop-2*u))
	layouter.LayoutWidget(&r.manualMenu, image.Rect(b.Min.X, navTop-2*u, b.Min.X+sidebarWidth, navTop))
	layouter.LayoutWidget(&r.nav, image.Rect(b.Min.X, navTop, b.Min.X+sidebarWidth, navTop+2*u))
	layouter.LayoutWidget(&r.divider, image.Rect(b.Min.X+u, navTop+2*u, b.Min.X+sidebarWidth-u, navTop+3*u))
	layouter.LayoutWidget(&r.about, image.Rect(b.Min.X+u, navTop+3*u, b.Min.X+sidebarWidth-u, navTop+7*u))
	content := image.Rect(b.Min.X+sidebarWidth+u, b.Min.Y+u, b.Max.X-u, b.Max.Y-u)
	r.dvdInfoPopup.area = content
	layouter.LayoutWidget(&r.title, image.Rect(content.Min.X, content.Min.Y, content.Max.X, content.Min.Y+u))
	if r.showSettings {
		layouter.LayoutWidget(&r.settingsPage, image.Rect(content.Min.X, content.Min.Y+3*u/2, content.Max.X, content.Max.Y))
	} else if r.showLicenses {
		layouter.LayoutWidget(&r.list, image.Rect(content.Min.X, content.Min.Y+u, content.Max.X, content.Max.Y))
	} else if r.showManual {
		layouter.LayoutWidget(&r.manualPanel, image.Rect(content.Min.X, content.Min.Y+u, content.Max.X, content.Max.Y))
	} else if r.showDVDWriting {
		queueTop := content.Min.Y + content.Dy()*11/20
		layouter.LayoutWidget(&r.dvdVideoHeading, image.Rect(content.Min.X, content.Min.Y+u, content.Max.X, content.Min.Y+2*u))
		layouter.LayoutWidget(&r.dvdVideoList, image.Rect(content.Min.X, content.Min.Y+2*u, content.Max.X, queueTop))
		queueListTop := queueTop + u
		estimateWidth := min(15*u, content.Dx()/2)
		layouter.LayoutWidget(&r.dvdQueueHeading, image.Rect(content.Min.X, queueTop, content.Max.X-estimateWidth, queueTop+u))
		layouter.LayoutWidget(&r.dvdQueueEstimate, image.Rect(content.Max.X-estimateWidth, queueTop, content.Max.X, queueTop+u))
		statusHeight := 0
		if r.dvdExportMessage != "" && !r.dvdExportInFlight {
			statusHeight = 2 * u
		}
		queueListBottom := content.Max.Y - statusHeight
		buttonWidth := 3 * u / 2
		buttonLeft := content.Max.X - buttonWidth
		buttonBounds := image.Rect(buttonLeft, queueListTop+u/4, content.Max.X, queueListTop+u/4+u)
		layouter.LayoutWidget(&r.dvdQueueList, image.Rect(content.Min.X, queueListTop, buttonLeft-u/4, queueListBottom))
		layouter.LayoutWidget(&r.dvdActionButton, buttonBounds)
		layouter.LayoutWidget(&r.dvdActionTooltip, buttonBounds)
		if r.dvdExportMessage != "" && !r.dvdExportInFlight {
			layouter.LayoutWidget(&r.dvdExportStatus, image.Rect(content.Min.X, queueListBottom, content.Max.X, content.Max.Y))
		}
	} else if !r.showDownloads {
		cardTop := content.Min.Y + 2*u
		cardHeight := 3 * u
		gap := u
		cardWidth := (content.Dx() - gap) / 2
		layouter.LayoutWidget(&r.internetCard, image.Rect(content.Min.X, cardTop, content.Min.X+cardWidth, cardTop+cardHeight))
		layouter.LayoutWidget(&r.driveCard, image.Rect(content.Min.X+cardWidth+gap, cardTop, content.Max.X, cardTop+cardHeight))
		dividerX := content.Min.X + cardWidth + gap/2
		layouter.LayoutWidget(&r.environmentDivider, image.Rect(dividerX, cardTop, dividerX+1, cardTop+cardHeight))
		noticeTop := cardTop + cardHeight
		layouter.LayoutWidget(&r.environmentNotice, image.Rect(content.Min.X, noticeTop, content.Max.X, noticeTop+7*u/2))
		helpTop := noticeTop + 4*u
		layouter.LayoutWidget(&r.helpDivider, image.Rect(content.Min.X, helpTop-u/2, content.Max.X, helpTop+u/2))
		layouter.LayoutWidget(&r.environmentHelp, image.Rect(content.Min.X, helpTop+u/2, content.Max.X, content.Max.Y))
		if len(r.helpPages) > 1 {
			// The pager shares the row of the help title, at its right end.
			pagerTop := helpTop + u/3
			buttonWidth := 5 * u / 2
			indicatorWidth := 2 * u
			nextLeft := content.Max.X - buttonWidth
			indicatorLeft := nextLeft - indicatorWidth
			prevLeft := indicatorLeft - buttonWidth
			layouter.LayoutWidget(&r.helpPrevButton, image.Rect(prevLeft, pagerTop, indicatorLeft, pagerTop+u))
			layouter.LayoutWidget(&r.helpPageIndicator, image.Rect(indicatorLeft, pagerTop, nextLeft, pagerTop+u))
			layouter.LayoutWidget(&r.helpNextButton, image.Rect(nextLeft, pagerTop, content.Max.X, pagerTop+u))
		}
	} else {
		if r.showDownloads {
			layouter.LayoutWidget(&r.urlLabel, image.Rect(content.Min.X, content.Min.Y+u, content.Max.X, content.Min.Y+2*u))
			// The page title only uses the left end of its row.
			methodRight := content.Max.X
			if r.showsCookieMethod() {
				// The cookie closes the line, about as tall as the text.
				size := u * 2 / 3
				top := content.Min.Y + (u-size)/2
				layouter.LayoutWidget(&r.downloadMethodCookie, image.Rect(content.Max.X-size, top, content.Max.X, top+size))
				methodRight -= size + u/5
			}
			layouter.LayoutWidget(&r.downloadMethod, image.Rect(content.Min.X+6*u, content.Min.Y, methodRight, content.Min.Y+u))
			layouter.LayoutWidget(&r.urlInput, image.Rect(content.Min.X, content.Min.Y+2*u, content.Max.X-5*u, content.Min.Y+3*u))
			layouter.LayoutWidget(&r.confirmButton, image.Rect(content.Max.X-4*u, content.Min.Y+2*u, content.Max.X, content.Min.Y+3*u))
			// The notice is a two-line footnote below the hint.
			noticeTop := content.Max.Y - 5*u/4
			hintTop := noticeTop
			// The hint gives its room to the video once one is shown.
			if !r.videoReadyToDownload() {
				hintTop = noticeTop - 3*u
				layouter.LayoutWidget(&r.urlHint, image.Rect(content.Min.X, hintTop, content.Max.X, noticeTop))
			}
			layouter.LayoutWidget(&r.urlNotice, image.Rect(content.Min.X, noticeTop, content.Max.X, content.Max.Y))
			if r.urlErrorMessage != "" {
				layouter.LayoutWidget(&r.urlError, image.Rect(content.Min.X, content.Min.Y+3*u, content.Max.X, content.Min.Y+4*u))
			}
			buttonTop := content.Min.Y + 5*u
			// progressBase is the top of the last row above the progress.
			progressBase := buttonTop
			if r.confirmedVideoURL == strings.TrimSpace(r.urlInput.Value()) {
				r.videoTitle.SetWrapMode(basicwidget.WrapModeNormal)
				r.videoTitle.SetMultiline(true)
				r.videoTitle.SetSelectable(false)
				qualityStatusVisible := r.qualityLoading || r.qualityError != "" || (strings.TrimSpace(r.urlInput.Value()) != "" && validateYouTubeURL(r.urlInput.Value()) == "" && r.internetState != "接続あり")
				if r.thumbnail.HasImage() {
					previewTop := content.Min.Y + 7*u/2
					previewBottom := content.Min.Y + 17*u/2
					layouter.LayoutWidget(&r.thumbnail, image.Rect(content.Min.X, previewTop, content.Min.X+7*u, previewBottom))
					r.videoTitle.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
					layouter.LayoutWidget(&r.videoTitle, image.Rect(content.Min.X+8*u, previewTop, content.Max.X, previewBottom))
					qualityTop := content.Min.Y + 9*u
					if len(r.qualityOptions) > 0 {
						r.layoutQualityRow(context, layouter, content, qualityTop)
					} else if qualityStatusVisible {
						r.layoutQualityStatus(context, layouter, content, qualityTop)
					}
					buttonTop = qualityTop
				} else {
					r.videoTitle.SetVerticalAlign(basicwidget.VerticalAlignTop)
					titleTop := content.Min.Y + 3*u + u/4
					titleSize := r.videoTitle.Measure(context, guigui.FixedWidthConstraints(content.Dx()))
					titleHeight := max(u, titleSize.Y)
					titleBottom := titleTop + titleHeight
					layouter.LayoutWidget(&r.videoTitle, image.Rect(content.Min.X, titleTop, content.Max.X, titleBottom))
					qualityTop := titleBottom + u/2
					if len(r.qualityOptions) > 0 {
						r.layoutQualityRow(context, layouter, content, qualityTop)
					} else if qualityStatusVisible {
						r.layoutQualityStatus(context, layouter, content, qualityTop)
					}
					buttonTop = qualityTop
				}
				if len(r.qualityOptions) > 0 {
					captionTop := r.layoutAudioRows(context, layouter, content, buttonTop+5*u/4)
					progressBase = r.layoutCaptionRows(context, layouter, content, captionTop)
				}
			} else if r.qualityLoading || r.qualityError != "" || (strings.TrimSpace(r.urlInput.Value()) != "" && validateYouTubeURL(r.urlInput.Value()) == "" && r.internetState != "接続あり") {
				r.layoutQualityStatus(context, layouter, content, content.Min.Y+3*u)
			}
			progressBase = max(progressBase, buttonTop)
			if r.downloadInFlight {
				layouter.LayoutWidget(&r.downloadProgressBar, image.Rect(content.Min.X, progressBase+u, content.Max.X, progressBase+u+u/2))
			}
			if r.showDownloadProgressText() {
				progressTextTop := progressBase + u
				if r.downloadInFlight {
					progressTextTop += u / 2
				}
				layouter.LayoutWidget(&r.downloadProgressText, image.Rect(content.Min.X, progressTextTop, content.Max.X, progressTextTop+u))
			}
			bodyTop := progressBase + 3*u
			if bodyTop < hintTop {
				layouter.LayoutWidget(&r.body, image.Rect(content.Min.X, bodyTop, content.Max.X, hintTop))
			}
		} else {
			layouter.LayoutWidget(&r.body, image.Rect(content.Min.X, content.Min.Y+2*u, content.Max.X, content.Max.Y))
		}
	}
}

func main() {
	appIcon, err := png.Decode(bytes.NewReader(embeddedAppIcon))
	if err != nil {
		log.Fatal(err)
	}
	ebiten.SetWindowIcon([]image.Image{appIcon})

	repo, err := licenses.NewRepository()
	if err != nil {
		log.Fatal(err)
	}
	helpPages, err := loadHelpPages()
	if err != nil {
		log.Fatal(err)
	}
	manualContent, err := fs.ReadFile(embeddedHelp, "manual/ja.txt")
	if err != nil {
		log.Fatal(err)
	}
	packages := repo.List()
	full := make([]licenses.Package, 0, len(packages))
	byName := make(map[string]int, len(packages))
	for _, summary := range packages {
		if pkg, ok := repo.Find(summary.Name); ok {
			if index, exists := byName[pkg.Name]; exists {
				current := &full[index]
				current.License += " / " + pkg.License
				if current.Text != pkg.Text {
					current.Text += "\n\n--- " + pkg.License + " ---\n\n" + pkg.Text
				}
				continue
			}
			byName[pkg.Name] = len(full)
			full = append(full, pkg)
		}
	}
	// The app works without its settings file, so a failure to find or read
	// it only means starting from the defaults.
	settingsPath, _ := settingsFilePath()
	var settings appSettings
	if settingsPath != "" {
		settings, _ = loadSettings(settingsPath)
	}
	settings = usableSettings(settings, findYtDlp)
	checkResults := make(chan environmentCheck, 1)
	app := &root{
		packages:             full,
		helpPages:            helpPages,
		manualContent:        strings.TrimSpace(string(manualContent)),
		settings:             settings,
		settingsPath:         settingsPath,
		rows:                 make([]licenseRow, len(full)),
		selected:             -1,
		checks:               checkResults,
		downloadResults:      make(chan downloadResult, 1),
		downloadProgresses:   make(chan downloadProgress, 1),
		dvdDurationResults:   make(chan dvdDurationResult, 32),
		qualityLookupResults: make(chan qualityLookupResult, 8),
		dvdExportResults:     make(chan dvdExportResult, 1),
		dvdExportProgresses:  make(chan dvdExportProgressUpdate, 1),
		internetState:        "確認中…",
		driveState:           "確認中…",
		discState:            dvdMediaChecking,
	}
	go monitorEnvironment(checkResults)
	runErr := guigui.Run(app, &guigui.RunOptions{Title: "YouTube to DVD", WindowSize: image.Pt(800, 520), WindowMinSize: image.Pt(600, 420)})
	app.stopDVDExport()
	if runErr != nil {
		log.Fatal(runErr)
	}
}
