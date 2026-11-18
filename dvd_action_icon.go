package main

import (
	"image"
	"image/color"

	"github.com/guigui-gui/guigui"
	"github.com/guigui-gui/guigui/basicwidget"
	"github.com/hajimehoshi/ebiten/v2"
)

var dvdActionIcons = map[ebiten.ColorMode]*ebiten.Image{}

func dvdActionIcon(context *guigui.Context) *ebiten.Image {
	colorMode := context.ColorMode()
	if icon := dvdActionIcons[colorMode]; icon != nil {
		return icon
	}

	const size = 48
	const samplesPerAxis = 4
	const radius = 5.0
	centers := [...]float64{9, 24, 39}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			var covered int
			for sy := range samplesPerAxis {
				for sx := range samplesPerAxis {
					px := float64(x) + (float64(sx)+0.5)/samplesPerAxis
					py := float64(y) + (float64(sy)+0.5)/samplesPerAxis
					for _, cx := range centers {
						dx, dy := px-cx, py-size/2
						if dx*dx+dy*dy <= radius*radius {
							covered++
							break
						}
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
	dvdActionIcons[colorMode] = icon
	return icon
}
