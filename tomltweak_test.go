package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetTOMLKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config", "x.toml")
	if err := setTOMLKeys(p, map[string]string{"visible": "false", "acceptServerConfig": "false"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "acceptServerConfig = false\nvisible = false\n" {
		t.Fatalf("%q", b)
	}
	os.WriteFile(p, []byte("enabled = true\nvisible = false\nfadeStart = 10\n[sub]\nvisible = 1\n"), 0o644)
	setTOMLKeys(p, map[string]string{"visible": "true", "acceptServerConfig": "true"})
	b, _ = os.ReadFile(p)
	want := "enabled = true\nvisible = true\nfadeStart = 10\nacceptServerConfig = true\n[sub]\nvisible = 1\n"
	if string(b) != want {
		t.Fatalf("%q", b)
	}
	if !strings.Contains(string(b), "fadeStart") {
		t.Fatal()
	}
}
