package snapshot

import "unicode/utf8"

// Position uses zero-based line and character offsets in the selected code
// unit encoding. Conversions are against the immutable captured bytes.
type Position struct {
	Line      int
	Character int
}

type PositionEncoding string

const (
	UTF8  PositionEncoding = "utf-8"
	UTF16 PositionEncoding = "utf-16"
	UTF32 PositionEncoding = "utf-32"
)

// PositionAt rejects non-boundaries, offsets between CR and LF, and unknown
// encodings. Both CR, LF and CRLF count as one line ending.
func (f File) PositionAt(offset int, encoding PositionEncoding) (Position, error) {
	if _, err := f.Range(offset, offset); err != nil || !validEncoding(encoding) {
		return Position{}, ErrInvalidRange
	}
	if offset > 0 && offset < len(f.source) && f.source[offset-1] == '\r' && f.source[offset] == '\n' {
		return Position{}, ErrInvalidRange
	}
	var result Position
	for i := 0; i < offset; {
		if f.source[i] == '\r' || f.source[i] == '\n' {
			if f.source[i] == '\r' && i+1 < len(f.source) && f.source[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			result.Line++
			result.Character = 0
			continue
		}
		r, size := utf8.DecodeRune(f.source[i:])
		result.Character += codeUnits(r, size, encoding)
		i += size
	}
	return result, nil
}

// OffsetAt strictly rejects out-of-range positions and offsets that split a
// UTF-8 code point or UTF-16 surrogate pair; it never silently clamps them.
func (f File) OffsetAt(position Position, encoding PositionEncoding) (int, error) {
	if f.path == "" || position.Line < 0 || position.Character < 0 || !validEncoding(encoding) {
		return 0, ErrInvalidRange
	}
	i := 0
	for line := 0; line < position.Line; line++ {
		for i < len(f.source) && f.source[i] != '\r' && f.source[i] != '\n' {
			i++
		}
		if i == len(f.source) {
			return 0, ErrInvalidRange
		}
		if f.source[i] == '\r' && i+1 < len(f.source) && f.source[i+1] == '\n' {
			i += 2
		} else {
			i++
		}
	}
	units := 0
	for {
		if units == position.Character {
			return i, nil
		}
		if i == len(f.source) || f.source[i] == '\r' || f.source[i] == '\n' {
			return 0, ErrInvalidRange
		}
		r, size := utf8.DecodeRune(f.source[i:])
		next := units + codeUnits(r, size, encoding)
		if position.Character < next {
			return 0, ErrInvalidRange
		}
		units = next
		i += size
	}
}

func validEncoding(encoding PositionEncoding) bool {
	return encoding == UTF8 || encoding == UTF16 || encoding == UTF32
}

func codeUnits(r rune, size int, encoding PositionEncoding) int {
	switch encoding {
	case UTF8:
		return size
	case UTF16:
		if r > 0xffff {
			return 2
		}
	}
	return 1
}
