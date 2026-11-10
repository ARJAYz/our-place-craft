package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func packZip(t *testing.T, files map[string]string) string {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	p := filepath.Join(t.TempDir(), "pack.zip")
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p
}

func TestImportModrinthPack(t *testing.T) {
	srv := fakeWorld(t)
	mod := []byte("modjar")
	idx := map[string]any{"formatVersion": 1, "game": "minecraft", "name": "Cool Pack", "versionId": "1.2",
		"dependencies": map[string]string{"minecraft": "1.21.11", "fabric-loader": "0.16.9"},
		"files": []any{
			map[string]any{"path": "mods/mod.jar", "hashes": map[string]string{"sha1": sha(mod)}, "downloads": []string{srv.URL + "/mod.jar"}, "fileSize": len(mod)},
			map[string]any{"path": "mods/server-only.jar", "env": map[string]string{"client": "unsupported"}, "downloads": []string{srv.URL + "/nope"}},
			map[string]any{"path": "../../evil.jar", "downloads": []string{srv.URL + "/mod.jar"}},
		}}
	b, _ := json.Marshal(idx)
	p := packZip(t, map[string]string{"modrinth.index.json": string(b), "overrides/config/a.toml": "x=1", "client-overrides/options.txt": "fov:1", "overrides/../../bad.txt": "no"})
	a := newApp(t.TempDir())
	res, err := a.importPack(p)
	if err != nil {
		t.Fatal(err)
	}
	in, _ := a.store.Get(res.ID)
	if in.MC != "1.21.11" || in.Loader != LoaderFabric || in.LoaderVersion != "0.16.9" || in.Name != "Cool Pack 1.2" {
		t.Fatalf("%+v", in)
	}
	g := a.store.GameDir(in)
	for _, f := range []string{"mods/mod.jar", "config/a.toml", "options.txt"} {
		if !fileExists(filepath.Join(g, f)) {
			t.Fatalf("missing %s", f)
		}
	}
	if fileExists(filepath.Join(g, "mods/server-only.jar")) || fileExists(filepath.Join(filepath.Dir(filepath.Dir(g)), "evil.jar")) {
		t.Fatal("server-only or escaping file written")
	}
}

func TestImportCurseForgePack(t *testing.T) {
	jar := []byte("cfjar")
	var gotKey atomic.Value
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/mods/files":
			gotKey.Store(r.Header.Get("x-api-key"))
			fmt.Fprintf(w, `{"data":[{"id":11,"displayName":"JEI","fileName":"jei.jar","downloadUrl":"%s/jei.jar","fileLength":%d,"hashes":[{"value":"%s","algo":1}]},{"id":12,"displayName":"NoDist","fileName":"nd.jar","downloadUrl":null}]}`,
				"http://"+r.Host, len(jar), sha(jar))
		case "/jei.jar":
			w.Write(jar)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cf.Close()
	cfAPI = cf.URL + "/v1"
	man := `{"name":"CF Pack","version":"3","overrides":"overrides","minecraft":{"version":"1.20.1","modLoaders":[{"id":"forge-47.3.0","primary":true}]},
"files":[{"projectID":1,"fileID":11,"required":true},{"projectID":2,"fileID":12,"required":true}]}`
	p := packZip(t, map[string]string{"manifest.json": man, "overrides/config/x.cfg": "y"})
	a := newApp(t.TempDir())
	// without a key: everything is listed for manual download, the instance is still created
	res, err := a.importPack(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Manual) != 2 || res.Mods != 0 {
		t.Fatalf("%+v", res)
	}
	in, _ := a.store.Get(res.ID)
	if in.Loader != LoaderForge || in.LoaderVersion != "1.20.1-47.3.0" {
		t.Fatalf("%+v", in)
	}
	// with a key
	a.SetCFKey("KEY")
	res, err = a.importPack(p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mods != 1 || len(res.Manual) != 1 || res.Manual[0].URL != "https://www.curseforge.com/projects/2" || gotKey.Load() != "KEY" {
		t.Fatalf("%+v", res)
	}
	in, _ = a.store.Get(res.ID)
	if !fileExists(filepath.Join(a.store.GameDir(in), "mods", "jei.jar")) || !fileExists(filepath.Join(a.store.GameDir(in), "config", "x.cfg")) {
		t.Fatal("cf files not installed")
	}
}

func TestSafeJoin(t *testing.T) {
	base := t.TempDir()
	for _, bad := range []string{"../x", "../../etc/passwd", "/", ""} {
		if p, ok := safeJoin(base, bad); ok && !strings.HasPrefix(p, base) {
			t.Fatalf("%s escaped: %s", bad, p)
		}
	}
	if p, ok := safeJoin(base, "mods/a.jar"); !ok || p != filepath.Join(base, "mods", "a.jar") {
		t.Fatal(p)
	}
}
