package main

import (
	"image"
	"strings"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
)

type dvdWriteDialog struct {
	guigui.DefaultWidget

	popup   basicwidget.Popup
	content dvdWriteDialogContent

	queue     []dvdVideoFile
	estimate  string
	onSaveISO func(menuTitle string)
}

func (d *dvdWriteDialog) SetQueue(queue []dvdVideoFile, estimate string) {
	d.queue = append(d.queue[:0], queue...)
	d.estimate = estimate
}

func (d *dvdWriteDialog) SetOpen(open bool) {
	d.popup.SetOpen(open)
}

func (d *dvdWriteDialog) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	adder.AddWidget(&d.popup)
	d.content.dialog = d
	d.popup.SetContent(&d.content)
	d.popup.SetModal(true)
	d.popup.SetBackgroundDark(true)
	d.popup.SetCloseByClickingOutside(true)
	d.popup.SetAnimated(true)
	return nil
}

func (d *dvdWriteDialog) Layout(context *guigui.Context, _ *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	size := d.content.Measure(context, guigui.Constraints{})
	app := context.AppBounds()
	pos := image.Pt(app.Min.X+(app.Dx()-size.X)/2, app.Min.Y+(app.Dy()-size.Y)/2)
	layouter.LayoutWidget(&d.popup, image.Rectangle{Min: pos, Max: pos.Add(size)})
}

type dvdWriteDialogContent struct {
	guigui.DefaultWidget

	dialog        *dvdWriteDialog
	heading       basicwidget.Text
	queueHeading  basicwidget.Text
	queueList     basicwidget.List[string]
	estimate      basicwidget.Text
	menuTitleText basicwidget.Text
	menuTitle     basicwidget.TextInput
	burnNote      basicwidget.Text
	burnButton    basicwidget.Button
	isoButton     basicwidget.Button
	cancelButton  basicwidget.Button
}

func (c *dvdWriteDialogContent) Build(context *guigui.Context, adder *guigui.ChildAdder) error {
	for _, widget := range []guigui.Widget{
		&c.heading,
		&c.queueHeading,
		&c.queueList,
		&c.estimate,
		&c.menuTitleText,
		&c.menuTitle,
		&c.burnNote,
		&c.burnButton,
		&c.isoButton,
		&c.cancelButton,
	} {
		adder.AddWidget(widget)
	}

	c.heading.SetValue("書き込み内容の確認")
	c.heading.SetSelectable(false)
	c.queueHeading.SetValue("書き込む動画")
	c.queueHeading.SetSelectable(false)
	c.queueHeading.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	c.queueList.SetStyle(basicwidget.ListStyleNormal)
	c.queueList.SetStripeVisible(true)
	items := make([]basicwidget.ListItem[string], 0, max(1, len(c.dialog.queue)))
	if len(c.dialog.queue) == 0 {
		items = append(items, basicwidget.ListItem[string]{Text: "キューに動画がありません。", Unselectable: true})
	} else {
		for _, file := range c.dialog.queue {
			row := &dvdVideoRow{name: file.name, size: formatBytes(file.size)}
			items = append(items, basicwidget.ListItem[string]{Content: row, Value: file.path, Unselectable: true})
		}
	}
	c.queueList.SetItems(items)
	c.estimate.SetValue(c.dialog.estimate)
	c.estimate.SetSelectable(false)
	c.estimate.SetWrapMode(basicwidget.WrapModeNone)
	c.estimate.SetHorizontalAlign(basicwidget.HorizontalAlignEnd)
	c.menuTitleText.SetValue("DVDメニュータイトル")
	c.menuTitleText.SetSelectable(false)
	c.menuTitleText.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	c.menuTitle.SetPlaceholder("タイトル")
	hasMenuTitle := strings.TrimSpace(c.menuTitle.Value()) != ""
	c.burnNote.SetValue("DVDへの書き込みは現在利用できません。")
	c.burnNote.SetSelectable(false)
	c.burnNote.SetWrapMode(basicwidget.WrapModeNormal)
	c.burnButton.SetText("DVDに書き込む")
	const directDVDWritingAvailable = false
	context.SetEnabled(&c.burnButton, hasMenuTitle && directDVDWritingAvailable)
	c.isoButton.SetText("ISOファイルとして保存する")
	context.SetEnabled(&c.isoButton, hasMenuTitle && len(c.dialog.queue) > 0 && c.dialog.onSaveISO != nil)
	c.isoButton.OnDown(func(_ *guigui.Context) {
		menuTitle := strings.TrimSpace(c.menuTitle.Value())
		if menuTitle == "" || c.dialog.onSaveISO == nil || len(c.dialog.queue) == 0 {
			return
		}
		c.dialog.SetOpen(false)
		c.dialog.onSaveISO(menuTitle)
	})
	c.cancelButton.SetText("キャンセル")
	c.cancelButton.OnDown(func(_ *guigui.Context) {
		c.dialog.SetOpen(false)
	})
	return nil
}

func (c *dvdWriteDialogContent) Measure(context *guigui.Context, _ guigui.Constraints) image.Point {
	u := basicwidget.UnitSize(context)
	app := context.AppBounds()
	queueHeight := c.queueListHeight(context)
	// Keep the dialog close to its contents instead of reserving a large,
	// empty list area for short queues.
	height := 19*u/2 + queueHeight
	return image.Pt(min(24*u, app.Dx()-2*u), min(height, app.Dy()-2*u))
}

func (c *dvdWriteDialogContent) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	x := b.Min.X + u/2
	width := b.Dx() - 2*(x-b.Min.X)

	top := b.Min.Y + u/3
	layouter.LayoutWidget(&c.heading, image.Rect(x, top, x+width, top+u))

	queueHeadingTop := top + 3*u/2
	layouter.LayoutWidget(&c.queueHeading, image.Rect(x, queueHeadingTop, x+width, queueHeadingTop+u))

	listTop := queueHeadingTop + u + u/4
	listBottom := listTop + c.queueListHeight(context)
	layouter.LayoutWidget(&c.queueList, image.Rect(x, listTop, x+width, listBottom))
	layouter.LayoutWidget(&c.estimate, image.Rect(x, listBottom, x+width, listBottom+u))

	menuLabelTop := listBottom + 5*u/4
	layouter.LayoutWidget(&c.menuTitleText, image.Rect(x, menuLabelTop, x+width, menuLabelTop+u))
	inputTop := menuLabelTop + u
	layouter.LayoutWidget(&c.menuTitle, image.Rect(x, inputTop, x+width, inputTop+u))
	noteTop := inputTop + u + u/4
	layouter.LayoutWidget(&c.burnNote, image.Rect(x, noteTop, x+width, noteTop+u))

	buttonTop := b.Max.Y - u - u/3
	buttonGap := u / 4
	groupWidth := 22*u + 2*buttonGap
	groupX := x + width - groupWidth
	cancelWidth := 11 * u / 2
	burnWidth := 7 * u
	isoWidth := 19 * u / 2
	layouter.LayoutWidget(&c.cancelButton, image.Rect(groupX, buttonTop, groupX+cancelWidth, buttonTop+u))
	layouter.LayoutWidget(&c.burnButton, image.Rect(groupX+cancelWidth+buttonGap, buttonTop, groupX+cancelWidth+buttonGap+burnWidth, buttonTop+u))
	isoX := groupX + cancelWidth + buttonGap + burnWidth + buttonGap
	layouter.LayoutWidget(&c.isoButton, image.Rect(isoX, buttonTop, isoX+isoWidth, buttonTop+u))
}

func (c *dvdWriteDialogContent) queueListHeight(context *guigui.Context) int {
	u := basicwidget.UnitSize(context)
	rows := max(1, len(c.dialog.queue))
	return min(5*u, (rows+1)*u)
}
