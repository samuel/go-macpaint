package macpaint

import (
	"bytes"
	"image"
	"os"
	"testing"
)

// fuzzSeeds adds the real fixtures to a fuzz target's corpus so it starts from
// valid files rather than having to discover the format from scratch.
func fuzzSeeds(f *testing.F) {
	f.Helper()
	for _, fn := range []string{"../testdata/header.mac", "../testdata/noheader.mac"} {
		data, err := os.ReadFile(fn)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
}

// FuzzDecode exercises the header parser and the PackBits decoder. It is
// deliberately cheap so it can reach a high execution rate; the encoder is
// covered separately by FuzzEncodeRoundTrip.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		// Malformed input is expected and is not a failure. What matters is that
		// the decoder never panics and that these invariants always hold.
		cfg, cfgErr := DecodeConfig(bytes.NewReader(data))
		img, err := Decode(bytes.NewReader(data))

		if err != nil {
			if img != nil {
				t.Fatalf("Decode returned an image alongside error %q", err)
			}
			return
		}

		// Decode and DecodeConfig share the header parse, so a successful Decode
		// implies a successful DecodeConfig. The converse does not hold: the header
		// can be valid while the image data is truncated or corrupt.
		if cfgErr != nil {
			t.Fatalf("Decode succeeded but DecodeConfig failed: %s", cfgErr)
		}

		// Every successful decode is a full 576x720 image agreeing with the config.
		wantBounds := image.Rect(0, 0, Width, Height)
		if got := img.Bounds(); got != wantBounds {
			t.Fatalf("bounds = %v, want %v", got, wantBounds)
		}
		if cfg.Width != Width || cfg.Height != Height {
			t.Fatalf("config = %dx%d, want %dx%d", cfg.Width, cfg.Height, Width, Height)
		}
	})
}

// FuzzEncodeRoundTrip checks that decoding, re-encoding, and decoding again is
// lossless. Encode touches all 576x720 pixels, so each execution costs roughly
// 100x a bare Decode; this lives in its own target to keep FuzzDecode fast.
func FuzzEncodeRoundTrip(f *testing.F) {
	fuzzSeeds(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		img, err := Decode(bytes.NewReader(data))
		if err != nil {
			return // Not a valid file; FuzzDecode covers the error paths.
		}
		pal, ok := img.(*image.Paletted)
		if !ok {
			t.Fatalf("Decode returned %T, want *image.Paletted", img)
		}

		var buf bytes.Buffer
		if err := Encode(&buf, pal); err != nil {
			t.Fatalf("Encode: %s", err)
		}
		reImg, err := Decode(&buf)
		if err != nil {
			t.Fatalf("Decode of re-encoded image: %s", err)
		}
		rePal, ok := reImg.(*image.Paletted)
		if !ok {
			t.Fatalf("re-Decode returned %T, want *image.Paletted", reImg)
		}
		if !bytes.Equal(pal.Pix, rePal.Pix) {
			t.Fatal("pixel data mismatch after round-trip")
		}
	})
}
