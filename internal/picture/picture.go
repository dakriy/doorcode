// Package picture turns an uploaded image into the two JPEG renditions the
// site keeps of it: an image no larger than MaxSide pixels on its longest
// side, and a thumbnail no larger than ThumbnailSide. Decoding, resizing,
// and encoding are pure Go.
package picture

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"

	// Registered decoders: the formats an upload may be in.
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"github.com/rwcarlsen/goexif/exif"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/dakriy/doorcode/internal/check"
)

const (
	// MaxBytes is the largest upload accepted, in bytes: 10 MB.
	MaxBytes = 10_485_760
	// MaxPixels is how many pixels an upload may have, so a small file
	// cannot claim an image too large to decode.
	MaxPixels = 40_000_000
	// MaxSide is the longest side of the image kept.
	MaxSide = 1600
	// ThumbnailSide is the longest side of the thumbnail.
	ThumbnailSide = 320
	// quality is the JPEG quality both renditions are encoded with.
	quality = 85
)

// ContentType is the type of both renditions.
const ContentType = "image/jpeg"

// Field is the name of the form field and the problem an upload arrives
// in.
const Field = "picture"

// Megabytes is MaxBytes in whole megabytes, as pages say it.
func Megabytes() int {
	return MaxBytes >> 20
}

// TooBig is the problem with an upload over MaxBytes, which whoever reads
// the upload refuses before it reaches Prepare.
func TooBig() string {
	return fmt.Sprintf("The picture is larger than %d MB. Choose a smaller file.", Megabytes())
}

const unreadable = "The file is not a JPEG, PNG, GIF, or WebP image that can be read."

func problem(message string) error {
	return check.Problems{{Field: Field, Message: message}}
}

// Prepared is an upload made ready to keep: the image, its size in
// pixels, and its thumbnail.
type Prepared struct {
	Image     []byte
	Width     int
	Height    int
	Thumbnail []byte
}

// Prepare reads data as an image and returns its renditions, or the
// problem with it. Transparent parts become white, and an image whose EXIF
// orientation says it is stored turned or mirrored is turned upright.
func Prepare(data []byte) (Prepared, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Prepared{}, problem(unreadable)
	}
	if config.Width*config.Height > MaxPixels {
		return Prepared{}, problem(fmt.Sprintf("The picture has more than %d million pixels. Choose a smaller one.", MaxPixels/1_000_000))
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Prepared{}, problem(unreadable)
	}
	full := upright(fit(decoded, MaxSide), orientation(data))
	thumbnail := fit(full, ThumbnailSide)
	return Prepared{
		Image: encode(full), Width: full.Bounds().Dx(), Height: full.Bounds().Dy(),
		Thumbnail: encode(thumbnail),
	}, nil
}

// orientation is the EXIF orientation data's metadata gives, from 1 to 8,
// or 1, upright as stored, when it gives none that can be read.
func orientation(data []byte) int {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return 1
	}
	tag, err := x.Get(exif.Orientation)
	if err != nil || tag.Count != 1 {
		return 1
	}
	o, err := tag.Int(0)
	if err != nil {
		return 1
	}
	return o
}

// turn is how to read an image stored in an EXIF orientation to see it
// upright: the pixel at x, y upright is the stored one at y, x when swap is
// set, then counted from the right when flipX is set and from the bottom
// when flipY is.
type turn struct {
	swap, flipX, flipY bool
}

// turns is the turn of each EXIF orientation that is not upright as
// stored; any other is read as stored.
var turns = map[int]turn{
	2: {flipX: true},
	3: {flipX: true, flipY: true},
	4: {flipY: true},
	5: {swap: true},
	6: {swap: true, flipY: true},
	7: {swap: true, flipX: true, flipY: true},
	8: {swap: true, flipX: true},
}

// upright is src, stored in the EXIF orientation o, as it is meant to be
// seen.
func upright(src image.Image, o int) image.Image {
	t, turned := turns[o]
	if !turned {
		return src
	}
	width, height := src.Bounds().Dx(), src.Bounds().Dy()
	size := image.Pt(width, height)
	if t.swap {
		size = image.Pt(height, width)
	}
	dst := image.NewRGBA(image.Rectangle{Max: size})
	for y := range size.Y {
		for x := range size.X {
			u, v := x, y
			if t.swap {
				u, v = y, x
			}
			if t.flipX {
				u = width - 1 - u
			}
			if t.flipY {
				v = height - 1 - v
			}
			dst.Set(x, y, src.At(u, v))
		}
	}
	return dst
}

// fit is src scaled down, keeping its proportions, so that its longest side
// is at most side, drawn over white.
func fit(src image.Image, side int) image.Image {
	width, height := src.Bounds().Dx(), src.Bounds().Dy()
	longest := max(width, height)
	target := min(longest, side)
	dst := image.NewRGBA(image.Rect(0, 0, scaled(width, target, longest), scaled(height, target, longest)))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// scaled is length times target over longest, rounded, and at least one
// pixel.
func scaled(length, target, longest int) int {
	return max(1, (length*target+longest/2)/longest)
}

func encode(img image.Image) []byte {
	var out bytes.Buffer
	// Encoding into memory fails only for images too large for JPEG, which
	// fit never makes.
	_ = jpeg.Encode(&out, img, &jpeg.Options{Quality: quality})
	return out.Bytes()
}
