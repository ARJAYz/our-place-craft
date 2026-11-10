package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"opcl/packcrypt"
)

func TestRepairCustom(t *testing.T) {
	fakeWorld(t)
	ruleOSName, javaGUIName, javaCLIName, classpathSep = "windows", "javaw.exe", "java.exe", ";"
	t.Cleanup(func() { ruleOSName, javaGUIName, javaCLIName, classpathSep = "linux", "java", "java", ":" })
	oldJW := javaWorks
	javaWorks = func(string) bool { return true }
	defer func() { javaWorks = oldJW }()

	a := newApp(t.TempDir())
	in, _ := a.store.Create(NewInstance{Name: "R", MC: "1.21.11", Loader: LoaderFabric, LoaderVersion: "0.16.9"})
	if _, err := a.store.Prepare(in, &Progress{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	in, _ = a.store.Get(in.ID)
	client := filepath.Join(a.store.Root, "versions", "1.21.11", "1.21.11.jar")
	good, _ := os.ReadFile(client)
	os.WriteFile(client, []byte("broken!"), 0o644)
	game := a.store.GameDir(in)
	os.WriteFile(filepath.Join(game, "mods", "bad.jar"), []byte("not a zip"), 0o644)
	os.WriteFile(filepath.Join(game, "mods", "good.jar"), zipBytes(map[string]string{"a": "b"}), 0o644)
	os.MkdirAll(filepath.Join(game, "saves", "W"), 0o755)

	rep, err := a.repair(in)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(client); !bytes.Equal(b, good) {
		t.Fatal("client not repaired")
	}
	if len(rep.Redownloaded) != 1 || rep.Redownloaded[0] != "1.21.11.jar" {
		t.Fatalf("%+v", rep)
	}
	if len(rep.BrokenMods) != 1 || rep.BrokenMods[0] != "bad.jar" || rep.Checked < 3 {
		t.Fatalf("%+v", rep)
	}
	if !fileExists(filepath.Join(game, "saves", "W")) {
		t.Fatal("world touched")
	}
	// a second run finds nothing
	rep, err = a.repair(in)
	if err != nil || len(rep.Redownloaded) != 0 {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestRepairPack(t *testing.T) {
	var enc bytes.Buffer
	packcrypt.Encrypt(&enc, bytes.NewReader(zipBytes(map[string]string{
		"mods/a.jar": "A", "mods/journeymap-1.jar": "J", "config/x.toml": "1", "options.txt": "pack"})), "CODE")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pack.json":
			fmt.Fprint(w, `{"version":"20261030-1200"}`)
		case "/pack.bin":
			w.Write(enc.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	packBase, metaLatest = srv.URL+"/", PackMeta{}
	defer func() { packBase, metaLatest = "http://127.0.0.1:1/", PackMeta{} }()

	a := newApp(t.TempDir())
	in, _ := a.store.ensurePackInstance()
	if _, err := a.store.syncPackIfNeeded(in, "CODE", &Progress{}); err != nil {
		t.Fatal(err)
	}
	g := a.store.GameDir(in)
	if loadManifest(g) == nil {
		t.Fatal("no manifest")
	}
	// healthy: nothing to do, nothing downloaded
	rep := &RepairReport{}
	if err := a.repairPack(in, g, rep); err != nil || len(rep.Restored)+len(rep.Removed) != 0 || rep.Checked != 4 {
		t.Fatalf("%+v %v", rep, err)
	}
	// break things
	os.Remove(filepath.Join(g, "mods", "a.jar"))
	os.WriteFile(filepath.Join(g, "config", "x.toml"), []byte("changed"), 0o644)
	os.WriteFile(filepath.Join(g, "options.txt"), []byte("mine"), 0o644)
	os.WriteFile(filepath.Join(g, "mods", "xray.jar"), []byte("X"), 0o644)
	os.Rename(filepath.Join(g, "mods", "journeymap-1.jar"), filepath.Join(g, "mods", "journeymap-1.jar.off")) // tweak off
	cfg := loadConfig(a.store.packCfgRoot(in))
	cfg.Tweaks = map[string]bool{"journeymap": false}
	saveConfig(a.store.packCfgRoot(in), cfg)
	os.MkdirAll(filepath.Join(g, tweaksDir, "xaero_mini"), 0o755)
	os.WriteFile(filepath.Join(g, tweaksDir, "xaero_mini", "xaerominimap.jar"), []byte("XM"), 0o644)
	os.WriteFile(filepath.Join(g, "mods", "xaerominimap.jar"), []byte("XM"), 0o644)
	os.MkdirAll(filepath.Join(g, "saves", "W"), 0o755)

	rep = &RepairReport{}
	if err := a.repairPack(in, g, rep); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(g, "config", "x.toml")); string(b) != "1" || !fileExists(filepath.Join(g, "mods", "a.jar")) {
		t.Fatal("pack files not restored")
	}
	if b, _ := os.ReadFile(filepath.Join(g, "options.txt")); string(b) != "mine" {
		t.Fatal("player's options overwritten")
	}
	if fileExists(filepath.Join(g, "mods", "xray.jar")) || !fileExists(filepath.Join(g, "mods-removed", "mods", "xray.jar")) {
		t.Fatal("extra mod not moved aside")
	}
	if fileExists(filepath.Join(g, "mods", "journeymap-1.jar")) {
		t.Fatal("switched-off tweak turned back on")
	}
	if len(rep.Removed) != 1 || rep.Removed[0] != "xray.jar" || len(rep.Restored) != 2 {
		t.Fatalf("%+v", rep)
	}
	if !fileExists(filepath.Join(g, "saves", "W")) {
		t.Fatal("world touched")
	}
}

func TestMigrateLegacy(t *testing.T) {
	pub := t.TempDir()
	t.Setenv("PUBLIC", pub)
	old := filepath.Join(pub, "OurPlaceCraft")
	w := func(rel, data string) {
		p := filepath.Join(old, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(data), 0o644)
	}
	w("launcher.json", `{"nick":"Steve","ram_mb":8192,"pack_code":"CODE","pack_version":"20261030-1200","tweaks":{"xaero":true}}`)
	w("versions/"+adventureNeoForge+"/"+adventureNeoForge+".json", `{"id":"x"}`)
	w("libraries/net/x/x.jar", "L")
	w("assets/objects/ab/abc", "A")
	w("runtime/bin/"+javaGUIName, "J")
	w("game/saves/World/level.dat", "W")
	w("game/mods/a.jar", "A")
	w("game/options.txt", "keys")
	w("Our Place Craft - Adventure.exe", "EXE")

	removed := ""
	removeOldLauncher = func(dir string) { removed = dir; os.RemoveAll(dir) }
	defer func() { removeOldLauncher = func(string) {} }()

	a := newApp(t.TempDir()) // links the old install first, like a real start
	if in, _ := a.store.Get(adventureID); in == nil || in.Kind != KindAdventure {
		t.Fatal("old install not linked")
	}
	if err := a.migrateLegacy(old); err != nil {
		t.Fatal(err)
	}
	in, err := a.store.Get(adventureID)
	if err != nil || in.Kind != KindPack || in.RamMB != 8192 || in.VersionID != adventureNeoForge {
		t.Fatalf("%+v %v", in, err)
	}
	g := a.store.GameDir(in)
	for _, f := range []string{"saves/World/level.dat", "mods/a.jar", "options.txt"} {
		if !fileExists(filepath.Join(g, filepath.FromSlash(f))) {
			t.Fatal("not moved:", f)
		}
	}
	for _, f := range []string{"libraries/net/x/x.jar", "assets/objects/ab/abc", "versions/" + adventureNeoForge + "/" + adventureNeoForge + ".json"} {
		if !fileExists(filepath.Join(a.store.Root, filepath.FromSlash(f))) {
			t.Fatal("not reused:", f)
		}
	}
	if javaFor(a.store.Root, 21, javaGUIName, "") == "" {
		t.Fatal("java not reused")
	}
	cfg := loadConfig(a.store.packCfgRoot(in))
	if cfg.PackVersion != "20261030-1200" || !cfg.Tweaks["xaero"] {
		t.Fatalf("config not moved: %+v", cfg)
	}
	if removed != old || fileExists(old) {
		t.Fatal("old launcher not removed")
	}
	if a.store.Settings().Nick != "Steve" {
		t.Fatal("nick")
	}
	// a restart does not bring the old instance back
	b := newApp(a.store.Root)
	if in, _ := b.store.Get(adventureID); in.Kind != KindPack {
		t.Fatal("instance reverted")
	}
	if legacyRoot() != "" {
		t.Fatal("legacy still found")
	}
}

func TestBannedModsNeverInstalled(t *testing.T) {
	var enc bytes.Buffer
	packcrypt.Encrypt(&enc, bytes.NewReader(zipBytes(map[string]string{
		"mods/a.jar": "A", "mods/carryon-neoforge-1.21.1-2.2.4.4.jar": "C", "config/carryon-client.toml": "x"})), "CODE")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pack.json":
			fmt.Fprint(w, `{"version":"20261103-1200"}`)
		case "/pack.bin":
			w.Write(enc.Bytes())
		}
	}))
	defer srv.Close()
	packBase, metaLatest = srv.URL+"/", PackMeta{}
	defer func() { packBase, metaLatest = "http://127.0.0.1:1/", PackMeta{} }()
	a := newApp(t.TempDir())
	in, _ := a.store.ensurePackInstance()
	g := a.store.GameDir(in)
	os.MkdirAll(filepath.Join(g, "mods"), 0o755)
	os.WriteFile(filepath.Join(g, "mods", "carryon-neoforge-1.21.1-2.2.4.3.jar"), []byte("old"), 0o644) // left from before
	if _, err := a.store.syncPackIfNeeded(in, "CODE", &Progress{}); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(g, "mods", "carryon*")); len(m) != 0 || !fileExists(filepath.Join(g, "mods", "a.jar")) {
		t.Fatal("carryon installed:", m)
	}
	if _, ok := loadManifest(g)["mods/carryon-neoforge-1.21.1-2.2.4.4.jar"]; ok {
		t.Fatal("carryon in manifest")
	}
	rep := &RepairReport{}
	if err := a.repairPack(in, g, rep); err != nil || len(rep.Restored) != 0 {
		t.Fatalf("repair brought something back: %+v %v", rep, err)
	}
	if m, _ := filepath.Glob(filepath.Join(g, "mods", "carryon*")); len(m) != 0 {
		t.Fatal("repair restored carryon")
	}
}
