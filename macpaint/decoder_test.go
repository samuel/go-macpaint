package macpaint

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// testOutDir is where -save writes PNGs. It's created on demand.
const testOutDir = "../testout"

// saveOutput is enabled with "go test -save" to write each decoded image as a
// PNG in testOutDir for visual inspection.
var saveOutput = flag.Bool("save", false, "save decoded images as PNGs to "+testOutDir)

// savePNG writes img to testOutDir/name, creating the directory if needed.
func savePNG(t *testing.T, name string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(testOutDir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %s", testOutDir, err)
	}
	fn := filepath.Join(testOutDir, name)
	f, err := os.Create(fn)
	if err != nil {
		t.Fatalf("create %s: %s", fn, err)
	}
	if err := png.Encode(f, img); err != nil {
		// The encode error is the useful one, so drop any close error here.
		_ = f.Close()
		t.Fatalf("encode %s: %s", fn, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %s", fn, err)
	}
}

// readFixture reads a file from testdata, skipping the test if it isn't present.
// Only a subset of the sample images is committed.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../testdata", name))
	if err != nil {
		t.Skipf("fixture not present: %s", err)
	}
	return data
}

// golden pins the decoded pixel data of every committed fixture. A change to bit
// order, row stride, or black/white polarity changes these hashes.
var golden = []struct {
	file   string
	sha256 string
	black  int // Black pixels, as a human-checkable cross-reference.
}{
	{"1066.mac", "988c4d4f41afadeda22f5f0badccd310eb743fb892292576378b082440a4c5c3", 96712},
	{"aladin.mac", "b06a5532aeef542445e80fa34ca09e86c1ee33d63de9a985691f76f69f9c6471", 3282},
	{"armored.mac", "ef263b753333216af2b5a2827589f84eadee9c9bf53788904bbbc174112c5f8b", 50332},
	{"awards.mac", "99e5a9fe49b50bf9c7fe5ba271515bdacf45bbed954d48be17e3da10288435b5", 3880},
	{"bamboo.mac", "04fe01a1eca0359fb3d5b012eec359f989f33187f5db12570d300d478e37d63d", 202328},
	{"bigbinky.mac", "54c1f7efaac38245f4092084f1fc478a9cbadfd51f5faf6d496be42e0d73c391", 12265},
	{"header.mac", "7c822f82c47c766b2564561f5d7100bccce153c547ae34097e33e2a2249a53a1", 5086},
	{"noheader.mac", "7c822f82c47c766b2564561f5d7100bccce153c547ae34097e33e2a2249a53a1", 5086},
}

func TestDecodeGolden(t *testing.T) {
	for _, g := range golden {
		t.Run(g.file, func(t *testing.T) {
			img, err := Decode(bytes.NewReader(readFixture(t, g.file)))
			if err != nil {
				t.Fatalf("Decode: %s", err)
			}
			pal, ok := img.(*image.Paletted)
			if !ok {
				t.Fatalf("Decode returned %T, want *image.Paletted", img)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(pal.Pix)); got != g.sha256 {
				t.Errorf("pixel hash = %s, want %s", got, g.sha256)
			}
			var black int
			for _, v := range pal.Pix {
				if v != 0 {
					black++
				}
			}
			if black != g.black {
				t.Errorf("black pixels = %d, want %d", black, g.black)
			}
		})
	}
}

// TestHeaderAndHeaderlessAgree checks that the two file variants decode through
// their separate code paths to the same pixels. The two fixtures hold the same
// drawing, one wrapped in a MacBinary header and one not.
func TestHeaderAndHeaderlessAgree(t *testing.T) {
	withHdr, err := Decode(bytes.NewReader(readFixture(t, "header.mac")))
	if err != nil {
		t.Fatal(err)
	}
	without, err := Decode(bytes.NewReader(readFixture(t, "noheader.mac")))
	if err != nil {
		t.Fatal(err)
	}
	a, aok := withHdr.(*image.Paletted)
	b, bok := without.(*image.Paletted)
	if !aok || !bok {
		t.Fatalf("got %T and %T, want *image.Paletted", withHdr, without)
	}
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("headered and headerless fixtures decode to different pixels")
	}
}

func TestDecodeConfig(t *testing.T) {
	cfg, err := DecodeConfig(bytes.NewReader(readFixture(t, "header.mac")))
	if err != nil {
		t.Fatalf("DecodeConfig: %s", err)
	}
	if cfg.Width != Width {
		t.Errorf("Width = %d, want %d", cfg.Width, Width)
	}
	if cfg.Height != Height {
		t.Errorf("Height = %d, want %d", cfg.Height, Height)
	}
	pal, ok := cfg.ColorModel.(color.Palette)
	if !ok {
		t.Fatalf("ColorModel = %T, want color.Palette", cfg.ColorModel)
	}
	if len(pal) != 2 {
		t.Fatalf("palette has %d colors, want 2", len(pal))
	}
	// Index 0 must be white and index 1 black, so a zero Pix is a blank page.
	if r, _, _, _ := pal[0].RGBA(); r != 0xffff {
		t.Errorf("palette[0] = %v, want white", pal[0])
	}
	if r, _, _, _ := pal[1].RGBA(); r != 0 {
		t.Errorf("palette[1] = %v, want black", pal[1])
	}
}

// TestDecodeConfigMatchesDecode checks that the cheap path agrees with the full one.
func TestDecodeConfigMatchesDecode(t *testing.T) {
	data := readFixture(t, "header.mac")
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := img.Bounds(), image.Rect(0, 0, cfg.Width, cfg.Height); got != want {
		t.Errorf("bounds = %v, want %v from the config", got, want)
	}
}

func TestDecode(t *testing.T) {
	fns, err := filepath.Glob("../testdata/*.mac")
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) == 0 {
		t.Skip("no fixtures present")
	}
	for _, fn := range fns {
		_, filename := filepath.Split(fn)
		t.Run(filename, func(t *testing.T) {
			f, err := os.Open(fn)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close() //nolint:errcheck // Read-only fixture; the close error is not actionable.
			img, err := Decode(f)
			if err != nil {
				t.Fatalf("Decode: %s", err)
			}
			if got, want := img.Bounds(), image.Rect(0, 0, Width, Height); got != want {
				t.Errorf("bounds = %v, want %v", got, want)
			}
			if *saveOutput {
				savePNG(t, filename+".png", img)
			}
		})
	}
}

func TestDecodeHeader(t *testing.T) {
	f, err := os.Open("../testdata/header.mac")
	if err != nil {
		t.Skipf("fixture not present: %s", err)
	}
	defer f.Close() //nolint:errcheck // Read-only fixture; the close error is not actionable.
	img, format, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if format != "mac" {
		t.Fatalf("format = %q, want \"mac\"", format)
	}
	if *saveOutput {
		savePNG(t, "header.png", img)
	}
}

// TestImageDecodeSniffing pins which variants image.Decode recognizes: only the
// MacBinary-headered one is registered, because a headerless file's four-byte
// signature also matches unrelated formats.
func TestImageDecodeSniffing(t *testing.T) {
	if _, format, err := image.Decode(bytes.NewReader(readFixture(t, "header.mac"))); err != nil {
		t.Errorf("image.Decode(header.mac): %s", err)
	} else if format != "mac" {
		t.Errorf("image.Decode(header.mac) format = %q, want \"mac\"", format)
	}

	headerless := readFixture(t, "noheader.mac")
	if _, _, err := image.Decode(bytes.NewReader(headerless)); !errors.Is(err, image.ErrFormat) {
		t.Errorf("image.Decode(noheader.mac) err = %v, want %v", err, image.ErrFormat)
	}
	if _, err := Decode(bytes.NewReader(headerless)); err != nil {
		t.Errorf("Decode(noheader.mac): %s", err)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	fns, err := filepath.Glob("../testdata/*.mac")
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) == 0 {
		t.Skip("no fixtures present")
	}
	for _, fn := range fns {
		_, filename := filepath.Split(fn)
		t.Run(filename, func(t *testing.T) {
			f, err := os.Open(fn)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close() //nolint:errcheck // Read-only fixture; the close error is not actionable.

			img1, err := Decode(f)
			if err != nil {
				t.Fatalf("decode: %s", err)
			}

			var buf bytes.Buffer
			if err := Encode(&buf, img1); err != nil {
				t.Fatalf("encode: %s", err)
			}

			img2, err := Decode(&buf)
			if err != nil {
				t.Fatalf("re-decode: %s", err)
			}

			p1, ok := img1.(*image.Paletted)
			if !ok {
				t.Fatalf("Decode returned %T, want *image.Paletted", img1)
			}
			p2, ok := img2.(*image.Paletted)
			if !ok {
				t.Fatalf("re-Decode returned %T, want *image.Paletted", img2)
			}
			if !bytes.Equal(p1.Pix, p2.Pix) {
				t.Error("pixel data mismatch after round-trip")
			}
		})
	}
}

func TestDecodeNoHeader(t *testing.T) {
	f, err := os.Open("../testdata/noheader.mac")
	if err != nil {
		t.Skipf("fixture not present: %s", err)
	}
	defer f.Close() //nolint:errcheck // Read-only fixture; the close error is not actionable.
	img, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := img.(*image.Paletted); !ok {
		t.Errorf("got %T, want *image.Paletted", img)
	} else if len(f.Palette) != 2 {
		t.Errorf("palette has %d colors, want 2", len(f.Palette))
	}
	if *saveOutput {
		savePNG(t, "noheader.png", img)
	}
}

// macBinaryII2 returns hdr rewritten as a MacBinary II header with a valid CRC and
// the given secondary-header length, so the MacBinary II code paths are reachable.
func macBinaryII2(hdr []byte, secondHeader uint16) []byte {
	b := make([]byte, len(hdr))
	copy(b, hdr)
	b[122], b[123] = macBinaryII, macBinaryII
	binary.BigEndian.PutUint16(b[120:122], secondHeader)
	binary.BigEndian.PutUint16(b[124:126], crcCCITT(b[:124]))
	return b
}

func TestDecodeErrors(t *testing.T) {
	valid := readFixture(t, "header.mac")

	zeroDataFork := make([]byte, len(valid))
	copy(zeroDataFork, valid)
	for i := 83; i < 87; i++ {
		zeroDataFork[i] = 0
	}

	wrongType := make([]byte, len(valid))
	copy(wrongType, valid)
	copy(wrongType[65:69], "JPEG")

	badCRC := macBinaryII2(valid, 0)
	badCRC[125] ^= 0xff

	trailing := macBinaryII2(valid, 0)
	forkLen := binary.BigEndian.Uint32(trailing[83:87])
	trailingLen := uint32(3)
	binary.BigEndian.PutUint32(trailing[83:87], forkLen+trailingLen)
	binary.BigEndian.PutUint16(trailing[124:126], crcCCITT(trailing[:124]))
	trailing = append(trailing[:128+forkLen], 0x80, 0x80, 0x80)

	// A headerless document whose RLE runs past the end of the image. The image is
	// 51840 bytes of packed pixels; 409 runs of 127 bytes overruns it.
	overflow := make([]byte, docVersionLen+patternLen+paddingLen)
	overflow[3] = 2
	for range 409 {
		overflow = append(overflow, byte(257-127), 0xff)
	}

	badVersion := make([]byte, len(readFixture(t, "noheader.mac")))
	copy(badVersion, readFixture(t, "noheader.mac"))
	badVersion[3] = 7

	tests := []struct {
		name    string
		data    []byte
		wantErr error  // Matched with errors.Is when non-nil.
		wantMsg string // Otherwise matched as an exact error string.
	}{
		{name: "empty", data: nil, wantErr: io.ErrUnexpectedEOF},
		{name: "one byte", data: []byte{'0'}, wantErr: io.ErrUnexpectedEOF},
		{name: "truncated header", data: valid[:60], wantErr: io.ErrUnexpectedEOF},
		{name: "truncated doc header", data: valid[:300], wantErr: io.ErrUnexpectedEOF},
		{name: "truncated image data", data: valid[:1500], wantErr: io.ErrUnexpectedEOF},
		{
			name: "wrong file type", data: wrongType,
			wantMsg: FormatError("invalid file type").Error(),
		},
		{
			name: "bad document version", data: badVersion,
			wantMsg: FormatError("unrecognized document version").Error(),
		},
		{
			name: "crc mismatch", data: badCRC,
			wantMsg: FormatError("CRC mismatch").Error(),
		},
		{
			name: "zero data fork", data: zeroDataFork,
			wantMsg: FormatError("zero data fork length").Error(),
		},
		{
			name: "trailing data", data: trailing,
			wantMsg: FormatError("trailing data after image").Error(),
		},
		{
			name: "secondary header", data: macBinaryII2(valid, 3615),
			wantMsg: UnsupportedError("MacBinary secondary header").Error(),
		},
		{
			name: "rle overflow", data: overflow,
			wantMsg: FormatError("overflow decoding RLE").Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := Decode(bytes.NewReader(tt.data))
			if err == nil {
				t.Fatal("Decode succeeded, want an error")
			}
			if img != nil {
				t.Errorf("Decode returned a %T alongside the error", img)
			}
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("err = %v, want %v", err, tt.wantErr)
				}
				// A truncated file must never look like a clean end of stream.
				if errors.Is(err, io.EOF) {
					t.Errorf("err = %v, which callers read as a clean EOF", err)
				}
			case err.Error() != tt.wantMsg:
				t.Errorf("err = %q, want %q", err, tt.wantMsg)
			}
		})
	}
}

// TestDecodeMacBinaryIIValid confirms a well-formed MacBinary II header decodes,
// so the CRC and secondary-header checks are not rejecting valid input.
func TestDecodeMacBinaryIIValid(t *testing.T) {
	data := macBinaryII2(readFixture(t, "header.mac"), 0)
	f, err := DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeFile: %s", err)
	}
	if f.Header == nil {
		t.Fatal("Header is nil")
	}
	if f.Header.UploadVersion != macBinaryII {
		t.Errorf("UploadVersion = %d, want %d", f.Header.UploadVersion, macBinaryII)
	}
	if f.Header.CRC == 0 {
		t.Error("CRC was not read")
	}
}

func BenchmarkDecode(b *testing.B) {
	data, err := os.ReadFile("../testdata/1066.mac")
	if err != nil {
		b.Skipf("fixture not present: %s", err)
	}
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeFile(b *testing.B) {
	data, err := os.ReadFile("../testdata/1066.mac")
	if err != nil {
		b.Skipf("fixture not present: %s", err)
	}
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := DecodeFile(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	data, err := os.ReadFile("../testdata/1066.mac")
	if err != nil {
		b.Skipf("fixture not present: %s", err)
	}
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(Width * Height / 8))
	for b.Loop() {
		if err := Encode(io.Discard, img); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeGeneric measures the fallback path, which goes through the color
// model rather than reading pixel data directly.
func BenchmarkEncodeGeneric(b *testing.B) {
	data, err := os.ReadFile("../testdata/1066.mac")
	if err != nil {
		b.Skipf("fixture not present: %s", err)
	}
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	rgba := image.NewRGBA(img.Bounds())
	for y := range Height {
		for x := range Width {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	b.SetBytes(int64(Width * Height / 8))
	for b.Loop() {
		if err := Encode(io.Discard, rgba); err != nil {
			b.Fatal(err)
		}
	}
}
