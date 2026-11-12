package runtime

import (
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

var (
	ErrConfigLimit       = errors.New("runtime config size limit exceeded")
	ErrConfigUnstable    = errors.New("runtime config changed while reading")
	ErrConfigPending     = errors.New("runtime config update pending or recovery required")
	ErrConfigUnsupported = errors.New("atomic runtime config operation unsupported")
)

// ReadRuntimeConfig reads a bounded config from an already authorized state
// root. It does not create a root, lock, config, or record, and does not grant
// access. The observed checks cannot certify an atomic filesystem snapshot.
func ReadRuntimeConfig(root *paths.Root, maxBytes int) (data []byte, present bool, resultErr error) {
	return ReadRuntimeConfigAt(root, runtimeConfigName, maxBytes)
}

// ReadRuntimeConfigAt reads one approved file name directly below root. The
// caller is responsible for choosing and authorizing that parent and name.
func ReadRuntimeConfigAt(root *paths.Root, name string, maxBytes int) (data []byte, present bool, resultErr error) {
	if root == nil || !validRuntimeConfigName(name) {
		return nil, false, ErrConfigUnsupported
	}
	pending, err := runtimeConfigPending(root)
	if err != nil {
		return nil, false, err
	}
	if pending {
		return nil, false, ErrConfigPending
	}
	data, present, err = readRuntimeConfigUnlocked(root, name, maxBytes)
	if err != nil {
		return nil, false, err
	}
	pending, err = runtimeConfigPending(root)
	if err != nil {
		return nil, false, err
	}
	if pending {
		return nil, false, ErrConfigPending
	}
	return data, present, nil
}

func readRuntimeConfigUnlocked(root *paths.Root, name string, maxBytes int) (data []byte, present bool, resultErr error) {
	if maxBytes <= 0 || maxBytes > 1<<20 {
		return nil, false, ErrConfigLimit
	}
	file, err := root.OpenRegular(name)
	if errors.Is(err, os.ErrNotExist) {
		if verifyErr := root.Verify(); verifyErr != nil {
			return nil, false, verifyErr
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			data, present = nil, false
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	if err = verifyRuntimeConfigFile(file); err != nil {
		return nil, false, err
	}
	data, err = readObservedFile(root, name, file, file, maxBytes, ErrConfigLimit, ErrConfigUnstable)
	if errors.Is(err, paths.ErrHardLinked) {
		return nil, false, ErrConfigUnstable
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}
