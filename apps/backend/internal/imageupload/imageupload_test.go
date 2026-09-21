package imageupload

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func testLimits() Limits {
	return Limits{
		MaxBytes:   2 << 20,
		MaxEdge:    4096,
		MaxPixels:  4096 * 4096,
		TargetEdge: 512,
	}
}

// opaquePNG returns a solid PNG of the given size.
func opaquePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x40, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// transparentPNG returns a PNG whose pixels are not fully opaque.
func transparentPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 0xFF, G: 0x00, B: 0x00, A: 0x80})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func opaqueJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 0x20, G: uint8(x % 256), B: uint8(y % 256), A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestSniff_recognisesTheAcceptedFormats(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
		ok   bool
	}{
		{name: "jpeg", data: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, want: "jpeg", ok: true},
		{name: "png", data: []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00}, want: "png", ok: true},
		{name: "webp", data: append([]byte("RIFF\x00\x00\x00\x00WEBP"), 0x00), want: "webp", ok: true},
		{name: "gif is not accepted", data: []byte("GIF89a...."), ok: false},
		{name: "text", data: []byte("not-an-image"), ok: false},
		{name: "empty", data: nil, ok: false},
		// A RIFF container that is not WEBP (a .wav, say) must not pass.
		{name: "riff wave", data: []byte("RIFF\x00\x00\x00\x00WAVEfmt "), ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Sniff(tc.data)
			if ok != tc.ok {
				t.Fatalf("Sniff ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("Sniff = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrepare_rejectsEmptyInput(t *testing.T) {
	if _, err := Prepare(nil, testLimits()); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
}

func TestPrepare_rejectsWrongType(t *testing.T) {
	_, err := Prepare([]byte("#!/bin/sh\nrm -rf /\n"), testLimits())
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestPrepare_rejectsOversizedInput(t *testing.T) {
	limits := testLimits()
	limits.MaxBytes = 64
	_, err := Prepare(opaquePNG(t, 40, 40), limits)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestPrepare_rejectsMalformedImageBehindAValidHeader(t *testing.T) {
	// A real PNG signature followed by garbage: the sniff passes, the decode must not.
	data := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x41}, 128)...)
	_, err := Prepare(data, testLimits())
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestPrepare_rejectsTruncatedImage(t *testing.T) {
	full := opaquePNG(t, 64, 64)
	_, err := Prepare(full[:len(full)/2], testLimits())
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestPrepare_rejectsDimensionsOverTheCap(t *testing.T) {
	limits := testLimits()
	limits.MaxEdge = 32
	_, err := Prepare(opaquePNG(t, 64, 8), limits)
	if !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("err = %v, want ErrTooManyPixels", err)
	}
}

func TestPrepare_rejectsPixelCountOverTheCap(t *testing.T) {
	limits := testLimits()
	limits.MaxEdge = 4096
	limits.MaxPixels = 100
	_, err := Prepare(opaquePNG(t, 64, 64), limits)
	if !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("err = %v, want ErrTooManyPixels", err)
	}
}

func TestPrepare_downscalesToTheTargetEdgeKeepingAspect(t *testing.T) {
	limits := testLimits()
	limits.TargetEdge = 100
	result, err := Prepare(opaquePNG(t, 800, 400), limits)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result.Width != 100 || result.Height != 50 {
		t.Fatalf("stored size = %dx%d, want 100x50", result.Width, result.Height)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatalf("decode stored config: %v", err)
	}
	if config.Width != 100 || config.Height != 50 {
		t.Fatalf("encoded size = %dx%d, want 100x50", config.Width, config.Height)
	}
}

func TestPrepare_doesNotUpscaleASmallImage(t *testing.T) {
	limits := testLimits()
	limits.TargetEdge = 512
	result, err := Prepare(opaquePNG(t, 24, 12), limits)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result.Width != 24 || result.Height != 12 {
		t.Fatalf("stored size = %dx%d, want 24x12", result.Width, result.Height)
	}
}

func TestPrepare_storesAnOpaqueSourceAsJPEG(t *testing.T) {
	result, err := Prepare(opaquePNG(t, 64, 64), testLimits())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result.ContentType != "image/jpeg" || result.Extension != "jpg" {
		t.Fatalf("content type = %q ext = %q, want image/jpeg/jpg", result.ContentType, result.Extension)
	}
	if _, err := jpeg.Decode(bytes.NewReader(result.Data)); err != nil {
		t.Fatalf("stored bytes do not decode as jpeg: %v", err)
	}
}

func TestPrepare_keepsTransparencyByStoringPNG(t *testing.T) {
	result, err := Prepare(transparentPNG(t, 48, 48), testLimits())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result.ContentType != "image/png" || result.Extension != "png" {
		t.Fatalf("content type = %q ext = %q, want image/png/png", result.ContentType, result.Extension)
	}
	decoded, err := png.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatalf("stored bytes do not decode as png: %v", err)
	}
	if _, _, _, a := decoded.At(0, 0).RGBA(); a == 0xFFFF {
		t.Fatalf("alpha = %d, want the source's partial transparency preserved", a)
	}
}

func TestPrepare_acceptsJPEGSource(t *testing.T) {
	result, err := Prepare(opaqueJPEG(t, 120, 90), testLimits())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result.Width != 120 || result.Height != 90 {
		t.Fatalf("stored size = %dx%d, want 120x90", result.Width, result.Height)
	}
}

// A re-encode is what keeps a polyglot from reaching storage: the appended bytes
// are not pixels, so they are not carried into the output.
func TestPrepare_dropsTrailingBytesAppendedToAValidImage(t *testing.T) {
	smuggled := []byte("<?php system($_GET['c']); ?>")
	data := append(opaquePNG(t, 32, 32), smuggled...)

	result, err := Prepare(data, testLimits())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if bytes.Contains(result.Data, smuggled) {
		t.Fatalf("stored image still carries the appended payload")
	}
}

func TestPrepare_isDeterministicForTheSameInput(t *testing.T) {
	data := opaquePNG(t, 200, 150)
	first, err := Prepare(data, testLimits())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	second, err := Prepare(data, testLimits())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !bytes.Equal(first.Data, second.Data) {
		t.Fatalf("two runs produced different bytes")
	}
}
