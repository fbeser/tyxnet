package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

const iconSize = 1024

func main() {
	if len(os.Args) != 2 {
		panic("usage: icon OUTPUT.png")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	drawBackground(canvas)
	drawMark(canvas)
	output, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer func() { _ = output.Close() }()
	if err := png.Encode(output, canvas); err != nil {
		panic(err)
	}
}

func drawBackground(canvas *image.RGBA) {
	const inset = 70
	const radius = 210
	for vertical := inset; vertical < iconSize-inset; vertical++ {
		progress := float64(vertical-inset) / float64(iconSize-2*inset)
		fill := color.RGBA{R: uint8(47 + 23*progress), G: uint8(167 + 34*progress), B: uint8(218 + 8*progress), A: 255}
		for horizontal := inset; horizontal < iconSize-inset; horizontal++ {
			if insideRoundedSquare(horizontal, vertical, inset, iconSize-inset, radius) {
				canvas.SetRGBA(horizontal, vertical, fill)
			}
		}
	}
}

func insideRoundedSquare(horizontal, vertical, minimum, maximum, radius int) bool {
	centerHorizontal := horizontal
	centerVertical := vertical
	if horizontal < minimum+radius {
		centerHorizontal = minimum + radius
	} else if horizontal >= maximum-radius {
		centerHorizontal = maximum - radius - 1
	}
	if vertical < minimum+radius {
		centerVertical = minimum + radius
	} else if vertical >= maximum-radius {
		centerVertical = maximum - radius - 1
	}
	deltaHorizontal := horizontal - centerHorizontal
	deltaVertical := vertical - centerVertical
	return deltaHorizontal*deltaHorizontal+deltaVertical*deltaVertical <= radius*radius
}

func drawMark(canvas *image.RGBA) {
	ink := color.RGBA{R: 4, G: 29, B: 46, A: 255}
	drawRectangle(canvas, 270, 300, 754, 420, ink)
	drawRectangle(canvas, 450, 395, 574, 745, ink)
	drawCircle(canvas, 270, 360, 58, ink)
	drawCircle(canvas, 754, 360, 58, ink)
	drawCircle(canvas, 512, 745, 58, ink)
}

func drawRectangle(canvas *image.RGBA, left, top, right, bottom int, fill color.RGBA) {
	for vertical := top; vertical < bottom; vertical++ {
		for horizontal := left; horizontal < right; horizontal++ {
			canvas.SetRGBA(horizontal, vertical, fill)
		}
	}
}

func drawCircle(canvas *image.RGBA, centerHorizontal, centerVertical, radius int, fill color.RGBA) {
	for vertical := centerVertical - radius; vertical <= centerVertical+radius; vertical++ {
		for horizontal := centerHorizontal - radius; horizontal <= centerHorizontal+radius; horizontal++ {
			deltaHorizontal := horizontal - centerHorizontal
			deltaVertical := vertical - centerVertical
			if deltaHorizontal*deltaHorizontal+deltaVertical*deltaVertical <= radius*radius {
				canvas.SetRGBA(horizontal, vertical, fill)
			}
		}
	}
}
