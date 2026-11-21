package sqlite

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"golang.org/x/sys/windows"
)

const windowsProfileName = "profile.json"
const windowsCursorKeyName = "cursor.key"
const windowsGlobalIgnoreName = "git.ignore"

// WindowsProfilePrepareOptions are inputs to the offline controller. The SID
// declarations reserve roles pending worker authentication and store bootstrap.
type WindowsProfilePrepareOptions struct {
	Root        string
	EngineSID   string
	ClientSID   string
	LspSID      string
	TargetRoots []string
	GitIgnore   string
}

type windowsProfilePreparation struct {
	FormatVersion int      `json:"format_version"`
	Platform      string   `json:"platform"`
	ProfileID     string   `json:"profile_id"`
	Stage         string   `json:"stage"`
	ControllerSID string   `json:"controller_sid"`
	EngineSID     string   `json:"engine_sid"`
	ClientSID     string   `json:"client_sid"`
	LspSID        string   `json:"lsp_sid,omitempty"`
	TargetRoots   []string `json:"target_roots"`
}

// ProtectedProfileRootFromEnvironment selects the Windows state directory. A
// caller-controlled path never establishes the controller identity.
func ProtectedProfileRootFromEnvironment() (string, error) {
	root := os.Getenv("MCE4D_HOME")
	if root == "" {
		base := os.Getenv("LOCALAPPDATA")
		if !filepath.IsAbs(base) {
			return "", ErrProfileInput
		}
		root = filepath.Join(base, "mce4d")
	}
	if !validWindowsPreparationRoot(root) {
		return "", ErrProfileInput
	}
	return root, nil
}

func validWindowsPreparationRoot(name string) bool {
	base := filepath.Base(name)
	return filepath.IsAbs(name) && filepath.Clean(name) == name &&
		filepath.Dir(name) != name && len(base) <= 64 && filepath.IsLocal(base) &&
		!strings.ContainsAny(base, "<>\"|?*") && !strings.HasSuffix(base, ".") && !strings.HasSuffix(base, " ")
}

func windowsPreparationController() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	container, err := windowsTokenDWORD(token, windowsTokenIsAppContainer)
	if err != nil || container != 0 {
		return nil, errors.Join(ErrProfileDenied, err)
	}
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, errors.Join(ErrProfileDenied, err)
	}
	return user.User.Sid, nil
}

func windowsPreparationSID(name string) bool {
	if len(name) > 184 {
		return false
	}
	sid, err := windows.StringToSid(name)
	if err != nil || !sid.IsValid() || sid.String() != name {
		return false
	}
	// A role identifies an account or package. Built-in groups such as
	// ALL APPLICATION PACKAGES share the package authority prefix.
	return strings.HasPrefix(name, "S-1-5-21-") && sid.SubAuthorityCount() == 5 ||
		strings.HasPrefix(name, "S-1-15-2-") && sid.SubAuthorityCount() == 8
}

func windowsPathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && filepath.IsLocal(relative)
}

func (m windowsProfilePreparation) valid(root string, controller *windows.SID) bool {
	if m.FormatVersion != 1 || m.Platform != "windows" || m.Stage != "CREATING" ||
		!model.ValidUUID(m.ProfileID) || m.ControllerSID != controller.String() ||
		!windowsPreparationSID(m.EngineSID) || !windowsPreparationSID(m.ClientSID) ||
		m.EngineSID == m.ClientSID || m.EngineSID == m.ControllerSID || m.ClientSID == m.ControllerSID ||
		len(m.TargetRoots) < 1 || len(m.TargetRoots) > 64 {
		return false
	}
	if m.LspSID != "" && (!windowsPreparationSID(m.LspSID) || !strings.HasPrefix(m.LspSID, "S-1-15-2-") ||
		m.LspSID == m.EngineSID || m.LspSID == m.ClientSID || m.LspSID == m.ControllerSID) {
		return false
	}
	for i, target := range m.TargetRoots {
		if !filepath.IsAbs(target) || filepath.Clean(target) != target ||
			windowsPathContains(root, target) || windowsPathContains(target, root) ||
			(i > 0 && strings.ToUpper(m.TargetRoots[i-1]) >= strings.ToUpper(target)) {
			return false
		}
	}
	return true
}

// PrepareWindowsProtectedProfile creates only the sealed controller metadata
// for a fresh profile. No SQLite handles or state capabilities are available
// until provisioning authenticates the engine and initializes its catalog.
func PrepareWindowsProtectedProfile(ctx context.Context, opts WindowsProfilePrepareOptions) (out ProtectedProfileSummary, resultErr error) {
	if ctx == nil || !validWindowsPreparationRoot(opts.Root) || len(opts.TargetRoots) < 1 || len(opts.TargetRoots) > 64 {
		return out, ErrProfileInput
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	controller, err := windowsPreparationController()
	if err != nil {
		return out, err
	}
	parent, err := paths.OpenRoot(filepath.Dir(opts.Root))
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return out, err
	}
	id[6] = 0x40 | (id[6] & 15)
	id[8] = 0x80 | (id[8] & 63)
	m := windowsProfilePreparation{FormatVersion: 1, Platform: "windows", Stage: "CREATING",
		ProfileID:     fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]),
		ControllerSID: controller.String(), EngineSID: opts.EngineSID, ClientSID: opts.ClientSID, LspSID: opts.LspSID}
	for _, raw := range opts.TargetRoots {
		target, err := paths.OpenRoot(raw)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, target.Close()) }()
		name, err := target.CanonicalPath()
		if err != nil {
			return out, err
		}
		m.TargetRoots = append(m.TargetRoots, name)
	}
	sort.Slice(m.TargetRoots, func(i, j int) bool {
		return strings.ToUpper(m.TargetRoots[i]) < strings.ToUpper(m.TargetRoots[j])
	})
	if !m.valid(opts.Root, controller) {
		return out, ErrProfileInput
	}
	globalIgnore, err := readWindowsProfileIgnore(ctx, opts.GitIgnore)
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > maintenanceJournalMaxBytes {
		return out, ErrProfileInput
	}
	acl := windowsControlACL{owner: controller}
	if err := createWindowsProfileDirectory(ctx, parent, acl, filepath.Base(opts.Root)); err != nil {
		return out, err
	}
	root, err := parent.OpenDirectory(filepath.Base(opts.Root))
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	if err := createWindowsProfileDirectory(ctx, root, acl, "control"); err != nil {
		return out, err
	}
	files := windowsMaintenanceFiles{control: root, acl: acl, profile: m.ProfileID, scope: "catalog"}
	control, err := files.openWindowsPoolDirectory("control")
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, control.control.Close()) }()
	var cursorKey [32]byte
	if _, err := rand.Read(cursorKey[:]); err != nil {
		return out, err
	}
	defer clear(cursorKey[:])
	if err := control.publishStagedWorktreeFile(ctx, windowsCursorKeyName, cursorKey[:]); err != nil {
		return out, err
	}
	if len(globalIgnore) != 0 {
		if err := files.publishStagedWorktreeFile(ctx, windowsGlobalIgnoreName, globalIgnore); err != nil {
			return out, err
		}
	}
	dir, err := files.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	descriptor, err := acl.descriptor()
	if err != nil {
		return out, err
	}
	if err := files.publish(ctx, dir, descriptor, windowsProfileName, raw, false, nil); err != nil {
		return out, err
	}
	return ProtectedProfileSummary{ProfileID: m.ProfileID, Stage: m.Stage}, nil
}

func readWindowsProfileIgnore(ctx context.Context, name string) (_ []byte, resultErr error) {
	if name == "" {
		return nil, nil
	}
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, ErrProfileInput
	}
	root, err := paths.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	file, err := root.OpenRegular(filepath.Base(name))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	const limit = 128 << 10
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(raw) > limit {
		return nil, errors.Join(ErrProfileInput, err)
	}
	scanner, err := policy.NewScanner(limit)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(ctx.Err(), scanner.CheckContent(raw), paths.VerifyRegularFile(file), root.Verify()); err != nil {
		return nil, err
	}
	return raw, nil
}

func createWindowsProfileDirectory(ctx context.Context, parent *paths.Root, acl windowsControlACL, name string) (resultErr error) {
	return createWindowsProfileDirectoryWithACL(ctx, parent, acl, acl, name)
}

func createWindowsProfileDirectoryWithACL(ctx context.Context, parent *paths.Root, parentACL, acl windowsControlACL, name string) (resultErr error) {
	return createWindowsDirectory(ctx, parent, parentACL.check, acl, name)
}

func createWindowsDirectory(ctx context.Context, parent *paths.Root, checkParent func(*os.File) error, acl windowsControlACL, name string) (resultErr error) {
	dir, err := parent.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	if err := errors.Join(ctx.Err(), checkParent(dir)); err != nil {
		return err
	}
	descriptor, err := acl.descriptor()
	if err != nil {
		return err
	}
	relative, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return err
	}
	attr := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(dir.Fd()), ObjectName: relative,
		Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE, SecurityDescriptor: descriptor}
	attr.Length = uint32(unsafe.Sizeof(attr))
	var h windows.Handle
	var status windows.IO_STATUS_BLOCK
	if err := windows.NtCreateFile(&h, windows.FILE_GENERIC_READ, &attr, &status, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_CREATE,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0); err != nil {
		if errors.Is(err, windows.STATUS_OBJECT_NAME_COLLISION) {
			return ErrProfileExists
		}
		return err
	}
	child := os.NewFile(uintptr(h), "controller-profile-directory")
	return errors.Join(acl.check(child), child.Close(), ctx.Err(), parent.Verify())
}

func readWindowsProfilePreparation(ctx context.Context, root *paths.Root, name string) (out windowsProfilePreparation, acl windowsControlACL, resultErr error) {
	if ctx == nil || !validWindowsPreparationRoot(name) {
		return out, acl, ErrProfileInput
	}
	controller, err := windowsPreparationController()
	if err != nil {
		return out, acl, err
	}
	acl = windowsControlACL{owner: controller}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return out, acl, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	if err := errors.Join(ctx.Err(), acl.check(dir)); err != nil {
		return out, acl, err
	}
	files := windowsMaintenanceFiles{control: root, acl: acl}
	raw, exists, err := files.readEntry(ctx, dir, windowsProfileName)
	if err != nil {
		return out, acl, err
	}
	if !exists || model.ValidateWireJSON(raw, maintenanceJournalMaxBytes) != nil {
		return out, acl, ErrProfileIncomplete
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || !out.valid(name, controller) {
		return windowsProfilePreparation{}, acl, ErrProfileIncomplete
	}
	if _, ready, err := readWindowsReadyRecord(ctx, root, dir, acl, out); err != nil {
		return windowsProfilePreparation{}, acl, err
	} else if ready {
		out.Stage = "READY"
	}
	return out, acl, nil
}

func CheckProtectedProfile(ctx context.Context, name string) (ProtectedProfileSummary, error) {
	return withWindowsProfileControl(ctx, name, func(m windowsProfilePreparation, files windowsMaintenanceFiles) (ProtectedProfileSummary, error) {
		if m.Stage == "READY" {
			return checkWindowsReadyProfile(ctx, name, m, files)
		}
		return files.checkCatalogSeed(ctx, m)
	})
}

func withWindowsProfileControl[T any](ctx context.Context, name string, use func(windowsProfilePreparation, windowsMaintenanceFiles) (T, error)) (out T, resultErr error) {
	if ctx == nil || !validWindowsPreparationRoot(name) {
		return out, ErrProfileInput
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	root, err := paths.OpenRoot(name)
	if err != nil {
		return out, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			var empty T
			out = empty
		}
	}()
	m, acl, err := readWindowsProfilePreparation(ctx, root, name)
	if err != nil {
		return out, err
	}
	control, err := root.OpenDirectory("control")
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	return use(m, windowsMaintenanceFiles{control: control, acl: acl, profile: m.ProfileID, scope: "catalog"})
}
