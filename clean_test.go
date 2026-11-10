package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanReinstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "OurPlaceCraftLauncher")
	docs := t.TempDir()
	documentsDir = func() string { return docs }
	a := newSetupApp(root)
	if j := waitJob(t, a, a.Install(SetupOpts{Root: root, Nick: "Tester"})); j.Error != "" {
		t.Fatal(j.Error)
	}
	in, err := a.store.Create(NewInstance{Name: "Мой мир", MC: "1.21.1", Loader: "vanilla"})
	if err != nil {
		t.Fatal(err)
	}
	w := filepath.Join(a.store.GameDir(in), "saves", "World1", "level.dat")
	os.MkdirAll(filepath.Dir(w), 0o755)
	os.WriteFile(w, []byte("lvl"), 0o644)
	os.MkdirAll(filepath.Join(root, "runtimes", "java-21"), 0o755)

	b := newSetupApp(root)
	if j := waitJob(t, b, b.Install(SetupOpts{Root: root, Nick: "Tester2", Clean: true})); j.Error != "" {
		t.Fatal(j.Error)
	}
	if fileExists(filepath.Join(root, "runtimes")) || fileExists(filepath.Join(root, "instances", in.ID)) {
		t.Fatal("folder not wiped")
	}
	if !fileExists(filepath.Join(root, installExe)) {
		t.Fatal("launcher missing after reinstall")
	}
	si, _ := b.SetupInfo()
	if si.Backup == "" || !fileExists(filepath.Join(si.Backup, in.ID, "World1", "level.dat")) {
		t.Fatalf("world not backed up: %q", si.Backup)
	}
	if b.store.Settings().Nick != "Tester2" {
		t.Fatal("nick")
	}
	// a folder that isn't ours is never wiped
	foreign := t.TempDir()
	os.WriteFile(filepath.Join(foreign, "keep.txt"), []byte("x"), 0o644)
	if err := b.wipeRoot(foreign); err != nil || !fileExists(filepath.Join(foreign, "keep.txt")) {
		t.Fatal("foreign folder touched")
	}
}
