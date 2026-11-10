package main

// Moving the Adventure install of the first launcher (C:\OurPlaceCraft) into this launcher and
// removing the old one: worlds, settings, controls, screenshots and the pack files are moved (not
// downloaded again), Minecraft/NeoForge/Java files are reused, then the old program is removed.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// platform hooks (setup_windows.go)
var (
	oldLauncherRunning = func() bool { return false }
	removeOldLauncher  = func(oldRoot string) {} // shortcuts + «Apps» entry + the folder
)

// legacyRoot is the folder of the old launcher's install ("" if there is none).
func legacyRoot() string {
	for _, r := range adventureRoots() {
		if fileExists(configPath(r)) && fileExists(filepath.Join(r, "versions", adventureNeoForge)) {
			return r
		}
	}
	return ""
}

// MigrateLegacy (API, launcher): the same as the installer's option.
func (a *App) MigrateLegacy() int {
	return a.async(func() (any, error) {
		old := legacyRoot()
		if old == "" {
			return nil, errors.New("старый лаунчер не найден")
		}
		if !a.claim("migrate", "Переносим сборку из старого лаунчера…") {
			return nil, errors.New("подождите, идёт установка или игра")
		}
		if err := a.migrateLegacy(old); err != nil {
			a.finish("Перенос не удался: "+err.Error(), true)
			return nil, err
		}
		a.finish("Сборка перенесена, старый лаунчер удалён", false)
		return true, nil
	})
}

func (a *App) migrateLegacy(old string) error {
	if oldLauncherRunning() {
		return errors.New("закройте старый лаунчер Our Place Craft и игру, потом повторите")
	}
	s := a.store
	cfg := loadConfig(old)
	// the instance becomes the one managed by this launcher
	if in, err := s.Get(adventureID); err == nil && in.Kind == KindAdventure {
		os.Remove(filepath.Join(s.instDir(adventureID), "instance.json"))
	}
	in, err := s.ensurePackInstance()
	if err != nil {
		return err
	}
	newRoot := s.packCfgRoot(in)
	newGame := gameDir(newRoot)
	oldGame := gameDir(old)

	a.prog.set("Переносим миры и сборку", 0)
	if empty(newGame) {
		os.RemoveAll(newGame)
		if err := moveDir(oldGame, newGame); err != nil {
			return errors.New("не удалось перенести папку игры (закройте старый лаунчер и Minecraft): " + err.Error())
		}
	} else { // the pack was already installed here: bring the worlds and screenshots over
		for _, sub := range []string{"saves", "screenshots"} {
			es, _ := os.ReadDir(filepath.Join(oldGame, sub))
			for _, e := range es {
				dst := filepath.Join(newGame, sub, e.Name())
				if fileExists(dst) {
					dst += " (старый лаунчер)"
				}
				os.MkdirAll(filepath.Dir(dst), 0o755)
				if err := moveDir(filepath.Join(oldGame, sub, e.Name()), dst); err != nil {
					return errors.New("не удалось перенести " + e.Name() + ": " + err.Error())
				}
			}
		}
	}
	// settings of the old launcher (pack version, code, tweaks, memory, nick)
	if !fileExists(configPath(newRoot)) {
		if b, err := os.ReadFile(configPath(old)); err == nil {
			os.MkdirAll(newRoot, 0o755)
			os.WriteFile(configPath(newRoot), b, 0o644)
		}
	}
	// Minecraft, NeoForge, libraries and Java: reuse instead of downloading again
	a.prog.set("Переносим файлы Minecraft", 0)
	for _, d := range []string{"versions", "libraries", "assets"} {
		mergeMove(filepath.Join(old, d), filepath.Join(s.Root, d))
	}
	if findJava(runtimeDir(s.Root, 21), javaGUIName) == "" && findJava(filepath.Join(old, "runtime"), javaGUIName) != "" {
		os.MkdirAll(filepath.Join(s.Root, "runtimes"), 0o755)
		moveDir(filepath.Join(old, "runtime"), runtimeDir(s.Root, 21))
	}
	in.VersionID = adventureNeoForge
	if cfg.RamMB > 0 {
		in.RamMB = cfg.RamMB
	}
	if err := s.Save(in); err != nil {
		return err
	}
	st := s.Settings()
	if st.Nick == "" && cfg.Nick != "" {
		st.Nick, st.Skin = cfg.Nick, cfg.Skin
	}
	if st.Code == "" && cfg.PackCode != "" {
		st.Code = cfg.PackCode
	}
	st.Selected = adventureID
	s.SaveSettings(st)
	s.unlockServers()
	s.ensureFeaturedInstances()

	a.prog.set("Удаляем старый лаунчер", 0)
	removeOldLauncher(old)
	return nil
}

func empty(dir string) bool {
	es, err := os.ReadDir(dir)
	return err != nil || len(es) == 0
}

// moveDir renames src to dst; across disks it copies and then removes src.
func moveDir(src, dst string) error {
	if !fileExists(src) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if strings.EqualFold(filepath.VolumeName(src), filepath.VolumeName(dst)) {
		return err // same disk: something holds the files open
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// mergeMove moves the files of src into dst, keeping what dst already has.
func mergeMove(src, dst string) {
	filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		if fileExists(t) {
			return nil
		}
		os.MkdirAll(filepath.Dir(t), 0o755)
		if os.Rename(p, t) != nil {
			copyFile(p, t)
		}
		return nil
	})
}

