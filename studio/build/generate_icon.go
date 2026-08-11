//go:build ignore

package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

const size = 1024

func main() {
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	roundedRect(canvas, 52, 52, 972, 972, 218, color.RGBA{R: 18, G: 21, B: 24, A: 255})
	roundedBorder(canvas, 52, 52, 972, 972, 218, 8, color.RGBA{R: 63, G: 73, B: 79, A: 255})

	// The H is intentionally geometric so the source stays font-independent.
	roundedRect(canvas, 246, 242, 366, 782, 34, color.RGBA{R: 89, G: 199, B: 188, A: 255})
	roundedRect(canvas, 658, 242, 778, 782, 34, color.RGBA{R: 89, G: 199, B: 188, A: 255})
	roundedRect(canvas, 324, 452, 700, 572, 34, color.RGBA{R: 237, G: 241, B: 242, A: 255})

	for index, x := range []int{402, 492, 582} {
		shade := []color.RGBA{
			{R: 99, G: 193, B: 116, A: 255},
			{R: 224, G: 184, B: 95, A: 255},
			{R: 114, G: 174, B: 230, A: 255},
		}[index]
		roundedRect(canvas, x, 692, x+52, 744, 14, shade)
	}

	file, err := os.Create("build/appicon.png")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if err := png.Encode(file, canvas); err != nil {
		panic(err)
	}
}

func roundedBorder(target *image.RGBA, x0, y0, x1, y1, radius, width int, fill color.RGBA) {
	roundedRect(target, x0, y0, x1, y1, radius, fill)
	roundedRect(target, x0+width, y0+width, x1-width, y1-width, radius-width, color.RGBA{R: 18, G: 21, B: 24, A: 255})
}

func roundedRect(target *image.RGBA, x0, y0, x1, y1, radius int, fill color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dx := 0
			if x < x0+radius {
				dx = x0 + radius - x
			} else if x >= x1-radius {
				dx = x - (x1 - radius - 1)
			}
			dy := 0
			if y < y0+radius {
				dy = y0 + radius - y
			} else if y >= y1-radius {
				dy = y - (y1 - radius - 1)
			}
			if dx == 0 || dy == 0 || dx*dx+dy*dy <= radius*radius {
				target.SetRGBA(x, y, fill)
			}
		}
	}
}
