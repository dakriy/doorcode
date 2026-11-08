package testsupport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"

	// The format pictures are served in.
	_ "image/jpeg"
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
	return fmt.Sprintf("%s %dx%d", format, config.Width, config.Height)
}

// Padded is content followed by zero bytes to make size bytes, for an
// upload of a given size; image decoders ignore what follows the image.
func Padded(content []byte, size int) []byte {
	return append(bytes.Clone(content), make([]byte, size-len(content))...)
}
