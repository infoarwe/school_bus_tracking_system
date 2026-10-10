package storage

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidKey(t *testing.T) {
	for key, want := range map[string]bool{
		"logos/0123abcd.png":              true,
		"a.png":                           true,
		"logos/x_y-z/1.jpg":               true,
		"../etc/passwd.png":               false,
		"logos/../../x.png":               false,
		"/abs/x.png":                      false,
		`logos\x.png`:                     false,
		"logos//x.png":                    false,
		"Logos/X.png":                     false,
		"logos/x":                         false,
		"logos/x.png/":                    false,
		"":                                false,
		strings.Repeat("a", 300) + ".png": false,
	} {
		if got := ValidKey(key); got != want {
			t.Errorf("ValidKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestLocalPutOpenDelete(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Put(ctx, "logos/abc.png", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "logos/abc.png", []byte("two")); err != nil { // overwrite
		t.Fatal(err)
	}
	f, err := s.Open(ctx, "logos/abc.png")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	_ = f.Close()
	if string(got) != "two" {
		t.Errorf("read %q", got)
	}
	// No temporary files are left behind.
	entries, _ := os.ReadDir(filepath.Join(dir, "logos"))
	if len(entries) != 1 {
		t.Errorf("files in dir: %v", entries)
	}

	if err := s.Delete(ctx, "logos/abc.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, "logos/abc.png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("open after delete: %v", err)
	}
	if err := s.Delete(ctx, "logos/abc.png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}

	// Unsafe keys are refused and never touch the disk outside the root.
	if err := s.Put(ctx, "../escape.png", []byte("x")); err == nil {
		t.Error("put outside root must fail")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.png")); err == nil {
		t.Error("file written outside the root")
	}
	if _, err := s.Open(ctx, "../escape.png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("open outside root: %v", err)
	}
}

func encode(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, x%h, color.NRGBA{200, 30, 30, 255})
	}
	var b bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// webpLossless builds a valid single-colour w×h lossless WebP (VP8L): no
// transforms, and one-symbol prefix codes so every pixel takes zero bits.
func webpLossless(w, h int) []byte {
	var bits []byte
	var acc uint64
	var n uint
	put := func(v uint64, width uint) { // VP8L packs bits LSB first
		acc |= v << n
		n += width
		for n >= 8 {
			bits = append(bits, byte(acc))
			acc >>= 8
			n -= 8
		}
	}
	bits = append(bits, 0x2f)                        // VP8L signature
	put(uint64(w-1), 14)                             // width - 1
	put(uint64(h-1), 14)                             // height - 1
	put(1, 1)                                        // alpha used
	put(0, 3)                                        // version
	put(0, 1)                                        // no transforms
	put(0, 1)                                        // no colour cache
	put(0, 1)                                        // no meta prefix codes
	for _, sym := range []uint64{200, 30, 30, 255} { // green, red, blue, alpha
		put(1, 1) // simple code
		put(0, 1) // one symbol
		put(1, 1) // 8-bit symbol
		put(sym, 8)
	}
	put(1, 1) // distance code: simple, one 1-bit symbol 0
	put(0, 1)
	put(0, 1)
	put(0, 1)
	if n > 0 {
		bits = append(bits, byte(acc))
	}
	if len(bits)%2 == 1 {
		bits = append(bits, 0)
	}
	le := func(v int) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
	out := []byte("RIFF")
	out = append(out, le(4+8+len(bits))...)
	out = append(out, "WEBPVP8L"...)
	out = append(out, le(len(bits))...)
	return append(out, bits...)
}

func TestProcessImage(t *testing.T) {
	pngData := encode(t, "png", 64, 32)
	img, err := ProcessImage(pngData)
	if err != nil {
		t.Fatal(err)
	}
	if img.ContentType != "image/png" || img.Ext != "png" || img.Width != 64 || img.Height != 32 {
		t.Errorf("png: %+v", img)
	}

	// Trailing data (e.g. a script appended to the file) is dropped by re-encoding.
	withPayload := append(append([]byte{}, encode(t, "jpeg", 40, 40)...), []byte("<script>alert(1)</script>")...)
	img, err = ProcessImage(withPayload)
	if err != nil {
		t.Fatal(err)
	}
	if img.ContentType != "image/jpeg" || bytes.Contains(img.Data, []byte("<script>")) {
		t.Errorf("jpeg not cleaned: %s", img.ContentType)
	}

	img, err = ProcessImage(webpLossless(48, 24))
	if err != nil {
		t.Fatal(err)
	}
	if img.ContentType != "image/png" {
		t.Errorf("webp should be stored as png, got %s", img.ContentType)
	}

	for name, data := range map[string][]byte{
		"svg":       []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`),
		"gif":       []byte("GIF89a......"),
		"empty":     nil,
		"truncated": pngData[:40],
		"too small": encode(t, "png", 8, 8),
		"too big":   encode(t, "png", MaxImageSide+1, 16),
	} {
		_, err := ProcessImage(data)
		if _, ok := IsImageError(err); !ok {
			t.Errorf("%s: want ImageError, got %v", name, err)
		}
	}
}
