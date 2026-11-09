package demo

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// The size of a generated door picture.
const (
	pictureWidth  = 960
	pictureHeight = 720
)

// doorPicture draws a door in a wall, in the door's color, with its
// keypad beside it, as a PNG.
func doorPicture(wall, door color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, pictureWidth, pictureHeight))
	fill(img, img.Bounds(), wall)
	fill(img, image.Rect(0, 600, pictureWidth, pictureHeight), shade(wall, 0.7))
	fill(img, image.Rect(330, 110, 630, 610), shade(door, 0.6))
	fill(img, image.Rect(345, 125, 615, 600), door)
	fill(img, image.Rect(370, 150, 590, 330), shade(door, 0.85))
	fill(img, image.Rect(370, 370, 590, 575), shade(door, 0.85))
	fill(img, image.Rect(570, 345, 600, 360), color.RGBA{R: 200, G: 200, B: 205, A: 255})
	keypad(img, image.Pt(680, 300), 1)
	return encodePNG(img)
}

// keypadPicture draws the keypad up close, as a PNG.
func keypadPicture(wall color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, pictureWidth, pictureHeight))
	fill(img, img.Bounds(), wall)
	keypad(img, image.Pt(330, 90), 5)
	return encodePNG(img)
}

// keypad draws a keypad scale times its normal size with its top-left
// corner at at: a dark case, a green light, and twelve keys.
func keypad(img *image.RGBA, at image.Point, scale int) {
	unit := func(x, y, w, h int) image.Rectangle {
		return image.Rect(at.X+x*scale, at.Y+y*scale, at.X+(x+w)*scale, at.Y+(y+h)*scale)
	}
	fill(img, unit(0, 0, 60, 105), color.RGBA{R: 40, G: 44, B: 52, A: 255})
	fill(img, unit(26, 6, 8, 5), color.RGBA{R: 60, G: 220, B: 120, A: 255})
	for row := range 4 {
		for col := range 3 {
			fill(img, unit(6+col*17, 18+row*21, 14, 16), color.RGBA{R: 205, G: 208, B: 214, A: 255})
		}
	}
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// shade is c darkened to the fraction f of its brightness.
func shade(c color.RGBA, f float64) color.RGBA {
	scale := func(v uint8) uint8 { return uint8(float64(v) * f) }
	return color.RGBA{R: scale(c.R), G: scale(c.G), B: scale(c.B), A: 255}
}

func encodePNG(img image.Image) []byte {
	var out bytes.Buffer
	// Encoding an in-memory RGBA image to a buffer cannot fail.
	_ = png.Encode(&out, img)
	return out.Bytes()
}
