package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func TestReadJPEGEXIFOrientation(t *testing.T) {
	tests := []struct {
		name         string
		littleEndian bool
		orientation  uint16
		want         int
	}{
		{name: "little endian rotate clockwise", littleEndian: true, orientation: 6, want: 6},
		{name: "big endian rotate counterclockwise", orientation: 8, want: 8},
		{name: "normal", littleEndian: true, orientation: 1, want: 1},
		{name: "invalid orientation is ignored", littleEndian: true, orientation: 9, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jpegData := jpegWithEXIFOrientation(tt.littleEndian, tt.orientation)
			if got := readJPEGEXIFOrientation(bytes.NewReader(jpegData)); got != tt.want {
				t.Fatalf("orientation = %d, want %d", got, tt.want)
			}
		})
	}

	for name, data := range map[string][]byte{
		"not jpeg":       []byte("not-a-jpeg"),
		"truncated jpeg": {0xff, 0xd8, 0xff, 0xe1, 0x00},
		"no exif":        {0xff, 0xd8, 0xff, 0xd9},
	} {
		t.Run(name, func(t *testing.T) {
			if got := readJPEGEXIFOrientation(bytes.NewReader(data)); got != 1 {
				t.Fatalf("orientation = %d, want default 1", got)
			}
		})
	}
}

func TestApplyEXIFOrientation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(10, 20, 12, 23))
	labels := []byte("abcdef")
	index := 0
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: labels[index], A: 0xff})
			index++
		}
	}

	wants := map[int][]string{
		1: {"ab", "cd", "ef"},
		2: {"ba", "dc", "fe"},
		3: {"fe", "dc", "ba"},
		4: {"ef", "cd", "ab"},
		5: {"ace", "bdf"},
		6: {"eca", "fdb"},
		7: {"fdb", "eca"},
		8: {"bdf", "ace"},
	}
	for orientation, want := range wants {
		t.Run(string(rune('0'+orientation)), func(t *testing.T) {
			got := imageRows(applyEXIFOrientation(source, orientation))
			if len(got) != len(want) {
				t.Fatalf("rows = %v, want %v", got, want)
			}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("rows = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestAvatarFileAndCacheNameValidation(t *testing.T) {
	for _, size := range []int64{0, 1, maxAvatarFileBytes} {
		if !validAvatarFileSize(size) {
			t.Errorf("valid avatar size %d rejected", size)
		}
	}
	for _, size := range []int64{-1, maxAvatarFileBytes + 1} {
		if validAvatarFileSize(size) {
			t.Errorf("invalid avatar size %d accepted", size)
		}
	}
	if maxAvatarFileBytes+maxAvatarMultipartOverheadBytes <= maxAvatarFileBytes {
		t.Fatal("multipart request allowance must include overhead beyond the file limit")
	}

	for _, name := range []string{"1-123.jpg", "42-999999999999.jpg"} {
		if !isVersionedAvatarFilename(name) {
			t.Errorf("versioned avatar name %q rejected", name)
		}
	}
	for _, name := range []string{"avatar.jpg", "1-.jpg", "-1.jpg", "1-2-3.jpg", "a-2.jpg", "1-2.png", "1-2.JPG"} {
		if isVersionedAvatarFilename(name) {
			t.Errorf("unversioned avatar name %q accepted", name)
		}
	}
	if got := avatarFilename("1-123.jpg"); got != "1-123.jpg" {
		t.Fatalf("safe avatar filename = %q", got)
	}
	for _, name := range []string{"../1-123.jpg", "avatars/1-123.jpg", `avatars\1-123.jpg`, ".", "..", ""} {
		if got := avatarFilename(name); got != "" {
			t.Errorf("unsafe avatar filename %q normalized to %q", name, got)
		}
	}
}

func jpegWithEXIFOrientation(littleEndian bool, orientation uint16) []byte {
	tiff := make([]byte, 8+2+12)
	var order binary.ByteOrder = binary.BigEndian
	copy(tiff[:2], "MM")
	if littleEndian {
		order = binary.LittleEndian
		copy(tiff[:2], "II")
	}
	order.PutUint16(tiff[2:4], 42)
	order.PutUint32(tiff[4:8], 8)
	order.PutUint16(tiff[8:10], 1)
	entry := tiff[10:22]
	order.PutUint16(entry[0:2], 0x0112)
	order.PutUint16(entry[2:4], 3)
	order.PutUint32(entry[4:8], 1)
	order.PutUint16(entry[8:10], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segmentLength := len(payload) + 2
	jpegData := []byte{0xff, 0xd8, 0xff, 0xe1, byte(segmentLength >> 8), byte(segmentLength)}
	jpegData = append(jpegData, payload...)
	return append(jpegData, 0xff, 0xd9)
}

func imageRows(img image.Image) []string {
	bounds := img.Bounds()
	rows := make([]string, 0, bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := make([]byte, 0, bounds.Dx())
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			row = append(row, pixel.R)
		}
		rows = append(rows, string(row))
	}
	return rows
}
