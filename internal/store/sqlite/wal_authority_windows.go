package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

// A session is local to an exclusive scope lifecycle lease. The controller
// can read the key and write the two logs. Role clients have no file access.
// The independent head authenticates each retained receipt, including prepare.
type windowsWALAuthority struct {
	walReceiptState
	identity     walAuthorityIdentity
	key          [32]byte
	receiptChain [32]byte
	headChain    [32]byte
	records      int
}

func replayWindowsWALAuthority(ctx context.Context, identity walAuthorityIdentity, key [32]byte, log, head []byte) (windowsWALAuthority, error) {
	if key == [32]byte{} || len(log) < walAuthorityRecordSize || len(log) > walAuthorityMaxBytes ||
		len(log)%walAuthorityRecordSize != 0 || len(head) != len(log)/walAuthorityRecordSize*walHeadRecordSize {
		return windowsWALAuthority{}, ErrStoreRecoveryRequired
	}
	current := windowsWALAuthority{identity: identity, key: key}
	for start := 0; start < len(log); start += walAuthorityRecordSize {
		if err := ctx.Err(); err != nil {
			return windowsWALAuthority{}, err
		}
		end := start + walAuthorityRecordSize
		record := log[start:end]
		event, err := decodeWALAuthorityRecord(identity, key, current.receiptChain, record)
		if err != nil {
			return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
		}
		// E782's staged genesis encoded sequence one. It is authenticated
		// with its original bytes and normalized for subsequent transitions.
		if start == 0 && event.kind == walAuthorityBootstrap && event.Sequence == 1 {
			event.Sequence = 0
		}
		if err := current.apply(event, start == 0); err != nil {
			return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
		}
		position := walJournalPosition{Size: int64(end), SHA256: sha256.Sum256(record)}
		want := encodeWALHeadRecord(identity, key, current.headChain, position)
		offset := current.records * walHeadRecordSize
		entry := head[offset : offset+walHeadRecordSize]
		if !bytes.Equal(want, entry) {
			return windowsWALAuthority{}, ErrStoreRecoveryRequired
		}
		current.receiptChain, current.headChain = position.SHA256, sha256.Sum256(entry)
		current.records++
	}
	return current, nil
}

func readWindowsWALAuthority(ctx context.Context, control windowsMaintenanceFiles, controlDir *os.File, staged windowsMaintenanceFiles, stageDir *os.File, identity walAuthorityIdentity) (windowsWALAuthority, error) {
	key, present, err := control.readEntryLimit(ctx, controlDir, windowsCatalogHeadKey, 32)
	if err != nil || !present || len(key) != 32 {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	defer clear(key)
	head, present, err := control.readEntryLimit(ctx, controlDir, walHeadName, walHeadMaxBytes)
	if err != nil || !present {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	receipt, present, err := staged.readEntryLimit(ctx, stageDir, walAuthorityName, walAuthorityMaxBytes)
	if err != nil || !present {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return replayWindowsWALAuthority(ctx, identity, [32]byte(key), receipt, head)
}

func (w *windowsWALAuthority) append(ctx context.Context, lease *runtime.Lifecycle, owner, control windowsMaintenanceFiles, controlDir *os.File, staged windowsMaintenanceFiles, stageDir *os.File, event walAuthorityRecord) error {
	if err := lease.CheckRoot(ctx, owner.control, owner.scope, ports.ExclusiveLease); err != nil {
		return err
	}
	return w.appendFiles(ctx, owner, control, controlDir, staged, stageDir, event)
}

// The caller verifies either the exclusive maintenance lease or the live
// worktree writer together with the exclusive index snapshot gate.
func (w *windowsWALAuthority) appendFiles(ctx context.Context, owner, control windowsMaintenanceFiles, controlDir *os.File, staged windowsMaintenanceFiles, stageDir *os.File, event walAuthorityRecord) error {
	if owner.scope != control.scope || owner.scope != staged.scope || owner.profile != control.profile || owner.profile != staged.profile {
		return ErrStateUnsafe
	}
	if w.records < 1 || w.records >= walAuthorityMaxBytes/walAuthorityRecordSize {
		return ErrStateLimit
	}
	next := w.walReceiptState
	if err := next.apply(event, false); err != nil {
		return err
	}
	record := encodeWALAuthorityRecord(w.identity, w.key, w.receiptChain, event)
	position := walJournalPosition{Size: int64(w.records+1) * walAuthorityRecordSize, SHA256: sha256.Sum256(record[:])}
	head := encodeWALHeadRecord(w.identity, w.key, w.headChain, position)
	if err := staged.appendAuthorityFile(ctx, stageDir, walAuthorityName, w.records, w.receiptChain, record[:]); err != nil {
		return errors.Join(ErrStoreRecoveryRequired, err)
	}
	if err := control.appendAuthorityFile(ctx, controlDir, walHeadName, w.records, w.headChain, head); err != nil {
		// The unmatched extent is intentionally retained. Never ACK or retry
		// the SQL operation using a possibly older or shorter authority.
		return errors.Join(ErrStoreRecoveryRequired, err)
	}
	w.walReceiptState = next
	w.receiptChain, w.headChain = position.SHA256, sha256.Sum256(head)
	w.records++
	return nil
}

func (j windowsMaintenanceFiles) appendAuthorityFile(ctx context.Context, dir *os.File, name string, records int, last [32]byte, data []byte) (resultErr error) {
	if name != walHeadName && name != walAuthorityName || records < 1 ||
		name == walHeadName && len(data) != walHeadRecordSize || name == walAuthorityName && len(data) != walAuthorityRecordSize {
		return ErrStateUnsafe
	}
	if err := errors.Join(ctx.Err(), j.acl.check(dir)); err != nil {
		return err
	}
	pinned, err := j.control.OpenRegular(name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, pinned.Close()) }()
	if err := j.acl.check(pinned); err != nil {
		return err
	}
	size := int64(records) * int64(len(data))
	tail := make([]byte, len(data))
	if err := readLogicalAt(ctx, pinned, tail, size-int64(len(tail))); err != nil {
		return err
	}
	if sha256.Sum256(tail) != last {
		return ErrStateChanged
	}
	relative, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return err
	}
	attr := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(dir.Fd()), ObjectName: relative,
		Attributes: windows.OBJ_DONT_REPARSE | windows.OBJ_CASE_INSENSITIVE}
	attr.Length = uint32(unsafe.Sizeof(attr))
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	if err := windows.NtCreateFile(&handle, windows.FILE_APPEND_DATA|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.SYNCHRONIZE,
		&attr, &status, nil, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_WRITE_THROUGH, 0, 0); err != nil {
		return err
	}
	file := os.NewFile(uintptr(handle), "authority-append")
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	check := func(want int64) error {
		var a, b windows.ByHandleFileInformation
		if err := errors.Join(paths.VerifyRegularFile(file), j.acl.check(file), j.acl.check(dir), j.control.Verify(),
			windows.GetFileInformationByHandle(windows.Handle(pinned.Fd()), &a), windows.GetFileInformationByHandle(handle, &b)); err != nil {
			return err
		}
		size := int64(b.FileSizeHigh)<<32 | int64(b.FileSizeLow)
		if a.VolumeSerialNumber != b.VolumeSerialNumber || a.FileIndexHigh != b.FileIndexHigh ||
			a.FileIndexLow != b.FileIndexLow || size != want {
			return ErrStateChanged
		}
		return ctx.Err()
	}
	if err := check(size); err != nil {
		return err
	}
	n, err := file.Write(data)
	if n != len(data) {
		err = errors.Join(err, io.ErrShortWrite)
	}
	if err != nil {
		return err
	}
	return check(size + int64(n))
}
