package main

// The launcher's API for the web UI. Long work runs in the background; the page polls State().

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type App struct {
	store *Store
	prog  *Progress

	mu      sync.Mutex
	busy    string // id of the instance being prepared/running ("" = idle)
	running bool
	status  string
	isErr   bool

	jobs map[int]*job

	setup     bool // running as the installer
	setupDone bool
	update    *UpdateInfo
	backup    string
	quit      func() // closes the window

	nextID int

	screenW, screenH int
}

type job struct {
	Done   bool   `json:"done"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// newSetupApp is the installer: nothing is written until the player presses «Установить».
func newSetupApp(root string) *App {
	return &App{store: &Store{Root: root}, prog: &Progress{}, jobs: map[int]*job{}, setup: true, status: "Готов к установке"}
}

func newApp(root string) *App {
	a := &App{store: &Store{Root: root}, prog: &Progress{}, jobs: map[int]*job{}}
	os.MkdirAll(root, 0o755)
	a.store.unlockServers()
	a.store.linkAdventure()
	a.store.ensureFeaturedInstances()
	a.status = "Готов к запуску"
	return a
}

func readJSONFile(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// async runs f in the background and returns a job id the page waits for with Job().
func (a *App) async(f func() (any, error)) int {
	a.mu.Lock()
	a.nextID++
	id := a.nextID
	j := &job{}
	a.jobs[id] = j
	a.mu.Unlock()
	go func() {
		res, err := f()
		a.mu.Lock()
		j.Done, j.Result = true, res
		if err != nil {
			j.Error = err.Error()
		}
		a.mu.Unlock()
	}()
	return id
}

func (a *App) Job(id int) job {
	a.mu.Lock()
	defer a.mu.Unlock()
	j, ok := a.jobs[id]
	if !ok {
		return job{Done: true, Error: "нет такой задачи"}
	}
	if j.Done {
		delete(a.jobs, id)
	}
	return *j
}

// ---------------------------------------------------------------- state

type InstanceView struct {
	*Instance
	LoaderName string `json:"loader_name"`
	Installed  bool   `json:"installed"`
	Folder     string `json:"folder"`
}

type StateView struct {
	Settings  Settings       `json:"settings"`
	Instances []InstanceView `json:"instances"`
	Busy      string         `json:"busy"`
	Running   bool           `json:"running"`
	Status    string         `json:"status"`
	IsErr     bool           `json:"is_err"`
	Stage     string         `json:"stage"`
	Done      int64          `json:"done"`
	Total     int64          `json:"total"`
	MaxRamMB  int            `json:"max_ram_mb"`
	Locked    bool           `json:"locked"` // our servers are hidden until the access code is entered
	Update    *UpdateInfo    `json:"update,omitempty"`
	Version   string         `json:"version"`
	Root      string         `json:"root"`
}

func (a *App) State() StateView {
	st := StateView{Instances: []InstanceView{}, Settings: a.store.Settings(), MaxRamMB: maxRamMB(), Locked: len(featured()) == 0, Version: launcherVersion, Root: a.store.Root}
	st.Settings.Code = "" // never sent to the page
	for _, in := range a.store.List() {
		root := a.store.FilesRoot(in)
		installed := in.VersionID != "" && fileExists(filepath.Join(root, "versions", in.VersionID, in.VersionID+".json"))
		st.Instances = append(st.Instances, InstanceView{Instance: in, LoaderName: loaderNames[in.Loader], Installed: installed, Folder: a.store.GameDir(in)})
	}
	a.mu.Lock()
	st.Busy, st.Running, st.Status, st.IsErr = a.busy, a.running, a.status, a.isErr
	st.Update = a.update
	a.mu.Unlock()
	st.Stage, st.Done, st.Total = a.prog.snapshot()
	return st
}

func (a *App) setStatus(msg string, isErr bool) {
	a.mu.Lock()
	a.status, a.isErr = msg, isErr
	a.mu.Unlock()
}

// ---------------------------------------------------------------- settings & instances

var nickRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

func (a *App) SaveSettings(nick, skin string) error {
	nick = strings.TrimSpace(nick)
	if !nickRe.MatchString(nick) {
		return errors.New("ник: от 3 до 16 символов — латиница, цифры и _")
	}
	st := a.store.Settings()
	st.Nick = nick
	if skin != "" || st.Skin != "" {
		v, ok := normalizeSkin(skin)
		if !ok {
			return errors.New("скин: ник лицензии Minecraft или ссылка на PNG")
		}
		st.Skin = v
	}
	return a.store.SaveSettings(st)
}

func (a *App) Select(id string) error {
	st := a.store.Settings()
	st.Selected = id
	return a.store.SaveSettings(st)
}

func (a *App) Versions(all bool) int {
	return a.async(func() (any, error) {
		list, err := mcVersions()
		if err != nil {
			return nil, err
		}
		type v struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Date string `json:"date"`
		}
		var out []v
		for _, x := range list {
			if !all && x.Type != "release" {
				continue
			}
			out = append(out, v{x.ID, x.Type, x.ReleaseTime.Format("02.01.2006")})
		}
		return out, nil
	})
}

func (a *App) LoaderVersions(loader, mc string) int {
	return a.async(func() (any, error) {
		list, err := loaderVersions(loader, mc)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("%s для Minecraft %s нет", loaderNames[loader], mc)
		}
		return list, nil
	})
}

func (a *App) Create(n NewInstance) (string, error) {
	in, err := a.store.Create(n)
	if err != nil {
		return "", err
	}
	a.Select(in.ID)
	return in.ID, nil
}

func (a *App) Update(id string, name, server string, ram int) error {
	in, err := a.store.Get(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) != "" {
		in.Name = strings.TrimSpace(name)
	}
	in.Server, in.RamMB = strings.TrimSpace(server), ram
	if isAdventurePack(in) {
		croot := a.store.packCfgRoot(in)
		cfg := loadConfig(croot)
		cfg.RamMB = ram
		if in.Server != "" {
			cfg.Server = in.Server
		}
		saveConfig(croot, cfg)
	}
	return a.store.Save(in)
}

func (a *App) Delete(id string) error {
	if a.isBusy() {
		return errors.New("подождите, идёт установка или игра")
	}
	return a.store.Delete(id)
}

func (a *App) OpenFolder(id string) {
	if in, err := a.store.Get(id); err == nil {
		os.MkdirAll(a.store.GameDir(in), 0o755)
		openFolder(a.store.GameDir(in))
	}
}

func (a *App) Tweaks(id string) ([]TweakView, error) {
	in, err := a.store.Get(id)
	if err != nil {
		return nil, err
	}
	return tweakViews(in, a.store.packCfgRoot(in)), nil
}

func (a *App) SetTweak(id, tweak string, on bool) error {
	in, err := a.store.Get(id)
	if err != nil {
		return err
	}
	if isAdventurePack(in) {
		croot := a.store.packCfgRoot(in)
		cfg := loadConfig(croot)
		if cfg.Tweaks == nil {
			cfg.Tweaks = map[string]bool{}
		}
		cfg.Tweaks[tweak] = on
		return saveConfig(croot, cfg)
	}
	if in.Tweaks == nil {
		in.Tweaks = map[string]bool{}
	}
	in.Tweaks[tweak] = on
	return a.store.Save(in)
}

// ---------------------------------------------------------------- play

func (a *App) isBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy != ""
}

func (a *App) Play(id string, toServer bool) error {
	st := a.store.Settings()
	if !nickRe.MatchString(st.Nick) {
		return errors.New("сначала впишите ник в «Настройках»")
	}
	in, err := a.store.Get(id)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.busy != "" {
		a.mu.Unlock()
		return errors.New("уже идёт установка или игра")
	}
	a.busy, a.running, a.status, a.isErr = id, false, "Готовим "+in.Name+"…", false
	a.mu.Unlock()
	a.Select(id)
	go a.play(in, st, toServer)
	return nil
}

func (a *App) finish(msg string, isErr bool) {
	a.prog.set("", 0)
	a.mu.Lock()
	a.busy, a.running, a.status, a.isErr = "", false, msg, isErr
	a.mu.Unlock()
}

func (a *App) play(in *Instance, st Settings, toServer bool) {
	root := a.store.FilesRoot(in)
	game := a.store.GameDir(in)
	os.MkdirAll(filepath.Join(game, "logs"), 0o755)
	logw, _ := os.Create(filepath.Join(game, "logs", "install.log"))
	var lw io.Writer = io.Discard
	if logw != nil {
		lw = logw
		defer logw.Close()
	}
	if in.Kind == KindPack {
		if _, err := a.store.syncPackIfNeeded(in, "", a.prog); err != nil {
			fmt.Fprintln(lw, "ERROR:", err)
			a.finish("Ошибка: "+err.Error(), true)
			return
		}
	}
	r, err := a.store.Prepare(in, a.prog, lw)
	if err != nil {
		fmt.Fprintln(lw, "ERROR:", err)
		a.finish("Ошибка: "+err.Error(), true)
		return
	}
	java := a.store.javaPath(in, r)
	if java == "" {
		a.finish("Ошибка: не найдена Java "+fmt.Sprint(javaMajorFor(r.JavaMajor)), true)
		return
	}
	var warns []string
	uuid := offlineUUID(st.Nick)
	server := in.Server
	if isAdventurePack(in) {
		croot := a.store.packCfgRoot(in)
		cfg := loadConfig(croot)
		cfg.Nick = st.Nick
		cfg.Skin = st.Skin
		uuid = cfg.uuid()
		saveConfig(croot, cfg)
		removeBannedMods(game)
		a.prog.set("Применяем твики", 0)
		warns = applyTweaks(croot, cfg, a.prog)
		if err := writeSkinScript(game, st.Skin, toServer); err != nil {
			warns = append(warns, "скин: "+err.Error())
		}
		if server == "" {
			server = cfg.Server
		}
	} else {
		a.prog.set("Применяем твики", 0)
		warns = applyModTweaks(in, game, a.prog)
	}
	if !toServer {
		server = ""
	}
	opts := LaunchOpts{Root: root, GameDir: game, Nick: st.Nick, UUID: uuid, RamMB: in.RamMB, Server: server}

	if a.screenW > 0 {
		opts.Width, opts.Height = a.screenW*9/10, a.screenH*88/100
	}
	args, err := buildArgs(r, opts)
	if err != nil {
		a.finish("Ошибка запуска: "+err.Error(), true)
		return
	}
	out, _ := os.Create(filepath.Join(game, "logs", "launcher-output.log"))
	cmd := exec.Command(java, args...)
	cmd.Dir = game
	if out != nil {
		cmd.Stdout, cmd.Stderr = out, out
		defer out.Close()
	}
	if err := cmd.Start(); err != nil {
		a.finish("Не удалось запустить Java: "+err.Error(), true)
		return
	}
	in.LastPlayed = time.Now().Unix()
	a.store.Save(in)
	a.prog.set("", 0)
	msg := "Игра запущена: " + in.Name
	if len(warns) > 0 {
		msg += ". Не применились твики: " + strings.Join(warns, "; ")
	}
	a.mu.Lock()
	a.running, a.status, a.isErr = true, msg, len(warns) > 0
	a.mu.Unlock()
	err = cmd.Wait()
	var ee *exec.ExitError
	if err != nil && errors.As(err, &ee) && ee.ExitCode() != 0 {
		a.finish(fmt.Sprintf("Игра закрылась с ошибкой (код %d). Логи — в папке сборки → logs и crash-reports.", ee.ExitCode()), true)
		return
	}
	a.finish("Готов к запуску", false)
}

func maxRamMB() int {
	gb := totalRAMGB()
	if gb <= 0 {
		return 16384
	}
	m := (gb - 2) * 1024
	if m < 2048 {
		m = 2048
	}
	return m
}

// SetLoader (API) switches an instance to another mod loader (or back to plain Minecraft).
// The loader itself is installed on the next «Играть». Mods of the previous loader are put aside
// and come back if the player switches back, so Fabric and Forge mods never mix.
func (a *App) SetLoader(id, loader, lv string) error {
	if a.isBusy() {
		return errors.New("подождите, идёт установка или игра")
	}
	in, err := a.store.Get(id)
	if err != nil {
		return err
	}
	if in.Kind == KindAdventure || in.Kind == KindPack {
		return errors.New("у сборки Adventure загрузчик менять нельзя — её моды работают только на NeoForge")
	}
	if _, ok := loaderNames[loader]; !ok {
		return errors.New("неизвестный загрузчик")
	}
	if loader == LoaderVanilla {
		lv = ""
	} else if lv == "" {
		return errors.New("выберите версию " + loaderNames[loader])
	}
	if loader == in.Loader && lv == in.LoaderVersion {
		return nil
	}
	if loader != in.Loader {
		if err := swapLoaderFiles(a.store.GameDir(in), in.Loader, loader); err != nil {
			return errors.New("не удалось переложить моды: " + err.Error())
		}
		if in.RamMB < 6144 && loader != LoaderVanilla && in.Loader == LoaderVanilla {
			in.RamMB = min(6144, maxRamMB())
		}
	}
	in.Loader, in.LoaderVersion, in.VersionID = loader, lv, ""
	return a.store.Save(in)
}

// swapLoaderFiles puts the mods (and tweak state) of loader `from` aside in .opc-loaders/<from>
// and brings back what was put aside for loader `to`.
func swapLoaderFiles(game, from, to string) error {
	items := []string{"mods", filepath.Join(".opc-tweaks", "state.json")}
	if from == "" {
		from = LoaderVanilla
	}
	for _, it := range items {
		src := filepath.Join(game, it)
		if !fileExists(src) {
			continue
		}
		dst := filepath.Join(game, ".opc-loaders", from, it)
		os.RemoveAll(dst)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	for _, it := range items {
		src := filepath.Join(game, ".opc-loaders", to, it)
		if !fileExists(src) {
			continue
		}
		dst := filepath.Join(game, it)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(game, "mods"), 0o755)
}

// Unlock (API) opens our servers with the access code.
func (a *App) Unlock(code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("введите код")
	}
	if _, err := openServers(code); err != nil {
		return errors.New("Неверный код — его даёт владелец сервера")
	}
	a.store.unlockServers(code)
	a.store.ensureFeaturedInstances()
	return nil
}
