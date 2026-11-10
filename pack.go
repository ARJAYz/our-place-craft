package main

// Our Place Craft: Adventure installed by the new launcher: the encrypted pack is downloaded from
// the GitHub release with the access code, unpacked into the instance's game folder and kept up to
// date (a newer release is synced before the game starts).

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const KindPack = "pack" // the Adventure pack managed by this launcher

// packCfgRoot is the folder whose launcher.json keeps the pack's code, version and tweaks;
// its "game" subfolder is the game folder.
func (s *Store) packCfgRoot(in *Instance) string {
	if in.Kind == KindAdventure {
		return in.Root
	}
	return s.instDir(in.ID)
}

func isAdventurePack(in *Instance) bool { return in.Kind == KindAdventure || in.Kind == KindPack }

var (
	metaMu     sync.Mutex
	metaLatest PackMeta
	metaAt     time.Time
)

// latestPack returns the published pack version (cached for 10 minutes).
func latestPack() (PackMeta, bool) {
	metaMu.Lock()
	defer metaMu.Unlock()
	if metaLatest.Version != "" && time.Since(metaAt) < 10*time.Minute {
		return metaLatest, true
	}
	m, ok := fetchMeta()
	if ok {
		metaLatest, metaAt = m, time.Now()
	}
	return m, ok
}

// packCode: the code saved with the pack, else the one stamped into this exe.
func (s *Store) packCode(in *Instance) string {
	if in != nil {
		if c := loadConfig(s.packCfgRoot(in)).PackCode; c != "" {
			return c
		}
	}
	if c := s.Settings().Code; c != "" {
		return c
	}
	return stampedCode
}

// ensurePackInstance creates the Adventure instance managed by this launcher (once).
func (s *Store) ensurePackInstance() (*Instance, error) {
	if in, err := s.Get(adventureID); err == nil {
		return in, nil
	}
	in := &Instance{ID: adventureID, Name: "Our Place Craft: Adventure", Kind: KindPack, MC: "1.21.1",
		Loader: LoaderNeoForge, LoaderVersion: "21.1.264", Server: adventureServer(), RamMB: defaultPackRam(), Created: time.Now().Unix()}
	in.GameDir = filepath.Join(s.instDir(in.ID), "game")
	if err := os.MkdirAll(in.GameDir, 0o755); err != nil {
		return nil, err
	}
	return in, s.Save(in)
}

func defaultPackRam() int {
	gb := totalRAMGB()
	switch {
	case gb >= 24:
		return 10240
	case gb >= 16:
		return 8192
	case gb >= 12:
		return 6144
	}
	return 4096
}

// syncPack downloads the newest pack if this instance does not have it yet.
// force=false: a failed check (no internet) is not an error when the pack is already installed.
func (s *Store) syncPackIfNeeded(in *Instance, code string, p *Progress) (updated bool, err error) {
	if in.Kind != KindPack {
		return false, nil
	}
	updated, err = s.syncPackCore(in, code, p)
	if err == nil {
		game := gameDir(s.packCfgRoot(in))
		if fileExists(filepath.Join(game, "mods")) {
			if _, xerr := applyPackExtra(game, p); xerr != nil {
				return updated, xerr
			}
		}
	}
	return updated, err
}

func (s *Store) syncPackCore(in *Instance, code string, p *Progress) (updated bool, err error) {
	root := s.packCfgRoot(in)
	cfg := loadConfig(root)
	if code == "" {
		code = s.packCode(in)
	}
	installed := fileExists(filepath.Join(gameDir(root), "mods")) && cfg.PackVersion != ""
	p.set("Проверяем обновления сборки", 0)
	meta, ok := latestPack()
	if !ok {
		if installed {
			return false, nil
		}
		return false, errors.New("не удалось связаться с GitHub, чтобы скачать сборку")
	}
	if installed && meta.Version == cfg.PackVersion {
		return false, nil
	}
	if code == "" {
		return false, errNeedCode
	}
	zipPath := filepath.Join(s.Root, "downloads", "opc_pack_download.zip")
	os.MkdirAll(filepath.Dir(zipPath), 0o755)
	if err := downloadPack(code, zipPath, p); err != nil {
		return false, err
	}
	defer os.Remove(zipPath)
	pl, err := openPackZip(zipPath)
	if err != nil {
		return false, err
	}
	defer pl.Close()
	if _, err := pl.syncPack(gameDir(root), p); err != nil {
		return false, err
	}
	cfg = loadConfig(root)
	cfg.PackCode, cfg.PackVersion = code, meta.Version
	if err := saveConfig(root, cfg); err != nil {
		return false, err
	}
	return true, nil
}

var errNeedCode = errors.New("нужен код доступа к сборке (его даёт владелец сервера)")

// InstallPack (API): creates the Adventure instance and downloads the pack (job id).
func (a *App) InstallPack(code string) int {
	return a.async(func() (any, error) {
		if !a.claim("pack", "Устанавливаем сборку Adventure…") {
			return nil, errors.New("подождите, идёт установка или игра")
		}
		err := a.installPack(strings.TrimSpace(code))
		if err != nil {
			a.finish("Ошибка: "+err.Error(), true)
			return nil, err
		}
		a.finish("Сборка Adventure установлена. Нажмите «Играть».", false)
		a.Select(adventureID)
		return adventureID, nil
	})
}

func (a *App) installPack(code string) error {
	if in, err := a.store.Get(adventureID); err == nil && in.Kind == KindAdventure {
		return errors.New("сборка уже установлена старым лаунчером (" + in.Root + ")")
	}
	in, err := a.store.ensurePackInstance()
	if err != nil {
		return err
	}
	if code == "" {
		code = a.store.packCode(in)
	}
	if code == "" {
		return errNeedCode
	}
	if _, err := a.store.syncPackIfNeeded(in, code, a.prog); err != nil {
		return err
	}
	_, err = a.store.Prepare(in, a.prog, io.Discard)
	return err
}

// claim marks the launcher busy; false if something else is running.
func (a *App) claim(what, status string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy != "" {
		return false
	}
	a.busy, a.running, a.status, a.isErr = what, false, status, false
	return true
}

// PackInfo (API): what the page shows on the Adventure card.
type PackInfo struct {
	Installed bool   `json:"installed"`
	Legacy    bool   `json:"legacy"`
	Version   string `json:"version"`
	Latest    string `json:"latest"`
	HasCode   bool   `json:"has_code"`
}

func (a *App) PackInfo() PackInfo {
	pi := PackInfo{HasCode: stampedCode != ""}
	in, err := a.store.Get(adventureID)
	if err == nil {
		cfg := loadConfig(a.store.packCfgRoot(in))
		pi.Legacy = in.Kind == KindAdventure
		pi.Installed = pi.Legacy || cfg.PackVersion != ""
		pi.Version = cfg.PackVersion
		pi.HasCode = pi.HasCode || cfg.PackCode != ""
	}
	metaMu.Lock()
	pi.Latest = metaLatest.Version
	metaMu.Unlock()
	if pi.Latest == "" {
		go latestPack()
	}
	return pi
}

func packVersionText(v string) string {
	if t, err := time.Parse("20060102-1504", v); err == nil {
		return t.Format("02.01.2006 15:04")
	}
	return v
}
