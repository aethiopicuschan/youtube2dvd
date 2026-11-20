package main

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
)

type ytDlpCheckResult struct {
	requestID uint64
	version   string
	err       error
}

// settingsPage lets the user choose how videos are downloaded.
type settingsPage struct {
	guigui.DefaultWidget

	heading     basicwidget.Text
	useCheckbox basicwidget.Checkbox
	useLabel    basicwidget.Text
	pathLabel   basicwidget.Text
	pathInput   basicwidget.TextInput
	checkButton basicwidget.Button
	status      basicwidget.Text
	note        basicwidget.Text

	settings *appSettings
	// busy disables the controls while a lookup or download is using the
	// settings.
	busy bool
	// onChange is called after the user changed a setting. An error it
	// returns is shown on the page.
	onChange func() error

	statusMessage  string
	statusFailed   bool
	checking       bool
	checkRequestID uint64
	checkResults   chan ytDlpCheckResult
	cancelCheck    context.CancelFunc
}

func (p *settingsPage) changed() {
	p.resetCheck()
	if p.onChange != nil {
		if err := p.onChange(); err != nil {
			p.statusMessage = "設定を保存できませんでした: " + err.Error()
			p.statusFailed = true
		}
	}
	guigui.RequestRebuild()
}

func (p *settingsPage) resetCheck() {
	if p.cancelCheck != nil {
		p.cancelCheck()
		p.cancelCheck = nil
	}
	p.checkRequestID++
	p.checking = false
	p.statusMessage = ""
	p.statusFailed = false
}

func (p *settingsPage) startCheck() {
	p.resetCheck()
	if p.checkResults == nil {
		p.checkResults = make(chan ytDlpCheckResult, 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	p.cancelCheck = cancel
	p.checking = true
	requestID := p.checkRequestID
	runner := execYtDlpRunner{path: p.settings.YtDlpPath}
	results := p.checkResults
	go func() {
		version, err := ytDlpVersion(ctx, runner)
		select {
		case results <- ytDlpCheckResult{requestID: requestID, version: version, err: err}:
		default:
		}
	}()
}

func (p *settingsPage) Build(context *guigui.Context, adder *guigui.ChildAdder) error {
	for _, widget := range []guigui.Widget{
		&p.heading,
		&p.useCheckbox,
		&p.useLabel,
		&p.pathLabel,
		&p.pathInput,
		&p.checkButton,
		&p.status,
		&p.note,
	} {
		adder.AddWidget(widget)
	}

	p.heading.SetValue("ダウンロード方法")
	p.heading.SetSelectable(false)
	p.heading.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	var headingStyle basicwidget.TextStyles
	headingStyle.SetWeightInRange(0, len(p.heading.Value()), 600)
	p.heading.SetOverrideStyles(&headingStyle, false)

	p.useCheckbox.SetValue(p.settings.UseYtDlp)
	context.SetEnabled(&p.useCheckbox, !p.busy)
	p.useCheckbox.OnValueChanged(func(_ *guigui.Context, value bool) {
		if p.busy || p.settings.UseYtDlp == value {
			return
		}
		p.settings.UseYtDlp = value
		p.changed()
	})
	p.useLabel.SetValue("外部の yt-dlp を使ってダウンロードする")
	p.useLabel.SetSelectable(false)
	p.useLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)

	p.pathLabel.SetValue("yt-dlp 実行ファイルのパス")
	p.pathLabel.SetSelectable(false)
	p.pathLabel.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	p.pathInput.SetValue(p.settings.YtDlpPath)
	p.pathInput.SetPlaceholder("yt-dlp が見つかりません。パスを入力してください")
	context.SetEnabled(&p.pathInput, !p.busy)
	p.pathInput.OnValueChanged(func(_ *guigui.Context, text string, _ bool) {
		path := strings.TrimSpace(text)
		if p.busy || p.settings.YtDlpPath == path {
			return
		}
		p.settings.YtDlpPath = path
		p.changed()
	})

	p.checkButton.SetText("確認")
	context.SetEnabled(&p.checkButton, !p.checking && p.settings.YtDlpPath != "")
	p.checkButton.OnDown(func(_ *guigui.Context) {
		if p.checking || p.settings.YtDlpPath == "" {
			return
		}
		p.startCheck()
		guigui.RequestRebuild()
	})

	status := p.statusMessage
	if p.checking {
		status = "yt-dlp を確認しています…"
	}
	p.status.SetValue(status)
	p.status.SetSelectable(false)
	p.status.SetMultiline(true)
	p.status.SetWrapMode(basicwidget.WrapModeNormal)
	var statusStyle basicwidget.TextStyles
	switch {
	case p.checking:
		statusStyle.SetColorInRange(0, len(status), color.RGBA{R: 100, G: 100, B: 100, A: 255})
	case p.statusFailed:
		statusStyle.SetColorInRange(0, len(status), color.RGBA{R: 190, G: 35, B: 35, A: 255})
	default:
		statusStyle.SetColorInRange(0, len(status), color.RGBA{R: 35, G: 120, B: 65, A: 255})
	}
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

func (p *settingsPage) Tick(_ *guigui.Context, _ *guigui.WidgetBounds) error {
	if p.checkResults == nil {
		return nil
	}
	for {
		select {
		case result := <-p.checkResults:
			if result.requestID != p.checkRequestID {
				continue
			}
			if p.cancelCheck != nil {
				p.cancelCheck()
				p.cancelCheck = nil
			}
			p.checking = false
			if result.err != nil {
				p.statusMessage = result.err.Error()
				p.statusFailed = true
			} else {
				p.statusMessage = "yt-dlp " + result.version + " を実行できました。"
				p.statusFailed = false
			}
			guigui.RequestRebuild()
		default:
			return nil
		}
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
	layouter.LayoutWidget(&p.useLabel, image.Rect(b.Min.X+checkboxSize.X+u/3, top, b.Max.X, top+u))

	top += 3 * u / 2
	layouter.LayoutWidget(&p.pathLabel, image.Rect(b.Min.X, top, b.Max.X, top+u))
	top += u
	layouter.LayoutWidget(&p.pathInput, image.Rect(b.Min.X, top, b.Max.X-5*u, top+u))
	layouter.LayoutWidget(&p.checkButton, image.Rect(b.Max.X-4*u, top, b.Max.X, top+u))

	top += u + u/4
	layouter.LayoutWidget(&p.status, image.Rect(b.Min.X, top, b.Max.X, top+2*u))

	top += 2*u + u/4
	layouter.LayoutWidget(&p.note, image.Rect(b.Min.X, top, b.Max.X, b.Max.Y))
}
