package macpaint

import (
	"errors"
	"image"
	"image/color"
	"io"
)

// maxRun is the longest run or literal a single PackBits control byte can encode.
const maxRun = 128

// Encode writes img to w as a headerless MacPaint document.
//
// For best results, img should be a black-and-white (1-bit equivalent) image; the
// caller is responsible for any dithering and thresholding. Other images are
// converted by thresholding: pixels with luminance below 50% are encoded as black,
// all others as white.
//
// The output is always Width x Height pixels. If img is smaller it is padded with
// white; if larger it is cropped.
//
// Use EncodeFile to also write a MacBinary header.
func Encode(w io.Writer, img image.Image) error {
	if img == nil {
		return errors.New("macpaint: nil image")
	}
	// MacPaint document header: 4-byte version marker plus patterns and padding.
	var hdr [docVersionLen + patternLen + paddingLen]byte
	hdr[3] = 2
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	return encodeRows(w, img)
}

// EncodeFile writes f to w. When f.Header is nil the output is a headerless
// document, identical to what Encode produces. Otherwise a 128-byte MacBinary II
// header precedes the document and the data fork is padded to a 128-byte multiple,
// as the MacBinary specification requires.
//
// FileType is forced to "PNTG" and SizeOfDataFork is computed. The resource
// fork, unpacked length and secondary header length are forced to zero, and
// UploadVersion and ReadVersion are set to MacBinary II. CRC is computed. An
// empty FileName becomes "untitled" and an empty FileCreator becomes "MPNT".
func EncodeFile(w io.Writer, f *File) error {
	if f == nil {
		return errors.New("macpaint: nil file")
	}
	if f.Image == nil {
		return errors.New("macpaint: nil image")
	}
	if f.Header == nil {
		return Encode(w, f.Image)
	}

	// The header carries the data fork length, so the document must be built first.
	var doc []byte
	doc = append(doc, make([]byte, docVersionLen+patternLen+paddingLen)...)
	doc[3] = 2
	buf := bytesWriter{buf: doc}
	if err := encodeRows(&buf, f.Image); err != nil {
		return err
	}
	doc = buf.buf

	// The document is a fixed 576x720 image, so it is at most ~53 KB and the
	// conversion cannot overflow.
	//nolint:gosec // G115: len(doc) is bounded by the fixed image dimensions.
	hdr, err := appendHeader(nil, f.Header, uint32(len(doc)))
	if err != nil {
		return err
	}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if _, err := w.Write(doc); err != nil {
		return err
	}
	// MacBinary pads each fork to a 128-byte boundary.
	if pad := (macBinaryHeaderLen - len(doc)%macBinaryHeaderLen) % macBinaryHeaderLen; pad != 0 {
		if _, err := w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return nil
}

// bytesWriter is a minimal io.Writer over a byte slice, avoiding a bytes.Buffer
// import just to measure the document length.
type bytesWriter struct{ buf []byte }

func (b *bytesWriter) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// rowPacker packs one row of an image into MacPaint's 1-bit-per-pixel layout,
// where a set bit is black.
type rowPacker func(row []byte, y int)

// newRowPacker returns a packer specialized to img's concrete type. The fast paths
// read pixel data directly; the fallback goes through the color model, which costs
// two interface conversions per pixel.
func newRowPacker(img image.Image) rowPacker {
	b := img.Bounds()
	maxX := min(b.Min.X+Width, b.Max.X)

	switch m := img.(type) {
	case *image.Paletted:
		// Precompute which palette indices count as black.
		black := make([]bool, len(m.Palette))
		for i, c := range m.Palette {
			gr := color.GrayModel.Convert(c)
			g, ok := gr.(color.Gray)
			black[i] = ok && g.Y < 0x80
		}
		return func(row []byte, y int) {
			base := m.PixOffset(b.Min.X, y)
			for x, sx := 0, b.Min.X; sx < maxX; x, sx = x+1, sx+1 {
				if idx := m.Pix[base+x]; int(idx) < len(black) && black[idx] {
					row[x/8] |= 0x80 >> (x % 8)
				}
			}
		}
	case *image.Gray:
		return func(row []byte, y int) {
			base := m.PixOffset(b.Min.X, y)
			for x, sx := 0, b.Min.X; sx < maxX; x, sx = x+1, sx+1 {
				if m.Pix[base+x] < 0x80 {
					row[x/8] |= 0x80 >> (x % 8)
				}
			}
		}
	default:
		return func(row []byte, y int) {
			for x, sx := 0, b.Min.X; sx < maxX; x, sx = x+1, sx+1 {
				if g, ok := color.GrayModel.Convert(img.At(sx, y)).(color.Gray); ok && g.Y < 0x80 {
					row[x/8] |= 0x80 >> (x % 8)
				}
			}
		}
	}
}

// encodeRows writes the PackBits-compressed image data for img.
func encodeRows(w io.Writer, img image.Image) error {
	bounds := img.Bounds()
	pack := newRowPacker(img)

	var row [Width / 8]byte
	var enc []byte
	for y := range Height {
		clear(row[:])
		if sy := bounds.Min.Y + y; sy < bounds.Max.Y {
			pack(row[:], sy)
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
		runLen := 1
		for runLen < maxRun && i+runLen < len(src) && src[i+runLen] == src[i] {
			runLen++
		}
		// A 2-byte run pays off only at a packet start; mid-literal it splits
		// the literal and adds a control byte.
		if runLen >= 3 || (runLen == 2 && i+runLen == len(src)) {
			// runLen is in [2, maxRun], so 257-runLen is in [129, 255].
			//nolint:gosec // G115: provably within byte range, see above.
			dst = append(dst, byte(257-runLen), src[i])
			i += runLen
			continue
		}
		// Collect literal bytes, stopping before a run long enough to be worth
		// encoding as one.
		litLen := 1
		for litLen < maxRun && i+litLen < len(src) {
			if i+litLen+2 < len(src) &&
				src[i+litLen] == src[i+litLen+1] && src[i+litLen] == src[i+litLen+2] {
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
