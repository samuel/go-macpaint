package macpaint

import (
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
			defer f.Close()
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
		defer f.Close()
		if err := png.Encode(fo, img); err != nil {
			t.Fatal(err)
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
		defer f.Close()
		if err := png.Encode(fo, img); err != nil {
			t.Fatal(err)
		}
	}
}
