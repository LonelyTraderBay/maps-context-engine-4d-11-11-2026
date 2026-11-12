package policy

import (
	"errors"
	"strings"
	"testing"
)

func TestDenySensitivePathsBeforeOpen(t *testing.T) {
	for _, p := range []string{".env", "app/.env.production", ".ssh/config", "config/private.key", "keys/id_rsa", "wallet.dat", "dump.dmp", "../escape"} {
		if !errors.Is(CheckPath(p), ErrDeniedPath) {
			t.Fatalf("sensitive path accepted: %s", p)
		}
	}
	for _, p := range []string{".env.example", "example/.env.sample", "src/main.go"} {
		if err := CheckPath(p); err != nil {
			t.Fatalf("ordinary path rejected: %s: %v", p, err)
		}
	}
}
func TestSecretCheckPrecedesPersistence(t *testing.T) {
	scan, err := NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	marker := "TESTONLY" + strings.Repeat("X", 16)
	for _, text := range []string{"password=" + marker, "mixed source\napi_key=\"" + marker + "\"", "-----BEGIN PRIVATE KEY-----\n" + marker} {
		err := scan.CheckContent([]byte(text))
		if !errors.Is(err, ErrSecretBlocked) {
			t.Fatal("secret fixture accepted")
		}
		if strings.Contains(err.Error(), marker) {
			t.Fatal("error leaked content")
		}
	}
	if err = CheckPath(".env.example"); err != nil {
		t.Fatal(err)
	}
	if err = scan.CheckContent([]byte("PASSWORD=" + marker)); !errors.Is(err, ErrSecretBlocked) {
		t.Fatal("example containing secret accepted")
	}
	if err = scan.CheckContent([]byte("package main\nconst mode = \"example\"\n")); err != nil {
		t.Fatal(err)
	}
	if err = scan.CheckContent(make([]byte, 1025)); !errors.Is(err, ErrContentLimit) {
		t.Fatal("oversize accepted")
	}
	if err = scan.CheckContent([]byte{0xff}); !errors.Is(err, ErrInvalidText) {
		t.Fatal("invalid UTF-8 accepted")
	}
}
