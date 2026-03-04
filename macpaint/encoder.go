package macpaint

import (
	"image"
	"image/color"
	"io"
)

// Encode writes img to w in MacPaint headerless format.
//
// For best results, img should be a black-and-white (1-bit equivalent) image;
// the caller is responsible for any dithering and thresholding. Non-black-and-white
// images are converted by thresholding: pixels with luminance below 50% are encoded
// as black, all others as white.
//
// The output is always 576×720 pixels. If img is smaller it is padded with white;
// if larger it is cropped.
func Encode(w io.Writer, img image.Image) error {
	// MacPaint document header: 4-byte version marker + 508 bytes of patterns/padding.
	var hdr [512]byte
	hdr[3] = 2
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}

	bounds := img.Bounds()
	var row [width / 8]byte
	var enc []byte

	for y := range height {
		for i := range row {
			row[i] = 0
		}
		sy := bounds.Min.Y + y
		if sy < bounds.Max.Y {
			for x := range width {
				sx := bounds.Min.X + x
				if sx < bounds.Max.X {
					g := color.GrayModel.Convert(img.At(sx, sy)).(color.Gray)
					if g.Y < 128 {
						row[x/8] |= 0x80 >> uint(x%8)
					}
				}
			}
		}
		enc = packBitsEncode(enc[:0], row[:])
		if _, err := w.Write(enc); err != nil {
			return err
		}
	}
	return nil
}

// packBitsEncode encodes src using PackBits RLE, appending to dst.
func packBitsEncode(dst, src []byte) []byte {
	for i := 0; i < len(src); {
		// Count run of identical bytes (max 128).
		runLen := 1
		for runLen < 128 && i+runLen < len(src) && src[i+runLen] == src[i] {
			runLen++
		}
		if runLen >= 2 {
			dst = append(dst, byte(257-runLen), src[i])
			i += runLen
			continue
		}
		// Collect literal bytes, stopping before a run of 2+ identical bytes.
		litLen := 1
		for litLen < 128 && i+litLen < len(src) {
			if i+litLen+1 < len(src) && src[i+litLen] == src[i+litLen+1] {
				break
			}
			litLen++
		}
		dst = append(dst, byte(litLen-1))
		dst = append(dst, src[i:i+litLen]...)
		i += litLen
	}
	return dst
}
