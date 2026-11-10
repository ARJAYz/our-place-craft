package main

// Setup mode: the same exe, started from anywhere but its install folder, shows an installer page.
// It copies itself to <root>\OurPlaceCraftLauncher.exe, creates shortcuts and the uninstall entry,
// can install the Adventure pack right away and then starts the installed launcher.

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const installExe = "OurPlaceCraftLauncher.exe"

type SetupInfo struct {
	Root      string `json:"root"`
	Stamped   bool   `json:"stamped"`   // the access code is built into this installer
	Legacy    string `json:"legacy"`    // folder of the old Our Place Craft install ("" = none)
	Installed bool   `json:"installed"` // the launcher is already installed in Root
	Nick      string `json:"nick"`
	Done      bool   `json:"done"`
	Backup    string `json:"backup"` // where worlds were saved by a clean reinstall
}

type SetupOpts struct {
	Root      string `json:"root"`
	Nick      string `json:"nick"`
	Desktop   bool   `json:"desktop"`
	Adventure bool   `json:"adventure"`
	Code      string `json:"code"`
	Migrate   bool   `json:"migrate"` // move the old launcher's install here and remove the old launcher
	Clean     bool   `json:"clean"` // reinstall from scratch: worlds are copied to Documents, the rest of the folder is wiped
}

// platform hooks (setup_windows.go); tests replace them
var (
	setupShortcuts = func(root, exe, ico string, desktop bool) error { return nil }
	setupRegister  = func(root, exe, ico string) {}
	setupPickDir   = func(title string) string { return "" }
	setupStart     = func(exe string) error { return nil }
	launcherBusy   = func() bool { return false }
	documentsDir   = func() string { h, _ := os.UserHomeDir(); return filepath.Join(h, "Documents") }
)

func (a *App) needSetup() error {
	if !a.setup {
		return errors.New("доступно только в установщике")
	}
	return nil
}

func (a *App) SetupInfo() (SetupInfo, error) {
	if err := a.needSetup(); err != nil {
		return SetupInfo{}, err
	}
	a.mu.Lock()
	si := SetupInfo{Root: a.store.Root, Stamped: stampedCode != "", Done: a.setupDone, Backup: a.backup}
	a.mu.Unlock()
	si.Installed = fileExists(filepath.Join(a.store.Root, installExe))
	si.Nick = a.store.Settings().Nick
	for _, r := range adventureRoots() {
		if fileExists(configPath(r)) && fileExists(filepath.Join(r, "versions", adventureNeoForge)) {
			si.Legacy = r
			if si.Nick == "" {
				si.Nick = loadConfig(r).Nick
			}
			break
		}
	}
	return si, nil
}

func (a *App) PickFolder() (string, error) {
	if err := a.needSetup(); err != nil {
		return "", err
	}
	p := setupPickDir("Выберите папку для лаунчера и игр (в пути только латинские буквы)")
	if p == "" {
		return a.store.Root, nil
	}
	if !strings.EqualFold(filepath.Base(p), "OurPlaceCraftLauncher") {
		p = filepath.Join(p, "OurPlaceCraftLauncher")
	}
	return p, nil
}

func validRoot(p string) error {
	if p == "" || !filepath.IsAbs(p) {
		return errors.New("укажите полный путь к папке")
	}
	for _, r := range p {
		if r > 127 {
			return errors.New("в пути есть русские буквы или другие не латинские символы — Java с таким путём не запустится. Выберите, например, C:\\OurPlaceCraftLauncher")
		}
	}
	return nil
}

// Install (API) runs the installation in the background (job id); progress is in State().
func (a *App) Install(o SetupOpts) int {
	return a.async(func() (any, error) {
		if err := a.needSetup(); err != nil {
			return nil, err
		}
		if !a.claim("setup", "Устанавливаем…") {
			return nil, errors.New("установка уже идёт")
		}
		err := a.install(o)
		if err != nil {
			a.finish("Ошибка: "+err.Error(), true)
			return nil, err
		}
		a.setupDone = true
		a.finish("Готово!", false)
		return true, nil
	})
}

func (a *App) install(o SetupOpts) error {
	root := filepath.Clean(strings.TrimSpace(o.Root))
	if err := validRoot(root); err != nil {
		return err
	}
	o.Nick = strings.TrimSpace(o.Nick)
	if !nickRe.MatchString(o.Nick) {
		return errors.New("ник: от 3 до 16 символов — латиница, цифры и _")
	}
	if launcherBusy() {
		return errors.New("закройте открытый лаунчер Our Place Craft и нажмите «Установить» ещё раз")
	}
	if o.Migrate && legacyRoot() != "" && oldLauncherRunning() {
		return errors.New("закройте старый лаунчер Our Place Craft и игру, потом нажмите «Установить» ещё раз")
	}
	if c := strings.TrimSpace(o.Code); c != "" {
		if _, err := openServers(c); err != nil {
			return errors.New("неверный код доступа — проверьте его или снимите галочку сборки")
		}
	}
	if o.Clean {
		a.prog.set("Сохраняем миры и очищаем папку", 0)
		if err := a.wipeRoot(root); err != nil {
			return err
		}
	}
	a.prog.set("Копируем лаунчер", 0)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return errors.New("не удалось создать папку " + root + ": " + err.Error())
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	exe := filepath.Join(root, installExe)
	if !samePath(self, exe) {
		if err := copyExe(self, exe); err != nil {
			return errors.New("не удалось записать лаунчер (закройте его, если он открыт): " + err.Error())
		}
	}
	ico := writeIcon(root)
	setupRegister(root, exe, ico)
	a.prog.set("Создаём ярлыки", 0)
	if err := setupShortcuts(root, exe, ico, o.Desktop); err != nil {
		return errors.New("ярлыки не создались: " + err.Error())
	}
	// from here on the launcher's data lives in the chosen folder
	a.mu.Lock()
	a.store = &Store{Root: root}
	a.mu.Unlock()
	a.store.unlockServers(strings.TrimSpace(o.Code))
	if old := legacyRoot(); o.Migrate && old != "" {
		if err := a.migrateLegacy(old); err != nil {
			return errors.New("лаунчер установлен, но перенос из старого не удался: " + err.Error())
		}
	}
	a.store.linkAdventure()
	a.store.ensureFeaturedInstances()
	st := a.store.Settings()
	st.Nick = o.Nick
	if err := a.store.SaveSettings(st); err != nil {
		return err
	}
	if o.Adventure {
		if in, err := a.store.Get(adventureID); err == nil && in.Kind == KindAdventure {
			return nil // the old install is used as it is
		}
		a.mu.Lock()
		a.status = "Скачиваем сборку Adventure…"
		a.mu.Unlock()
		if err := a.installPack(strings.TrimSpace(o.Code)); err != nil {
			return errors.New("лаунчер установлен, но сборка не скачалась: " + err.Error() + ". Её можно поставить позже кнопкой на главной")
		}
		a.Select(adventureID)
	}
	return nil
}

// copyExe writes the launcher via a temp file so a half-written exe never stays behind.
func copyExe(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	os.Remove(dst)
	return os.Rename(tmp, dst)
}

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

// StartLauncher (API) starts the installed launcher; the installer window then closes.
func (a *App) StartLauncher() error {
	if err := a.needSetup(); err != nil {
		return err
	}
	if err := setupStart(filepath.Join(a.store.Root, installExe)); err != nil {
		return err
	}
	if a.quit != nil {
		go a.quit()
	}
	return nil
}

// wipeRoot clears the install folder for a from-scratch reinstall. Worlds of every instance stored
// there are copied to Documents first; an Adventure install of the old launcher outside the folder
// is left alone.
func (a *App) wipeRoot(root string) error {
	if !fileExists(filepath.Join(root, installExe)) && !fileExists(filepath.Join(root, "instances")) {
		return nil // nothing of ours there — never wipe a foreign folder
	}
	old := &Store{Root: root}
	var backup string
	for _, in := range old.List() {
		if in.Kind == KindAdventure {
			continue
		}
		saves := filepath.Join(old.GameDir(in), "saves")
		if es, _ := os.ReadDir(saves); len(es) == 0 {
			continue
		}
		if backup == "" {
			backup = filepath.Join(documentsDir(), "Our Place Craft - миры", time.Now().Format("2006-01-02_15-04"))
		}
		if err := copyTree(saves, filepath.Join(backup, in.ID)); err != nil {
			return errors.New("не удалось сохранить миры в " + backup + ": " + err.Error() + ". Папка не тронута")
		}
	}
	a.mu.Lock()
	a.backup = backup
	a.mu.Unlock()
	self, _ := os.Executable()
	es, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, e := range es {
		p := filepath.Join(root, e.Name())
		if samePath(p, self) {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return errors.New("не удалось удалить " + p + " (закройте игру и лаунчер): " + err.Error())
		}
	}
	return nil
}

//go:embed icon.ico
var iconICO []byte

// writeIcon puts the launcher icon next to the exe under a name that changes with the picture:
// Windows caches shortcut icons by file path, so a new name makes every shortcut show the new icon.
func writeIcon(root string) string {
	sum := sha256.Sum256(iconICO)
	name := "icon-" + hex.EncodeToString(sum[:4]) + ".ico"
	if old, _ := filepath.Glob(filepath.Join(root, "icon-*.ico")); len(old) > 0 {
		for _, f := range old {
			if filepath.Base(f) != name {
				os.Remove(f)
			}
		}
	}
	p := filepath.Join(root, name)
	if err := os.WriteFile(p, iconICO, 0o644); err != nil {
		return ""
	}
	return p
}
