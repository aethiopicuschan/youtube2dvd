package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
)

// ytDlpLookupResult is what a search for yt-dlp found: the executable and
// the version it reports, or why it cannot be used.
type ytDlpLookupResult struct {
	requestID uint64
	path      string
	version   string
	err       error
}

// errYtDlpNotFound reports that no yt-dlp was found where it is usually
// installed.
var errYtDlpNotFound = errors.New("yt-dlp が見つかりませんでした。yt-dlp をインストールしてから、もう一度チェックを入れてください。")

// settingsPage lets the user choose how videos are downloaded. yt-dlp is
// turned on with a checkbox alone: the app then looks for the executable
// itself, checks that it runs, and only shows where it is.
type settingsPage struct {
	guigui.DefaultWidget

	heading     basicwidget.Text
	useCheckbox basicwidget.Checkbox
	useLabel    basicwidget.Text
	status      basicwidget.Text
	note        basicwidget.Text

	settings *appSettings
	// busy disables the controls while a lookup or download is using the
	// settings.
	busy bool
	// onChange is called after a setting changed. An error it returns is
	// shown on the page.
	onChange func() error
	// lookup finds and checks yt-dlp; nil uses lookUpYtDlp. Tests replace
	// it.
	lookup func(ctx context.Context) (path, version string, err error)

	// searching is set while yt-dlp is looked for after the checkbox was
	// turned on; the checkbox shows on, but yt-dlp is not used yet.
	searching bool
	// version is the version of the yt-dlp in use, once it was checked.
	version        string
	versionChecked bool
	errorMessage   string
	// saveError tells that the settings could not be saved; it goes away
	// with the next save that works.
	saveError       string
	lookupRequestID uint64
	lookupResults   chan ytDlpLookupResult
	cancelLookup    context.CancelFunc
}

// lookUpYtDlp finds yt-dlp where it is usually installed and checks that it
// runs.
func lookUpYtDlp(ctx context.Context) (string, string, error) {
	path := findYtDlp()
	if path == "" {
		return "", "", errYtDlpNotFound
	}
	version, err := ytDlpVersion(ctx, execYtDlpRunner{path: path})
	return path, version, err
}

// checkVersion checks the yt-dlp at path, which is already in use, so that
// the page can show its version.
func checkVersion(path string) func(ctx context.Context) (string, string, error) {
	return func(ctx context.Context) (string, string, error) {
		version, err := ytDlpVersion(ctx, execYtDlpRunner{path: path})
		return path, version, err
	}
}

func (p *settingsPage) save() {
	p.saveError = ""
	if p.onChange != nil {
		if err := p.onChange(); err != nil {
			p.saveError = "設定を保存できませんでした: " + err.Error()
		}
	}
	guigui.RequestRebuild()
}

// opened is called each time the page is shown, so that the yt-dlp in use
// is checked again: it may have been removed or updated meanwhile.
func (p *settingsPage) opened() {
	if !p.searching && p.cancelLookup == nil {
		p.versionChecked = false
	}
}

func (p *settingsPage) cancel() {
	if p.cancelLookup != nil {
		p.cancelLookup()
		p.cancelLookup = nil
	}
	p.lookupRequestID++
	p.searching = false
}

// start runs lookup in the background; Tick takes its result.
func (p *settingsPage) start(lookup func(ctx context.Context) (string, string, error)) {
	p.cancel()
	if p.lookupResults == nil {
		p.lookupResults = make(chan ytDlpLookupResult, 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	p.cancelLookup = cancel
	requestID := p.lookupRequestID
	results := p.lookupResults
	go func() {
		path, version, err := lookup(ctx)
		select {
		case results <- ytDlpLookupResult{requestID: requestID, path: path, version: version, err: err}:
		default:
		}
	}()
}

// turnOn starts looking for yt-dlp. It is used once it was found and runs.
func (p *settingsPage) turnOn() {
	p.errorMessage = ""
	p.version = ""
	p.versionChecked = false
	lookup := p.lookup
	if lookup == nil {
		lookup = lookUpYtDlp
	}
	p.start(lookup)
	p.searching = true
	guigui.RequestRebuild()
}

func (p *settingsPage) turnOff() {
	p.cancel()
	p.errorMessage = ""
	p.settings.UseYtDlp = false
	p.save()
}

func (p *settingsPage) Build(context *guigui.Context, adder *guigui.ChildAdder) error {
	for _, widget := range []guigui.Widget{
		&p.heading,
		&p.useCheckbox,
		&p.useLabel,
		&p.status,
		&p.note,
	} {
		adder.AddWidget(widget)
	}

	// The yt-dlp in use is checked when the page is shown, but not while a
	// lookup or download uses it.
	if p.settings.UseYtDlp && !p.busy && !p.searching && !p.versionChecked && p.cancelLookup == nil {
		p.start(checkVersion(p.settings.YtDlpPath))
	}

	p.heading.SetValue("ダウンロード方法")
	p.heading.SetSelectable(false)
	p.heading.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	var headingStyle basicwidget.TextStyles
	headingStyle.SetWeightInRange(0, len(p.heading.Value()), 600)
	p.heading.SetOverrideStyles(&headingStyle, false)

	p.useCheckbox.SetValue(p.settings.UseYtDlp || p.searching)
	context.SetEnabled(&p.useCheckbox, !p.busy && !p.searching)
	p.useCheckbox.OnValueChanged(func(_ *guigui.Context, value bool) {
		if p.busy || p.searching || value == p.settings.UseYtDlp {
			return
		}
		if value {
			p.turnOn()
		} else {
			p.turnOff()
		}
	})
	useLabel := "外部の yt-dlp を使ってダウンロードする"
	p.useLabel.SetValue(useLabel)
	p.useLabel.SetSelectable(false)
	p.useLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	// The label is greyed out together with its checkbox.
	var useLabelStyle basicwidget.TextStyles
	if p.busy || p.searching {
		useLabelStyle.SetColorInRange(0, len(useLabel), color.RGBA{R: 145, G: 145, B: 145, A: 255})
	}
	p.useLabel.SetOverrideStyles(&useLabelStyle, false)

	status, statusColor := p.statusText()
	p.status.SetValue(status)
	p.status.SetSelectable(false)
	p.status.SetMultiline(true)
	p.status.SetWrapMode(basicwidget.WrapModeNormal)
	var statusStyle basicwidget.TextStyles
	statusStyle.SetColorInRange(0, len(status), statusColor)
	p.status.SetOverrideStyles(&statusStyle, false)

	const ytDlpURL = "https://github.com/yt-dlp/yt-dlp"
	const ytDlpURLText = "yt-dlp"
	note := "yt-dlpを別途導入することで、より高画質な動画をダウンロードできます。\n利用したい場合はご自身でインストールしていただく必要があります。"
	p.note.SetValue(note)
	p.note.SetSelectable(false)
	p.note.SetMultiline(true)
	p.note.SetWrapMode(basicwidget.WrapModeNormal)
	var noteStyle basicwidget.TextStyles
	noteStyle.SetColorInRange(0, len(note), color.RGBA{R: 90, G: 90, B: 90, A: 255})
	linkStart := strings.Index(note, ytDlpURLText)
	linkEnd := linkStart + len(ytDlpURLText)
	noteStyle.SetColorInRange(linkStart, linkEnd, color.RGBA{R: 30, G: 100, B: 220, A: 255})
	noteStyle.SetUnderlineInRange(linkStart, linkEnd, true)
	p.note.SetOverrideStyles(&noteStyle, false)
	p.note.SetHotspotRanges([]basicwidget.TextRange{{StartInBytes: linkStart, EndInBytes: linkEnd}})
	p.note.OnHotspotUp(func(_ *guigui.Context, _ basicwidget.TextRange) {
		openURL(ytDlpURL)
	})
	return nil
}

// statusText is what the page says about yt-dlp below the checkbox.
func (p *settingsPage) statusText() (string, color.Color) {
	gray := color.RGBA{R: 100, G: 100, B: 100, A: 255}
	switch {
	case p.saveError != "":
		return p.saveError, color.RGBA{R: 190, G: 35, B: 35, A: 255}
	case p.searching:
		return "yt-dlp を探しています…", gray
	case p.errorMessage != "":
		return p.errorMessage, color.RGBA{R: 190, G: 35, B: 35, A: 255}
	case p.settings.UseYtDlp:
		text := "使用する yt-dlp：" + p.settings.YtDlpPath
		if p.version != "" {
			text += "\nバージョン：" + p.version
		}
		return text, gray
	}
	return "", gray
}

// poll takes the result of a search or check, if there is one. The app
// calls it on every frame, whichever page is shown, so that a search the user
// left the page during still ends. While busy, a lookup or download uses the
// settings, and the result waits.
func (p *settingsPage) poll(busy bool) {
	p.busy = busy
	if p.lookupResults == nil || busy {
		return
	}
	for {
		select {
		case result := <-p.lookupResults:
			if result.requestID != p.lookupRequestID {
				continue
			}
			p.handle(result)
			guigui.RequestRebuild()
		default:
			return
		}
	}
}

// handle takes the result of a search for yt-dlp, or of the check of the
// one in use.
func (p *settingsPage) handle(result ytDlpLookupResult) {
	if p.cancelLookup != nil {
		p.cancelLookup()
		p.cancelLookup = nil
	}
	wasSearching := p.searching
	p.searching = false
	p.versionChecked = true
	if result.err != nil {
		// A yt-dlp that cannot be found or does not run is not used.
		p.errorMessage = result.err.Error()
		p.version = ""
		if wasSearching || p.settings.UseYtDlp {
			p.settings.UseYtDlp = false
			p.save()
		}
		return
	}
	p.errorMessage = ""
	p.version = result.version
	if wasSearching {
		p.settings.YtDlpPath = result.path
		p.settings.UseYtDlp = true
		p.save()
	}
}

func (p *settingsPage) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)

	top := b.Min.Y
	layouter.LayoutWidget(&p.heading, image.Rect(b.Min.X, top, b.Max.X, top+u))

	top += 3 * u / 2
	checkboxSize := p.useCheckbox.Measure(context, guigui.Constraints{})
	checkboxTop := top + (u-checkboxSize.Y)/2
	layouter.LayoutWidget(&p.useCheckbox, image.Rect(b.Min.X, checkboxTop, b.Min.X+checkboxSize.X, checkboxTop+checkboxSize.Y))
	labelLeft := b.Min.X + checkboxSize.X + u/3
	layouter.LayoutWidget(&p.useLabel, image.Rect(labelLeft, top, b.Max.X, top+u))

	// The status is indented under the label of the checkbox it belongs to,
	// and as tall as its wrapped lines.
	top += u + u/4
	statusHeight := max(2*u, p.status.Measure(context, guigui.FixedWidthConstraints(b.Max.X-labelLeft)).Y)
	layouter.LayoutWidget(&p.status, image.Rect(labelLeft, top, b.Max.X, top+statusHeight))

	top += statusHeight + u/2
	layouter.LayoutWidget(&p.note, image.Rect(b.Min.X, top, b.Max.X, b.Max.Y))
}
