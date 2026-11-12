package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// checkCheckpoint reads Git objects only; it never creates or changes a ref.
func checkCheckpoint(root, oid, tag string) error {
	if !regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`).MatchString(oid) || !regexp.MustCompile(`^mce4d/checkpoints/B[0-9]{3,}$`).MatchString(tag) {
		return fmt.Errorf("invalid checkpoint object ID or tag")
	}
	read := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", root}, args...)...)
		for _, variable := range os.Environ() {
			if !strings.HasPrefix(strings.ToUpper(variable), "GIT_") {
				cmd.Env = append(cmd.Env, variable)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_NO_REPLACE_OBJECTS=1", "GIT_TERMINAL_PROMPT=0")
		out := &boundedBuffer{limit: 1 << 20}
		cmd.Stdout = out
		cmd.Stderr = io.Discard
		err := cmd.Run()
		if err != nil {
			return "", fmt.Errorf("read Git object: %w", err)
		}
		return out.String(), nil
	}
	ref := "refs/tags/" + tag
	kind, err := read("cat-file", "-t", ref)
	if err != nil || strings.TrimSpace(kind) != "tag" {
		return fmt.Errorf("checkpoint is missing or is not annotated")
	}
	resolved, err := read("rev-parse", "--verify", ref+"^{commit}")
	if err != nil || strings.TrimSpace(resolved) != oid {
		return fmt.Errorf("checkpoint tag does not resolve to recorded commit")
	}
	commit, err := read("cat-file", "-p", oid)
	if err != nil {
		return err
	}
	parts := strings.SplitN(commit, "\n\n", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "tree ") {
		return fmt.Errorf("invalid commit object")
	}
	tree := strings.TrimPrefix(strings.SplitN(parts[0], "\n", 2)[0], "tree ")
	trailers := map[string]string{}
	for _, line := range strings.Split(parts[1], "\n") {
		if !strings.HasPrefix(line, "MCE4D-") {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok || trailers[key] != "" {
			return fmt.Errorf("malformed or duplicate checkpoint trailer")
		}
		trailers[key] = strings.TrimSpace(value)
	}
	if trailers["MCE4D-Tree"] != tree || trailers["MCE4D-Checkpoint"] != tag || trailers["MCE4D-Batch"] != strings.TrimPrefix(tag, "mce4d/checkpoints/") || trailers["MCE4D-Verification"] == "" || trailers["MCE4D-Spec-Version"] == "" {
		return fmt.Errorf("checkpoint trailers do not match tree, tag or evidence")
	}
	count, err := strconv.Atoi(trailers["MCE4D-Task-Count"])
	listed := strings.Split(trailers["MCE4D-Tasks"], ",")
	seen := map[string]bool{}
	for _, id := range listed {
		if !regexp.MustCompile(`^T[0-9]{3}$`).MatchString(id) || seen[id] {
			return fmt.Errorf("checkpoint has invalid or duplicate task IDs")
		}
		seen[id] = true
	}
	if err != nil || count != len(listed) || count < 1 || count > 10 {
		return fmt.Errorf("checkpoint task count invalid")
	}
	switch trailers["MCE4D-Batch-Type"] {
	case "full":
		if count != 10 {
			return fmt.Errorf("full checkpoint requires ten tasks")
		}
	case "scope_final", "authorized_recovery":
		// Authority and final scope are reviewed separately, not inferred from a trailer.
	default:
		return fmt.Errorf("unknown checkpoint batch type")
	}
	annotation, err := read("cat-file", "-p", ref)
	if err != nil || !strings.Contains(annotation, tree) {
		return fmt.Errorf("checkpoint annotation lacks tree evidence")
	}
	for _, id := range listed {
		if !strings.Contains(annotation, id) {
			return fmt.Errorf("checkpoint annotation lacks task %s", id)
		}
	}
	return nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("Git object exceeds limit")
	}
	return b.Buffer.Write(p)
}
