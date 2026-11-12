package snapshot

import (
	"errors"
	"testing"
)

func TestCapturedFileHashRangesLinesAndOwnership(t *testing.T) {
	raw := []byte("Aລ😀\r\nB\n")
	file, err := NewFile("src/example.go", raw, 128)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'Z'
	if string(file.Bytes()) != "Aລ😀\r\nB\n" {
		t.Fatal("capture changed when caller changed input")
	}
	copy := file.Bytes()
	copy[0] = 'Z'
	part, err := file.Range(1, 8)
	if err != nil || string(part) != "ລ😀" {
		t.Fatalf("multibyte range: %q %v", part, err)
	}
	part[0] = 'X'
	if string(file.Bytes()) != "Aລ😀\r\nB\n" {
		t.Fatal("returned bytes mutate capture")
	}
	for _, r := range [][2]int{{-1, 0}, {0, 13}, {2, 4}, {4, 5}, {8, 7}} {
		if _, err := file.Range(r[0], r[1]); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("invalid range accepted: %v %v", r, err)
		}
	}
	for _, item := range []struct{ start, end, first, last int }{{0, 10, 1, 1}, {10, 11, 2, 2}, {11, 12, 2, 2}, {12, 12, 3, 3}} {
		first, last, err := file.Lines(item.start, item.end)
		if err != nil || first != item.first || last != item.last {
			t.Fatalf("lines for %+v: %d %d %v", item, first, last, err)
		}
	}
	plain, err := NewFile("a.go", []byte("abc"), 3)
	if err != nil || plain.ContentHash() != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("raw SHA-256: %q %v", plain.ContentHash(), err)
	}
}

func TestCapturedFileRejectsInvalidSourceAndLimits(t *testing.T) {
	for _, path := range []string{"", "../outside", "a/./b", `C:\\escape`} {
		if _, err := NewFile(path, []byte("data"), 10); !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("invalid path %q accepted: %v", path, err)
		}
	}
	if _, err := NewFile("a.go", []byte{0xff}, 1); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("invalid UTF-8 accepted: %v", err)
	}
	if _, err := NewFile("a.go", []byte("abcd"), 3); !errors.Is(err, ErrSourceLimit) {
		t.Fatalf("limit ignored: %v", err)
	}
	if _, err := (File{}).Range(0, 0); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("zero file accepted: %v", err)
	}
}
