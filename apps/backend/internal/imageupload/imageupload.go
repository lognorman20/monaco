// Package imageupload turns bytes a client uploaded into an image the backend is
// willing to put in object storage.
//
// Three rules hold for every caller:
//
//   - The format comes from sniffing the bytes, never from a client-supplied
//     content type or file name. A request that claims "image/png" and carries a
//     shell script is rejected on the magic number, not on the claim.
//   - Dimensions are checked from the image header before a full decode runs, so a
//     small file that expands to a hundred megapixels never allocates.
//   - The stored bytes are re-encoded from the decoded pixels. EXIF, colour
//     profiles, trailing archives and anything else riding along in the original
//     container do not survive, because only the pixel grid is carried over.
package imageupload

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"

	// Decoders for the formats Sniff accepts. jpeg and png register themselves
	// through the imports above; webp has no encoder, so it is decode-only.
	_ "golang.org/x/image/webp"
)

// Errors returned by Prepare. Callers map these onto their own HTTP responses.
var (
	// ErrEmpty means no bytes were supplied.
	ErrEmpty = errors.New("imageupload: image is empty")
	// ErrTooLarge means the encoded upload is over Limits.MaxBytes.
	ErrTooLarge = errors.New("imageupload: image is over the size limit")
	// ErrUnsupportedFormat means the bytes are not one of the accepted formats.
	ErrUnsupportedFormat = errors.New("imageupload: image must be jpeg, png, or webp")
	// ErrTooManyPixels means the image's dimensions are over Limits.MaxEdge or
	// Limits.MaxPixels. It is reported before the image is decoded.
	ErrTooManyPixels = errors.New("imageupload: image dimensions are over the limit")
	// ErrMalformed means the bytes carry a recognised header but do not decode.
	ErrMalformed = errors.New("imageupload: image could not be decoded")
)

// Limits bound what Prepare accepts and what it produces.
type Limits struct {
	// MaxBytes is the largest accepted upload, before decoding.
	MaxBytes int
	// MaxEdge is the longest accepted side of the source image, in pixels.
	MaxEdge int
	// MaxPixels is the largest accepted width*height of the source image. It
	// stops a long, thin image from passing MaxEdge and still exhausting memory.
	MaxPixels int
	// TargetEdge is the longest side of the stored image. A source no bigger than
	// this is re-encoded at its own size; Prepare never upscales.
	TargetEdge int
	// JPEGQuality is the quality of a stored JPEG, 1-100. Zero means 85.
	JPEGQuality int
}

// Result is a validated, re-encoded image ready to store.
type Result struct {
	// Data is the re-encoded image.
	Data []byte
	// ContentType is the media type of Data, for the storage write and the CDN.
	ContentType string
	// Extension is the file extension for Data, without a dot.
	Extension string
	// Width and Height are the dimensions of Data, after any downscale.
	Width  int
	Height int
}

// sourceFormat is a format Prepare is willing to decode.
type sourceFormat struct {
	name string
	// matches reports whether data starts with this format's magic number.
	matches func(data []byte) bool
}

var sourceFormats = []sourceFormat{
	{
		name: "jpeg",
		matches: func(data []byte) bool {
			return len(data) >= 3 && bytes.Equal(data[:3], []byte{0xFF, 0xD8, 0xFF})
		},
	},
	{
		name: "png",
		matches: func(data []byte) bool {
			return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
		},
	},
	{
		name: "webp",
		matches: func(data []byte) bool {
			return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
		},
	},
}

// Sniff reports the format of data from its magic number. The second return is
// false when the bytes are not an accepted image.
func Sniff(data []byte) (string, bool) {
	for _, format := range sourceFormats {
		if format.matches(data) {
			return format.name, true
		}
	}
	return "", false
}

// Prepare validates data against limits and returns the image to store.
//
// The stored image keeps the source's aspect ratio. It is a PNG when the source
// has pixels that are not fully opaque, so a logo on a transparent background
// stays transparent instead of being flattened onto black, and a JPEG otherwise.
func Prepare(data []byte, limits Limits) (Result, error) {
	if len(data) == 0 {
		return Result{}, ErrEmpty
	}
	if limits.MaxBytes > 0 && len(data) > limits.MaxBytes {
		return Result{}, ErrTooLarge
	}
	if _, ok := Sniff(data); !ok {
		return Result{}, ErrUnsupportedFormat
	}

	// Header first: this reads dimensions without allocating the pixel grid, so an
	// image that is small on disk and enormous in memory is refused before decode.
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return Result{}, ErrMalformed
	}
	if limits.MaxEdge > 0 && (config.Width > limits.MaxEdge || config.Height > limits.MaxEdge) {
		return Result{}, ErrTooManyPixels
	}
	if limits.MaxPixels > 0 && config.Width*config.Height > limits.MaxPixels {
		return Result{}, ErrTooManyPixels
	}

	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}

	stored := downscale(source, limits.TargetEdge)
	bounds := stored.Bounds()

	if isOpaque(stored) {
		quality := limits.JPEGQuality
		if quality <= 0 {
			quality = 85
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, stored, &jpeg.Options{Quality: quality}); err != nil {
			return Result{}, fmt.Errorf("encode jpeg: %w", err)
		}
		return Result{
			Data:        buf.Bytes(),
			ContentType: "image/jpeg",
			Extension:   "jpg",
			Width:       bounds.Dx(),
			Height:      bounds.Dy(),
		}, nil
	}

	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, stored); err != nil {
		return Result{}, fmt.Errorf("encode png: %w", err)
	}
	return Result{
		Data:        buf.Bytes(),
		ContentType: "image/png",
		Extension:   "png",
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
	}, nil
}

// downscale returns src resized so its longest side is at most targetEdge,
// preserving the aspect ratio. A src already within the target is returned
// unchanged: nothing is ever upscaled.
func downscale(src image.Image, targetEdge int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if targetEdge <= 0 || (width <= targetEdge && height <= targetEdge) {
		return src
	}

	scaledWidth, scaledHeight := width, height
	if width >= height {
		scaledWidth = targetEdge
		scaledHeight = height * targetEdge / width
	} else {
		scaledHeight = targetEdge
		scaledWidth = width * targetEdge / height
	}
	// Integer division can round a very lopsided image's short side to zero.
	if scaledWidth < 1 {
		scaledWidth = 1
	}
	if scaledHeight < 1 {
		scaledHeight = 1
	}

	dst := image.NewNRGBA(image.Rect(0, 0, scaledWidth, scaledHeight))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Src, nil)
	return dst
}

// isOpaque reports whether every pixel of img is fully opaque. Image types that
// cannot carry alpha answer through the Opaque method; the rest are scanned.
func isOpaque(img image.Image) bool {
	type opaquer interface{ Opaque() bool }
	if o, ok := img.(opaquer); ok {
		return o.Opaque()
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xFFFF {
				return false
			}
		}
	}
	return true
}
