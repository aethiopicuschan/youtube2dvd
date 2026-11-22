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
	"github.com/ncruces/zenity"
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

	heading      basicwidget.Text
	useCheckbox  basicwidget.Checkbox
	useLabel     basicwidget.Text
	status       basicwidget.Text
	cookieLabel  basicwidget.Text
	cookieButton iconButton
	cookieClear  iconButton
	cookieError  basicwidget.Text
	note         basicwidget.Text
	fileLink     basicwidget.Text
	reloadLink   basicwidget.Text
	resetLink    basicwidget.Text
	fileNote     basicwidget.Text

	settings *appSettings
	// busy disables the controls while a lookup or download is using the
	// settings.
	busy bool
	// onChange is called after a setting changed. An error it returns is
	// shown on the page.
	onChange func() error
	// openFile shows the settings file in the file manager, reloadFile
	// reads the settings file again and applies it, and resetFile empties
	// it and reloads it. They are nil without a settings file, which hides
	// the links.
	openFile   func() error
	reloadFile func() error
	resetFile  func() error
	// fileMessage tells why the last of them failed.
	fileMessage string
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
	saveError string
	// cookieErrorMessage tells why the chosen cookies.txt was not taken.
	cookieErrorMessage string
	lookupRequestID    uint64
	lookupResults      chan ytDlpLookupResult
	cancelLookup       context.CancelFunc
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
	if p.openFile != nil {
		adder.AddWidget(&p.fileLink)
		adder.AddWidget(&p.reloadLink)
		adder.AddWidget(&p.resetLink)
		if p.fileMessage != "" {
			adder.AddWidget(&p.fileNote)
		}
	}
	if p.showCookies() {
		adder.AddWidget(&p.cookieLabel)
		adder.AddWidget(&p.cookieButton)
		if p.settings.YtDlpCookiesPath != "" {
			adder.AddWidget(&p.cookieClear)
		}
		if p.cookieErrorMessage != "" {
			adder.AddWidget(&p.cookieError)
		}
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

	p.buildCookies(context)
	p.buildFileLinks()

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

// showCookies reports whether the cookies row is shown: with the yt-dlp in
// use, under its path and version.
func (p *settingsPage) showCookies() bool {
	return p.settings.UseYtDlp && !p.searching && p.errorMessage == "" && p.saveError == ""
}

// cookiesText is the cookies row.
func (p *settingsPage) cookiesText() string {
	if p.settings.YtDlpCookiesPath == "" {
		return "クッキー：なし"
	}
	return "クッキー：" + p.settings.YtDlpCookiesPath
}

// chooseCookies lets the user pick a cookies.txt and stores its path when
// yt-dlp can read it.
func (p *settingsPage) chooseCookies(choose func(current string) (string, error)) {
	path, err := choose(p.settings.YtDlpCookiesPath)
	if errors.Is(err, zenity.ErrCanceled) {
		return
	}
	if err == nil && path == "" {
		err = errors.New("ファイルが選択されていません")
	}
	if err == nil {
		err = checkCookiesFile(path)
	}
	if err != nil {
		p.cookieErrorMessage = "cookies.txt を使えません：" + err.Error()
		guigui.RequestRebuild()
		return
	}
	p.cookieErrorMessage = ""
	p.settings.YtDlpCookiesPath = path
	p.save()
}

// clearCookies stops passing a cookies.txt to yt-dlp.
func (p *settingsPage) clearCookies() {
	p.cookieErrorMessage = ""
	if p.settings.YtDlpCookiesPath == "" {
		guigui.RequestRebuild()
		return
	}
	p.settings.YtDlpCookiesPath = ""
	p.save()
}

func (p *settingsPage) buildCookies(context *guigui.Context) {
	gray := color.RGBA{R: 100, G: 100, B: 100, A: 255}
	text := p.cookiesText()
	p.cookieLabel.SetValue(text)
	p.cookieLabel.SetSelectable(false)
	p.cookieLabel.SetVerticalAlign(basicwidget.VerticalAlignTop)
	var labelStyle basicwidget.TextStyles
	labelStyle.SetColorInRange(0, len(text), gray)
	p.cookieLabel.SetOverrideStyles(&labelStyle, false)

	p.cookieButton.SetIcon(pencilIcon(context))
	p.cookieButton.SetTooltip("cookies.txtを探す")
	context.SetEnabled(&p.cookieButton, !p.busy)
	p.cookieButton.OnDown(func() {
		if p.busy {
			return
		}
		p.chooseCookies(chooseCookiesFile)
	})
	p.cookieClear.SetIcon(trashIcon(context))
	p.cookieClear.SetTooltip("クッキーの設定を解除する")
	context.SetEnabled(&p.cookieClear, !p.busy)
	p.cookieClear.OnDown(func() {
		if p.busy {
			return
		}
		p.clearCookies()
	})

	message := p.cookieErrorMessage
	p.cookieError.SetValue(message)
	p.cookieError.SetSelectable(false)
	p.cookieError.SetMultiline(true)
	p.cookieError.SetWrapMode(basicwidget.WrapModeNormal)
	var errorStyle basicwidget.TextStyles
	errorStyle.SetColorInRange(0, len(message), color.RGBA{R: 190, G: 35, B: 35, A: 255})
	p.cookieError.SetOverrideStyles(&errorStyle, false)
}

// buildFileLinks sets up the links that act on the settings file, and the
// reason the last one used failed, if it did.
func (p *settingsPage) buildFileLinks() {
	link := func(text *basicwidget.Text, label string, onClick func()) {
		text.SetValue(label)
		text.SetSelectable(false)
		var style basicwidget.TextStyles
		style.SetColorInRange(0, len(label), color.RGBA{R: 30, G: 100, B: 220, A: 255})
		style.SetUnderlineInRange(0, len(label), true)
		text.SetOverrideStyles(&style, false)
		text.SetHotspotRanges([]basicwidget.TextRange{{StartInBytes: 0, EndInBytes: len(label)}})
		text.OnHotspotUp(func(_ *guigui.Context, _ basicwidget.TextRange) {
			onClick()
		})
	}
	link(&p.fileLink, "設定ファイルの場所を開く", func() {
		p.runFileAction(p.openFile, "設定ファイルの場所を開けませんでした")
	})
	link(&p.reloadLink, "設定をリロードする", func() {
		p.runFileAction(p.reloadFile, "設定をリロードできませんでした")
	})
	link(&p.resetLink, "設定をリセットする", func() {
		p.runFileAction(p.resetFile, "設定をリセットできませんでした")
	})

	message := p.fileMessage
	p.fileNote.SetValue(message)
	p.fileNote.SetSelectable(false)
	p.fileNote.SetMultiline(true)
	p.fileNote.SetWrapMode(basicwidget.WrapModeNormal)
	var style basicwidget.TextStyles
	style.SetColorInRange(0, len(message), color.RGBA{R: 190, G: 35, B: 35, A: 255})
	p.fileNote.SetOverrideStyles(&style, false)
}

// runFileAction runs one of the settings file actions, and tells on the
// page when it failed, after failed. Success says nothing.
func (p *settingsPage) runFileAction(action func() error, failed string) {
	p.fileMessage = ""
	if action != nil {
		if err := action(); err != nil {
			p.fileMessage = failed + ": " + err.Error()
		}
	}
	guigui.RequestRebuild()
}

// reloaded forgets what the page knew about the settings it showed, after
// they were read again from the file: the yt-dlp in use is checked again.
func (p *settingsPage) reloaded() {
	p.cancel()
	p.errorMessage = ""
	p.saveError = ""
	p.cookieErrorMessage = ""
	p.version = ""
	p.versionChecked = false
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
	statusHeight := p.status.Measure(context, guigui.FixedWidthConstraints(b.Max.X-labelLeft)).Y
	if !p.showCookies() {
		// Room for a message of two lines, so that the note below does not
		// move while yt-dlp is looked for.
		statusHeight = max(2*u, statusHeight)
	}
	layouter.LayoutWidget(&p.status, image.Rect(labelLeft, top, b.Max.X, top+statusHeight))

	top += statusHeight
	if p.showCookies() {
		// The cookies row continues the lines of the status, with the
		// button right after its text.
		buttons := 1
		if p.settings.YtDlpCookiesPath != "" {
			buttons = 2
		}
		// The text takes one line of the status's height, so that the lines
		// are evenly spaced; the buttons are centred on it.
		labelSize := p.cookieLabel.Measure(context, guigui.Constraints{})
		labelWidth := min(labelSize.X, b.Max.X-labelLeft-buttons*u-u/4)
		lineHeight := labelSize.Y
		layouter.LayoutWidget(&p.cookieLabel, image.Rect(labelLeft, top, labelLeft+labelWidth, top+lineHeight))
		buttonLeft := labelLeft + labelWidth + u/4
		buttonTop := top + (lineHeight-u)/2
		layouter.LayoutWidget(&p.cookieButton, image.Rect(buttonLeft, buttonTop, buttonLeft+u, buttonTop+u))
		if buttons == 2 {
			layouter.LayoutWidget(&p.cookieClear, image.Rect(buttonLeft+u, buttonTop, buttonLeft+2*u, buttonTop+u))
		}
		// The buttons reach a little past the line, which only their clear
		// margin does.
		top += lineHeight
		if p.cookieErrorMessage != "" {
			errorHeight := p.cookieError.Measure(context, guigui.FixedWidthConstraints(b.Max.X-labelLeft)).Y
			layouter.LayoutWidget(&p.cookieError, image.Rect(labelLeft, top, b.Max.X, top+errorHeight))
			top += errorHeight
		}
	}
	top += u / 2
	bottom := b.Max.Y
	if p.openFile != nil {
		// The links to the settings file sit at the bottom of the page, side
		// by side as far as the width allows. The message goes above them,
		// so that the links stay where they are when it appears.
		links := []*basicwidget.Text{&p.fileLink, &p.reloadLink, &p.resetLink}
		var rects []image.Rectangle
		x, y, lineHeight := b.Min.X, 0, 0
		for _, link := range links {
			size := link.Measure(context, guigui.Constraints{})
			if x > b.Min.X && x+size.X > b.Max.X {
				x, y = b.Min.X, y+lineHeight
			}
			rects = append(rects, image.Rect(x, y, x+size.X, y+size.Y))
			lineHeight = max(lineHeight, size.Y)
			x += size.X + u
		}
		height := y + lineHeight
		messageHeight := 0
		if p.fileMessage != "" {
			messageHeight = p.fileNote.Measure(context, guigui.FixedWidthConstraints(b.Dx())).Y
		}
		linksTop := b.Max.Y - height
		for i, link := range links {
			layouter.LayoutWidget(link, rects[i].Add(image.Pt(0, linksTop)))
		}
		bottom = linksTop - messageHeight
		if messageHeight > 0 {
			layouter.LayoutWidget(&p.fileNote, image.Rect(b.Min.X, bottom, b.Max.X, linksTop))
		}
	}
	layouter.LayoutWidget(&p.note, image.Rect(b.Min.X, top, b.Max.X, max(top, bottom)))
}
