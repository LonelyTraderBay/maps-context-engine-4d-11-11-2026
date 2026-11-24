// Package powershellprovider binds the Windows declaration parser and its pins
// to the closed S43 profile.
package powershellprovider

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

//go:embed toolchain-pin.json
var pins []byte

//go:embed SyntaxProbe.psm1
var parserModule string

//go:embed worker.ps1
var workerDriver string

const ID = "mce4d-powershell-syntax"
const MaxSourceBytes = 65536

var ErrInstallation = errors.New("PowerShell syntax installation unavailable")
var ErrWorker = errors.New("PowerShell syntax worker did not return a verified result")
var supportedPath = regexp.MustCompile(`^(?:[A-Za-z0-9_-][A-Za-z0-9_.-]*/)*[A-Za-z0-9_-][A-Za-z0-9_.-]*\.ps1$`)
var declarationName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,95}$`)

func SupportsPath(path string) bool {
	return len(path) <= 256 && supportedPath.MatchString(path)
}

type toolchainPin struct {
	Version string            `json:"powershell_version"`
	Files   map[string]string `json:"files"`
}

func loadPin() (pin toolchainPin, err error) {
	err = json.Unmarshal(pins, &pin)
	return pin, err
}

func program() string {
	return "$ErrorActionPreference = 'Stop'\n" +
		"$trustedModule = New-Module -ScriptBlock {\n" + parserModule +
		"\n} -ArgumentList ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" +
		base64.StdEncoding.EncodeToString(pins) + "')))\n" + workerDriver
}

// Profile supplies the pinned values for an explicit local profile request.
func Profile() (model.PowerShellProfile, error) {
	pin, err := loadPin()
	if err != nil {
		return model.PowerShellProfile{}, err
	}
	return model.PowerShellProfile{SchemaVersion: 1, Language: "powershell", PowerShellVersion: pin.Version,
		ExecutableDigest: pin.Files["pwsh.exe"], ParserDigest: pin.Files["System.Management.Automation.dll"],
		ProviderDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(program()))), AnalysisMode: "syntax"}, nil
}

func VerifyProfile(profile *model.PowerShellProfile) error {
	expected, err := Profile()
	if err != nil || profile == nil || *profile != expected {
		return ErrInstallation
	}
	return nil
}
