package macpaint

// http://fileformats.archiveteam.org/wiki/MacPaint
// http://www.fileformat.info/format/macpaint/egff.htm
// http://www.computerhistory.org/atchm/macpaint-and-quickdraw-source-code/
// http://www.textfiles.com/programming/FORMATS/pix_fmt.txt
// http://www.idea2ic.com/File_Formats/macpaint.pdf
// http://www.fileformat.info/format/macpaint/sample/index.htm

// http://cd.textfiles.com/vgaspectrum/mac/

import (
	"bufio"
	"image"
	"image/color"
	"io"
	"io/ioutil"
)

const (
	width    = 576
	height   = 720
	fileType = "PNTG"
)

// file flag bits
const (
	inited = 1 << iota
	changed
	busy
	bozo
	system
	bundle
	invisible
	locked
)

type decoder struct {
	r        io.Reader
	buf      []byte
	noHeader bool
	header   header
}

type header struct {
	fileName           string
	fileType           string // Type of Macintosh file
	fileCreator        string // ID of program that created file
	fileFlags          byte   // File attribute flags
	fileVertPos        uint16 // File vertical position in window
	fileHorzPos        uint16 // File horizontal position in window
	windowID           uint16 // Window or folder ID
	protected          bool   // File protection
	sizeOfDataFork     uint32 // Size of file data fork in bytes
	sizeOfResourceFork uint32 // Size of file resource fork in bytes
	creationStamp      uint32 // Time and date file created
	modificationStamp  uint32 // Time and date file last modified
	getInfoLength      uint16 // GetInfo message length
	// The following fields were added for MacBinary II
	finderFlags      uint16 // Finder flags
	unpackedLength   uint32 // Total unpacked file length
	secondHeadLength uint16 // Length of secondary header
	uploadVersion    byte   // MacBinary version used with uploader
	readVersion      byte   // MacBinary version needed to read
	crcValue         uint16 // CRC value of previous 124 bytes
}

// A ErrFormat reports that the input is not a valid MacPaint.
type ErrFormat string

func (e ErrFormat) Error() string {
	return "macpaint: invalid format: " + string(e)
}

// An ErrUnsupported reports that the variant of the MacPaint file is not supported.
type ErrUnsupported string

func (e ErrUnsupported) Error() string {
	return "macpaint: unsupported variant: " + string(e)
}

func init() {
	image.RegisterFormat("mac", "\x00????????????????????????????????????????????????????????????????PNTG", Decode, DecodeConfig)
}

// Decode reads a MacPaint image from r and returns it as an image.Image.
// The type of Image returned depends on the MacPaint contents.
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

// DecodeConfig returns the color model and dimensions of a MacPaint image
// without decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	return image.Config{
		ColorModel: color.GrayModel,
		Width:      width,
		Height:     height,
	}, nil
}

func newDecoder(r io.Reader) (*decoder, error) {
	d := &decoder{
		r:   r,
		buf: make([]byte, 512),
	}
	if err := d.readHeader(); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return d, nil
}

func (d *decoder) readHeader() error {
	if _, err := io.ReadFull(d.r, d.buf[:4]); err != nil {
		return err
	}
	if d.buf[0] == 0 && d.buf[1] == 0 && d.buf[2] == 0 && d.buf[3] == 2 {
		d.noHeader = true
		return nil
	}
	if _, err := io.ReadFull(d.r, d.buf[4:128]); err != nil {
		return err
	}
	if d.buf[0] != 0 {
		return ErrFormat("expected version 0")
	}
	if d.buf[1] > 63 {
		return ErrFormat("invalid filename length")
	}
	d.header.fileName = string(d.buf[2 : 2+d.buf[1]])
	d.header.fileType = string(d.buf[65:69])
	if d.header.fileType != fileType {
		return ErrFormat("invalid file type")
	}
	d.header.fileCreator = string(d.buf[69:73])
	d.header.fileFlags = d.buf[73]
	d.header.fileVertPos = decodeUint16(d.buf[75:77])
	d.header.fileHorzPos = decodeUint16(d.buf[77:79])
	d.header.windowID = decodeUint16(d.buf[79:81])
	d.header.protected = d.buf[81] == 1
	d.header.sizeOfDataFork = decodeUint32(d.buf[83:87])
	d.header.sizeOfResourceFork = decodeUint32(d.buf[87:91])
	d.header.creationStamp = decodeUint32(d.buf[65+26 : 65+30])
	d.header.modificationStamp = decodeUint32(d.buf[65+30 : 65+34])
	d.header.getInfoLength = decodeUint16(d.buf[65+34 : 65+36])
	d.header.finderFlags = decodeUint16(d.buf[65+36 : 65+38])
	d.header.unpackedLength = decodeUint32(d.buf[65+52 : 65+56])
	d.header.secondHeadLength = decodeUint16(d.buf[65+56 : 65+58])
	d.header.uploadVersion = d.buf[65+58]
	d.header.readVersion = d.buf[65+59]
	d.header.crcValue = decodeUint16(d.buf[65+60 : 65+62])
	return nil
}

func (d *decoder) decode() (image.Image, error) {
	if !d.noHeader {
		if _, err := io.ReadFull(d.r, d.buf[:4]); err != nil {
			return nil, err
		}
		// TODO: not sure why this differs between some files
		// if d.buf[0] != 0 || d.buf[1] != 0 || d.buf[2] != 0 || d.buf[3] != 2 {
		// 	return nil, ErrFormat("missing data marker")
		// }
	}
	// 304 for pattern data, 204 for padding
	if _, err := io.CopyN(ioutil.Discard, d.r, 304+204); err != nil {
		return nil, err
	}
	rd := bufio.NewReader(d.r)
	img := image.NewGray(image.Rect(0, 0, width, height))
	for o := 0; o < len(img.Pix); {
		n, err := rd.ReadByte()
		if err != nil {
			return nil, err
		}
		if n&0x80 != 0 {
			n = 1 - n
			b, err := rd.ReadByte()
			if err != nil {
				return nil, err
			}
			for i := 0; i < int(n); i++ {
				c := b
				for j := 0; j < 8; j++ {
					if o == len(img.Pix) {
						return nil, ErrFormat("overflow decoding RLE")
					}
					if c&0x80 != 0 {
						img.Pix[o] = 0
					} else {
						img.Pix[o] = 255
					}
					o++
					c <<= 1
				}
			}
		} else {
			n++
			if _, err := io.ReadFull(rd, d.buf[:int(n)]); err != nil {
				return nil, err
			}
			for _, b := range d.buf[:int(n)] {
				for j := 0; j < 8; j++ {
					if o == len(img.Pix) {
						return nil, ErrFormat("overflow decoding RLE")
					}
					if b&0x80 != 0 {
						img.Pix[o] = 0
					} else {
						img.Pix[o] = 255
					}
					o++
					b <<= 1
				}
			}
		}
	}
	return img, nil
}

func decodeUint16(b []byte) uint16 {
	return (uint16(b[0]) << 8) | uint16(b[1])
}

func decodeUint32(b []byte) uint32 {
	return (uint32(b[0]) << 24) | (uint32(b[1]) << 16) | (uint32(b[2]) << 8) | uint32(b[3])
}
