//go:build windows || darwin

package main

import (
	"image"
	"image/color"
)

func drawTrayIcon() *image.RGBA {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 2; y < 30; y++ {
		for x := 2; x < 30; x++ {
			dx, dy := min(x-2, 29-x), min(y-2, 29-y)
			if dx < 3 && dy < 3 && (3-dx)*(3-dx)+(3-dy)*(3-dy) > 10 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{R: 70, G: uint8(181 + y), B: 209, A: 255})
		}
	}
	ink := color.RGBA{R: 4, G: 29, B: 46, A: 255}
	for y := 10; y < 14; y++ {
		for x := 9; x < 23; x++ {
			img.SetRGBA(x, y, ink)
		}
	}
	for y := 13; y < 23; y++ {
		for x := 14; x < 18; x++ {
			img.SetRGBA(x, y, ink)
		}
	}
	return img
}
