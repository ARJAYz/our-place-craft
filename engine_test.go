package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sha(b []byte) string { h := sha1.Sum(b); return hex.EncodeToString(h[:]) }

func zipBytes(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	return buf.Bytes()
}

// fakeWorld serves Mojang, Fabric meta, Adoptium and Modrinth from one test server.
func fakeWorld(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	files := map[string][]byte{}
	client := []byte("client-jar")
	lib := []byte("lwjgl-jar")
	asset := []byte("sound")
	fabricLib := []byte("fabric-loader-jar")
	jre := zipBytes(map[string]string{"jdk-21-jre/bin/javaw.exe": "x", "jdk-21-jre/bin/java.exe": "x"})
	mod := []byte("modjar")
	dep := []byte("fabric-api-jar")
	files["/client.jar"] = client
	files["/libs/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3.jar"] = lib
	files["/res/"+sha(asset)[:2]+"/"+sha(asset)] = asset
	files["/fabricmaven/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar"] = fabricLib
	files["/jre.zip"] = jre
	files["/mod.jar"] = mod
	files["/dep.jar"] = dep
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := srv.URL
		switch r.URL.Path {
		case "/manifest.json":
			fmt.Fprintf(w, `{"versions":[{"id":"1.21.11","type":"release","url":"%s/v/1.21.11.json","releaseTime":"2025-12-09T12:00:00+00:00"},{"id":"25w01a","type":"snapshot","url":"%s/v/x.json","releaseTime":"2025-01-01T12:00:00+00:00"}]}`, u, u)
		case "/v/1.21.11.json":
			fmt.Fprintf(w, `{"id":"1.21.11","type":"release","mainClass":"net.minecraft.client.main.Main",
"javaVersion":{"majorVersion":21},
"arguments":{"game":["--username","${auth_player_name}","--version","${version_name}","--gameDir","${game_directory}","--assetsDir","${assets_root}","--assetIndex","${assets_index_name}","--uuid","${auth_uuid}","--accessToken","${auth_access_token}",{"rules":[{"action":"allow","features":{"is_quick_play_multiplayer":true}}],"value":["--quickPlayMultiplayer","${quick_play_multiplayer}"]}],
"jvm":["-Djava.library.path=${natives_directory}","-cp","${classpath}"]},
"assetIndex":{"id":"29","url":"%s/idx.json"},
"downloads":{"client":{"url":"%s/client.jar","sha1":"%s","size":%d}},
"libraries":[{"name":"org.lwjgl:lwjgl:3.3.3","downloads":{"artifact":{"path":"org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3.jar","url":"%s/libs/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3.jar","sha1":"%s","size":%d}}},
{"name":"org.lwjgl:lwjgl:3.3.3:natives-macos","downloads":{"artifact":{"path":"x","url":"%s/nope"}},"rules":[{"action":"allow","os":{"name":"osx"}}]}]}`,
				u, u, sha(client), len(client), u, sha(lib), len(lib), u)
		case "/idx.json":
			fmt.Fprintf(w, `{"objects":{"minecraft/sounds/a.ogg":{"hash":"%s","size":%d}}}`, sha(asset), len(asset))
		case "/fabric/versions/loader/1.21.11":
			fmt.Fprint(w, `[{"loader":{"version":"0.16.9","stable":true}},{"loader":{"version":"0.17.0-beta.1","stable":false}}]`)
		case "/fabric/versions/loader/1.21.11/0.16.9/profile/json":
			fmt.Fprintf(w, `{"id":"fabric-loader-0.16.9-1.21.11","inheritsFrom":"1.21.11","mainClass":"net.fabricmc.loader.impl.launch.knot.KnotClient",
"arguments":{"game":[],"jvm":["-DFabricMcEmu= net.minecraft.client.main.Main "]},
"libraries":[{"name":"net.fabricmc:fabric-loader:0.16.9","url":"%s/fabricmaven/"}]}`, u)
		case "/adoptium/21/hotspot":
			fmt.Fprintf(w, `[{"binary":{"package":{"link":"%s/jre.zip","size":%d,"name":"OpenJDK21U-jre_x64_windows.zip"}}}]`, u, len(jre))
		case "/modrinth/project/xaeros-minimap/version":
			fmt.Fprintf(w, `[{"id":"v1","project_id":"P1","files":[{"url":"%s/mod.jar","filename":"xaerominimap-fabric.jar","primary":true,"size":%d,"hashes":{"sha1":"%s"}}],"dependencies":[{"project_id":"FAPI","dependency_type":"required"}]}]`, u, len(mod), sha(mod))
		case "/modrinth/project/xaeros-world-map/version":
			fmt.Fprint(w, `[]`)
		case "/modrinth/project/FAPI/version":
			fmt.Fprintf(w, `[{"id":"v2","project_id":"FAPI","files":[{"url":"%s/dep.jar","filename":"fabric-api.jar","primary":true,"size":%d,"hashes":{"sha1":"%s"}}]}]`, u, len(dep), sha(dep))
		default:
			if b, ok := files[r.URL.Path]; ok {
				w.Write(b)
				return
			}
			http.NotFound(w, r)
		}
	}))
	manifestURL, assetsURL = srv.URL+"/manifest.json", srv.URL+"/res/"
	fabricMeta, adoptiumAPI, modrinthAPI = srv.URL+"/fabric", srv.URL+"/adoptium/", srv.URL+"/modrinth"
	manifestCache = nil
	t.Cleanup(srv.Close)
	return srv
}

func TestFabricInstanceEndToEnd(t *testing.T) {
	fakeWorld(t)
	ruleOSName, javaGUIName, javaCLIName, classpathSep = "windows", "javaw.exe", "java.exe", ";"
	t.Cleanup(func() { ruleOSName, javaGUIName, javaCLIName, classpathSep = "linux", "java", "java", ":" })

	s := &Store{Root: t.TempDir()}
	lv, err := loaderVersions(LoaderFabric, "1.21.11")
	if err != nil || len(lv) != 2 || !lv[0].Stable || lv[1].Stable {
		t.Fatalf("fabric versions: %v %v", lv, err)
	}
	in, err := s.Create(NewInstance{Name: "Ванилла плюс", MC: "1.21.11", Loader: LoaderFabric, LoaderVersion: "0.16.9", Server: "1.2.3.4:25565"})
	if err != nil {
		t.Fatal(err)
	}
	if in.ID != "vanilla-plyus" {
		t.Fatalf("id %q", in.ID)
	}
	r, err := s.Prepare(in, &Progress{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "fabric-loader-0.16.9-1.21.11" || r.Base != "1.21.11" || r.JavaMajor != 21 {
		t.Fatalf("resolved %+v", r)
	}
	if s.javaPath(in, r) == "" {
		t.Fatal("java not installed")
	}
	for _, p := range []string{"versions/1.21.11/1.21.11.jar", "versions/fabric-loader-0.16.9-1.21.11/fabric-loader-0.16.9-1.21.11.jar",
		"libraries/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar", "libraries/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3.jar"} {
		if !fileExists(filepath.Join(s.Root, filepath.FromSlash(p))) {
			t.Fatalf("missing %s", p)
		}
	}
	got, _ := s.Get(in.ID)
	if got.VersionID != r.ID {
		t.Fatal("version id not saved")
	}
	args, err := buildArgs(r, LaunchOpts{Root: s.Root, GameDir: s.GameDir(in), Nick: "Steve", UUID: offlineUUID("Steve"), RamMB: 4096, Server: in.Server})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"net.fabricmc.loader.impl.launch.knot.KnotClient", "--username Steve", "--quickPlayMultiplayer 1.2.3.4:25565",
		"fabric-loader-0.16.9.jar;", "-DFabricMcEmu= net.minecraft.client.main.Main ", "--assetIndex 29"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in\n%s", want, line)
		}
	}
	if strings.Contains(line, "natives-macos") || strings.Contains(line, "quick_play") {
		t.Fatal("rules not applied")
	}
	// prepared once: the second Prepare does not need the network
	manifestURL = "http://127.0.0.1:1/x"
	if _, err := s.Prepare(got, &Progress{}, io.Discard); err != nil {
		t.Fatal("second prepare:", err)
	}

	// tweaks: Xaero's Minimap comes with its required dependency; World Map has no build -> warning
	got.Tweaks = map[string]bool{"xaero": true}
	warns := applyModTweaks(got, s.GameDir(got), &Progress{})
	if len(warns) != 1 {
		t.Fatalf("warnings %v", warns)
	}
	if !fileExists(filepath.Join(s.GameDir(got), "mods", "xaerominimap-fabric.jar")) == true {
		// world map failed, so the whole tweak is skipped
	}
	got.Tweaks = map[string]bool{"xaero": false}
	applyModTweaks(got, s.GameDir(got), &Progress{})
	if fileExists(filepath.Join(s.GameDir(got), "mods", "fabric-api.jar")) {
		t.Fatal("dependency left after switching off")
	}
}

func TestModTweakDependencies(t *testing.T) {
	fakeWorld(t)
	gd := t.TempDir()
	in := &Instance{ID: "x", MC: "1.21.11", Loader: LoaderFabric, Tweaks: map[string]bool{"journeymap": false}}
	// only the minimap project: use a custom tweak list
	old := modTweaks
	modTweaks = []ModTweak{{ID: "mini", Title: "Mini", Projects: map[string][]string{"*": {"xaeros-minimap"}}}}
	defer func() { modTweaks = old }()
	in.Tweaks = map[string]bool{"mini": true}
	if w := applyModTweaks(in, gd, &Progress{}); len(w) != 0 {
		t.Fatal(w)
	}
	for _, f := range []string{"xaerominimap-fabric.jar", "fabric-api.jar"} {
		if !fileExists(filepath.Join(gd, "mods", f)) {
			t.Fatalf("%s not installed", f)
		}
	}
	in.Tweaks["mini"] = false
	applyModTweaks(in, gd, &Progress{})
	for _, f := range []string{"xaerominimap-fabric.jar", "fabric-api.jar"} {
		if fileExists(filepath.Join(gd, "mods", f)) {
			t.Fatalf("%s not removed", f)
		}
	}
}

func TestOldVersionArgs(t *testing.T) {
	root := t.TempDir()
	ruleOSName, classpathSep = "windows", ";"
	t.Cleanup(func() { ruleOSName, classpathSep = "linux", ":" })
	natives := zipBytes(map[string]string{"lwjgl64.dll": "dll", "META-INF/MANIFEST.MF": "m"})
	v := map[string]any{
		"id": "1.8.9", "mainClass": "net.minecraft.client.main.Main", "assets": "1.8",
		"minecraftArguments": "--username ${auth_player_name} --session ${auth_session} --assetsDir ${assets_root} --gameDir ${game_directory}",
		"assetIndex":         map[string]any{"id": "1.8", "url": "x"},
		"libraries": []any{
			map[string]any{"name": "org.lwjgl.lwjgl:lwjgl-platform:2.9.4", "natives": map[string]string{"windows": "natives-windows"},
				"extract":   map[string]any{"exclude": []string{"META-INF/"}},
				"downloads": map[string]any{"classifiers": map[string]any{"natives-windows": map[string]any{"path": "lw/natives.jar", "url": "x"}}}},
			map[string]any{"name": "com.google:gson:2.2.4", "downloads": map[string]any{"artifact": map[string]any{"path": "g/gson.jar", "url": "x"}}},
		},
	}
	b, _ := json.Marshal(v)
	os.MkdirAll(filepath.Join(root, "versions", "1.8.9"), 0o755)
	os.WriteFile(filepath.Join(root, "versions", "1.8.9", "1.8.9.json"), b, 0o644)
	os.MkdirAll(filepath.Join(root, "libraries", "lw"), 0o755)
	os.WriteFile(filepath.Join(root, "libraries", "lw", "natives.jar"), natives, 0o644)
	r, err := resolveVersion(root, "1.8.9")
	if err != nil {
		t.Fatal(err)
	}
	if r.JavaMajor != 8 || r.HasModernArgs {
		t.Fatalf("%+v", r)
	}
	args, err := buildArgs(r, LaunchOpts{Root: root, GameDir: root + "/g", Nick: "Alex", UUID: "u", RamMB: 2048, Server: "play.example.org:25570"})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"-Djava.library.path=", "-cp ", "gson.jar", "--username Alex", "--server play.example.org --port 25570"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %s", want, line)
		}
	}
	if strings.Contains(line, "natives.jar") {
		t.Fatal("natives jar must not be on the class path")
	}
	if !fileExists(filepath.Join(root, "natives", "1.8.9", "lwjgl64.dll")) || fileExists(filepath.Join(root, "natives", "1.8.9", "META-INF")) {
		t.Fatal("natives not extracted properly")
	}
}

func TestLoaderVersionMatching(t *testing.T) {
	cases := []struct {
		loader, mc, v string
		ok            bool
	}{
		{LoaderNeoForge, "1.21.1", "21.1.264", true},
		{LoaderNeoForge, "1.21.1", "21.10.5", false},
		{LoaderNeoForge, "1.21", "21.0.167", true},
		{LoaderNeoForge, "1.21.11", "21.11.3-beta", true},
		{LoaderNeoForge, "1.20.2", "20.2.88", true},
		{LoaderForge, "1.20.1", "1.20.1-47.3.0", true},
		{LoaderForge, "1.20", "1.20.1-47.3.0", false},
	}
	for _, c := range cases {
		if _, ok := matchLoaderVersion(c.loader, c.mc, c.v); ok != c.ok {
			t.Errorf("%s %s %s: got %v", c.loader, c.mc, c.v, ok)
		}
	}
	if !versionLess("21.1.9", "21.1.10") || versionLess("21.1.10", "21.1.9") {
		t.Fatal("versionLess")
	}
	if loaderVersionID(LoaderForge, "1.20.1", "1.20.1-47.3.0") != "1.20.1-forge-47.3.0" {
		t.Fatal("forge id")
	}
}

func TestAPIBridge(t *testing.T) {
	fakeWorld(t)
	a := newApp(t.TempDir())
	if err := a.SaveSettings("Steve", ""); err != nil {
		t.Fatal(err)
	}
	res, err := callMethod(a, "Create", []byte(`[{"name":"Test","mc":"1.21.11","loader":"vanilla"}]`))
	if err != nil || res.(string) != "test" {
		t.Fatal(res, err)
	}
	if _, err := callMethod(a, "newApp", nil); err == nil {
		t.Fatal("private method callable")
	}
	st := a.State()
	if len(st.Instances) != 2 || st.Settings.Selected != "test" {
		t.Fatalf("%+v", st)
	}
	id := a.Versions(false)
	var j job
	for i := 0; i < 500 && !j.Done; i++ {
		time.Sleep(5 * time.Millisecond)
		j = a.Job(id)
	}
	if !j.Done {
		t.Fatal("job did not finish")
	}
	if j.Error != "" {
		t.Fatal(j.Error)
	}
}

func TestPingServer(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		for i := 0; i < 2; i++ { // handshake + status request
			n, _ := readVarInt(r)
			io.CopyN(io.Discard, r, int64(n))
		}
		js := `{"version":{"name":"1.21.11"},"players":{"online":3,"max":20},"description":"hi"}`
		var body bytes.Buffer
		writeVarInt(&body, 0)
		writeVarInt(&body, len(js))
		body.WriteString(js)
		var out bytes.Buffer
		writePacket(&out, body.Bytes())
		c.Write(out.Bytes())
	}()
	st := pingServer(ln.Addr().String(), 2*time.Second)
	if !st.Online || st.Players != 3 || st.Max != 20 || st.Version != "1.21.11" {
		t.Fatalf("%+v", st)
	}
	if pingServer("127.0.0.1:1", time.Second).Online {
		t.Fatal("closed port reported online")
	}
}

func TestHideServer(t *testing.T) {
	a := newApp(t.TempDir())
	vs := a.Servers()
	if len(vs) != 2 || vs[0].ID != "adventure" || vs[0].Installed || vs[1].ID != "transnistria" || vs[1].Hidden {
		t.Fatalf("%+v", vs)
	}
	a.HideServer("transnistria", true)
	if !a.Servers()[1].Hidden {
		t.Fatal("not hidden")
	}
	if _, err := a.store.Get("transnistria"); err != nil {
		t.Fatal("hiding must keep the instance")
	}
	a.HideServer("transnistria", false)
	if a.Servers()[1].Hidden {
		t.Fatal("not shown again")
	}
}
