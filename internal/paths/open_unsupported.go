//go:build !linux && !windows

package paths

import "os"

// Root is intentionally unavailable until a platform-specific secure adapter is verified.
type Root struct{}

func OpenRoot(string) (*Root, error)               { return nil, ErrUnsupported }
func (*Root) OpenRegular(string) (*os.File, error) { return nil, ErrUnsupported }
func (*Root) OpenDirectory(string) (*Root, error)  { return nil, ErrUnsupported }
func (*Root) CanonicalPath() (string, error)       { return "", ErrUnsupported }
func (*Root) Close() error                         { return ErrUnsupported }
func (*Root) DirectoryHandle() (*os.File, error)   { return nil, ErrUnsupported }

func (*Root) Verify() error            { return ErrUnsupported }
func VerifyRegularFile(*os.File) error { return ErrUnsupported }
