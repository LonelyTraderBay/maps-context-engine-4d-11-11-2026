package powershellprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/sys/windows"
)

const workerOutputLimit = 1 << 20
const MaxFiles = 64

type Declaration struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Start int64  `json:"start_byte"`
	End   int64  `json:"end_byte"`
}

type FileResult struct {
	SchemaVersion int           `json:"schema_version"`
	Language      string        `json:"language"`
	ParserProfile string        `json:"parser_profile"`
	Status        string        `json:"status"`
	Reason        string        `json:"reason"`
	Path          string        `json:"path"`
	ContentHash   string        `json:"content_hash"`
	Declarations  []Declaration `json:"declarations"`
}

type sourceInput struct {
	Path string `json:"path"`
	Hash string `json:"sha256"`
	Body string `json:"body"`
}

// Analyze uses the installation-owned worker and captured source bytes. The
// child receives no live repository path, script file, profile or arguments
// from the project.
func Analyze(ctx context.Context, root *paths.Root, profile *model.PowerShellProfile, capture *snapshot.Snapshot, check func(context.Context) error) ([]FileResult, error) {
	if VerifyProfile(profile) != nil || root == nil || capture == nil || check == nil {
		return nil, ErrInstallation
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := check(ctx); err != nil {
		return nil, err
	}
	var input = []sourceInput{}
	for _, item := range capture.Manifest().Files {
		file, captured := capture.File(item.Path)
		if !captured {
			continue
		}
		if len(input) == MaxFiles || file.Len() > MaxSourceBytes || !SupportsPath(item.Path) {
			return nil, ErrWorker
		}
		input = append(input, sourceInput{Path: item.Path, Hash: file.ContentHash(), Body: base64.StdEncoding.EncodeToString(file.Bytes())})
	}
	home, err := root.CanonicalPath()
	if err != nil {
		return nil, errors.Join(ErrInstallation, err)
	}
	files, err := verifyMaterials(ctx, root)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > 2<<20 {
		return nil, ErrWorker
	}
	raw, err := runWorker(ctx, filepath.Join(home, "pwsh.exe"), home, wire, func(ctx context.Context) error {
		return errors.Join(root.Verify(), check(ctx))
	})
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var rows []FileResult
	if model.ValidateWireJSON(raw, workerOutputLimit) != nil || decoder.Decode(&rows) != nil || rows == nil || len(rows) != len(input) {
		return nil, ErrWorker
	}
	for i := range rows {
		result := &rows[i]
		file, _ := capture.File(input[i].Path)
		if result.SchemaVersion != 1 || result.Language != "powershell" || result.ParserProfile != "powershell-syntax-discovery-v1" ||
			result.Path != input[i].Path || result.ContentHash != input[i].Hash || result.Declarations == nil {
			return nil, ErrWorker
		}
		switch {
		case result.Status == "partial" && result.Reason == "SYNTAX_DECLARATIONS_ONLY":
			var previousEnd int64
			for _, item := range result.Declarations {
				if item.Kind != "filter" && item.Kind != "function" || !declarationName.MatchString(item.Name) ||
					item.Start < previousEnd || item.End <= item.Start || item.End > int64(file.Len()) ||
					file.CheckRange(int(item.Start), int(item.End)) != nil {
					return nil, ErrWorker
				}
				previousEnd = item.End
			}
		case result.Status == "unsupported" && result.Reason == "SYNTAX_OUTSIDE_PROFILE",
			result.Status == "parse_error" && result.Reason == "INVALID_SYNTAX":
			if len(result.Declarations) != 0 {
				return nil, ErrWorker
			}
		default:
			return nil, ErrWorker
		}
	}
	return rows, errors.Join(root.Verify(), check(ctx))
}

func verifyMaterials(ctx context.Context, root *paths.Root) (files []*os.File, resultErr error) {
	pin, err := loadPin()
	if err != nil || len(pin.Files) != 4 {
		return nil, ErrInstallation
	}
	defer func() {
		if resultErr != nil {
			for _, file := range files {
				_ = file.Close()
			}
			files = nil
		}
	}()
	for _, name := range []string{"pwsh.exe", "System.Management.Automation.dll", "LICENSE.txt", "ThirdPartyNotices.txt"} {
		if err := ctx.Err(); err != nil {
			return files, err
		}
		file, err := root.OpenRegular(name)
		if err != nil {
			return files, errors.Join(ErrInstallation, err)
		}
		files = append(files, file)
		digest := sha256.New()
		const capBytes = 32 << 20
		size, err := io.Copy(digest, io.LimitReader(file, capBytes+1))
		if err != nil || size < 1 || size > capBytes || fmt.Sprintf("%x", digest.Sum(nil)) != pin.Files[name] {
			return files, ErrInstallation
		}
	}
	return files, root.Verify()
}

func runWorker(ctx context.Context, executable, home string, input []byte, check func(context.Context) error) (_ []byte, resultErr error) {
	system, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return nil, err
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer outR.Close()
	defer outW.Close()
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	defer stderr.Close()
	job, err := runtime.NewWindowsProcessJob(1)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, job.Close()) }()
	var command []byte
	for _, unit := range utf16.Encode([]rune(program())) {
		command = binary.LittleEndian.AppendUint16(command, unit)
	}
	child, err := job.StartChild(ctx, runtime.WindowsJobChild{
		Executable: executable, Directory: home,
		Arguments:   []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(command)},
		Environment: []string{"SystemRoot=" + system, "WINDIR=" + system, "PSModulePath=" + filepath.Join(home, "Modules"), "POWERSHELL_TELEMETRY_OPTOUT=1"},
		Stdin:       inR, Stdout: outW, Stderr: stderr,
	}, func(process windows.Handle) error {
		buf := make([]uint16, 32768)
		length := uint32(len(buf))
		if err := windows.QueryFullProcessImageName(process, 0, &buf[0], &length); err != nil ||
			!strings.EqualFold(windows.UTF16ToString(buf[:length]), executable) {
			return errors.Join(ErrInstallation, err)
		}
		return check(ctx)
	})
	if err != nil {
		return nil, errors.Join(ErrWorker, err)
	}
	defer windows.CloseHandle(child)
	_ = inR.Close()
	_ = outW.Close()
	type result struct {
		raw []byte
		err error
	}
	output := make(chan result, 1)
	write := make(chan error, 1)
	go func() {
		_, err := inW.Write(input)
		write <- errors.Join(err, inW.Close())
	}()
	go func() {
		raw, err := io.ReadAll(io.LimitReader(outR, workerOutputLimit+1))
		output <- result{raw, err}
	}()
	var complete result
	select {
	case complete = <-output:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if complete.err != nil || len(complete.raw) > workerOutputLimit {
		return nil, ErrWorker
	}
	if err := job.WaitEmpty(ctx); err != nil {
		return nil, err
	}
	var exit uint32
	if err := windows.GetExitCodeProcess(child, &exit); err != nil || exit != 0 || <-write != nil {
		return nil, errors.Join(ErrWorker, err)
	}
	return complete.raw, check(ctx)
}
