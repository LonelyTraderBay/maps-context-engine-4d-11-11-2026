//go:build linux

package inventory

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func sealedMemoryFile(name string, data []byte) (*os.File, error) {
	fd, err := unix.MemfdCreate(name, unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, errors.Join(ErrIgnoreUnsupported, err)
	}
	file := os.NewFile(uintptr(fd), name)
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if _, err = unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, errors.Join(ErrIgnoreUnsupported, err, file.Close())
	}
	return file, nil
}
