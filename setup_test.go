package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opcl/packcrypt"
)

func waitJob(t *testing.T, a *App, id int) job {
	for i := 0; i < 600; i++ {
		j := a.Job(id)
		if j.Done {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return job{}
}

func TestSetupInstall(t *testing.T) {
	var enc bytes.Buffer
	packcrypt.Encrypt(&enc, bytes.NewReader(zipBytes(map[string]string{"mods/a.jar": "A"})), "CODE")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pack.json":
			fmt.Fprint(w, `{"version":"20261030-1300"}`)
		case "/pack.bin":
			w.Write(enc.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	fakeWorld(t) // Mojang/Java for the NeoForge part is not faked: Prepare will fail -> warning path
	packBase, metaLatest = srv.URL+"/", PackMeta{}
	var shortcuts, registered string
	setupShortcuts = func(root, exe, ico string, desktop bool) error { shortcuts = fmt.Sprint(root, desktop); return nil }
	setupRegister = func(root, exe, ico string) { registered = exe }
	defer func() {
		setupShortcuts = func(root, exe, ico string, desktop bool) error { return nil }
		setupRegister = func(root, exe, ico string) {}
	}()

	base := t.TempDir()
	a := newSetupApp(filepath.Join(base, "default"))
	if fileExists(filepath.Join(base, "default")) {
		t.Fatal("the installer must not write anything before «Установить»")
	}
	si, err := a.SetupInfo()
	if err != nil || si.Installed {
		t.Fatal(si, err)
	}
	// bad input is refused
	if j := waitJob(t, a, a.Install(SetupOpts{Root: filepath.Join(base, "Папка"), Nick: "Steve"})); !strings.Contains(j.Error, "русские") {
		t.Fatal(j)
	}
	if j := waitJob(t, a, a.Install(SetupOpts{Root: filepath.Join(base, "ok"), Nick: "x"})); !strings.Contains(j.Error, "ник") {
		t.Fatal(j)
	}
	root := filepath.Join(base, "OurPlaceCraftLauncher")
	j := waitJob(t, a, a.Install(SetupOpts{Root: root, Nick: "Steve", Desktop: true, Adventure: true, Code: "CODE"}))
	if !fileExists(filepath.Join(root, installExe)) || registered != filepath.Join(root, installExe) || shortcuts != fmt.Sprint(root, true) {
		t.Fatalf("launcher not installed: %+v %s %s", j, registered, shortcuts)
	}
	if a.store.Settings().Nick != "Steve" {
		t.Fatal("nick not saved")
	}
	// the pack itself was downloaded into the new root even if the game files could not be prepared here
	if !fileExists(filepath.Join(root, "instances", adventureID, "game", "mods", "a.jar")) {
		t.Fatalf("pack not installed: %+v", j)
	}
	if j.Error != "" && !strings.HasPrefix(j.Error, "лаунчер установлен") {
		t.Fatal(j.Error)
	}
	// the normal launcher started from that folder sees the pack
	l := newApp(root)
	if pi := l.PackInfo(); !pi.Installed || pi.Version != "20261030-1300" {
		t.Fatalf("%+v", pi)
	}
	if _, err := l.SetupInfo(); err == nil {
		t.Fatal("setup API available in the launcher")
	}
	os.RemoveAll(root)
}
