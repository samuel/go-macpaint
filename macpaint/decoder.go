// Package macpaint decodes and encodes MacPaint (PNTG) image files.
//
// MacPaint images are always 576x720 pixels and one bit per pixel. Two file
// variants are supported: files wrapped in a 128-byte MacBinary header, and
// headerless files beginning with the four-byte document version.
//
// Only the MacBinary-headered variant is registered with the image package for
// use through image.Decode. A headerless file begins with three zero bytes and a
// small version number, which is far too weak a signature to sniff on; decode
// those by calling Decode or DecodeConfig directly.
//
// Format references:
//   - http://fileformats.archiveteam.org/wiki/MacPaint
//   - http://www.fileformat.info/format/macpaint/egff.htm
//   - http://www.computerhistory.org/atchm/macpaint-and-quickdraw-source-code/
//   - http://www.textfiles.com/programming/FORMATS/pix_fmt.txt
//   - https://web.archive.org/web/20230209064403/http://www.idea2ic.com/File_Formats/macpaint.pdf
//   - https://files.stairways.com/other/macbinaryii-standard-info.txt
//
// Sample files:
//   - http://www.fileformat.info/format/macpaint/sample/index.htm
//   - http://cd.textfiles.com/vgaspectrum/mac/
package macpaint

import (
	"bufio"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
)

// Width and Height are the fixed pixel dimensions of every MacPaint image.
const (
	Width  = 576
	Height = 720
)

const (
	fileType = "PNTG"

	// defaultCreator is the creator ID EncodeFile writes when none is supplied:
	// MacPaint's own.
	defaultCreator = "MPNT"

	// macBinaryHeaderLen is the size of a MacBinary header, which also bounds the
	// longest PackBits literal run (128 bytes), so one buffer serves both.
	macBinaryHeaderLen = 128

	// A MacPaint document begins with a 4-byte version, 304 bytes of fill patterns
	// and 204 bytes of padding.
	docVersionLen = 4
	patternLen    = 304
	paddingLen    = 204

	// macBinaryII is the smallest uploader-version byte (offset 122) denoting a
	// MacBinary II header. MacBinary II added the secondary-header length and the
	// CRC; in MacBinary I those bytes are reserved and may hold arbitrary data.
	macBinaryII = 129

	maxFileNameLen = 63
)

// File is a MacPaint file: its optional MacBinary header and its image.
type File struct {
	// Header describes the MacBinary wrapper. It is nil for headerless files, and
	// EncodeFile writes a headerless file when it is nil.
	Header *Header
	Image  *image.Paletted
}

type decoder struct {
	r      io.Reader
	buf    []byte
	header *Header // nil for a headerless file
}

// FormatError reports that the input is not a valid MacPaint.
type FormatError string

func (e FormatError) Error() string {
	return "macpaint: invalid format: " + string(e)
}

// An UnsupportedError reports an unsupported MacPaint file variant.
type UnsupportedError string

func (e UnsupportedError) Error() string {
	return "macpaint: unsupported variant: " + string(e)
}

// Palette is the two-color palette of a decoded MacPaint image. Index 0 is white
// and index 1 is black, so a zero-valued Pix is a blank page, matching MacPaint's
// convention that an unset bit is white.
var Palette = color.Palette{color.Gray{Y: 0xff}, color.Gray{Y: 0x00}}

// bitPixels maps each byte of packed MacPaint pixels to the eight palette indices
// it expands to. A set bit is black, which is index 1.
var bitPixels = func() [256][8]byte {
	var t [256][8]byte
	for b := range 256 {
		for i := range 8 {
			if b&(0x80>>i) != 0 {
				t[b][i] = 1
			}
		}
	}
	return t
}()

func init() {
	image.RegisterFormat("mac", "\x00????????????????????????????????????????????????????????????????PNTG", Decode, DecodeConfig)
}

// Decode reads a MacPaint image from r and returns it as an image.Image. The
// concrete type is always *image.Paletted, using Palette.
//
// Use DecodeFile instead to also read the MacBinary header.
func Decode(r io.Reader) (image.Image, error) {
	d, err := newDecoder(r)
	if err != nil {
		return nil, err
	}
	img, err := d.decode()
	if err != nil {
		return nil, err
	}
	return img, nil
}

// DecodeFile reads a MacPaint image from r along with its MacBinary header. The
// returned File has a nil Header when r holds a headerless file.
func DecodeFile(r io.Reader) (*File, error) {
	d, err := newDecoder(r)
	if err != nil {
		return nil, err
	}
	img, err := d.decode()
	if err != nil {
		return nil, err
	}
	return &File{Header: d.header, Image: img}, nil
}

// DecodeConfig returns the color model and dimensions of a MacPaint image
// without decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	if _, err := newDecoder(r); err != nil {
		return image.Config{}, err
	}
	return image.Config{
		ColorModel: Palette,
		Width:      Width,
		Height:     Height,
	}, nil
}

// unexpectedEOF reports the io.EOF of a partial read as io.ErrUnexpectedEOF, so
// truncation doesn't look like a clean end of stream.
func unexpectedEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// validDocVersion reports whether v is a MacPaint document version seen in real
// files. Version 0 means the fill patterns are absent (the space is still
// reserved); 2 and 3 carry patterns.
func validDocVersion(v uint32) bool {
	return v == 0 || v == 2 || v == 3
}

func newDecoder(r io.Reader) (*decoder, error) {
	d := &decoder{
		r:   r,
		buf: make([]byte, macBinaryHeaderLen),
	}
	if err := d.readHeader(); err != nil {
		return nil, unexpectedEOF(err)
	}
	return d, nil
}

func (d *decoder) readHeader() error {
	if _, err := io.ReadFull(d.r, d.buf[:docVersionLen]); err != nil {
		return err
	}
	// A headerless file opens with the MacPaint document version instead of a
	// MacBinary header. MacBinary requires a filename length of 1 to 63 at offset
	// 1, so three leading zero bytes cannot begin a MacBinary header and the input
	// can only be a headerless document.
	if d.buf[0] == 0 && d.buf[1] == 0 && d.buf[2] == 0 {
		if !validDocVersion(uint32(d.buf[3])) {
			return FormatError("unrecognized document version")
		}
		return nil // Headerless: d.header stays nil.
	}
	if _, err := io.ReadFull(d.r, d.buf[docVersionLen:macBinaryHeaderLen]); err != nil {
		return err
	}
	h, err := parseHeader(d.buf[:macBinaryHeaderLen])
	if err != nil {
		return err
	}
	d.header = h
	return nil
}

// crcCCITT computes the CRC-CCITT (polynomial 0x1021) checksum over data.
func crcCCITT(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		for range 8 {
			if (crc>>15)^(uint16(b)>>7) != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
			b <<= 1
		}
	}
	return crc
}

// body returns the reader for the MacPaint document, bounded by the declared data
// fork length so that decoding cannot run into a following resource fork.
func (d *decoder) body() io.Reader {
	if d.header == nil || d.header.SizeOfDataFork == 0 {
		return d.r
	}
	return io.LimitReader(d.r, int64(d.header.SizeOfDataFork))
}

// writePixels expands the packed bits of b into pix at offset o and returns the
// new offset. Both the image size and every run are whole bytes, so o always
// lands on a multiple of 8.
func writePixels(pix []byte, o int, b byte) (int, error) {
	if o+8 > len(pix) {
		return o, FormatError("overflow decoding RLE")
	}
	copy(pix[o:o+8], bitPixels[b][:])
	return o + 8, nil
}

// skipDocHeader consumes the MacPaint document header: the version long, which is
// only present when a MacBinary header preceded it, plus the fill patterns and
// padding.
func (d *decoder) skipDocHeader(r io.Reader) error {
	if d.header != nil {
		if _, err := io.ReadFull(r, d.buf[:docVersionLen]); err != nil {
			return unexpectedEOF(err)
		}
		// The document version leads the data fork. Real files carry 0, 2 or 3
		// here, so only clearly bogus values are rejected.
		if v := binary.BigEndian.Uint32(d.buf[:docVersionLen]); !validDocVersion(v) {
			return FormatError("unrecognized document version")
		}
	}
	if _, err := io.CopyN(io.Discard, r, patternLen+paddingLen); err != nil {
		return unexpectedEOF(err)
	}
	return nil
}

// readRun expands a PackBits run, repeating one byte 257-n times (2 to 128).
func readRun(rd *bufio.Reader, pix []byte, o int, n byte) (int, error) {
	b, err := rd.ReadByte()
	if err != nil {
		return o, unexpectedEOF(err)
	}
	for range 257 - int(n) {
		if o, err = writePixels(pix, o, b); err != nil {
			return o, err
		}
	}
	return o, nil
}

// readLiteral expands a PackBits literal, copying the next n+1 bytes verbatim.
func (d *decoder) readLiteral(rd *bufio.Reader, pix []byte, o int, n byte) (int, error) {
	litLen := int(n) + 1
	if _, err := io.ReadFull(rd, d.buf[:litLen]); err != nil {
		return o, unexpectedEOF(err)
	}
	for _, b := range d.buf[:litLen] {
		var err error
		if o, err = writePixels(pix, o, b); err != nil {
			return o, err
		}
	}
	return o, nil
}

// decodePixels fills pix from the PackBits-compressed image data in rd.
func (d *decoder) decodePixels(rd *bufio.Reader, pix []byte) error {
	for o := 0; o < len(pix); {
		n, err := rd.ReadByte()
		if err != nil {
			return unexpectedEOF(err)
		}
		switch {
		case n == 0x80:
			// No operation, per the PackBits specification.
		case n&0x80 != 0:
			o, err = readRun(rd, pix, o, n)
		default:
			o, err = d.readLiteral(rd, pix, o, n)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *decoder) decode() (*image.Paletted, error) {
	r := d.body()
	if err := d.skipDocHeader(r); err != nil {
		return nil, err
	}
	img := image.NewPaletted(image.Rect(0, 0, Width, Height), Palette)
	if err := d.decodePixels(bufio.NewReader(r), img.Pix); err != nil {
		return nil, err
	}
	return img, nil
}
