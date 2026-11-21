//go:build linux || windows

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
)

var ErrLSPProcess = errors.New("LSP process failed or could not be contained")

type outboundFrame struct {
	data []byte
	done chan error
}

type processPipes struct {
	stdin, stdout, stderr *os.File
	incoming              chan inboundFrame
	writes                chan outboundFrame
	stop, done            chan struct{}
	workers               sync.WaitGroup
	kill                  func()
}

func newProcessPipes(stdin, stdout, stderr *os.File, kill func()) *processPipes {
	p := &processPipes{stdin: stdin, stdout: stdout, stderr: stderr,
		incoming: make(chan inboundFrame, 1), writes: make(chan outboundFrame),
		stop: make(chan struct{}), done: make(chan struct{}), kill: kill}
	p.workers.Add(3)
	go p.readLoop()
	go p.writeLoop()
	go p.stderrLoop()
	return p
}

func (p *processPipes) readLoop() {
	defer p.workers.Done()
	defer close(p.incoming)
	reader := bufio.NewReaderSize(p.stdout, 8192)
	var total int64
	for {
		data, err := readLSPFrame(reader, int(resultHardBytes))
		total += int64(len(data))
		if total > 128*1024*1024 {
			err = ErrLSPFrame
		}
		select {
		case p.incoming <- inboundFrame{data: data, err: err}:
		case <-p.stop:
			return
		}
		if err != nil {
			return
		}
	}
}

func (p *processPipes) writeLoop() {
	defer p.workers.Done()
	for {
		select {
		case <-p.stop:
			return
		case frame := <-p.writes:
			data := frame.data
			var err error
			for len(data) > 0 && err == nil {
				var n int
				n, err = p.stdin.Write(data)
				if n == 0 && err == nil {
					err = io.ErrShortWrite
				}
				data = data[n:]
			}
			frame.done <- err
		}
	}
}

func (p *processPipes) stderrLoop() {
	defer p.workers.Done()
	buffer := make([]byte, 8192)
	var total int64
	for {
		n, err := p.stderr.Read(buffer)
		total += int64(n)
		if total > 1024*1024 {
			// Do not retain or print provider output, which can contain source.
			p.kill()
			return
		}
		if err != nil {
			return
		}
	}
}

func (p *processPipes) send(ctx context.Context, value any) error {
	if p == nil || ctx == nil {
		return ErrLSPProcess
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ErrLSPProtocol
	}
	data, err = encodeLSPFrame(data, int(resultHardBytes))
	if err != nil {
		return err
	}
	frame := outboundFrame{data: data, done: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.stop:
		return ErrLSPProcess
	case <-p.done:
		return ErrLSPProcess
	case p.writes <- frame:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.stop:
		return ErrLSPProcess
	case <-p.done:
		return ErrLSPProcess
	case err := <-frame.done:
		if err != nil {
			return ErrLSPProcess
		}
		return ctx.Err()
	}
}
