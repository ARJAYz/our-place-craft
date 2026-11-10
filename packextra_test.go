package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPackExtra(t *testing.T) {
	jar := []byte("PK-fake-jar-content")
	h := sha1.Sum(jar)
	x := PackExtra{Version: "1", Files: []PackExtraFile{{Path: "mods/cdg.jar", File: "cdg.jar", SHA1: hex.EncodeToString(h[:]), Size: int64(len(jar))},
		{Path: "../evil.jar", File: "cdg.jar", SHA1: hex.EncodeToString(h[:])}}, Remove: []string{"mods/old-*.jar"}}
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pack-extra.json":
			json.NewEncoder(w).Encode(x)
		case "/pack-extra/cdg.jar":
			hits++
			w.Write(jar)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := launcherUpdateBase
	launcherUpdateBase = srv.URL + "/"
	defer func() { launcherUpdateBase = old }()
	game := t.TempDir()
	os.MkdirAll(filepath.Join(game, "mods"), 0o755)
	os.WriteFile(filepath.Join(game, "mods", "old-1.jar"), []byte("x"), 0o644)
	writeJSON(filepath.Join(game, manifestName), packManifest{})
	p := &Progress{}
	if n, err := applyPackExtra(game, p); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if fileExists(filepath.Join(game, "mods", "old-1.jar")) || fileExists(filepath.Join(filepath.Dir(game), "evil.jar")) {
		t.Fatal("remove/escape")
	}
	if _, ok := loadManifest(game)["mods/cdg.jar"]; !ok {
		t.Fatal("not in manifest")
	}
	if len(extraPackMods(game, loadManifest(game))) != 0 {
		t.Fatal("repair would move it")
	}
	if n, _ := applyPackExtra(game, p); n != 0 || hits != 1 {
		t.Fatal("redownloaded", n, hits)
	}
	// offline: cached json still works, no error
	srv.Close()
	if _, err := applyPackExtra(game, p); err != nil {
		t.Fatal(err)
	}
}
