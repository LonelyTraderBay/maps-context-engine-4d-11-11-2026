package lsp

import (
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

func resultFixtureDir(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "private")
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := runtime.WindowsPrivateDescriptor(user.User.Sid)
	if err != nil {
		t.Fatal(err)
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: sd}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	for _, name := range []string{base, filepath.Join(base, "scratch"), filepath.Join(base, "scratch", "rename")} {
		path, err := windows.UTF16PtrFromString(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.CreateDirectory(path, &attributes); err != nil {
			t.Fatal(err)
		}
	}
	return base
}
