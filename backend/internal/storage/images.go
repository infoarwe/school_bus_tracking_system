package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/webp"
)

// ImageError is a problem with an uploaded image that the user can fix.
type ImageError struct{ Reason string }

func (e *ImageError) Error() string { return e.Reason }

// IsImageError returns the user-facing problem, if err is one.
func IsImageError(err error) (*ImageError, bool) {
	var ie *ImageError
	ok := errors.As(err, &ie)
	return ie, ok
}

// Image is an uploaded image after validation and re-encoding.
type Image struct {
	Data          []byte
	ContentType   string // image/png or image/jpeg
	Ext           string // png or jpg
	Width, Height int
}

// Image size limits: big enough for any logo, small enough that decoding a
// hostile file cannot use much memory (4096² × 4 bytes = 64 MB worst case).
const (
	MinImageSide = 16
	MaxImageSide = 4096
)

// ProcessImage checks that data is a real PNG, JPEG or WebP image (by its
// content, never the file name or the client's Content-Type), within the size
// limits, and re-encodes it. Re-encoding drops metadata (EXIF, GPS position)
// and anything appended to or hidden in the file. WebP is stored as PNG.
func ProcessImage(data []byte) (*Image, error) {
	format := sniffImage(data)
	if format == "" {
		return nil, &ImageError{"must be a PNG, JPEG or WebP image"}
	}
	var cfg image.Config
	var err error
	switch format {
	case "png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "webp":
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return nil, &ImageError{"the image file is damaged or not supported"}
	}
	if cfg.Width < MinImageSide || cfg.Height < MinImageSide {
		return nil, &ImageError{fmt.Sprintf("must be at least %d×%d pixels", MinImageSide, MinImageSide)}
	}
	if cfg.Width > MaxImageSide || cfg.Height > MaxImageSide {
		return nil, &ImageError{fmt.Sprintf("must be at most %d×%d pixels", MaxImageSide, MaxImageSide)}
	}

	var img image.Image
	switch format {
	case "png":
		img, err = png.Decode(bytes.NewReader(data))
	case "jpeg":
		img, err = jpeg.Decode(bytes.NewReader(data))
	case "webp":
		img, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, &ImageError{"the image file is damaged or not supported"}
	}

	var out bytes.Buffer
	res := &Image{Width: cfg.Width, Height: cfg.Height}
	if format == "jpeg" {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
		res.ContentType, res.Ext = "image/jpeg", "jpg"
	} else {
		// PNG keeps transparency, which logos usually need.
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
		res.ContentType, res.Ext = "image/png", "png"
	}
	if err != nil {
		return nil, fmt.Errorf("re-encode image: %w", err)
	}
	res.Data = out.Bytes()
	return res, nil
}

// sniffImage identifies PNG, JPEG and WebP by their magic bytes.
func sniffImage(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

// ContentTypeForKey is the Content-Type to serve a stored image with.
func ContentTypeForKey(key string) string {
	switch {
	case len(key) > 4 && key[len(key)-4:] == ".png":
		return "image/png"
	case len(key) > 4 && key[len(key)-4:] == ".jpg":
		return "image/jpeg"
	}
	return "application/octet-stream"
}

// RandomName is 128 random bits as 32 hex characters: unguessable file names.
func RandomName() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+)
	return hex.EncodeToString(b[:])
}

func randomSuffix() string { return RandomName()[:12] }
