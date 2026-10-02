package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestStripRemovesEXIF(t *testing.T) {
	base := encodeJPEG(t)
	payload := []byte("Exif\x00\x00GPS-SECRET-PLATE")
	marked := insertAPP1(base, payload)
	if !bytes.Contains(marked, []byte("GPS-SECRET-PLATE")) {
		t.Fatal("fixture should contain the marker")
	}
	out, err := Strip(marked, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("Exif")) || bytes.Contains(out, []byte("GPS-SECRET-PLATE")) {
		t.Fatal("re-encoded jpeg still contains EXIF payload")
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || format != "jpeg" {
		t.Fatalf("output is not jpeg: %v %s", err, format)
	}
}

func TestStripAcceptsPNGAndRejectsGarbage(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := Strip(buf.Bytes(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 4 || out[0] != 0xFF || out[1] != 0xD8 {
		t.Fatal("expected jpeg output")
	}
	if _, err := Strip([]byte("not an image"), 1<<20); err != ErrBadImage {
		t.Fatalf("got %v", err)
	}
	if _, err := Strip(buf.Bytes(), 8); err != ErrTooLarge {
		t.Fatalf("got %v", err)
	}
}

func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{G: 180, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func insertAPP1(jpegBytes, payload []byte) []byte {
	segmentLen := len(payload) + 2
	segment := []byte{0xFF, 0xE1, byte(segmentLen >> 8), byte(segmentLen)}
	segment = append(segment, payload...)
	out := append([]byte{}, jpegBytes[:2]...)
	out = append(out, segment...)
	out = append(out, jpegBytes[2:]...)
	return out
}
