package main

import (
	"image"
	"strconv"
	"strings"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
)

// trackPickerDialog lets the user pick additional tracks, of captions or
// audio: one checkbox per track, in a list that scrolls for videos with many
// languages.
type trackPickerDialog struct {
	guigui.DefaultWidget

	popup   basicwidget.Popup
	content trackPickerContent

	// heading names what is picked, such as "追加の字幕".
	heading string
	// choices are the tracks that can be added; selected tells which are.
	choices  []trackChoice
	selected map[string]bool
	// onToggle is called when the user checks or unchecks a track.
	onToggle func(id string, on bool)
}

func (d *trackPickerDialog) SetOpen(open bool) {
	d.popup.SetOpen(open)
}

func (d *trackPickerDialog) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	adder.AddWidget(&d.popup)
	// The popup measures its content before building it, so the content
	// and its list must know the dialog already.
	d.content.dialog = d
	d.content.list.dialog = d
	d.popup.SetContent(&d.content)
	d.popup.SetModal(true)
	d.popup.SetBackgroundDark(true)
	d.popup.SetCloseByClickingOutside(true)
	d.popup.SetAnimated(true)
	return nil
}

func (d *trackPickerDialog) Layout(context *guigui.Context, _ *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	size := d.content.Measure(context, guigui.Constraints{})
	app := context.AppBounds()
	pos := image.Pt(app.Min.X+(app.Dx()-size.X)/2, app.Min.Y+(app.Dy()-size.Y)/2)
	layouter.LayoutWidget(&d.popup, image.Rectangle{Min: pos, Max: pos.Add(size)})
}

// trackChoice is a track offered in a trackPickerDialog.
type trackChoice struct {
	ID    string
	Label string
}

type trackPickerContent struct {
	guigui.DefaultWidget

	dialog      *trackPickerDialog
	heading     basicwidget.Text
	panel       basicwidget.Panel
	list        trackChoiceList
	closeButton basicwidget.Button
}

func (c *trackPickerContent) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	c.heading.SetValue(c.dialog.heading)
	c.heading.SetSelectable(false)
	c.heading.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	c.list.dialog = c.dialog
	c.panel.SetStyle(basicwidget.PanelStyleDefault)
	c.panel.SetContentConstraints(basicwidget.PanelContentConstraintsFixedWidth)
	c.panel.SetContent(&c.list)
	c.closeButton.SetText("完了")
	c.closeButton.OnDown(func(_ *guigui.Context) {
		c.dialog.SetOpen(false)
	})
	adder.AddWidget(&c.heading)
	adder.AddWidget(&c.panel)
	adder.AddWidget(&c.closeButton)
	return nil
}

func (c *trackPickerContent) Measure(context *guigui.Context, _ guigui.Constraints) image.Point {
	u := basicwidget.UnitSize(context)
	app := context.AppBounds()
	listHeight := c.list.Measure(context, guigui.FixedWidthConstraints(14*u)).Y
	height := u/3 + u + u/4 + listHeight + u/2 + u + u/3
	return image.Pt(min(16*u, app.Dx()-2*u), min(height, app.Dy()-2*u))
}

func (c *trackPickerContent) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	x := b.Min.X + u/2
	width := b.Dx() - u
	top := b.Min.Y + u/3
	layouter.LayoutWidget(&c.heading, image.Rect(x, top, x+width, top+u))
	buttonTop := b.Max.Y - u - u/3
	layouter.LayoutWidget(&c.panel, image.Rect(x, top+u+u/4, x+width, buttonTop-u/2))
	buttonWidth := 4 * u
	layouter.LayoutWidget(&c.closeButton, image.Rect(b.Max.X-u/2-buttonWidth, buttonTop, b.Max.X-u/2, buttonTop+u))
}

// trackChoiceList is the scrolling content of the picker: one row per
// track.
type trackChoiceList struct {
	guigui.DefaultWidget

	dialog *trackPickerDialog
	rows   []trackChoiceRow
}

func (l *trackChoiceList) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	choices := l.dialog.choices
	if len(l.rows) != len(choices) {
		l.rows = make([]trackChoiceRow, len(choices))
	}
	for i, choice := range choices {
		row := &l.rows[i]
		row.choice = choice
		row.dialog = l.dialog
		adder.AddWidget(row)
	}
	return nil
}

func (l *trackChoiceList) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	for i := range l.rows {
		top := b.Min.Y + u/4 + i*(u+u/4)
		layouter.LayoutWidget(&l.rows[i], image.Rect(b.Min.X+u/4, top, b.Max.X-u/4, top+u))
	}
}

func (l *trackChoiceList) Measure(context *guigui.Context, constraints guigui.Constraints) image.Point {
	u := basicwidget.UnitSize(context)
	width, ok := constraints.FixedWidth()
	if !ok {
		width = 14 * u
	}
	count := 0
	if l.dialog != nil {
		count = len(l.dialog.choices)
	}
	return image.Pt(width, u/4+count*(u+u/4))
}

// trackChoiceRow is a checkbox with the name of a track.
type trackChoiceRow struct {
	guigui.DefaultWidget

	checkbox basicwidget.Checkbox
	label    basicwidget.Text

	choice trackChoice
	dialog *trackPickerDialog
}

func (r *trackChoiceRow) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	r.checkbox.SetValue(r.dialog.selected[r.choice.ID])
	id := r.choice.ID
	r.checkbox.OnValueChanged(func(_ *guigui.Context, value bool) {
		if r.dialog.selected[id] == value || r.dialog.onToggle == nil {
			return
		}
		r.dialog.onToggle(id, value)
		guigui.RequestRebuild()
	})
	r.label.SetValue(r.choice.Label)
	r.label.SetSelectable(false)
	r.label.SetVerticalAlign(basicwidget.VerticalAlignMiddle)
	adder.AddWidget(&r.checkbox)
	adder.AddWidget(&r.label)
	return nil
}

func (r *trackChoiceRow) Layout(context *guigui.Context, bounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	b := bounds.Bounds()
	u := basicwidget.UnitSize(context)
	size := r.checkbox.Measure(context, guigui.Constraints{})
	top := b.Min.Y + (b.Dy()-size.Y)/2
	layouter.LayoutWidget(&r.checkbox, image.Rect(b.Min.X, top, b.Min.X+size.X, top+size.Y))
	layouter.LayoutWidget(&r.label, image.Rect(b.Min.X+size.X+u/3, b.Min.Y, b.Max.X, b.Max.Y))
}

// additionalTracksSummary describes the chosen additional tracks, by their
// labels, for the download page. The count comes first, so that it stays
// readable when a narrow window cuts the names off.
func additionalTracksSummary(labels []string) string {
	if len(labels) == 0 {
		return "なし"
	}
	return strconv.Itoa(len(labels)) + "件（" + strings.Join(labels, "、") + "）"
}
