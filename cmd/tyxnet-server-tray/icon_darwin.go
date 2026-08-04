//go:build darwin

package main

import (
	"bytes"
	"image/png"
)

func trayIcon() []byte {
	var output bytes.Buffer
	_ = png.Encode(&output, drawTrayIcon())
	return output.Bytes()
}
