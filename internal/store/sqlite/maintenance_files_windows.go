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
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/windows"
)

const windowsMaintenanceName = "maintenance.json"

// windowsMaintenanceFiles only opens an already authenticated controller
// directory for one installation and scope. Its caller supplies the OS
// identities from protected installation metadata. It cannot authorize
// restore or verify the SQLite image and WAL receipt on its own.
type windowsMaintenanceFiles struct {
	control *paths.Root
	acl     windowsControlACL
	profile string
	scope   string
}

type windowsControlACL struct {
	owner      *windows.SID
	reader     *windows.SID
	toolOutput bool
}

const windowsControlRead = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE
const windowsToolOutputAccess = windowsControlRead | windows.FILE_GENERIC_WRITE | windows.DELETE

func (p windowsControlACL) valid() bool {
	return p.owner != nil && p.owner.IsValid() &&
		(!p.toolOutput || p.reader != nil) &&
		(p.reader == nil || p.reader.IsValid() && !p.reader.Equals(p.owner) && !p.reader.IsWellKnown(windows.WinLocalSystemSid))
}

func (p windowsControlACL) descriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	if !p.valid() {
		return nil, ErrProfileDenied
	}
	owner := p.owner.String()
	if p.toolOutput {
		return windows.SecurityDescriptorFromString("O:" + owner + "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + owner + ")" +
			fmt.Sprintf("(A;OICI;0x%x;;;%s)S:(ML;OICI;NW;;;LW)", windowsToolOutputAccess, p.reader.String()))
	}
	sddl := "O:" + owner + "D:P(A;;FA;;;SY)(A;;FA;;;" + owner + ")"
	if p.reader != nil {
		sddl += "(A;;FRFX;;;" + p.reader.String() + ")"
	}
	return windows.SecurityDescriptorFromString(sddl)
}

// Reject every ACL form whose effect this controller has not modeled. The
// reader may read or traverse, but may not replace an entry, modify its data,
// change attributes, or acquire the owner's ability to change the ACL.
func (p windowsControlACL) check(file *os.File) error {
	if !p.valid() || file == nil {
		return ErrProfileDenied
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return errors.Join(ErrProfileDenied, err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(p.owner) {
		return errors.Join(ErrProfileDenied, err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return errors.Join(ErrProfileDenied, err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return errors.Join(ErrProfileDenied, err)
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return errors.Join(ErrProfileDenied, err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !p.toolOutput && ace.Header.AceFlags != 0 {
			return ErrProfileDenied
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.Equals(p.owner) || sid.IsWellKnown(windows.WinLocalSystemSid) {
			continue
		}
		allowed := windows.ACCESS_MASK(windowsControlRead)
		if p.toolOutput {
			allowed = windows.ACCESS_MASK(windowsToolOutputAccess)
		}
		if p.reader == nil || !sid.Equals(p.reader) || ace.Mask & ^allowed != 0 {
			return ErrProfileDenied
		}
	}
	return nil
}

func (p windowsControlACL) checkWriter() error {
	if !p.valid() {
		return ErrProfileDenied
	}
	token := windows.GetCurrentProcessToken()
	user, err := windowsPreparationController()
	if err != nil {
		return errors.Join(ErrProfileDenied, err)
	}
	if user.Equals(p.owner) {
		return nil
	}
	if p.owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) && token.IsElevated() {
		member, err := windows.Token(0).IsMember(p.owner)
		if err == nil && member {
			return nil
		}
		return errors.Join(ErrProfileDenied, err)
	}
	return ErrProfileDenied
}

func (j windowsMaintenanceFiles) directory(ctx context.Context) (*os.File, error) {
	if ctx == nil || j.control == nil || !model.ValidUUID(j.profile) ||
		(j.scope != "catalog" && !model.ValidUUID(j.scope)) {
		return nil, ErrProfileInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := j.control.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	if err := j.acl.check(dir); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	return dir, nil
}

func (j windowsMaintenanceFiles) read(ctx context.Context) (out MaintenanceJournal, present bool, resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return out, false, errors.Join(ErrStoreRecoveryRequired, err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, dir.Close())
		if resultErr != nil {
			out = MaintenanceJournal{}
		}
	}()
	raw, present, err := j.readEntry(ctx, dir, windowsMaintenanceName)
	if err != nil || !present {
		return out, present, err
	}
	out, err = decodeMaintenanceJournal(raw, j.profile, j.scope)
	return out, true, err
}

func (j windowsMaintenanceFiles) readEntry(ctx context.Context, dir *os.File, name string) ([]byte, bool, error) {
	return j.readEntryLimit(ctx, dir, name, maintenanceJournalMaxBytes)
}

func (j windowsMaintenanceFiles) readEntryLimit(ctx context.Context, dir *os.File, name string, limit int64) (_ []byte, present bool, resultErr error) {
	file, err := j.control.OpenRegular(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, errors.Join(ctx.Err(), j.control.Verify(), j.acl.check(dir))
	}
	if err != nil {
		return nil, false, errors.Join(ErrStoreRecoveryRequired, err)
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := j.acl.check(file); err != nil {
		return nil, true, errors.Join(ErrStoreRecoveryRequired, err)
	}
	info, err := file.Stat()
	if err != nil || info.Size() < 1 || info.Size() > limit {
		return nil, true, errors.Join(ErrStoreRecoveryRequired, err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, true, errors.Join(ErrStoreRecoveryRequired, err)
	}
	if err := errors.Join(ctx.Err(), j.control.Verify(), paths.VerifyRegularFile(file), j.acl.check(file), j.acl.check(dir)); err != nil {
		return nil, true, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return raw, true, nil
}

func (j windowsMaintenanceFiles) checkServe(ctx context.Context, recoveryOperation string) error {
	state, present, err := j.read(ctx)
	if err != nil {
		return err
	}
	if present && state.Phase != "VERIFIED" && (recoveryOperation == "" || recoveryOperation != state.OperationID) {
		return ErrStoreRecoveryRequired
	}
	return nil
}

// The executor must also hold the selected store's exclusive lifecycle
// through replacement. This smaller lock only serializes journal transitions.
func (j windowsMaintenanceFiles) write(ctx context.Context, next MaintenanceJournal, advance bool, boundary func(string)) (resultErr error) {
	if err := j.acl.checkWriter(); err != nil {
		return err
	}
	dir, err := j.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err := decodeMaintenanceJournal(raw, j.profile, j.scope); err != nil {
		return err
	}
	descriptor, err := j.acl.descriptor()
	if err != nil {
		return err
	}
	lock, err := openWindowsControlEntry(dir, "maintenance.lock", descriptor, false, windows.FILE_OPEN_IF)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := errors.Join(paths.VerifyRegularFile(lock), j.acl.check(lock)); err != nil {
		return errors.Join(ErrStoreRecoveryRequired, err)
	}
	var offset windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &offset); err != nil {
		return errors.Join(ErrStoreRecoveryRequired, err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, &offset))
	}()
	previousRaw, exists, err := j.readEntry(ctx, dir, windowsMaintenanceName)
	if err != nil {
		return err
	}
	var previous MaintenanceJournal
	if exists {
		previous, err = decodeMaintenanceJournal(previousRaw, j.profile, j.scope)
		if err != nil {
			return err
		}
	}
	if advance {
		if !exists || !validMaintenanceAdvance(previous, next) {
			return ErrStoreRecoveryRequired
		}
	} else {
		if next.Phase != "PREPARED" && (next.Phase != "NEEDS_RECOVERY" || len(next.Files) != 0 || len(next.CompletedSteps) != 0) {
			return ErrStoreRecoveryRequired
		}
		if exists {
			if previous.Phase != "VERIFIED" || previous.OperationID == next.OperationID {
				return ErrStoreRecoveryRequired
			}
			archive := "maintenance.completed." + previous.OperationID + ".json"
			archived, present, err := j.readEntry(ctx, dir, archive)
			if err != nil || present && !bytes.Equal(previousRaw, archived) {
				return errors.Join(ErrStoreRecoveryRequired, err)
			}
			if !present {
				if err := j.publish(ctx, dir, descriptor, archive, previousRaw, false, nil); err != nil {
					return err
				}
			}
		}
	}
	return j.publish(ctx, dir, descriptor, windowsMaintenanceName, raw, exists, boundary)
}

func openWindowsControlEntry(dir *os.File, name string, descriptor *windows.SECURITY_DESCRIPTOR, rename bool, disposition uint32) (*os.File, error) {
	return openWindowsControlEntryShared(dir, name, descriptor, rename, disposition, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
}

func openWindowsControlEntryShared(dir *os.File, name string, descriptor *windows.SECURITY_DESCRIPTOR, rename bool, disposition, share uint32) (*os.File, error) {
	return paths.OpenWindowsEntry(dir, name, descriptor, rename, disposition, share)
}

func (j windowsMaintenanceFiles) publish(ctx context.Context, dir *os.File, descriptor *windows.SECURITY_DESCRIPTOR, name string, raw []byte, replace bool, boundary func(string)) (resultErr error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := fmt.Sprintf("control.pending.%x.json", nonce)
	file, err := openWindowsControlEntry(dir, temp, descriptor, true, windows.FILE_CREATE)
	if err != nil {
		return err
	}
	// Unpublished artifacts are diagnostic state. Never remove them based on a
	// filename and never consider them an authoritative journal on restart.
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := errors.Join(j.acl.check(file), paths.VerifyRegularFile(file)); err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if boundary != nil {
		boundary("prepared-file")
	}
	if err := errors.Join(ctx.Err(), j.acl.check(dir), j.control.Verify(), paths.VerifyRegularFile(file)); err != nil {
		return err
	}
	if err := renameWindowsControlEntry(file, name, replace); err != nil {
		return err
	}
	if boundary != nil {
		boundary("published-name")
	}
	return errors.Join(file.Sync(), j.acl.check(dir), j.control.Verify(), ctx.Err())
}

func renameWindowsControlEntry(file *os.File, name string, replace bool) error {
	return paths.RenameWindowsEntry(file, name, replace)
}
