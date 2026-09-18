package macpaint

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestMacTimeRoundTrip(t *testing.T) {
	for _, stamp := range []uint32{0, 1, 2, 60, 86400, 1 << 16, 2611440000, ^uint32(0)} {
		got := macStamp(macTime(stamp))
		if got != stamp {
			t.Errorf("macStamp(macTime(%d)) = %d, want %d", stamp, got, stamp)
		}
	}
}

func TestMacTimeZero(t *testing.T) {
	if got := macTime(0); !got.IsZero() {
		t.Errorf("macTime(0) = %v, want the zero time", got)
	}
	if got := macStamp(time.Time{}); got != 0 {
		t.Errorf("macStamp(zero) = %d, want 0", got)
	}
	for _, tt := range []struct {
		name string
		when time.Time
	}{
		{name: "before epoch", when: time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{name: "sub-second", when: macEpoch.Add(500 * time.Millisecond)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := macStamp(tt.when); got != 1 {
				t.Errorf("macStamp(%v) = %d, want 1", tt.when, got)
			}
		})
	}
	if got := macStamp(macEpoch.Add((time.Duration(^uint32(0)) + 1) * time.Second)); got != ^uint32(0) {
		t.Errorf("macStamp(after maximum) = %d, want %d", got, ^uint32(0))
	}
}

func TestAppendHeaderName(t *testing.T) {
	name := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789é"
	b, err := appendHeader(nil, &Header{FileName: name}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := b[1]; got != 62 {
		t.Errorf("filename length = %d, want 62", got)
	}
	if got := string(b[2:64]); got != name[:62] {
		t.Errorf("filename = %q, want %q", got, name[:62])
	}
	if got := b[64]; got != ' ' {
		t.Errorf("filename padding = %#x, want a space", got)
	}
}

func TestAppendHeaderFinderFlagsConflict(t *testing.T) {
	_, err := appendHeader(nil, &Header{FileFlags: 1, FinderFlags: 2 << 8}, 0)
	if err == nil {
		t.Fatal("appendHeader accepted conflicting FinderFlags high byte")
	}
}

func TestMacTimeEpoch(t *testing.T) {
	// A known stamp from testdata/1066.mac's creation date.
	want := time.Date(1984, time.September, 19, 0, 0, 0, 0, time.UTC)
	got := macTime(macStamp(want))
	if !got.Equal(want) {
		t.Errorf("round-trip of %v gave %v", want, got)
	}
}

func TestDecodeFileHeader(t *testing.T) {
	tests := []struct {
		file        string
		name        string
		creator     string
		fileFlags   byte
		created     string // YYYY-MM-DD in UTC, or "" for the zero time
		modified    string
		dataFork    uint32
		wantNoHdr   bool
		wantInited  bool
		wantInvisbl bool
	}{
		{
			file: "bigbinky.mac", name: "BigBinky", creator: "MPNT", fileFlags: 0x01,
			created: "1986-06-28", modified: "1986-08-29", dataFork: 11471, wantInited: true,
		},
		{
			file: "1066.mac", name: "HASTINGS 1066", creator: "MPNT", fileFlags: 0x01,
			created: "1984-09-19", modified: "1984-10-02", dataFork: 36352, wantInited: true,
		},
		{
			// Written by a non-Mac tool: the filename field holds a UTF-16LE Windows
			// path and the timestamps are zero. Decoded faithfully as-is.
			file: "header.mac", creator: "MPNT", dataFork: 2791,
		},
		{file: "noheader.mac", wantNoHdr: true},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile("../testdata/" + tt.file)
			if err != nil {
				t.Skipf("fixture not present: %s", err)
			}
			f, err := DecodeFile(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("DecodeFile: %s", err)
			}
			if f.Image == nil {
				t.Fatal("File.Image is nil")
			}
			if tt.wantNoHdr {
				if f.Header != nil {
					t.Fatalf("Header = %+v, want nil for a headerless file", f.Header)
				}
				return
			}
			h := f.Header
			if h == nil {
				t.Fatal("Header is nil")
			}
			if h.FileType != "PNTG" {
				t.Errorf("FileType = %q, want \"PNTG\"", h.FileType)
			}
			if tt.name != "" && h.FileName != tt.name {
				t.Errorf("FileName = %q, want %q", h.FileName, tt.name)
			}
			if h.FileCreator != tt.creator {
				t.Errorf("FileCreator = %q, want %q", h.FileCreator, tt.creator)
			}
			if h.FileFlags != tt.fileFlags {
				t.Errorf("FileFlags = %#02x, want %#02x", h.FileFlags, tt.fileFlags)
			}
			if h.SizeOfDataFork != tt.dataFork {
				t.Errorf("SizeOfDataFork = %d, want %d", h.SizeOfDataFork, tt.dataFork)
			}
			checkDate(t, "Created", h.Created, tt.created)
			checkDate(t, "Modified", h.Modified, tt.modified)
			if got := h.Flags(FlagInited); got != tt.wantInited {
				t.Errorf("Flags(FlagInited) = %v, want %v", got, tt.wantInited)
			}
			if got := h.Flags(FlagInvisible); got != tt.wantInvisbl {
				t.Errorf("Flags(FlagInvisible) = %v, want %v", got, tt.wantInvisbl)
			}
		})
	}
}

func checkDate(t *testing.T, field string, got time.Time, want string) {
	t.Helper()
	if want == "" {
		if !got.IsZero() {
			t.Errorf("%s = %v, want the zero time", field, got)
		}
		return
	}
	if g := got.UTC().Format("2006-01-02"); g != want {
		t.Errorf("%s = %s, want %s", field, g, want)
	}
}

func TestHeaderFlags(t *testing.T) {
	h := &Header{FileFlags: byte(FlagInited | FlagBundle)}
	for _, tt := range []struct {
		mask FinderFlag
		want bool
	}{
		{FlagInited, true},
		{FlagBundle, true},
		{FlagInited | FlagBundle, true},
		{FlagInvisible, false},
		{FlagInited | FlagInvisible, false},
	} {
		if got := h.Flags(tt.mask); got != tt.want {
			t.Errorf("Flags(%#02x) = %v, want %v", byte(tt.mask), got, tt.want)
		}
	}
}
