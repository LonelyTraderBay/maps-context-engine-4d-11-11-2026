package runtime

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// OpenLocalGitMetadata opens only a .git directory beneath an authorized
// worktree root. A .git file or symlink is not followed.
func OpenLocalGitMetadata(worktree *paths.Root) (*paths.Root, error) {
	return worktree.OpenDirectory(".git")
}

// LinkedGitMetadata checks the reciprocal Git worktree pointers using roots
// explicitly supplied by the caller. It does not establish registration,
// authorization, ownership, or trust in Git configuration. The caller must
// keep the borrowed worktree and common roots open and verify current policy.
type LinkedGitMetadata struct {
	worktree *paths.Root
	common   *paths.Root
	metadata *paths.Root
}

// BindLinkedGitMetadata opens only the supplied worktrees/<name> child of an
// explicitly supplied common directory. Pointer content is compared as data;
// it is never used to open an arbitrary path or run Git.
func BindLinkedGitMetadata(worktree, common *paths.Root, relative string) (*LinkedGitMetadata, error) {
	parts := strings.Split(relative, "/")
	if len(parts) != 2 || parts[0] != "worktrees" || parts[1] == "" {
		return nil, ErrGitMetadataUnverified
	}
	metadata, err := common.OpenDirectory(relative)
	if err != nil {
		return nil, errors.Join(ErrGitMetadataUnverified, err)
	}
	binding := &LinkedGitMetadata{worktree: worktree, common: common, metadata: metadata}
	if err := binding.Verify(); err != nil {
		return nil, errors.Join(err, metadata.Close())
	}
	return binding, nil
}

// Verify rechecks directory identities and reciprocal pointers. It cannot
// guarantee an atomic filesystem snapshot or current authorization.
func (b *LinkedGitMetadata) Verify() error {
	if b == nil || b.worktree == nil || b.common == nil || b.metadata == nil {
		return ErrGitMetadataUnverified
	}
	worktreePath, err := b.worktree.CanonicalPath()
	if err != nil {
		return errors.Join(ErrGitMetadataUnverified, err)
	}
	commonPath, err := b.common.CanonicalPath()
	if err != nil {
		return errors.Join(ErrGitMetadataUnverified, err)
	}
	metadataPath, err := b.metadata.CanonicalPath()
	if err != nil {
		return errors.Join(ErrGitMetadataUnverified, err)
	}
	gitfile, err := readGitPointer(b.worktree, ".git")
	if err != nil || !pointerMatches(gitfile, "gitdir: ", worktreePath, metadataPath) {
		return errors.Join(ErrGitMetadataUnverified, err)
	}
	gitdir, err := readGitPointer(b.metadata, "gitdir")
	if err != nil || !pointerMatches(gitdir, "", metadataPath, filepath.Join(worktreePath, ".git")) {
		return errors.Join(ErrGitMetadataUnverified, err)
	}
	commondir, err := readGitPointer(b.metadata, "commondir")
	if err != nil || !pointerMatches(commondir, "", metadataPath, commonPath) {
		return errors.Join(ErrGitMetadataUnverified, err)
	}
	return errors.Join(b.worktree.Verify(), b.common.Verify(), b.metadata.Verify())
}

// ReadIndex checks the binding before and after reading the bounded index.
// Authorization and index parsing remain the caller's responsibility.
func (b *LinkedGitMetadata) ReadIndex(maxBytes int) ([]byte, error) {
	if err := b.Verify(); err != nil {
		return nil, err
	}
	data, err := ReadGitIndex(b.metadata, maxBytes)
	if err != nil {
		return nil, err
	}
	if err = b.Verify(); err != nil {
		return nil, err
	}
	return data, nil
}

// Close releases only the metadata handle owned by this binding.
func (b *LinkedGitMetadata) Close() error {
	if b == nil || b.metadata == nil {
		return ErrGitMetadataUnverified
	}
	return b.metadata.Close()
}

func readGitPointer(root *paths.Root, path string) (data []byte, resultErr error) {
	file, err := root.OpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			data = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	return readObservedFile(root, path, file, file, 4096, ErrGitMetadataUnverified, ErrGitMetadataUnverified)
}

func pointerMatches(data []byte, prefix, base, expected string) bool {
	if !utf8.Valid(data) {
		return false
	}
	value := string(data)
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	value = strings.TrimPrefix(value, prefix)
	if strings.HasSuffix(value, "\n") {
		value = strings.TrimSuffix(value, "\n")
		value = strings.TrimSuffix(value, "\r")
	}
	if value == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	if filepath.IsAbs(value) {
		// Lexical cleanup across a symlink can differ from kernel path
		// resolution; do not attest such a pointer.
		if filepath.Clean(value) != value {
			return false
		}
	} else {
		seenName := false
		for _, part := range strings.Split(value, string(filepath.Separator)) {
			if part == "" || part == "." || (part == ".." && seenName) {
				return false
			}
			if part != ".." {
				seenName = true
			}
		}
		value = filepath.Join(base, value)
	}
	return filepath.Clean(value) == expected
}
