package macpaint

import (
	"encoding/binary"
	"time"
	"unicode/utf8"
)

// FinderFlag is a bit in Header.FileFlags, the Finder flags byte at MacBinary
// offset 73.
//
// These names follow the MacBinary and EGFF era documentation linked in the
// package comment. Later Macintosh systems reassigned two of the bits: FlagLocked
// is isAlias and FlagSystem is nameLocked in the modern 16-bit Finder flags; the
// rest are unchanged (FlagInited is hasBeenInited, FlagBundle is hasBundle,
// FlagInvisible is isInvisible).
type FinderFlag byte

// Finder flag bits, masking Header.FileFlags.
const (
	FlagInited FinderFlag = 1 << iota
	FlagChanged
	FlagBusy
	FlagBozo
	FlagSystem
	FlagBundle
	FlagInvisible
	FlagLocked
)

// macEpoch is the Macintosh HFS epoch: timestamps are seconds since this instant.
var macEpoch = time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC)

// macTime converts a Macintosh HFS timestamp to a time.Time. A zero stamp, which
// is what files with no recorded time carry, maps to the zero time.
func macTime(stamp uint32) time.Time {
	if stamp == 0 {
		return time.Time{}
	}
	return macEpoch.Add(time.Duration(stamp) * time.Second)
}

// macStamp converts a time.Time back to a Macintosh HFS timestamp. The zero time
// becomes zero; other times are clamped to the representable range.
func macStamp(t time.Time) uint32 {
	if t.IsZero() {
		return 0
	}
	secs := t.Unix() - macEpoch.Unix()
	if secs < 1 {
		return 1
	}
	if secs > int64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(secs)
}

// Header is the MacBinary header wrapping a MacPaint file.
//
// The fields below "Informational" are read from the file but are computed or
// fixed when encoding; see EncodeFile for which values are ignored.
type Header struct {
	FileName    string    // Mac filename, 1 to 63 bytes
	FileCreator string    // ID of the program that created the file, e.g. "MPNT"
	FileFlags   byte      // Finder flags byte; mask with the FinderFlag constants
	VertPos     uint16    // Vertical position in the file's Finder window
	HorzPos     uint16    // Horizontal position in the file's Finder window
	WindowID    uint16    // Window or folder ID
	Protected   bool      // File protection
	Created     time.Time // Zero if the file recorded no creation time
	Modified    time.Time // Zero if the file recorded no modification time

	// Informational.
	FileType           string // Always "PNTG"
	SizeOfDataFork     uint32 // Size of the file's data fork in bytes
	SizeOfResourceFork uint32 // Size of the file's resource fork in bytes
	GetInfoLength      uint16 // GetInfo comment length
	FinderFlags        uint16 // (FileFlags << 8) | byte 101; FileFlags is authoritative for the high byte
	UnpackedLength     uint32 // Total unpacked length; MacBinary II only
	SecondHeaderLength uint16 // Secondary header length; non-zero is unsupported
	UploadVersion      byte   // MacBinary version of the uploading program
	ReadVersion        byte   // Minimum MacBinary version needed to read
	CRC                uint16 // CRC-CCITT over the first 124 header bytes
}

// Flags reports whether every bit in mask is set in FileFlags.
func (h *Header) Flags(mask FinderFlag) bool {
	return FinderFlag(h.FileFlags)&mask == mask
}

// parseHeader decodes a 128-byte MacBinary header.
func parseHeader(b []byte) (*Header, error) {
	if b[0] != 0 {
		return nil, FormatError("expected version 0")
	}
	nameLen := b[1]
	if nameLen == 0 || nameLen > maxFileNameLen {
		return nil, FormatError("invalid filename length")
	}
	h := &Header{
		FileName:    string(b[2 : 2+nameLen]),
		FileType:    string(b[65:69]),
		FileCreator: string(b[69:73]),
		FileFlags:   b[73],
		VertPos:     binary.BigEndian.Uint16(b[75:77]),
		HorzPos:     binary.BigEndian.Uint16(b[77:79]),
		WindowID:    binary.BigEndian.Uint16(b[79:81]),
		Protected:   b[81] == 1,

		SizeOfDataFork:     binary.BigEndian.Uint32(b[83:87]),
		SizeOfResourceFork: binary.BigEndian.Uint32(b[87:91]),
		Created:            macTime(binary.BigEndian.Uint32(b[91:95])),
		Modified:           macTime(binary.BigEndian.Uint32(b[95:99])),
		GetInfoLength:      binary.BigEndian.Uint16(b[99:101]),
		// Byte 73 holds the Finder flags high byte; byte 101 the low byte.
		FinderFlags:   (uint16(b[73]) << 8) | uint16(b[101]),
		UploadVersion: b[122],
		ReadVersion:   b[123],
	}
	if h.FileType != fileType {
		return nil, FormatError("invalid file type")
	}

	// Bytes 116 to 125 are reserved in MacBinary I and older writers left junk
	// there (testdata/928.mac reports a 3615-byte secondary header and an uploader
	// version of 44), so only read them when the header is MacBinary II.
	if h.UploadVersion < macBinaryII {
		return h, nil
	}
	h.UnpackedLength = binary.BigEndian.Uint32(b[116:120])
	h.SecondHeaderLength = binary.BigEndian.Uint16(b[120:122])
	h.CRC = binary.BigEndian.Uint16(b[124:126])
	if computed := crcCCITT(b[:124]); computed != h.CRC {
		return nil, FormatError("CRC mismatch")
	}
	if h.SecondHeaderLength != 0 {
		return nil, UnsupportedError("MacBinary secondary header")
	}
	return h, nil
}

// appendHeader appends a 128-byte MacBinary II header for a data fork of
// dataForkLen bytes. FileType, the fork sizes, the version bytes, the secondary
// header length, the unpacked length and the CRC are all computed here.
func appendHeader(dst []byte, h *Header, dataForkLen uint32) ([]byte, error) {
	name := h.FileName
	if name == "" {
		name = "untitled"
	}
	if len(name) > maxFileNameLen {
		cut := maxFileNameLen
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	creator := h.FileCreator
	if creator == "" {
		creator = defaultCreator
	}
	if len(creator) != 4 {
		return nil, FormatError("file creator must be 4 bytes")
	}

	var b [macBinaryHeaderLen]byte
	b[0] = 0
	//nolint:gosec // G115: name was clamped to maxFileNameLen (63) above.
	b[1] = byte(len(name))
	for i := 2; i < 65; i++ {
		b[i] = ' '
	}
	copy(b[2:65], name)
	copy(b[65:69], fileType)
	copy(b[69:73], creator)
	b[73] = h.FileFlags
	binary.BigEndian.PutUint16(b[75:77], h.VertPos)
	binary.BigEndian.PutUint16(b[77:79], h.HorzPos)
	binary.BigEndian.PutUint16(b[79:81], h.WindowID)
	if h.Protected {
		b[81] = 1
	}
	binary.BigEndian.PutUint32(b[83:87], dataForkLen)
	// Bytes 87-91 (resource fork size) stay zero: MacPaint documents never use a
	// resource fork.
	binary.BigEndian.PutUint32(b[91:95], macStamp(h.Created))
	binary.BigEndian.PutUint32(b[95:99], macStamp(h.Modified))
	binary.BigEndian.PutUint16(b[99:101], h.GetInfoLength)
	if finderFileFlags := byte(h.FinderFlags >> 8); finderFileFlags != 0 && finderFileFlags != h.FileFlags {
		return nil, FormatError("FinderFlags high byte disagrees with FileFlags")
	}
	b[101] = byte(h.FinderFlags & 0xff) // Low byte only; the high byte is FileFlags.
	b[122] = macBinaryII
	b[123] = macBinaryII
	binary.BigEndian.PutUint16(b[124:126], crcCCITT(b[:124]))
	return append(dst, b[:]...), nil
}
