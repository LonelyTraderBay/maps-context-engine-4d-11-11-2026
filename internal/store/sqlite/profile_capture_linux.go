//go:build linux

package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

// A private pipe carries only fixed capture requests. This worker never opens
// native SQLite, accepts SQL, or runs target code. Reuse avoids rehashing and
// starting the installed executable for every short read transaction; each
// request still observes all DB/WAL bytes and current profile authority.
type profileCaptureRequest struct {
	Kind       string `json:"kind"`
	WorktreeID string `json:"worktree_id"`
}

func (r profileCaptureRequest) valid() bool {
	if r.Kind == "catalog" {
		return r.WorktreeID == ""
	}
	_, err := profileUUIDBytes(r.WorktreeID)
	return err == nil && (r.Kind == "index" || r.Kind == "records")
}

func serveProfileCaptures(ctx context.Context, root string, input io.Reader, output io.Writer) (resultErr error) {
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	p, err := openProtectedProfile(startup, root, "READY")
	if err != nil {
		cancel()
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, p.Close()) }()
	_, digest, err := trustedProfileExecutable(startup)
	cancel()
	if err != nil || digest != p.manifest.BootstrapExecutableSHA256 {
		return ErrProfileDenied
	}
	reader := bufio.NewReaderSize(input, 256)
	for {
		line, err := reader.ReadSlice('\n')
		if err == io.EOF && len(line) == 0 {
			return nil
		}
		if err != nil || len(line) > 256 {
			return ErrProfileInput
		}
		var request profileCaptureRequest
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || !request.valid() {
			return ErrProfileInput
		}
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		capture, err := captureWithProfile(bounded, p, request.WorktreeID, request.Kind)
		cancel()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(capture)
		if err != nil || len(raw) > 8191 {
			return ErrStateUnsafe
		}
		raw = append(raw, '\n')
		if n, err := output.Write(raw); err != nil || n != len(raw) {
			return errors.Join(io.ErrShortWrite, err)
		}
	}
}

type profileCaptureClient struct {
	cmd      *exec.Cmd
	input    io.WriteCloser
	output   *bufio.Reader
	done     chan error
	gate     chan struct{}
	broken   bool // owned by gate; an interrupted stream is never restarted
	closed   bool
	closeErr error
}

func startProfileCaptureClient(ctx context.Context, p *protectedProfile) (*profileCaptureClient, error) {
	executable, digest, err := trustedProfileExecutable(ctx)
	if err != nil || digest != p.manifest.BootstrapExecutableSHA256 {
		return nil, ErrProfileDenied
	}
	cmd := exec.Command(executable, "__capture-server-v1", p.rootPath)
	cmd.Env = []string{"HOME=" + filepath.Join(p.rootPath, "home"), "PATH=/usr/bin:/bin", "GOMAXPROCS=4", "MODERNC_SQLITE_OFD_LOCK=0"}
	// Worker failures are returned as a closed private channel, never arbitrary
	// stderr/log content. No output buffer can grow across requests.
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	c := &profileCaptureClient{cmd: cmd, input: input, output: bufio.NewReaderSize(output, 8192), done: make(chan error, 1), gate: make(chan struct{}, 1)}
	c.gate <- struct{}{}
	go func() { c.done <- cmd.Wait() }()
	return c, nil
}

func (c *profileCaptureClient) capture(ctx context.Context, request profileCaptureRequest) (nativeWALCapture, error) {
	if ctx == nil || !request.valid() {
		return nativeWALCapture{}, ErrProfileInput
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-c.gate:
	case <-bounded.Done():
		return nativeWALCapture{}, bounded.Err()
	}
	defer func() { c.gate <- struct{}{} }()
	if c.broken {
		return nativeWALCapture{}, errNativeOutcomeUnknown
	}
	if c.closed {
		return nativeWALCapture{}, ErrOwnerClosed
	}
	type reply struct {
		capture nativeWALCapture
		err     error
	}
	finished := make(chan reply, 1)
	go func() {
		raw, err := json.Marshal(request)
		if err == nil {
			raw = append(raw, '\n')
			var n int
			n, err = c.input.Write(raw)
			if err == nil && n != len(raw) {
				err = io.ErrShortWrite
			}
		}
		var out nativeWALCapture
		if err == nil {
			var line []byte
			line, err = c.output.ReadSlice('\n')
			if err == nil && len(line) <= 8192 {
				d := json.NewDecoder(bytes.NewReader(line))
				d.DisallowUnknownFields()
				if d.Decode(&out) != nil || d.Decode(&struct{}{}) != io.EOF || !validAuthorityImage(out.Image) {
					err = ErrStateUnsafe
				}
			}
		}
		finished <- reply{out, err}
	}()
	select {
	case r := <-finished:
		if r.err == nil {
			return r.capture, nil
		}
		c.broken = true
		_ = c.cmd.Process.Kill()
		_ = c.input.Close()
		return nativeWALCapture{}, errors.Join(errNativeOutcomeUnknown, r.err)
	case <-bounded.Done():
		c.broken = true
		_ = c.cmd.Process.Kill()
		_ = c.input.Close()
		// Kill closes the pipe; join the bounded exchange before another caller
		// can touch its reader, and retain no partially read result.
		<-finished
		return nativeWALCapture{}, errors.Join(errNativeOutcomeUnknown, bounded.Err())
	}
}

func (c *profileCaptureClient) close() error {
	<-c.gate
	defer func() { c.gate <- struct{}{} }()
	if c.closed {
		return c.closeErr
	}
	c.closed = true
	_ = c.input.Close()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-c.done:
		if !c.broken {
			c.closeErr = err
		}
		return c.closeErr
	case <-timer.C:
		_ = c.cmd.Process.Kill()
		<-c.done
		c.closeErr = errNativeOutcomeUnknown
		return c.closeErr
	}
}
