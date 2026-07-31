package macpaint

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// unpackBits expands a PackBits stream, so encoder output can be checked without
// going through the whole decoder.
func unpackBits(t *testing.T, src []byte) []byte {
	t.Helper()
	var out []byte
	for i := 0; i < len(src); {
		n := src[i]
		i++
		switch {
		case n == 0x80:
		case n&0x80 != 0:
			if i >= len(src) {
				t.Fatal("truncated run")
			}
			for range 257 - int(n) {
				out = append(out, src[i])
			}
			i++
		default:
			litLen := int(n) + 1
			if i+litLen > len(src) {
				t.Fatal("truncated literal")
			}
			out = append(out, src[i:i+litLen]...)
			i += litLen
		}
	}
	return out
}

func TestPackBitsEncode(t *testing.T) {
	rowOf := func(b byte) []byte {
		r := make([]byte, Width/8)
		for i := range r {
			r[i] = b
		}
		return r
	}
	alternating := make([]byte, Width/8)
	for i := range alternating {
		alternating[i] = byte(i % 2 * 0xff)
	}
	incrementing := make([]byte, Width/8)
	for i := range incrementing {
		incrementing[i] = byte(i)
	}

	tests := []struct {
		name string
		src  []byte
	}{
		{"empty", nil},
		{"single", []byte{0x42}},
		{"pair-same", []byte{7, 7}},
		{"pair-diff", []byte{1, 2}},
		{"run-of-2-at-end", []byte{1, 2, 3, 3}},
		{"run-of-2-mid", []byte{1, 2, 3, 3, 4, 5}},
		{"run-of-3-mid", []byte{1, 2, 3, 3, 3, 4, 5}},
		{"all-zero-row", rowOf(0x00)},
		{"all-ones-row", rowOf(0xff)},
		{"alternating-row", alternating},
		{"incrementing-row", incrementing},
		{"long-run", bytes.Repeat([]byte{9}, 300)},
		{"exactly-128", bytes.Repeat([]byte{5}, 128)},
		{"129", bytes.Repeat([]byte{5}, 129)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := packBitsEncode(nil, tt.src)

			// Must decode back to exactly the input.
			if got := unpackBits(t, enc); !bytes.Equal(got, tt.src) {
				t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(tt.src))
			}

			// Must never be longer than encoding everything as literals, which is
			// the baseline any PackBits encoder should beat or match.
			literalOnly := len(tt.src) + (len(tt.src)+maxRun-1)/maxRun
			if len(enc) > literalOnly {
				t.Errorf("encoded %d bytes, worse than literal-only %d", len(enc), literalOnly)
			}
		})
	}
}

func TestPackBitsEncodeNeverExpandsCorpusRows(t *testing.T) {
	fns, err := filepath.Glob("../testdata/*.mac")
	if err != nil || len(fns) == 0 {
		t.Skip("no corpus available")
	}
	for _, fn := range fns {
		data, err := os.ReadFile(fn)
		if err != nil {
			t.Fatal(err)
		}
		img, err := Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %s", filepath.Base(fn), err)
		}
		pal, ok := img.(*image.Paletted)
		if !ok {
			t.Fatalf("%s: got %T", filepath.Base(fn), img)
		}
		// Re-pack each row and confirm the encoder never expands it.
		var row [Width / 8]byte
		for y := range Height {
			clear(row[:])
			for x := range Width {
				if pal.Pix[y*pal.Stride+x] != 0 {
					row[x/8] |= 0x80 >> (x % 8)
				}
			}
			enc := packBitsEncode(nil, row[:])
			literalOnly := len(row) + (len(row)+maxRun-1)/maxRun
			if len(enc) > literalOnly {
				t.Fatalf("%s row %d: encoded %d bytes, worse than literal-only %d",
					filepath.Base(fn), y, len(enc), literalOnly)
			}
			if got := unpackBits(t, enc); !bytes.Equal(got, row[:]) {
				t.Fatalf("%s row %d: round-trip mismatch", filepath.Base(fn), y)
			}
		}
	}
}

func TestEncodeFileRoundTrip(t *testing.T) {
	fns, err := filepath.Glob("../testdata/*.mac")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range fns {
		t.Run(filepath.Base(fn), func(t *testing.T) {
			data, err := os.ReadFile(fn)
			if err != nil {
				t.Fatal(err)
			}
			f, err := DecodeFile(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("DecodeFile: %s", err)
			}

			if f.Header != nil {
				f.Header.FileName = "RENAMED.PNT"
				f.Header.FileCreator = "TEST"
				f.Header.FileFlags = byte(FlagInited | FlagInvisible)
			}

			var buf bytes.Buffer
			if err := EncodeFile(&buf, f); err != nil {
				t.Fatalf("EncodeFile: %s", err)
			}

			f2, err := DecodeFile(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("re-DecodeFile: %s", err)
			}
			if !bytes.Equal(f.Image.Pix, f2.Image.Pix) {
				t.Error("pixel data mismatch after round-trip")
			}
			if f.Header == nil {
				if f2.Header != nil {
					t.Errorf("Header = %+v, want nil", f2.Header)
				}
				return
			}
			if f2.Header == nil {
				t.Fatal("Header lost in round-trip")
			}
			if f2.Header.FileName != "RENAMED.PNT" {
				t.Errorf("FileName = %q, want \"RENAMED.PNT\"", f2.Header.FileName)
			}
			if f2.Header.FileCreator != "TEST" {
				t.Errorf("FileCreator = %q, want \"TEST\"", f2.Header.FileCreator)
			}
			if f2.Header.FileFlags != byte(FlagInited|FlagInvisible) {
				t.Errorf("FileFlags = %#02x, want %#02x", f2.Header.FileFlags, byte(FlagInited|FlagInvisible))
			}
			if !f2.Header.Created.Equal(f.Header.Created) {
				t.Errorf("Created = %v, want %v", f2.Header.Created, f.Header.Created)
			}
			if !f2.Header.Modified.Equal(f.Header.Modified) {
				t.Errorf("Modified = %v, want %v", f2.Header.Modified, f.Header.Modified)
			}
			// MacBinary requires each fork padded to a 128-byte boundary.
			if buf.Len()%macBinaryHeaderLen != 0 {
				t.Errorf("output length %d is not a multiple of %d", buf.Len(), macBinaryHeaderLen)
			}
		})
	}
}

// TestEncodeFileIsSniffable checks that EncodeFile output with a header is
// recognized by image.Decode, which headerless output deliberately is not.
func TestEncodeFileIsSniffable(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, Width, Height), Palette)
	img.Pix[0] = 1

	var withHeader bytes.Buffer
	if err := EncodeFile(&withHeader, &File{
		Header: &Header{FileName: "TEST.PNT", Created: time.Now().Truncate(time.Second)},
		Image:  img,
	}); err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(withHeader.Bytes()))
	if err != nil {
		t.Fatalf("image.Decode of headered output: %s", err)
	}
	if format != "mac" {
		t.Errorf("format = %q, want \"mac\"", format)
	}
	if pal, ok := decoded.(*image.Paletted); !ok {
		t.Errorf("got %T, want *image.Paletted", decoded)
	} else if !bytes.Equal(pal.Pix, img.Pix) {
		t.Error("pixel mismatch through image.Decode")
	}

	var headerless bytes.Buffer
	if err := EncodeFile(&headerless, &File{Image: img}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := image.Decode(bytes.NewReader(headerless.Bytes())); !errors.Is(err, image.ErrFormat) {
		t.Errorf("image.Decode of headerless output: err = %v, want %v", err, image.ErrFormat)
	}
	// ...but the package's own entry point still reads it.
	if _, err := Decode(bytes.NewReader(headerless.Bytes())); err != nil {
		t.Errorf("Decode of headerless output: %s", err)
	}
}

func TestEncodeNilGuards(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, nil); err == nil {
		t.Error("Encode(w, nil) = nil, want an error")
	}
	if err := EncodeFile(&buf, nil); err == nil {
		t.Error("EncodeFile(w, nil) = nil, want an error")
	}
	if err := EncodeFile(&buf, &File{}); err == nil {
		t.Error("EncodeFile(w, &File{}) = nil, want an error")
	}
}

// TestEncodeFastPathsAgree checks that the *image.Paletted and *image.Gray fast
// paths produce the same output as the generic color-model path.
func TestEncodeFastPathsAgree(t *testing.T) {
	data, err := os.ReadFile("../testdata/noheader.mac")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	pal, ok := img.(*image.Paletted)
	if !ok {
		t.Fatalf("got %T", img)
	}

	// Same pixels as *image.Gray.
	gray := image.NewGray(pal.Bounds())
	for i, idx := range pal.Pix {
		if idx != 0 {
			gray.Pix[i] = 0x00
		} else {
			gray.Pix[i] = 0xff
		}
	}

	var fromPal, fromGray, fromGeneric bytes.Buffer
	for _, tc := range []struct {
		w   *bytes.Buffer
		img image.Image
	}{
		{&fromPal, pal},
		{&fromGray, gray},
		{&fromGeneric, genericImage{gray}},
	} {
		if err := Encode(tc.w, tc.img); err != nil {
			t.Fatalf("Encode(%T): %s", tc.img, err)
		}
	}
	if !bytes.Equal(fromPal.Bytes(), fromGray.Bytes()) {
		t.Error("*image.Paletted and *image.Gray fast paths disagree")
	}
	if !bytes.Equal(fromPal.Bytes(), fromGeneric.Bytes()) {
		t.Error("fast path and generic path disagree")
	}
}

// genericImage hides its underlying concrete type so Encode takes the fallback path.
type genericImage struct{ img image.Image }

func (g genericImage) ColorModel() color.Model { return g.img.ColorModel() }
func (g genericImage) Bounds() image.Rectangle { return g.img.Bounds() }
func (g genericImage) At(x, y int) color.Color { return g.img.At(x, y) }
