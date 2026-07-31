package macpaint_test

import (
	"bytes"
	"fmt"
	"image"
	"log"
	"os"
	"time"

	"github.com/samuel/go-macpaint/macpaint"
)

// ExampleDecode decodes a MacPaint image. The concrete type is always
// *image.Paletted, so pixel data can be read without a conversion.
func ExampleDecode() {
	data, err := os.ReadFile("../testdata/header.mac")
	if err != nil {
		log.Fatal(err)
	}

	img, err := macpaint.Decode(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}

	pal, ok := img.(*image.Paletted)
	if !ok {
		log.Fatalf("got %T, want *image.Paletted", img)
	}
	var black int
	for _, idx := range pal.Pix {
		if idx != 0 {
			black++
		}
	}
	fmt.Printf("%dx%d, %d black pixels\n", pal.Bounds().Dx(), pal.Bounds().Dy(), black)
	// Output: 576x720, 5086 black pixels
}

// ExampleDecodeFile reads the image together with its MacBinary metadata.
// File.Header is nil for a headerless file.
func ExampleDecodeFile() {
	data, err := os.ReadFile("../testdata/bigbinky.mac")
	if err != nil {
		log.Fatal(err)
	}

	mac, err := macpaint.DecodeFile(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	if mac.Header == nil {
		fmt.Println("headerless file")
		return
	}
	fmt.Printf("%s by %s, modified %s\n",
		mac.Header.FileName,
		mac.Header.FileCreator,
		mac.Header.Modified.UTC().Format("2006-01-02"))
	fmt.Printf("has been inited: %v\n", mac.Header.Flags(macpaint.FlagInited))
	// Output:
	// BigBinky by MPNT, modified 1986-08-29
	// has been inited: true
}

// ExampleEncode writes a headerless MacPaint document. Images smaller than
// 576x720 are padded with white and larger ones are cropped.
func ExampleEncode() {
	img := image.NewPaletted(image.Rect(0, 0, macpaint.Width, macpaint.Height), macpaint.Palette)
	// Draw a black diagonal. Index 1 is black.
	for i := range macpaint.Height {
		img.SetColorIndex(i, i, 1)
	}

	var buf bytes.Buffer
	if err := macpaint.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	// The bitmap is 51840 bytes before compression.
	fmt.Printf("encoded %d bytes\n", buf.Len())
	// Output: encoded 4208 bytes
}

// ExampleEncodeFile writes a file with a MacBinary header, which is the form
// image.Decode can recognize. The same *File a decode returns can be passed
// straight back in, so metadata round-trips as one value.
func ExampleEncodeFile() {
	img := image.NewPaletted(image.Rect(0, 0, macpaint.Width, macpaint.Height), macpaint.Palette)
	img.SetColorIndex(0, 0, 1)

	var buf bytes.Buffer
	err := macpaint.EncodeFile(&buf, &macpaint.File{
		Header: &macpaint.Header{
			FileName:    "DIAGONAL.PNT",
			FileCreator: "MPNT",
			Created:     time.Date(1984, time.January, 24, 0, 0, 0, 0, time.UTC),
		},
		Image: img,
	})
	if err != nil {
		log.Fatal(err)
	}

	mac, err := macpaint.DecodeFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(mac.Header.FileName, mac.Header.Created.UTC().Format("2006-01-02"))
	// Output: DIAGONAL.PNT 1984-01-24
}
