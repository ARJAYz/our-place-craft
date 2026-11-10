package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetLoaderSwapsMods(t *testing.T) {
	a := newApp(t.TempDir())
	in, err := a.store.Create(NewInstance{Name: "Trans", MC: "1.21.11", Loader: LoaderVanilla})
	if err != nil {
		t.Fatal(err)
	}
	game := a.store.GameDir(in)
	if err := a.SetLoader(in.ID, LoaderFabric, "0.16.9"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(game, "mods", "sodium.jar"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(game, "saves", "W"), 0o755)
	got, _ := a.store.Get(in.ID)
	if got.Loader != LoaderFabric || got.LoaderVersion != "0.16.9" || got.VersionID != "" {
		t.Fatalf("%+v", got)
	}
	if err := a.SetLoader(in.ID, LoaderNeoForge, "21.11.1"); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(game, "mods", "sodium.jar")) {
		t.Fatal("fabric mod left in NeoForge mods")
	}
	if !fileExists(filepath.Join(game, "saves", "W")) {
		t.Fatal("world touched")
	}
	if err := a.SetLoader(in.ID, LoaderFabric, "0.16.9"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(game, "mods", "sodium.jar")) {
		t.Fatal("fabric mods not restored")
	}
	if err := a.SetLoader(in.ID, LoaderFabric, ""); err == nil {
		t.Fatal("empty version accepted")
	}
	adv := &Instance{ID: "adv", Name: "A", Kind: KindPack, MC: "1.21.1", Loader: LoaderNeoForge}
	a.store.Save(adv)
	if err := a.SetLoader("adv", LoaderFabric, "1"); err == nil {
		t.Fatal("pack loader changed")
	}
}
