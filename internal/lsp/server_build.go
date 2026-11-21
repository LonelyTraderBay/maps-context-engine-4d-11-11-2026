//go:build linux || windows

package lsp

import (
	"debug/buildinfo"
	"os"
)

func verifyGoplsBuild(file *os.File, goos string) error {
	info, err := buildinfo.Read(file)
	if err != nil || info.GoVersion != "go1.27.4" || info.Path != "golang.org/x/tools/gopls" ||
		info.Main.Path != "golang.org/x/tools/gopls" || info.Main.Replace != nil {
		return ErrLSPAuthority
	}
	official := info.Main.Version == "v0.24.0" && info.Main.Sum == "h1:nPO/LffYa4sP62Ueuema5PPnZPGaZjGl+vZGvbTek4g="
	windowsCompatibility := goos == "windows" && info.Main.Version == "v0.24.0-mce4d.windows.1" &&
		info.Main.Sum == "h1:Qr4n2Q1AmM9EzszvKdbwTd7vdppENxjTi+R2Smo/ZYc="
	if !official && !windowsCompatibility {
		return ErrLSPAuthority
	}
	asm := false
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/segmentio/asm" {
			asm = dependency.Version == "v1.2.1" && dependency.Replace != nil && dependency.Replace.Path == "github.com/segmentio/asm" && dependency.Replace.Version == "v1.1.3" && dependency.Replace.Sum == "h1:WM03sfUOENvvKexOLp+pCqgb/WDjsi7EK8gIsICtzhc="
		}
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if !asm || settings["GOOS"] != goos || settings["GOARCH"] != "amd64" || settings["CGO_ENABLED"] != "0" {
		return ErrLSPAuthority
	}
	return nil
}
