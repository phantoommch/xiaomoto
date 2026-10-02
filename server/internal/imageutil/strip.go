package imageutil

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
)

var (
	ErrTooLarge = errors.New("image too large")
	ErrBadImage = errors.New("bad image")
)

// Strip decodes a JPEG or PNG and re-encodes it as JPEG.
// Re-encoding drops EXIF, including GPS, because those segments are not part of the pixel buffer.
func Strip(data []byte, maxBytes int) ([]byte, error) {
	if len(data) == 0 || len(data) > maxBytes {
		return nil, ErrTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrBadImage
	}
	if format != "jpeg" && format != "png" {
		return nil, ErrBadImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 12_000_000 {
		return nil, ErrBadImage
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrBadImage
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
