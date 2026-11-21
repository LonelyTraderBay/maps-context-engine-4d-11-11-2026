//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

const profileManifestName = "profile.json"
const profileManifestLimit = 32768

// Root-owned installation metadata; this is not RuntimeConfig or an API model.
// The client UID defines the untrusted filesystem principal. The separately
// trusted engine OS account receives the explicitly provisioned root allowlist;
// this manifest is not a claim to authenticate a remote/network peer.
type protectedProfileManifest struct {
	FormatVersion             int      `json:"format_version"`
	ProfileID                 string   `json:"profile_id"`
	Stage                     string   `json:"stage"`
	EngineUID                 uint32   `json:"engine_uid"`
	EngineGID                 uint32   `json:"engine_gid"`
	ClientUID                 uint32   `json:"client_uid"`
	TargetRoots               []string `json:"target_roots"`
	KeyDigest                 string   `json:"key_digest"`
	CatalogAuthorityID        string   `json:"catalog_authority_id"`
	CatalogIncarnation        string   `json:"catalog_incarnation"`
	BootstrapExecutableSHA256 string   `json:"bootstrap_executable_sha256"`
	WorkerExecutable          string   `json:"worker_executable,omitempty"`
}

type protectedProfile struct {
	rootPath              string
	root, state, control  *paths.Root
	maintenanceControl    *paths.Root
	targets               []*paths.Root
	domain, journalDomain *stateDomain
	manifest              protectedProfileManifest
	manifestHash          [32]byte
	key                   [32]byte
	captureOnce           sync.Once
	captureClient         *profileCaptureClient
	captureError          error
	maintenanceOperation  string // Only a private offline worker may set this.
}

func profileStatePath(root string) string {
	return filepath.Join(root, "home", ".local", "state", "mce4d")
}

func validProfilePath(root string) bool {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || len(root) > 4096 {
		return false
	}
	name := filepath.Base(root)
	if len(name) < 1 || len(name) > 64 || name == "." || name == ".." {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func profileUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 15) | 64
	raw[8] = (raw[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

func profileUUIDBytes(s string) ([16]byte, error) {
	var out [16]byte
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || strings.ToLower(s) != s {
		return out, ErrProfileInput
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(raw) != 16 {
		return out, ErrProfileInput
	}
	copy(out[:], raw)
	if out == [16]byte{} {
		return out, ErrProfileInput
	}
	return out, nil
}

func validProfileHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && strings.ToLower(s) == s
}

func validateProfileManifest(m protectedProfileManifest) error {
	if m.WorkerExecutable != "" && (!filepath.IsAbs(m.WorkerExecutable) || filepath.Clean(m.WorkerExecutable) != m.WorkerExecutable || len(m.WorkerExecutable) > 4096) {
		return ErrProfileInput
	}
	if m.FormatVersion != 1 || m.EngineUID == 0 || m.EngineGID == 0 || m.ClientUID == 0 || m.ClientUID == m.EngineUID ||
		m.EngineUID == ^uint32(0) || m.EngineGID == ^uint32(0) || m.ClientUID == ^uint32(0) || len(m.TargetRoots) < 1 || len(m.TargetRoots) > 64 ||
		!validProfileHash(m.KeyDigest) || !validProfileHash(m.BootstrapExecutableSHA256) {
		return ErrProfileInput
	}
	if _, err := profileUUIDBytes(m.ProfileID); err != nil {
		return err
	}
	if _, err := profileUUIDBytes(m.CatalogAuthorityID); err != nil {
		return err
	}
	if m.Stage == "READY" {
		if _, err := profileUUIDBytes(m.CatalogIncarnation); err != nil {
			return err
		}
	} else if m.Stage != "CREATING" || m.CatalogIncarnation != "" {
		return ErrProfileInput
	}
	for i, path := range m.TargetRoots {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 4096 || i > 0 && m.TargetRoots[i-1] >= path {
			return ErrProfileInput
		}
	}
	return nil
}

func readProfileManifest(ctx context.Context, root *paths.Root) (protectedProfileManifest, [32]byte, error) {
	var empty protectedProfileManifest
	var zero [32]byte
	if rootManagedAncestors(ctx, root, true) != nil {
		return empty, zero, ErrProfileDenied
	}
	f, err := root.OpenRegular(profileManifestName)
	if err != nil {
		return empty, zero, ErrProfileDenied
	}
	var st unix.Stat_t
	statErr := unix.Fstat(int(f.Fd()), &st)
	raw, readErr := io.ReadAll(io.LimitReader(f, profileManifestLimit+1))
	closeErr := f.Close()
	if errors.Join(statErr, readErr, closeErr) != nil || st.Uid != 0 || st.Mode&0022 != 0 || st.Nlink != 1 || model.ValidateWireJSON(raw, profileManifestLimit) != nil {
		return empty, zero, ErrProfileDenied
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&empty) != nil || validateProfileManifest(empty) != nil {
		return protectedProfileManifest{}, zero, ErrProfileDenied
	}
	return empty, sha256.Sum256(raw), ctx.Err()
}

func openProtectedProfile(ctx context.Context, path, stage string) (_ *protectedProfile, resultErr error) {
	if !validProfilePath(path) || unprivilegedStateProcess(ctx) != nil {
		return nil, ErrProfileDenied
	}
	p := &protectedProfile{rootPath: path}
	defer func() {
		if resultErr != nil {
			_ = p.Close()
		}
	}()
	var err error
	p.root, err = paths.OpenRoot(path)
	if err != nil {
		return nil, ErrProfileDenied
	}
	p.manifest, p.manifestHash, err = readProfileManifest(ctx, p.root)
	if err != nil {
		return nil, err
	}
	m := p.manifest
	if m.Stage != stage {
		return nil, ErrProfileIncomplete
	}
	if m.EngineUID != uint32(unix.Geteuid()) || m.EngineGID != uint32(unix.Getegid()) {
		return nil, ErrProfileDenied
	}
	for _, name := range m.TargetRoots {
		root, err := paths.OpenRoot(name)
		if err != nil {
			return nil, ErrProfileDenied
		}
		p.targets = append(p.targets, root)
	}
	p.state, err = p.root.OpenDirectory("home/.local/state/mce4d")
	if err != nil {
		return nil, ErrProfileDenied
	}
	p.domain, err = openStateDomain(ctx, p.state, p.targets, m.ClientUID)
	if err != nil {
		return nil, err
	}
	p.journalDomain, err = p.domain.Child(ctx, ".maintenance/catalog")
	if err != nil {
		return nil, err
	}
	p.control, err = p.root.OpenDirectory("control/catalog")
	if err != nil {
		return nil, ErrProfileDenied
	}
	p.maintenanceControl, err = p.root.OpenDirectory("control")
	if err != nil {
		return nil, ErrProfileDenied
	}
	keyRoot, err := p.root.OpenDirectory("control")
	if err != nil {
		return nil, ErrProfileDenied
	}
	keyFile, err := keyRoot.OpenRegular("profile.key")
	if err != nil {
		_ = keyRoot.Close()
		return nil, ErrProfileDenied
	}
	var keyStat unix.Stat_t
	statErr := unix.Fstat(int(keyFile.Fd()), &keyStat)
	raw, readErr := io.ReadAll(io.LimitReader(keyFile, 33))
	defer clear(raw)
	closeErr := errors.Join(keyFile.Close(), keyRoot.Close())
	if errors.Join(statErr, readErr, closeErr) != nil || len(raw) != 32 || keyStat.Uid != m.EngineUID || keyStat.Gid != m.EngineGID || keyStat.Mode&0077 != 0 {
		return nil, ErrProfileDenied
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != m.KeyDigest {
		return nil, ErrProfileDenied
	}
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("MCE4D-PROFILE-CATALOG1\x00" + m.ProfileID))
	copy(p.key[:], mac.Sum(nil))
	clear(raw)
	return p, nil
}

func (p *protectedProfile) Check(ctx context.Context) error {
	if p == nil || p.domain == nil || p.domain.Check(ctx) != nil {
		return ErrProfileDenied
	}
	m, digest, err := readProfileManifest(ctx, p.root)
	if err != nil || digest != p.manifestHash || m.Stage != p.manifest.Stage {
		return ErrProfileDenied
	}
	if err := checkMaintenanceGate(ctx, p.control, p.manifest.ProfileID, "catalog", p.maintenanceOperation); err != nil {
		return err
	}
	if err := checkMaintenanceGate(ctx, p.maintenanceControl, p.manifest.ProfileID, "catalog", p.maintenanceOperation); err != nil {
		return err
	}
	return ctx.Err()
}

func (p *protectedProfile) identity(incarnation string) (walAuthorityIdentity, error) {
	a, err := profileUUIDBytes(p.manifest.CatalogAuthorityID)
	if err != nil {
		return walAuthorityIdentity{}, err
	}
	i, err := profileUUIDBytes(incarnation)
	if err != nil {
		return walAuthorityIdentity{}, err
	}
	return walAuthorityIdentity{Authority: a, Incarnation: i, Store: sha256.Sum256([]byte("MCE4D-PROFILE-STORE1\x00" + p.manifest.ProfileID + "\x00catalog"))}, nil
}

func (p *protectedProfile) openLog(ctx context.Context, incarnation string, image *walLogicalImage) (*walAuthorityLog, *walHeadAuthority, error) {
	if p.Check(ctx) != nil {
		return nil, nil, ErrProfileDenied
	}
	identity, err := p.identity(incarnation)
	if err != nil {
		return nil, nil, err
	}
	name, err := p.control.CanonicalPath()
	if err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(name, walHeadName), os.O_RDWR|os.O_APPEND|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, ErrProfileDenied
	}
	protected := append(append([]*paths.Root(nil), p.targets...), p.state)
	head, err := openWALHeadAuthority(ctx, p.control, protected, f, identity, p.key)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	name, err = p.journalDomain.root.CanonicalPath()
	if err != nil {
		_ = head.Close()
		return nil, nil, err
	}
	f, err = os.OpenFile(filepath.Join(name, walAuthorityName), os.O_RDWR|os.O_APPEND|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = head.Close()
		return nil, nil, ErrProfileDenied
	}
	log, err := openAnchoredWALAuthorityLog(ctx, p.journalDomain, f, head, identity, p.key, walAuthorityMaxBytes, image)
	if err != nil {
		_ = head.Close()
		return nil, nil, err
	}
	return log, head, nil
}

func (p *protectedProfile) Close() error {
	if p == nil {
		return nil
	}
	var err error
	if p.captureClient != nil {
		err = p.captureClient.close()
	}
	clear(p.key[:])
	if p.journalDomain != nil {
		err = errors.Join(err, p.journalDomain.root.Close())
	}
	for _, root := range []*paths.Root{p.control, p.maintenanceControl, p.state, p.root} {
		if root != nil {
			err = errors.Join(err, root.Close())
		}
	}
	for _, root := range p.targets {
		err = errors.Join(err, root.Close())
	}
	return err
}

// Profile workers call this before touching any state. AllThreadsSyscall is
// required: PR_SET_NO_NEW_PRIVS on just one Go thread is not a process boundary.
func HardenProtectedProfileWorker() error {
	_, _, errno := syscall.AllThreadsSyscall6(syscall.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0)
	if errno != 0 {
		return ErrProfileDenied
	}
	return nil
}

func sortedProfileTargets(input []string) []string {
	out := append([]string(nil), input...)
	sort.Strings(out)
	return out
}
