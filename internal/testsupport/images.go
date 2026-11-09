package testsupport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
)

// PNG is a PNG image width by height pixels, shaded from corner to corner,
// with its top left pixel transparent.
func PNG(width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.NRGBA{R: uint8(x * 255 / width), G: uint8(y * 255 / height), B: 128, A: 255})
		}
	}
	img.Set(0, 0, color.NRGBA{})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// PNGHeader is the start of a PNG image that says it is width by height
// pixels and holds no pixels at all, as a hostile upload might.
func PNGHeader(width, height int) []byte {
	out := []byte("\x89PNG\r\n\x1a\n")
	header := binary.BigEndian.AppendUint32(nil, uint32(width))
	header = binary.BigEndian.AppendUint32(header, uint32(height))
	header = append(header, 8, 6, 0, 0, 0)
	chunk := append([]byte("IHDR"), header...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(header)))
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

// ImageInfo says what image body holds, as its format and size, such as
// "jpeg 320x240", or why it holds none.
func ImageInfo(body string) string {
	config, format, err := image.DecodeConfig(bytes.NewReader([]byte(body)))
	if err != nil {
		return "not an image: " + err.Error()
	}
	return sized(format, config.Width, config.Height)
}

// sized is an image's format and size, such as "jpeg 320x240".
func sized(format string, width, height int) string {
	return fmt.Sprintf("%s %dx%d", format, width, height)
}

// Padded is content followed by zero bytes to make size bytes, for an
// upload of a given size; image decoders ignore what follows the image.
func Padded(content []byte, size int) []byte {
	return append(bytes.Clone(content), make([]byte, size-len(content))...)
}

// Upright is how an oriented test picture looks when seen the right way
// up: uprightWidth by uprightHeight pixels, red at the top left, green at
// the top right, blue at the bottom left, and white at the bottom right.
const Upright = "jpeg 64x32 red green blue white"

// uprightWidth and uprightHeight are the size of an upright test picture.
const uprightWidth, uprightHeight = 64, 32

// quadrantColors are the colors of an upright test picture's quarters, top
// left, top right, bottom left, bottom right.
var quadrantColors = []color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255}}

// Oriented is a JPEG of the upright test picture stored as a camera held
// in EXIF orientation o stores it, carrying that orientation, so turning
// it as o says shows it upright.
func Oriented(o uint16) []byte {
	return WithEXIF(StoredAs(o), Orientation, 3, 1, o)
}

// Orientation is the EXIF tag of a picture's orientation.
const Orientation = 0x0112

// StoredAs is the upright test picture as a camera stores it in EXIF
// orientation o: the stored pixel at x, y is the upright one where o's
// reading puts it.
func StoredAs(o uint16) image.Image {
	width, height := uprightWidth, uprightHeight
	if o >= 5 {
		width, height = uprightHeight, uprightWidth
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			ux, uy := uprightAt(o, x, y, width, height)
			img.Set(x, y, quadrantColors[uy/(uprightHeight/2)*2+ux/(uprightWidth/2)])
		}
	}
	return img
}

// uprightAt is where the stored pixel at x, y of a width by height image
// in EXIF orientation o is seen upright.
func uprightAt(o uint16, x, y, width, height int) (int, int) {
	switch o {
	case 2:
		return width - 1 - x, y
	case 3:
		return width - 1 - x, height - 1 - y
	case 4:
		return x, height - 1 - y
	case 5:
		return y, x
	case 6:
		return height - 1 - y, x
	case 7:
		return height - 1 - y, width - 1 - x
	case 8:
		return y, width - 1 - x
	}
	return x, y
}

// WithEXIF is img as a JPEG carrying one EXIF entry: tag, of the TIFF
// field type kind (3 is a short, 2 text), count values, and value.
func WithEXIF(img image.Image, tag, kind, count, value uint16) []byte {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
		panic(err)
	}
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01")
	tiff = binary.BigEndian.AppendUint16(tiff, tag)
	tiff = binary.BigEndian.AppendUint16(tiff, kind)
	tiff = binary.BigEndian.AppendUint32(tiff, uint32(count))
	tiff = binary.BigEndian.AppendUint16(tiff, value)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	segment := binary.BigEndian.AppendUint16([]byte{0xff, 0xe1}, uint16(len(app1)+2))
	data := encoded.Bytes()
	return slices.Concat(data[:2], segment, app1, data[2:])
}

// Quadrants says what JPEG or PNG image body holds as its format, size,
// and the color of each corner, top left, top right, bottom left, bottom
// right: red, green, blue, white, or other.
func Quadrants(body string) string {
	img, format, err := image.Decode(bytes.NewReader([]byte(body)))
	if err != nil {
		return "not an image: " + err.Error()
	}
	b := img.Bounds()
	corners := []image.Point{{b.Min.X, b.Min.Y}, {b.Max.X - 1, b.Min.Y}, {b.Min.X, b.Max.Y - 1}, {b.Max.X - 1, b.Max.Y - 1}}
	out := sized(format, b.Dx(), b.Dy())
	for _, p := range corners {
		out += " " + colorName(img.At(p.X, p.Y))
	}
	return out
}

// colorName names c when it is near red, green, blue, or white.
func colorName(c color.Color) string {
	r, g, b, _ := c.RGBA()
	if name, near := colorNames[level(r)+level(g)+level(b)]; near {
		return name
	}
	return "other"
}

// colorNames names a color by the level of its red, green, and blue.
var colorNames = map[string]string{"+++": "white", "+--": "red", "-+-": "green", "--+": "blue"}

// level is a color channel's value v as high, "+", low, "-", or between,
// "?".
func level(v uint32) string {
	switch {
	case v > 0xb000:
		return "+"
	case v < 0x5000:
		return "-"
	}
	return "?"
}
