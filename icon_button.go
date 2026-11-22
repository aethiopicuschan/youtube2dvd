package main

import (
	"image"
	"image/color"
	"math"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// iconCache keeps the icons drawn for each color mode, by name.
var iconCache = map[string]map[ebiten.ColorMode]*ebiten.Image{}

// shapeIcon draws a monochrome icon of 48x48 points whose shape is the
// points inside reports, anti-aliased by sampling each pixel 16 times.
func shapeIcon(context *guigui.Context, name string, inside func(x, y float64) bool) *ebiten.Image {
	colorMode := context.ColorMode()
	if icon := iconCache[name][colorMode]; icon != nil {
		return icon
	}

	const size = 48
	const samplesPerAxis = 4
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			var covered int
			for sy := range samplesPerAxis {
				for sx := range samplesPerAxis {
					if inside(float64(x)+(float64(sx)+0.5)/samplesPerAxis, float64(y)+(float64(sy)+0.5)/samplesPerAxis) {
						covered++
					}
				}
			}
			if covered > 0 {
				alpha := uint8(covered * 255 / (samplesPerAxis * samplesPerAxis))
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: alpha})
			}
		}
	}

	icon := ebiten.NewImageFromImage(basicwidget.CreateMonochromeImage(colorMode, img))
	if iconCache[name] == nil {
		iconCache[name] = map[ebiten.ColorMode]*ebiten.Image{}
	}
	iconCache[name][colorMode] = icon
	return icon
}

// pencilIcon is the icon of a button that edits a setting: a pencil
// pointing down to the left.
func pencilIcon(context *guigui.Context) *ebiten.Image {
	return shapeIcon(context, "pencil", func(x, y float64) bool {
		// Measured from the centre along the pencil (u, towards the
		// eraser) and across it (v).
		dx, dy := x-24, y-24
		u, v := (dx-dy)/math.Sqrt2, (dx+dy)/math.Sqrt2
		const halfWidth = 5.5
		switch {
		case u >= 10 && u <= 12.5:
			// The gap between the body and the eraser.
			return false
		case u > -11 && u <= 17:
			return math.Abs(v) <= halfWidth
		case u >= -19 && u <= -11:
			// The sharpened tip narrows to a point.
			return math.Abs(v) <= halfWidth*(u+19)/8
		}
		return false
	})
}

// trashIcon is the icon of a button that clears a setting: a trash can
// with a lid.
func trashIcon(context *guigui.Context) *ebiten.Image {
	return shapeIcon(context, "trash", func(x, y float64) bool {
		inRect := func(left, top, right, bottom float64) bool {
			return x >= left && x <= right && y >= top && y <= bottom
		}
		switch {
		case inRect(19, 6, 29, 12) && !inRect(22, 9, 26, 12):
			// The handle on the lid.
			return true
		case inRect(9, 12, 39, 16):
			// The lid.
			return true
		case y >= 19 && y <= 42:
			// The can narrows towards the bottom, with two slits.
			inset := (y - 19) / 23 * 2.5
			if x < 13+inset || x > 35-inset {
				return false
			}
			if y >= 23 && y <= 38 && (math.Abs(x-20.5) <= 1.2 || math.Abs(x-27.5) <= 1.2) {
				return false
			}
			return true
		}
		return false
	})
}

// iconButton is an icon that acts as a button: it is drawn without the
// background and border of basicwidget.Button, faded while disabled, shows
// the pointer cursor while it can be clicked, and says what it does in a
// tooltip.
type iconButton struct {
	guigui.DefaultWidget

	image   basicwidget.Image
	tooltip basicwidget.TooltipArea
	icon    *ebiten.Image
	text    string
	onDown  func()
}

func (b *iconButton) SetIcon(icon *ebiten.Image) {
	b.icon = icon
}

// SetTooltip sets what the tooltip shown while hovering the icon says.
func (b *iconButton) SetTooltip(text string) {
	b.text = text
}

// OnDown sets what a click on the icon does.
func (b *iconButton) OnDown(f func()) {
	b.onDown = f
}

func (b *iconButton) Build(_ *guigui.Context, adder *guigui.ChildAdder) error {
	b.image.SetImage(b.icon)
	adder.AddWidget(&b.image)
	if b.text != "" {
		b.tooltip.SetText(b.text)
		adder.AddWidget(&b.tooltip)
	}
	return nil
}

func (b *iconButton) Layout(context *guigui.Context, widgetBounds *guigui.WidgetBounds, layouter *guigui.ChildLayouter) {
	// The icon is drawn smaller than the area that takes the click.
	bounds := widgetBounds.Bounds()
	inset := min(bounds.Dx(), bounds.Dy()) / 5
	layouter.LayoutWidget(&b.image, bounds.Inset(inset))
	if b.text != "" {
		layouter.LayoutWidget(&b.tooltip, bounds)
	}
}

func (b *iconButton) HandlePointingInput(context *guigui.Context, widgetBounds *guigui.WidgetBounds) guigui.HandleInputResult {
	if !context.IsEnabled(b) || !widgetBounds.IsHitAtCursor() || !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return guigui.HandleInputResult{}
	}
	if b.onDown != nil {
		b.onDown()
	}
	return guigui.HandleInputByWidget(b)
}

func (b *iconButton) CursorShape(context *guigui.Context, widgetBounds *guigui.WidgetBounds) (ebiten.CursorShapeType, bool) {
	if context.IsEnabled(b) && widgetBounds.IsHitAtCursor() {
		return ebiten.CursorShapePointer, true
	}
	return 0, false
}

var cookieIconImage *ebiten.Image

// cookieIcon is a chocolate chip cookie in colour, standing in for the 🍪
// emoji that the app's fonts lack.
func cookieIcon() *ebiten.Image {
	if cookieIconImage != nil {
		return cookieIconImage
	}
	const size = 48
	const samplesPerAxis = 4
	dough := [3]float64{214, 160, 92}
	rim := [3]float64{176, 120, 58}
	chip := [3]float64{88, 52, 30}
	chips := [][2]float64{{16, 15}, {30, 13}, {24, 25}, {13, 29}, {34, 29}, {22, 36}, {35, 20}}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			var sum [3]float64
			covered := 0
			for sy := range samplesPerAxis {
				for sx := range samplesPerAxis {
					px := float64(x) + (float64(sx)+0.5)/samplesPerAxis
					py := float64(y) + (float64(sy)+0.5)/samplesPerAxis
					r := math.Hypot(px-24, py-24)
					if r > 22 {
						continue
					}
					c := dough
					if r > 19 {
						c = rim
					}
					for _, center := range chips {
						if math.Hypot(px-center[0], py-center[1]) <= 3.2 {
							c = chip
							break
						}
					}
					for i := range sum {
						sum[i] += c[i]
					}
					covered++
				}
			}
			if covered == 0 {
				continue
			}
			// Premultiplied by the share of the pixel the cookie covers.
			const samples = samplesPerAxis * samplesPerAxis
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(sum[0] / samples),
				G: uint8(sum[1] / samples),
				B: uint8(sum[2] / samples),
				A: uint8(covered * 255 / samples),
			})
		}
	}
	cookieIconImage = ebiten.NewImageFromImage(img)
	return cookieIconImage
}
