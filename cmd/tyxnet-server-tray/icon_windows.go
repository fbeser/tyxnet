//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
)

func trayIcon() []byte {
	const width, height = 32, 32
	const bitmapBytes = width * height * 4
	const maskBytes = width / 8 * height
	const imageBytes = 40 + bitmapBytes + maskBytes
	var output bytes.Buffer
	write := func(value any) { _ = binary.Write(&output, binary.LittleEndian, value) }
	write(uint16(0))
	write(uint16(1))
	write(uint16(1))
	output.WriteByte(width)
	output.WriteByte(height)
	output.Write([]byte{0, 0})
	write(uint16(1))
	write(uint16(32))
	write(uint32(imageBytes))
	write(uint32(22))
	write(uint32(40))
	write(int32(width))
	write(int32(height * 2))
	write(uint16(1))
	write(uint16(32))
	write(uint32(0))
	write(uint32(bitmapBytes))
	write(int32(0))
	write(int32(0))
	write(uint32(0))
	write(uint32(0))
	img := drawTrayIcon()
	for y := height - 1; y >= 0; y-- {
		for x := 0; x < width; x++ {
			pixel := img.RGBAAt(x, y)
			output.Write([]byte{pixel.B, pixel.G, pixel.R, pixel.A})
		}
	}
	output.Write(make([]byte, maskBytes))
	return output.Bytes()
}
