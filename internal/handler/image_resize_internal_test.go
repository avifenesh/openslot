package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"testing"
)

func TestFitUploadedImage(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		bounds                             image.Rectangle
		maxWidth, maxHeight, width, height int
	}{
		{"landscape", image.Rect(0, 0, 800, 400), 400, 400, 400, 200},
		{"portrait", image.Rect(0, 0, 400, 800), 400, 400, 200, 400},
		{"logo", image.Rect(0, 0, 1200, 400), 600, 160, 480, 160},
		{"banner", image.Rect(0, 0, 2400, 800), 1600, 800, 1600, 533},
		{"no upscale", image.Rect(0, 0, 80, 40), 400, 400, 80, 40},
		{"nonzero origin", image.Rect(10, 20, 810, 420), 400, 400, 400, 200},
		{"thin", image.Rect(0, 0, 1, 1000), 400, 400, 1, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Gray is the image type implicated in the removed imaging scanner's
			// TIFF panic. TIFF uploads remain rejected by decodeUploadedImage.
			src := image.NewGray(tc.bounds)
			got := fitUploadedImage(src, tc.maxWidth, tc.maxHeight)
			if got.Bounds().Dx() != tc.width || got.Bounds().Dy() != tc.height {
				t.Fatalf("bounds = %v, want %dx%d", got.Bounds(), tc.width, tc.height)
			}
		})
	}
}

func TestFitUploadedImageExtremeAspectMemory(t *testing.T) {
	for _, bounds := range []image.Rectangle{image.Rect(0, 0, 1, 1_000_000), image.Rect(7, 11, 1_000_007, 12)} {
		src := image.NewNRGBA(bounds)
		for i := 0; i < len(src.Pix); i += 4 {
			copy(src.Pix[i:i+4], []byte{120, 80, 40, 128})
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, src); err != nil {
			t.Fatal(err)
		}
		decoded, msg, err := decodeUploadedImage(bytes.NewReader(encoded.Bytes()), "image")
		if err != nil || msg != "" {
			t.Fatalf("valid extreme-aspect PNG rejected: %s, %v", msg, err)
		}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		got := fitUploadedImage(decoded, 400, 400)
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("%v: resize allocated %d bytes", bounds, allocated)
		if allocated > 12<<20 {
			t.Errorf("resize allocated %d bytes; want <12 MiB excluding source", allocated)
		}
		if got.Bounds().Dx()*got.Bounds().Dy() != 400 {
			t.Errorf("unexpected bounds: %v", got.Bounds())
		}
		pixel := color.NRGBAModel.Convert(got.At(0, 0)).(color.NRGBA)
		if pixel.A != 128 || pixel.R < 119 || pixel.R > 121 {
			t.Errorf("lost translucent color: %v", pixel)
		}
	}
}

func TestAreaPrefilterCoverageAndAlpha(t *testing.T) {
	// Non-integer footprints include partial pixels, not sparse bilinear samples.
	src := image.NewNRGBA(image.Rect(7, 11, 12, 12))
	for x := 7; x < 12; x++ {
		if x < 9 {
			src.SetNRGBA(x, 11, color.NRGBA{R: 255, A: 255})
		} else {
			// Hidden blue must not contaminate the translucent red edge.
			src.SetNRGBA(x, 11, color.NRGBA{B: 255})
		}
	}
	got := areaPrefilter(src, 2, 1)
	first := color.NRGBAModel.Convert(got.At(0, 0)).(color.NRGBA)
	if first != (color.NRGBA{R: 255, A: 204}) {
		t.Fatalf("fractional coverage = %v, want red with 80%% alpha", first)
	}
	if got.RGBA64At(1, 0) != (color.RGBA64{}) {
		t.Fatal("transparent pixels leaked hidden color")
	}
	// High-frequency content must average, not disappear through undersampling.
	stripes := image.NewGray(image.Rect(0, 0, 1000, 1))
	for x := 0; x < 1000; x += 2 {
		stripes.SetGray(x, 0, color.Gray{Y: 255})
	}
	resized := fitUploadedImage(stripes, 10, 10)
	for x := 0; x < 10; x++ {
		v := color.GrayModel.Convert(resized.At(x, 0)).(color.Gray).Y
		if v < 127 || v > 128 {
			t.Fatalf("aliased stripe at %d: %d", x, v)
		}
	}
}

func TestFitUploadedImagePreservesAlphaAndPixels(t *testing.T) {
	src := image.NewNRGBA(image.Rect(5, 7, 805, 407))
	want := color.NRGBA{R: 120, G: 80, B: 40, A: 128}
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			src.SetNRGBA(x, y, want)
		}
	}
	got := fitUploadedImage(src, 400, 400)
	pixel := color.NRGBAModel.Convert(got.At(100, 100)).(color.NRGBA)
	// Premultiply/unpremultiply rounding can shift an RGB channel by one.
	for i, pair := range [][2]uint8{{pixel.R, want.R}, {pixel.G, want.G}, {pixel.B, want.B}, {pixel.A, want.A}} {
		diff := int(pair[0]) - int(pair[1])
		if diff < -1 || diff > 1 {
			t.Fatalf("channel %d = %d, want %d +/- 1", i, pair[0], pair[1])
		}
	}
}
