package sqlite

import (
	"context"
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsTokenIsAppContainer  = 29
	windowsTokenAppContainerSID = 31
)

type WindowsProfileRoleCheck struct {
	ProfileID      string `json:"profile_id"`
	EngineVerified bool   `json:"engine_verified"`
	ClientVerified bool   `json:"client_verified"`
}

// VerifyWindowsProfileRoles checks the live process tokens of externally
// launched roles against the protected profile. The result is an observation
// of those process objects. It does not enroll a worker or authorize IPC.
func VerifyWindowsProfileRoles(ctx context.Context, root string, enginePID, clientPID uint32) (WindowsProfileRoleCheck, error) {
	if enginePID == 0 || clientPID == 0 || enginePID == clientPID {
		return WindowsProfileRoleCheck{}, ErrProfileInput
	}
	return withWindowsProfileControl(ctx, root, func(m windowsProfilePreparation, files windowsMaintenanceFiles) (out WindowsProfileRoleCheck, resultErr error) {
		dir, err := files.directory(ctx)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
		err = withVerifiedWindowsRoles(ctx, m, enginePID, clientPID, func(recheck func() error) error {
			return errors.Join(files.control.Verify(), files.acl.check(dir), recheck())
		})
		if err != nil {
			return out, err
		}
		return WindowsProfileRoleCheck{ProfileID: m.ProfileID, EngineVerified: true, ClientVerified: true}, nil
	})
}

func withVerifiedWindowsRoles(ctx context.Context, m windowsProfilePreparation, enginePID, clientPID uint32, use func(func() error) error) (resultErr error) {
	if enginePID == 0 || clientPID == 0 || enginePID == clientPID {
		return ErrProfileInput
	}
	engine, err := openWindowsRoleProcess(enginePID)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, windows.CloseHandle(engine)) }()
	client, err := openWindowsRoleProcess(clientPID)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, windows.CloseHandle(client)) }()
	recheck := func() error {
		return errors.Join(ctx.Err(), matchWindowsRole(engine, m.EngineSID, m.ControllerSID),
			matchWindowsRole(client, m.ClientSID, m.ControllerSID),
			liveWindowsRoleProcess(engine), liveWindowsRoleProcess(client))
	}
	if err := recheck(); err != nil {
		return err
	}
	return use(recheck)
}

func openWindowsRoleProcess(pid uint32) (windows.Handle, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return 0, errors.Join(ErrProfileDenied, err)
	}
	if err := liveWindowsRoleProcess(process); err != nil {
		return 0, errors.Join(err, windows.CloseHandle(process))
	}
	return process, nil
}

func liveWindowsRoleProcess(process windows.Handle) error {
	result, err := windows.WaitForSingleObject(process, 0)
	if err != nil || result != uint32(windows.WAIT_TIMEOUT) {
		return errors.Join(ErrProfileDenied, err)
	}
	return nil
}

func matchWindowsRole(process windows.Handle, expected, controller string) (resultErr error) {
	if !windowsPreparationSID(expected) {
		return ErrProfileInput
	}
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return errors.Join(ErrProfileDenied, err)
	}
	defer func() { resultErr = errors.Join(resultErr, token.Close()) }()
	container, err := windowsTokenDWORD(token, windowsTokenIsAppContainer)
	if err != nil || container > 1 || (container == 1) != strings.HasPrefix(expected, "S-1-15-2-") {
		return errors.Join(ErrProfileDenied, err)
	}
	elevated, err := windowsTokenDWORD(token, windows.TokenElevation)
	if err != nil || elevated != 0 {
		return errors.Join(ErrProfileDenied, err)
	}
	user, err := token.GetTokenUser()
	if err != nil || user.User.Sid == nil {
		return errors.Join(ErrProfileDenied, err)
	}
	actual := user.User.Sid.String()
	if container == 1 {
		// Windows package SIDs can be the same for multiple OS accounts.
		// This profile's AppContainers are installed for the controller user.
		if actual != controller {
			return ErrProfileDenied
		}
		actual, err = windowsPackageTokenSID(token)
	}
	if err != nil || actual != expected {
		return errors.Join(ErrProfileDenied, err)
	}
	return nil
}

func windowsTokenDWORD(token windows.Token, class uint32) (uint32, error) {
	var value, size uint32
	err := windows.GetTokenInformation(token, class, (*byte)(unsafe.Pointer(&value)), 4, &size)
	if err != nil || size != 4 {
		return 0, errors.Join(ErrProfileDenied, err)
	}
	return value, nil
}

func windowsPackageTokenSID(token windows.Token) (string, error) {
	var size uint32
	err := windows.GetTokenInformation(token, windowsTokenAppContainerSID, nil, 0, &size)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || size < uint32(unsafe.Sizeof(uintptr(0))) || size > 4096 {
		return "", errors.Join(ErrProfileDenied, err)
	}
	info := make([]byte, size)
	if err := windows.GetTokenInformation(token, windowsTokenAppContainerSID, &info[0], size, &size); err != nil {
		return "", errors.Join(ErrProfileDenied, err)
	}
	sid := (*struct{ SID *windows.SID })(unsafe.Pointer(&info[0])).SID
	if sid == nil || !sid.IsValid() {
		return "", ErrProfileDenied
	}
	return sid.String(), nil
}
