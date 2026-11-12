//go:build linux

package inventory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"golang.org/x/sys/unix"
)

const ignoreSandboxHEAD = "ref: refs/heads/fixture\n"
const ignoreSandboxConfig = "[core]\n\trepositoryformatversion = 0\n\tbare = false\n"

// EvaluateIgnoreRules evaluates only the supplied captured rules. The caller
// must verify complete effective rule discovery, actor and policy authority,
// and a trusted Git executable. The sandbox is an authorized owner-only
// directory containing only the fixed git/work scaffolding checked below.
// Git sees a sealed in-memory rules file and an empty synthetic worktree, never
// target Git config, target hooks, or target source files.
func EvaluateIgnoreRules(ctx context.Context, sandbox *paths.Root, gitBinary string, sources []IgnoreSource, queries []IgnoreQuery, scanner *policy.Scanner, options IgnoreOptions, budget IgnoreBudget, authorize func(context.Context) error) (ignored []bool, resultErr error) {
	if ctx == nil || sandbox == nil || authorize == nil || !filepath.IsAbs(gitBinary) || budget.MaxQueries < 1 || budget.MaxQueries > 100000 || budget.MaxQueryBytes < 1 || budget.MaxQueryBytes > 64*1024*1024 || budget.MaxOutputBytes < 1 || budget.MaxOutputBytes > 64*1024*1024 || len(queries) > budget.MaxQueries {
		return nil, ErrIgnoreBudget
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrIgnoreBudget
	}
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	compiled, err := compileIgnoreSources(sources, scanner, budget)
	if err != nil {
		return nil, err
	}
	var input bytes.Buffer
	wantPaths := make([]string, 0, len(queries))
	for _, query := range queries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if model.ValidateRelativePath(query.Path, false) != nil || policy.CheckPath(query.Path) != nil {
			return nil, ErrIgnoreUnsupported
		}
		name := query.Path
		if query.IsDirectory {
			name += "/"
		}
		if len(name)+1 > budget.MaxQueryBytes-input.Len() {
			return nil, ErrIgnoreBudget
		}
		input.WriteString(name)
		input.WriteByte(0)
		wantPaths = append(wantPaths, name)
	}
	if options.IgnoreCase {
		if err := validateIgnoreCaseScopes(sources, queries); err != nil {
			return nil, err
		}
	}
	gitDir, workDir, err := openIgnoreSandbox(sandbox)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := errors.Join(gitDir.Close(), workDir.Close()); err != nil {
			ignored = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	rules, err := sealedMemoryFile("mce4d-ignore-rules", compiled)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rules.Close(); err != nil {
			ignored = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if len(queries) == 0 {
		if err := authorize(ctx); err != nil {
			return nil, err
		}
		return []bool{}, nil
	}
	ignoreCase := "core.ignorecase=false"
	if options.IgnoreCase {
		ignoreCase = "core.ignorecase=true"
	}
	cmd := exec.CommandContext(ctx, gitBinary, "-c", "core.excludesfile=/proc/self/fd/5", "-c", ignoreCase, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.attributesfile=/dev/null", "check-ignore", "--no-index", "--verbose", "--non-matching", "-z", "--stdin")
	cmd.Dir = "/"
	cmd.Env = []string{"GIT_DIR=/proc/self/fd/3", "GIT_WORK_TREE=/proc/self/fd/4", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME=/nonexistent", "PATH=/usr/bin:/bin", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	cmd.ExtraFiles = []*os.File{gitDir, workDir, rules}
	cmd.Stdin = bytes.NewReader(input.Bytes())
	output := &ignoreBoundedWriter{max: budget.MaxOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if output.exceeded {
		return nil, ErrIgnoreBudget
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 1 {
			return nil, ErrIgnoreUnsupported
		}
	}
	fields := bytes.Split(output.buffer.Bytes(), []byte{0})
	if len(fields) != len(queries)*4+1 || len(fields[len(fields)-1]) != 0 {
		return nil, ErrIgnoreUnsupported
	}
	result := make([]bool, len(queries))
	for i, expected := range wantPaths {
		pattern, path := fields[i*4+2], fields[i*4+3]
		if string(path) != expected {
			return nil, ErrIgnoreUnsupported
		}
		result[i] = len(pattern) != 0 && pattern[0] != '!'
	}
	checkGit, checkWork, err := openIgnoreSandbox(sandbox)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(checkGit.Close(), checkWork.Close()); err != nil {
		return nil, err
	}
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// On a case-sensitive filesystem, Git can see different .gitignore files in
// directories that only differ in case even when core.ignorecase is true.
// Flattening would conflate their scopes, so refuse that ambiguous situation.
func validateIgnoreCaseScopes(sources []IgnoreSource, queries []IgnoreQuery) error {
	bases := make(map[string]string)
	for _, source := range sources {
		if source.Kind != "directory" || source.Base == "" {
			continue
		}
		folded := asciiLower(source.Base)
		if existing, ok := bases[folded]; ok && existing != source.Base {
			return ErrIgnoreUnsupported
		}
		bases[folded] = source.Base
	}
	for _, query := range queries {
		for i := 0; i <= len(query.Path); i++ {
			if i < len(query.Path) && query.Path[i] != '/' {
				continue
			}
			prefix := query.Path[:i]
			if original, ok := bases[asciiLower(prefix)]; ok && original != prefix {
				return ErrIgnoreUnsupported
			}
		}
	}
	return nil
}

func asciiLower(value string) string {
	data := []byte(value)
	for i, b := range data {
		if b >= 'A' && b <= 'Z' {
			data[i] = b + ('a' - 'A')
		}
	}
	return string(data)
}

type ignoreBoundedWriter struct {
	buffer   bytes.Buffer
	max      int
	exceeded bool
}

func (w *ignoreBoundedWriter) Write(p []byte) (int, error) {
	if len(p) > w.max-w.buffer.Len() {
		w.exceeded = true
		return 0, ErrIgnoreBudget
	}
	return w.buffer.Write(p)
}

func openIgnoreSandbox(root *paths.Root) (gitFD, workFD *os.File, resultErr error) {
	if err := checkSandboxNames(root, []string{"git", "work"}); err != nil {
		return nil, nil, err
	}
	gitRoot, err := root.OpenDirectory("git")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := gitRoot.Close(); err != nil {
			if gitFD != nil {
				err = errors.Join(err, gitFD.Close())
				gitFD = nil
			}
			if workFD != nil {
				err = errors.Join(err, workFD.Close())
				workFD = nil
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	workRoot, err := root.OpenDirectory("work")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := workRoot.Close(); err != nil {
			if gitFD != nil {
				err = errors.Join(err, gitFD.Close())
				gitFD = nil
			}
			if workFD != nil {
				err = errors.Join(err, workFD.Close())
				workFD = nil
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := checkSandboxNames(workRoot, []string{}); err != nil {
		return nil, nil, err
	}
	if err := checkSandboxNames(gitRoot, []string{"HEAD", "config", "objects", "refs"}); err != nil {
		return nil, nil, err
	}
	if err := checkSandboxFile(gitRoot, "HEAD", ignoreSandboxHEAD); err != nil {
		return nil, nil, err
	}
	if err := checkSandboxFile(gitRoot, "config", ignoreSandboxConfig); err != nil {
		return nil, nil, err
	}
	objects, err := gitRoot.OpenDirectory("objects")
	if err != nil {
		return nil, nil, err
	}
	if err = checkSandboxNames(objects, []string{}); err != nil {
		return nil, nil, errors.Join(err, objects.Close())
	}
	if err = objects.Close(); err != nil {
		return nil, nil, err
	}
	refs, err := gitRoot.OpenDirectory("refs")
	if err != nil {
		return nil, nil, err
	}
	if err = checkSandboxNames(refs, []string{"heads"}); err != nil {
		return nil, nil, errors.Join(err, refs.Close())
	}
	heads, err := refs.OpenDirectory("heads")
	if err != nil {
		return nil, nil, errors.Join(err, refs.Close())
	}
	if err = checkSandboxNames(heads, []string{}); err != nil {
		return nil, nil, errors.Join(err, heads.Close(), refs.Close())
	}
	if err = errors.Join(heads.Close(), refs.Close()); err != nil {
		return nil, nil, err
	}
	gitFD, err = gitRoot.DirectoryHandle()
	if err != nil {
		return nil, nil, err
	}
	workFD, err = workRoot.DirectoryHandle()
	if err != nil {
		return nil, nil, errors.Join(err, gitFD.Close())
	}
	if err := root.Verify(); err != nil {
		return nil, nil, errors.Join(err, gitFD.Close(), workFD.Close())
	}
	return gitFD, workFD, nil
}

func checkSandboxNames(root *paths.Root, expected []string) (resultErr error) {
	if err := root.Verify(); err != nil {
		return err
	}
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	var st unix.Stat_t
	if err := unix.Fstat(int(anchor.Fd()), &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 {
		return ErrIgnoreUnsupported
	}
	fd, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), "ignore-sandbox")
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	entries, err := dir.ReadDir(len(expected) + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, item := range entries {
		names = append(names, item.Name())
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, expected) {
		return ErrIgnoreUnsupported
	}
	return root.Verify()
}

func checkSandboxFile(root *paths.Root, name, expected string) (resultErr error) {
	file, err := root.OpenRegular(name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || st.Nlink != 1 {
		return ErrIgnoreUnsupported
	}
	before, err := file.Stat()
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	if err != nil {
		return err
	}
	if string(data) != expected {
		return ErrIgnoreUnsupported
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if !sameCapturedObservation(before, after) {
		return ErrIgnoreUnsupported
	}
	current, err := root.OpenRegular(name)
	if err != nil {
		return err
	}
	currentInfo, statErr := current.Stat()
	closeErr := current.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		return err
	}
	if !sameCapturedObservation(after, currentInfo) {
		return ErrIgnoreUnsupported
	}
	return root.Verify()
}
