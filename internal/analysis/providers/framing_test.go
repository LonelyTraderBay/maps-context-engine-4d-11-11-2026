package providers

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestFramingReadsExactlyOneBoundedMessage(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("Content-Length: 2\r\n\r\n{}Content-Length: 3\r\n\r\n[1]"))
	for _, want := range []string{"{}", "[1]"} {
		data, err := readFrame(r, 3)
		if err != nil || string(data) != want {
			t.Fatalf("read %q, %v", data, err)
		}
	}
	for _, input := range []string{
		"Content-Length: 2\n\n{}", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}",
		"Content-Length: 0\r\n\r\n", "Content-Length: 4\r\n\r\nabcd",
		"Content-Length: 2\r\n\r\n{", "Content-Length: +2\r\n\r\n{}",
		strings.Repeat("x", maxHeader+1),
	} {
		if _, err := readFrame(bufio.NewReader(strings.NewReader(input)), 3); !errors.Is(err, ErrProtocol) {
			t.Fatal("accepted invalid frame")
		}
	}
}
