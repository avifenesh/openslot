package handler

import (
	"image"
	"image/color"
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
