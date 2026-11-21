package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrProtocol = errors.New("invalid provider frame or stream")

const (
	maxHeader = 64 << 10
	maxFrame  = 16 << 20
	maxStream = 64 << 20
)

func readFrame(input *bufio.Reader, limit int) ([]byte, error) {
	if limit < 1 || limit > maxFrame {
		return nil, ErrProtocol
	}
	header := make([]byte, 0, 64)
	for {
		part, err := input.ReadSlice('\n')
		if len(part) > maxHeader-len(header) {
			return nil, ErrProtocol
		}
		header = append(header, part...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, ErrProtocol
		}
	}
	prefix := []byte("Content-Length: ")
	if !bytes.HasPrefix(header, prefix) || !bytes.HasSuffix(header, []byte("\r\n")) {
		return nil, ErrProtocol
	}
	digits := header[len(prefix) : len(header)-2]
	if len(digits) == 0 || len(digits) > 8 {
		return nil, ErrProtocol
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return nil, ErrProtocol
		}
	}
	size, err := strconv.Atoi(string(digits))
	if err != nil || size < 1 || size > limit {
		return nil, ErrProtocol
	}
	var end [2]byte
	if _, err := io.ReadFull(input, end[:]); err != nil || end != [2]byte{'\r', '\n'} {
		return nil, ErrProtocol
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(input, data); err != nil {
		return nil, ErrProtocol
	}
	return data, nil
}

func encodeFrame(message model.ProviderMessage, limit int) ([]byte, error) {
	data, err := json.Marshal(message)
	if err != nil || len(data) > limit || limit > maxFrame {
		return nil, ErrProtocol
	}
	return append(fmt.Appendf(nil, "Content-Length: %d\r\n\r\n", len(data)), data...), nil
}
