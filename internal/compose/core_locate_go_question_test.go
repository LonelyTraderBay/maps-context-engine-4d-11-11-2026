package compose

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestNumberedSourceLinesVerifiesPhysicalSourceRange(t *testing.T) {
	source := []byte("one\r\n\tconst tên = 1;\r\n\tuse(tên);\r\nlast")
	start := bytes.Index(source, []byte("tên"))
	end := bytes.Index(source, []byte("use(")) + len("use(tên);")
	span := model.ByteRange{StartByte: int64(start), EndByte: int64(end)}
	r, line := numberedSourceLines(source, span, 2)
	if line != 2 || string(source[r.StartByte:r.EndByte]) != "\tconst tên = 1;\r\n\tuse(tên);\r\n" {
		t.Fatalf("unexpected source reference at line %d: %+v", line, r)
	}
	long := []byte("x" + strings.Repeat("ê", 1000))
	span = model.ByteRange{EndByte: int64(len(long))}
	r, line = numberedSourceLines(long, span, 1)
	if line != 1 || r.EndByte > 1800 || !utf8.Valid(long[r.StartByte:r.EndByte]) {
		t.Fatalf("long excerpt exceeded its cap or split UTF-8: %+v", r)
	}
}
