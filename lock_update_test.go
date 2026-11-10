package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServersLockedWithoutCode(t *testing.T) {
	old := stampedCode
	stampedCode = ""
	featuredMu.Lock()
	featuredServers = nil
	featuredMu.Unlock()
	defer func() { stampedCode = old; newApp(t.TempDir()) }()
	t.Setenv("PUBLIC", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	a := newApp(t.TempDir())
	if !a.State().Locked || len(a.Servers()) != 0 {
		t.Fatal("servers visible without a code")
	}
	if _, err := a.store.Get("transnistria"); err == nil {
		t.Fatal("server instance created while locked")
	}
	if err := a.Unlock("wrong"); err == nil {
		t.Fatal("wrong code accepted")
	}
	if err := a.Unlock("CODE"); err != nil {
		t.Fatal(err)
	}
	if a.State().Locked || len(a.Servers()) != 2 {
		t.Fatal("not unlocked")
	}
	if a.store.Settings().Code != "CODE" || a.State().Settings.Code != "" {
		t.Fatal("code must be saved but never sent to the page")
	}
	// a restart remembers it
	featuredMu.Lock()
	featuredServers = nil
	featuredMu.Unlock()
	b := newApp(a.store.Root)
	if b.State().Locked {
		t.Fatal("code not remembered")
	}
}

func TestSelfUpdate(t *testing.T) {
	newExe := []byte("NEW LAUNCHER BUILD")
	zipped := zipBytes(map[string]string{"OurPlaceCraftLauncher-Setup.exe": string(newExe)})
	sum := sha256.Sum256(zipped)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/launcher.json":
			fmt.Fprintf(w, `{"version":"20261030-1600","sha256":"%s","size":%d}`, hex.EncodeToString(sum[:]), len(zipped))
		case "/" + updateAsset:
			w.Write(zipped)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldBase, oldVer := launcherUpdateBase, launcherVersion
	launcherUpdateBase, launcherVersion = srv.URL+"/", "20261030-1500"
	defer func() { launcherUpdateBase, launcherVersion = oldBase, oldVer }()

	a := newApp(t.TempDir())
	u := a.CheckUpdate()
	if u == nil || a.State().Update == nil {
		t.Fatal("update not found")
	}
	self := filepath.Join(t.TempDir(), installExe)
	os.WriteFile(self, []byte("OLD"), 0o755)
	if err := a.installUpdate(self, u); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(self); string(b) != string(newExe) {
		t.Fatal("exe not replaced")
	}
	cleanupOldExe(self)
	if fileExists(self + ".old") {
		t.Fatal(".old left")
	}
	// a damaged download keeps the old exe
	u.SHA256 = "00"
	if err := a.installUpdate(self, u); err == nil {
		t.Fatal("bad checksum accepted")
	}
	if b, _ := os.ReadFile(self); string(b) != string(newExe) {
		t.Fatal("exe damaged by failed update")
	}
	// same or older version: nothing offered
	launcherVersion = "20261030-1600"
	b := newApp(t.TempDir())
	if b.CheckUpdate() != nil {
		t.Fatal("offered the same version")
	}
	launcherVersion = "dev"
	if b.CheckUpdate() != nil {
		t.Fatal("dev build must not self-update")
	}
}
