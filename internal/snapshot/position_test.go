package snapshot

import (
	"errors"
	"testing"
)

func TestPositionRoundTripAcrossEncodingsAndLineEndings(t *testing.T) {
	file, err := NewFile("unicode.go", []byte("Aລ😀\r\nB\rC\n"), 128)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoding := range []PositionEncoding{UTF8, UTF16, UTF32} {
		for _, offset := range []int{0, 1, 4, 8, 10, 11, 12, 13, 14} {
			position, err := file.PositionAt(offset, encoding)
			if err != nil {
				t.Fatalf("%s byte %d: %v", encoding, offset, err)
			}
			back, err := file.OffsetAt(position, encoding)
			if err != nil || back != offset {
				t.Fatalf("%s byte %d -> %+v -> %d: %v", encoding, offset, position, back, err)
			}
		}
	}
	for _, item := range []struct {
		encoding PositionEncoding
		position Position
	}{
		{UTF8, Position{0, 8}},
		{UTF16, Position{0, 4}},
		{UTF32, Position{0, 3}},
	} {
		position, err := file.PositionAt(8, item.encoding)
		if err != nil || position != item.position {
			t.Fatalf("%s emoji end: %+v %v", item.encoding, position, err)
		}
	}
	first, last, err := file.Lines(10, 13)
	if err != nil || first != 2 || last != 3 {
		t.Fatalf("CR-only line display: %d %d %v", first, last, err)
	}
}

func TestPositionRejectsSplitAndOutOfRange(t *testing.T) {
	file, err := NewFile("unicode.go", []byte("Aລ😀\r\nB\rC\n"), 128)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{-1, 2, 5, 9, 15} {
		if _, err := file.PositionAt(offset, UTF16); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("invalid byte offset %d accepted: %v", offset, err)
		}
	}
	for _, item := range []struct {
		position Position
		encoding PositionEncoding
	}{
		{Position{0, 3}, UTF16}, // inside the emoji surrogate pair
		{Position{0, 2}, UTF8},  // inside the Lao UTF-8 encoding
		{Position{0, 5}, UTF16}, // past the end of the first line
		{Position{4, 0}, UTF16},
		{Position{-1, 0}, UTF16},
		{Position{0, -1}, UTF16},
		{Position{0, 0}, PositionEncoding("unknown")},
	} {
		if _, err := file.OffsetAt(item.position, item.encoding); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("invalid position %+v in %s accepted: %v", item.position, item.encoding, err)
		}
	}
}
