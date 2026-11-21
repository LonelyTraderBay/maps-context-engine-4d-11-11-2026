package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

func resultFixtureDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := runtime.WindowsPrivateDescriptor(user.User.Sid)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{base, filepath.Join(base, "scratch"), filepath.Join(base, "scratch", "rename")} {
		if name != base {
			if err := os.Mkdir(name, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := windows.SetNamedSecurityInfo(name, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
			t.Fatal(err)
		}
	}
	return base
}
