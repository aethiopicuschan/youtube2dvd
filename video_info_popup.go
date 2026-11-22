package main

import (
	"image"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// videoInfoPopup floats the length, picture, audio and subtitles of a video
// next to where it was clicked. It does not hold the rest of the window:
// a click anywhere closes it and still reaches what was clicked, so that a
// double click on a list row works as before.
type videoInfoPopup struct {
	guigui.DefaultWidget

	popup   basicwidget.Popup
	content videoInfoPopupContent

	// anchor is the point the popup shows next to, and area the bounds it
	// must stay within.
	anchor image.Point
	area   image.Rectangle
}

// ShowAt opens the popup with text, below anchor, or above it when there is
// no room below.
func (p *videoInfoPopup) ShowAt(anchor image.Point, text string) {
	p.anchor = anchor
	p.content.text.SetValue(text)
	p.popup.SetOpen(true)
}

// SetText changes what an open popup says, as the video is read.
func (p *videoInfoPopup) SetText(text string) {
	p.content.text.SetValue(text)
}

func (p *videoInfoPopup) Close() {
	p.popup.SetOpen(false)
}

func (p *videoInfoPopup) IsOpen() bool {
	return p.popup.IsOpen()
}

func (p *videoInfoPopup) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	adder.AddWidget(&p.popup)
	p.content.onPress = p.Close
	p.popup.SetContent(&p.content)
	p.popup.SetModal(false)
	p.popup.SetCloseByClickingOutside(true)
	p.popup.SetAnimated(true)
	return nil
}

// Layout places the popup in area, below the anchor by a row's height so
// that it does not cover the row that was clicked, or above it.
func (p *videoInfoPopup) Layout(context *guigui.Context, _ *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	u := basicwidget.UnitSize(context)
	area := p.area
	if area.Empty() {
		area = context.AppBounds()
	}
	width := min(18*u, area.Dx())
	size := p.content.Measure(context, guigui.FixedWidthConstraints(width))
	size.Y = min(size.Y, area.Dy())
	x := min(max(p.anchor.X-width/2, area.Min.X), area.Max.X-width)
	y := p.anchor.Y + u
	if y+size.Y > area.Max.Y {
		y = max(p.anchor.Y-u-size.Y, area.Min.Y)
	}
	layouter.LayoutWidget(&p.popup, image.Rect(x, y, x+width, y+size.Y))
}

type videoInfoPopupContent struct {
	guigui.DefaultWidget

	text basicwidget.Text
	// onPress closes the popup when it is clicked.
	onPress func()
}

// HandlePointingInput closes the popup on a click on it, and leaves the
// click to what is under it.
func (c *videoInfoPopupContent) HandlePointingInput(_ *guigui.Context, bounds *guigui.WidgetBounds) guigui.HandleInputResult {
	if bounds.IsHitAtCursor() && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) && c.onPress != nil {
		c.onPress()
	}
	return guigui.HandleInputResult{}
}

func (c *videoInfoPopupContent) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	c.text.SetSelectable(false)
	c.text.SetMultiline(true)
	c.text.SetWrapMode(basicwidget.WrapModeNormal)
	adder.AddWidget(&c.text)
	return nil
}

func (c *videoInfoPopupContent) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	padding := basicwidget.UnitSize(context) / 2
	layouter.LayoutWidget(&c.text, bounds.Bounds().Inset(padding))
}

func (c *videoInfoPopupContent) Measure(context *guigui.Context, constraints guigui.Constraints) image.Point {
	padding := basicwidget.UnitSize(context) / 2
	width, ok := constraints.FixedWidth()
	if !ok {
		width = 18 * basicwidget.UnitSize(context)
	}
	textSize := c.text.Measure(context, guigui.FixedWidthConstraints(width-2*padding))
	return image.Pt(width, textSize.Y+2*padding)
}
