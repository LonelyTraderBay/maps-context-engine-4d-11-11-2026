package compose

import (
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestVariableUsageLineKeepsByteOffsets(t *testing.T) {
	const source = "const tên = 1;\r\n  return tên + 1;\r\n"
	start := int64(strings.LastIndex(source, "tên"))
	anchor := model.ByteRange{StartByte: start, EndByte: start + int64(len("tên"))}
	span := variableUsageLine([]byte(source), anchor, 24000)
	if got := source[span.StartByte:span.EndByte]; got != "  return tên + 1;\r" {
		t.Fatalf("source line = %q", got)
	}
	if got := variableUsageLine([]byte(source), anchor, 4096); got != anchor {
		t.Fatalf("tight budget expanded anchor: %+v", got)
	}
}
