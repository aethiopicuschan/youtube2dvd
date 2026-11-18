package main

import (
	"image"
	"strconv"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
)

type dvdProgressDialog struct {
	guigui.DefaultWidget

	popup   basicwidget.Popup
	content dvdProgressDialogContent

	title     string
	message   string
	progress  int
	eta       string
	canceling bool
	onCancel  func()
}

func (d *dvdProgressDialog) SetOpen(open bool) {
	d.popup.SetOpen(open)
}

func (d *dvdProgressDialog) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	adder.AddWidget(&d.popup)
	d.content.dialog = d
	d.popup.SetContent(&d.content)
	d.popup.SetModal(true)
	d.popup.SetBackgroundDark(true)
	d.popup.SetCloseByClickingOutside(false)
	d.popup.SetAnimated(true)
	return nil
}

func (d *dvdProgressDialog) Layout(context *guigui.Context, _ *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	size := d.content.Measure(context, guigui.Constraints{})
	app := context.AppBounds()
	pos := image.Pt(app.Min.X+(app.Dx()-size.X)/2, app.Min.Y+(app.Dy()-size.Y)/2)
	layouter.LayoutWidget(&d.popup, image.Rectangle{Min: pos, Max: pos.Add(size)})
}

type dvdProgressDialogContent struct {
	guigui.DefaultWidget

	dialog   *dvdProgressDialog
	heading  basicwidget.Text
	status   basicwidget.Text
	progress basicwidget.ProgressBar
	details  basicwidget.Text
	cancel   basicwidget.Button
}

func (c *dvdProgressDialogContent) Build(context *guigui.Context, adder *guigui.ChildAdder) error {
	for _, widget := range []guigui.Widget{
		&c.heading,
		&c.status,
		&c.progress,
		&c.details,
		&c.cancel,
	} {
		adder.AddWidget(widget)
	}

	c.heading.SetValue(c.dialog.title)
	c.heading.SetSelectable(false)
	c.heading.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	c.statusText()
	c.progress.SetMinimumValue(0)
	c.progress.SetMaximumValue(100)
	c.progress.SetValue(min(100, max(0, c.dialog.progress)))
	c.details.SetValue(c.progressDetails())
	c.details.SetSelectable(false)
	c.details.SetHorizontalAlign(basicwidget.HorizontalAlignEnd)
	c.cancel.SetText("キャンセル")
	if c.dialog.canceling {
		c.cancel.SetText("キャンセル中…")
	}
	context.SetEnabled(&c.cancel, !c.dialog.canceling && c.dialog.onCancel != nil)
	c.cancel.OnDown(func(_ *guigui.Context) {
		if c.dialog.canceling || c.dialog.onCancel == nil {
			return
		}
		c.dialog.onCancel()
	})
	return nil
}

func (c *dvdProgressDialogContent) statusText() {
	message := c.dialog.message
	if c.dialog.canceling {
		message = "処理をキャンセルしています。しばらくお待ちください。"
	}
	c.status.SetValue(message)
	c.status.SetWrapMode(basicwidget.WrapModeNormal)
	c.status.SetMultiline(true)
	c.status.SetSelectable(false)
}

func (c *dvdProgressDialogContent) progressDetails() string {
	text := "進捗：" + strconv.Itoa(min(100, max(0, c.dialog.progress))) + "%"
	if c.dialog.eta != "" && !c.dialog.canceling {
		text += "　" + c.dialog.eta
	}
	return text
}

func (c *dvdProgressDialogContent) Measure(context *guigui.Context, _ guigui.Constraints) image.Point {
	u := basicwidget.UnitSize(context)
	app := context.AppBounds()
	return image.Pt(max(1, min(20*u, app.Dx()-2*u)), max(1, min(7*u, app.Dy()-2*u)))
}

func (c *dvdProgressDialogContent) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	x := b.Min.X + u/2
	width := b.Dx() - u

	headingTop := b.Min.Y + u/3
	layouter.LayoutWidget(&c.heading, image.Rect(x, headingTop, x+width, headingTop+u))

	statusTop := headingTop + u + u/3
	layouter.LayoutWidget(&c.status, image.Rect(x, statusTop, x+width, statusTop+u))

	progressTop := statusTop + u + u/2
	layouter.LayoutWidget(&c.progress, image.Rect(x, progressTop, x+width, progressTop+u/2))
	layouter.LayoutWidget(&c.details, image.Rect(x, progressTop+u/2, x+width, progressTop+3*u/2))

	buttonWidth := 6 * u
	buttonTop := b.Max.Y - u - u/3
	layouter.LayoutWidget(&c.cancel, image.Rect(b.Max.X-u/2-buttonWidth, buttonTop, b.Max.X-u/2, buttonTop+u))
}
