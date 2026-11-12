package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRuntimeConfigDataDigest(t *testing.T) {
	config, err := DecodeRuntimeConfigJSON([]byte(`{"schema_version":1}`), 16384, 4)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := RuntimeConfigDigest(config, 16384)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`{"operation":"runtime_config","config":%s,"config_digest":"%s","restart_required":false}`, raw, digest)
	if _, err := DecodeRuntimeConfigDataJSON([]byte(data), 16384, 4); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(data, digest, strings.Repeat("a", 64), 1)
	if _, err := DecodeRuntimeConfigDataJSON([]byte(bad), 16384, 4); err == nil {
		t.Fatal("accepted mismatched digest")
	}
}
