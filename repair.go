package main

// «Починить сборку»: checks every file of an instance against the known checksums and fixes what
// is broken. Worlds, screenshots, controls and tweaks are never touched.

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type RepairReport struct {
	Checked      int      `json:"checked"`
	Redownloaded []string `json:"redownloaded"` // game files that were broken or missing
	Restored     []string `json:"restored"`     // pack files put back
	Removed      []string `json:"removed"`      // extra mods moved to mods-removed
	BrokenMods   []string `json:"broken_mods"`  // own mods that are damaged (custom builds)
	Java         string   `json:"java"`         // "" = fine, otherwise what was done
	PackUpdated  bool     `json:"pack_updated"`
}

// Repair (API) runs the check in the background (job id); the result is a RepairReport.
func (a *App) Repair(id string) int {
	return a.async(func() (any, error) {
		in, err := a.store.Get(id)
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		running := a.running
		a.mu.Unlock()
		if running {
			return nil, errors.New("закройте игру, потом запустите проверку")
		}
		if !a.claim("repair", "Проверяем сборку «"+in.Name+"»…") {
			return nil, errors.New("подождите, идёт установка или игра")
		}
		rep, err := a.repair(in)
		if err != nil {
			a.finish("Проверка не удалась: "+err.Error(), true)
			return nil, err
		}
		a.finish("Сборка «"+in.Name+"» проверена", false)
		return rep, nil
	})
}

func (a *App) repair(in *Instance) (*RepairReport, error) {
	rep := &RepairReport{}
	s := a.store
	root := s.FilesRoot(in)
	game := s.GameDir(in)

	// 1. the pack (Adventure): mods, configs, scripts
	if isAdventurePack(in) {
		if err := a.repairPack(in, game, rep); err != nil {
			return rep, err
		}
	}

	// 2. Minecraft, loader, libraries, assets
	id := in.VersionID
	if id == "" {
		id = loaderVersionID(in.Loader, in.MC, in.LoaderVersion)
	}
	if r, err := resolveVersion(root, id); err == nil {
		if err := a.checkGameFiles(root, r, rep); err != nil {
			return rep, err
		}
	}
	if in.Kind != KindAdventure {
		if _, err := s.Prepare(in, a.prog, io.Discard); err != nil { // reinstalls whatever is still missing
			return rep, err
		}
	}

	// 3. Java
	if r, err := resolveVersion(root, id); err == nil {
		a.prog.set("Проверяем Java", 0)
		if j := s.javaPath(in, r); j == "" || !javaWorks(j) {
			major := javaMajorFor(r.JavaMajor)
			os.RemoveAll(runtimeDir(root, major))
			if err := ensureJavaMajor(root, major, a.prog); err != nil {
				rep.Java = "Java " + itoa(major) + " повреждена, а скачать заново не получилось: " + err.Error()
			} else {
				rep.Java = "Java " + itoa(major) + " установлена заново"
			}
		}
	}

	// 4. own mods of custom builds: damaged jars
	if !isAdventurePack(in) {
		a.prog.set("Проверяем моды", 0)
		st := loadTweakState(game)
		tweakFile := map[string]bool{}
		for _, files := range st {
			for _, f := range files {
				tweakFile[f] = true
			}
		}
		jars, _ := filepath.Glob(filepath.Join(game, "mods", "*.jar"))
		for _, j := range jars {
			rep.Checked++
			if jarOK(j) {
				continue
			}
			name := filepath.Base(j)
			if tweakFile[name] { // a tweak: drop it, it is downloaded again on «Играть»
				os.Remove(j)
				os.Remove(filepath.Join(game, tweaksDir, "cache", name))
				rep.Redownloaded = append(rep.Redownloaded, name)
				continue
			}
			rep.BrokenMods = append(rep.BrokenMods, name)
		}
	}
	return rep, nil
}

// checkGameFiles verifies the client, libraries and assets by SHA-1 and downloads the bad ones.
func (a *App) checkGameFiles(root string, r *Resolved, rep *RepairReport) error {
	libDir := filepath.Join(root, "libraries")
	var list []dl
	if r.ClientDL != nil {
		list = append(list, dl{URL: r.ClientDL.URL, Dest: filepath.Join(root, "versions", r.Base, r.Base+".jar"), SHA1: r.ClientDL.SHA1, Size: r.ClientDL.Size})
	}
	for _, l := range r.Libraries {
		if !rulesAllow(l.Rules) {
			continue
		}
		if d, ok := l.artifact(libDir); ok && d.URL != "" {
			list = append(list, d)
		}
		if d, ok := l.native(libDir); ok {
			list = append(list, d)
		}
	}
	if r.AssetIndex != nil {
		idx := filepath.Join(root, "assets", "indexes", r.AssetIndex.ID+".json")
		if !fileOK(dl{Dest: idx, SHA1: r.AssetIndex.SHA1, Size: r.AssetIndex.Size}) {
			os.Remove(idx)
			rep.Redownloaded = append(rep.Redownloaded, "assets/indexes/"+r.AssetIndex.ID+".json")
			download(dl{URL: r.AssetIndex.URL, Dest: idx, SHA1: r.AssetIndex.SHA1, Size: r.AssetIndex.Size}, nil)
		}
		if ai, err := readAssetIndex(idx); err == nil {
			seen := map[string]bool{}
			for _, o := range ai.Objects {
				if seen[o.Hash] || len(o.Hash) < 2 {
					continue
				}
				seen[o.Hash] = true
				list = append(list, dl{URL: assetsURL + o.Hash[:2] + "/" + o.Hash,
					Dest: filepath.Join(root, "assets", "objects", o.Hash[:2], o.Hash), SHA1: o.Hash, Size: o.Size})
			}
		}
	}
	a.prog.set("Проверяем файлы Minecraft", int64(len(list)))
	var bad []dl
	for _, d := range list {
		rep.Checked++
		if !fileOK(d) {
			os.Remove(d.Dest)
			bad = append(bad, d)
			rep.Redownloaded = append(rep.Redownloaded, filepath.Base(d.Dest))
		}
		a.prog.add(1)
	}
	if len(bad) == 0 {
		return nil
	}
	return downloadAll(bad, a.prog, "Скачиваем испорченные файлы")
}

// repairPack checks the pack files (by the manifest written at install) and syncs the pack again
// when something is wrong. Extra mods are moved to mods-removed.
func (a *App) repairPack(in *Instance, game string, rep *RepairReport) error {
	s := a.store
	croot := s.packCfgRoot(in)
	if _, err := applyPackExtra(game, a.prog); err != nil {
		return err
	}
	man := loadManifest(game)
	a.prog.set("Проверяем моды и настройки сборки", int64(len(man)))
	need := man == nil // no manifest yet (older install): compare with the pack itself
	var bad []string
	for name, f := range man {
		rep.Checked++
		a.prog.add(1)
		if keepIfPresent[strings.ToLower(name)] || isBanned(name) {
			continue
		}
		p := filepath.Join(game, filepath.FromSlash(name))
		if fileExists(p + offExt) { // switched off by a tweak
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Size() == f.Size {
			if c, _, err := fileCRC(p); err == nil && c == f.CRC {
				continue
			}
		}
		bad = append(bad, name)
	}
	rep.Removed = append(rep.Removed, removeBannedMods(game)...)
	extra := extraPackMods(game, man)
	if len(bad) == 0 && !need {
		for _, e := range extra { // nothing broken: just put the extra mods aside, no download
			bak := filepath.Join(game, "mods-removed", "mods", e)
			os.MkdirAll(filepath.Dir(bak), 0o755)
			os.Remove(bak)
			if os.Rename(filepath.Join(game, "mods", e), bak) == nil {
				rep.Removed = append(rep.Removed, e)
			}
		}
		return nil
	}
	code := s.packCode(in)
	if code == "" {
		return errNeedCode
	}
	zipPath := filepath.Join(s.Root, "downloads", "opc_pack_download.zip")
	os.MkdirAll(filepath.Dir(zipPath), 0o755)
	if err := downloadPack(code, zipPath, a.prog); err != nil {
		return err
	}
	defer os.Remove(zipPath)
	pl, err := openPackZip(zipPath)
	if err != nil {
		return err
	}
	defer pl.Close()
	st, err := pl.syncPack(game, a.prog, SyncOpts{Repair: true})
	if err != nil {
		return err
	}
	for _, n := range st.WrittenNames {
		if fileExists(filepath.Join(game, filepath.FromSlash(n)) + offExt) {
			continue
		}
		rep.Restored = append(rep.Restored, path.Base(n))
	}
	rep.Removed = append(rep.Removed, st.RemovedNames...)
	if _, err := applyPackExtra(game, a.prog); err != nil {
		return err
	}
	if meta, ok := latestPack(); ok {
		cfg := loadConfig(croot)
		if cfg.PackVersion != meta.Version {
			rep.PackUpdated = true
		}
		cfg.PackCode, cfg.PackVersion = code, meta.Version
		saveConfig(croot, cfg)
	}
	applyTweaks(croot, loadConfig(croot), a.prog) // put the switched tweaks back the way they were
	return nil
}

// extraPackMods: jars in mods that the pack does not ship and no tweak put there.
func extraPackMods(game string, man packManifest) []string {
	if man == nil {
		return nil
	}
	tw := tweakJarNames(game)
	var out []string
	es, _ := os.ReadDir(filepath.Join(game, "mods"))
	for _, e := range es {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".jar") || tw[strings.ToLower(e.Name())] {
			continue
		}
		if _, ok := man["mods/"+e.Name()]; !ok {
			out = append(out, e.Name())
		}
	}
	return out
}

// jarOK: the file is a readable zip.
func jarOK(p string) bool {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return false
	}
	zr.Close()
	return true
}

// javaWorks runs «java -version».
var javaWorks = func(java string) bool {
	if dir := filepath.Dir(java); strings.EqualFold(filepath.Base(java), "javaw.exe") {
		java = filepath.Join(dir, "java.exe")
	}
	c := newCmd(java, "-version")
	if err := c.Start(); err != nil {
		return false
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(20 * time.Second):
		c.Process.Kill()
		return false
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
