package macpaint

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeConfig(t *testing.T) {
	f, err := os.Open("../testdata/header.mac")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	config, err := DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%+v\n", config)
}

func TestDecode(t *testing.T) {
	save := os.Getenv("TEST_MACPAINT_SAVE") != ""
	fns, err := filepath.Glob("../testdata/*.mac")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range fns {
		_, filename := filepath.Split(fn)
		f, err := os.Open(fn)
		if err != nil {
			t.Fatalf("%s: %s", filename, err)

		}
		defer f.Close()
		img, err := Decode(f)
		if err != nil {
			t.Fatalf("%s: %s", filename, err)
		}
		if save {
			fo, err := os.Create(filepath.Join("out", filename+".png"))
			if err != nil {
				t.Fatalf("%s: %s", filename, err)
			}
			defer fo.Close()
			if err := png.Encode(fo, img); err != nil {
				t.Fatalf("%s: %s", filename, err)
			}
		}
	}
}

func TestDecodeHeader(t *testing.T) {
	save := os.Getenv("TEST_MACPAINT_SAVE") != ""
	f, err := os.Open("../testdata/header.mac")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, fmt, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if fmt != "mac" {
		t.Fatalf("Expected 'mac' got '%s'", fmt)
	}
	if save {
		fo, err := os.Create("header.png")
		if err != nil {
			t.Fatal(err)
		}
		defer fo.Close()
		if err := png.Encode(fo, img); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	fns, err := filepath.Glob("../testdata/*.mac")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range fns {
		_, filename := filepath.Split(fn)
		f, err := os.Open(fn)
		if err != nil {
			t.Fatalf("%s: %s", filename, err)
		}
		defer f.Close()

		img1, err := Decode(f)
		if err != nil {
			t.Fatalf("%s: decode: %s", filename, err)
		}

		var buf bytes.Buffer
		if err := Encode(&buf, img1); err != nil {
			t.Fatalf("%s: encode: %s", filename, err)
		}

		img2, err := Decode(&buf)
		if err != nil {
			t.Fatalf("%s: re-decode: %s", filename, err)
		}

		g1 := img1.(*image.Gray)
		g2 := img2.(*image.Gray)
		if !bytes.Equal(g1.Pix, g2.Pix) {
			t.Fatalf("%s: pixel data mismatch after round-trip", filename)
		}
	}
}

func TestDecodeNoHeader(t *testing.T) {
	save := os.Getenv("TEST_MACPAINT_SAVE") != ""
	f, err := os.Open("../testdata/noheader.mac")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if save {
		fo, err := os.Create("noheader.png")
		if err != nil {
			t.Fatal(err)
		}
		defer fo.Close()
		if err := png.Encode(fo, img); err != nil {
			t.Fatal(err)
		}
	}
}
