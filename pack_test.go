package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"opcl/packcrypt"
)

func TestPackInstallAndUpdate(t *testing.T) {
	ver := "20261030-1200"
	mk := func(files map[string]string) []byte {
		var enc bytes.Buffer
		if err := packcrypt.Encrypt(&enc, bytes.NewReader(zipBytes(files)), "CODE"); err != nil {
			t.Fatal(err)
		}
		return enc.Bytes()
	}
	bin := mk(map[string]string{"mods/a.jar": "A", "config/x.toml": "1", "options.txt": "pack"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pack.json":
			fmt.Fprintf(w, `{"version":%q}`, ver)
		case "/pack.bin":
			w.Write(bin)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	packBase = srv.URL + "/"
	metaLatest = PackMeta{}

	a := newApp(t.TempDir())
	if err := a.installPack("WRONG"); err == nil {
		t.Fatal("wrong code accepted")
	}
	// the instance exists, but nothing is marked installed
	if a.PackInfo().Installed {
		t.Fatal("installed with a wrong code")
	}
	in, err := a.store.ensurePackInstance()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.syncPackIfNeeded(in, "CODE", &Progress{}); err != nil {
		t.Fatal(err)
	}
	g := a.store.GameDir(in)
	if g != filepath.Join(a.store.Root, "instances", adventureID, "game") || !fileExists(filepath.Join(g, "mods", "a.jar")) {
		t.Fatal("pack not unpacked into", g)
	}
	pi := a.PackInfo()
	if !pi.Installed || pi.Version != ver || !pi.HasCode {
		t.Fatalf("%+v", pi)
	}
	// same version: nothing to do (works offline too)
	packBase = "http://127.0.0.1:1/"
	metaLatest = PackMeta{}
	if up, err := a.store.syncPackIfNeeded(in, "", &Progress{}); err != nil || up {
		t.Fatal(up, err)
	}
	// a new release: the next start syncs it with the saved code, player's options stay
	os.WriteFile(filepath.Join(g, "options.txt"), []byte("mine"), 0o644)
	packBase = srv.URL + "/"
	metaLatest = PackMeta{}
	ver = "20261031-0900"
	bin = mk(map[string]string{"mods/b.jar": "B", "options.txt": "pack"})
	if up, err := a.store.syncPackIfNeeded(in, "", &Progress{}); err != nil || !up {
		t.Fatal(up, err)
	}
	if fileExists(filepath.Join(g, "mods", "a.jar")) || !fileExists(filepath.Join(g, "mods", "b.jar")) {
		t.Fatal("mods not updated")
	}
	if b, _ := os.ReadFile(filepath.Join(g, "options.txt")); string(b) != "mine" {
		t.Fatal("player's options overwritten")
	}
	if loadConfig(a.store.packCfgRoot(in)).PackVersion != ver {
		t.Fatal("version not saved")
	}
	// tweaks of the pack live in its own launcher.json
	if err := a.SetTweak(adventureID, "journeymap", false); err != nil {
		t.Fatal(err)
	}
	tv, _ := a.Tweaks(adventureID)
	for _, x := range tv {
		if x.ID == "journeymap" && x.On {
			t.Fatal("tweak not saved for the pack")
		}
	}
}
