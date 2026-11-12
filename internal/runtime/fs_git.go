package runtime

import "errors"

var (
	ErrOutputLimit           = errors.New("git inventory output limit exceeded")
	ErrGitMetadataUnverified = errors.New("git metadata containment is not verified")
)
