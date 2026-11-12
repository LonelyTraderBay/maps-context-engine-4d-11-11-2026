package policy

import (
	"io"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// ReadSource rejects denied paths before opening and rejects sensitive or
// oversized content before returning bytes. Capture stability and persistence
// still require their own owner checks.
func (s *Scanner) ReadSource(root *paths.Root, path string) ([]byte, error) {
	if s == nil || s.maxBytes <= 0 {
		return nil, ErrContentLimit
	}
	if err := CheckPath(path); err != nil {
		return nil, err
	}
	f, err := root.OpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return s.readAndVerify(f, func() error {
		if err := paths.VerifyRegularFile(f); err != nil {
			return err
		}
		return root.Verify()
	})
}

// readAndVerify only releases validated bytes after the root identity is rechecked.
func (s *Scanner) readAndVerify(r io.Reader, verify func() error) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(s.maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if err = s.CheckContent(data); err != nil {
		return nil, err
	}
	if err = verify(); err != nil {
		return nil, err
	}
	return data, nil
}
