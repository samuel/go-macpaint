Go encoder and decoder package for MacPaint image files
=======================================================

[![CI](https://github.com/samuel/go-macpaint/actions/workflows/ci.yml/badge.svg)](https://github.com/samuel/go-macpaint/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/samuel/go-macpaint/macpaint.svg)](https://pkg.go.dev/github.com/samuel/go-macpaint/macpaint)

Documentation: https://pkg.go.dev/github.com/samuel/go-macpaint/macpaint

```
go get github.com/samuel/go-macpaint/macpaint
```

MacPaint images are always 576x720 pixels and one bit per pixel. Two file variants
are supported:

- Files wrapped in the 128-byte MacBinary I/II header layout. MacBinary III
  secondary headers are not supported.
- Headerless files, which begin with the four-byte MacPaint document version.

Usage
-----

Decode an image:

```go
img, err := macpaint.Decode(r)
```

Decode an image together with its MacBinary metadata. `File.Header` is nil for a
headerless file:

```go
f, err := macpaint.DecodeFile(r)
if err != nil {
    return err
}
if f.Header != nil {
    fmt.Println(f.Header.FileName, f.Header.FileCreator, f.Header.Modified)
}
```

`EncodeFile` takes the same struct back, so metadata round-trips without threading
two values around:

```go
f.Header.FileName = "RENAMED.PNT"
err = macpaint.EncodeFile(w, f)
```

A nil `Header` writes a headerless document, equivalent to `macpaint.Encode(w, img)`.
`FileType`, the fork sizes, the MacBinary version bytes and the CRC are always
computed, so caller-supplied values for those are ignored.

Notes
-----

- **`Decode` returns `*image.Paletted`**, using the two-color `macpaint.Palette`
  (index 0 white, index 1 black). This is faithful to the 1-bit format, and means
  `png.Encode` emits 1-bit PNGs — about 40% smaller than the 8-bit grayscale output
  of earlier versions. Code that type-asserted `*image.Gray` needs updating.
- Only the MacBinary-headered variant is registered with the `image` package, so
  `image.Decode` recognizes those automatically. A headerless file begins with three
  zero bytes and a small version number, which is too weak a signature to sniff on
  without misidentifying unrelated formats; decode those with `macpaint.Decode`,
  `macpaint.DecodeFile` or `macpaint.DecodeConfig` directly.
- `Width` and `Height` are exported for callers that need the fixed dimensions
  without decoding.

License
-------

MIT. See [LICENSE](LICENSE).
