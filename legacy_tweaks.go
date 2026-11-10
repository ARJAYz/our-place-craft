package main

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Tweak is an optional client-side feature the player can switch on or off in the launcher.
// Built-in tweaks control a mod that ships with the pack (its jar is renamed to *.jar.off while the
// tweak is off); downloadable tweaks fetch the mod from Modrinth the first time they are switched on
// and copy it into mods/ at every start.
type Tweak struct {
	ID      string
	Title   string
	Desc    string
	Default bool
	Glob    string    // built-in: jar name pattern inside mods/
	Parts   []modPart // downloadable mods switched together
	Legacy  []string  // older config keys that mean the same tweak
}

// modPart is one downloadable mod: cached in .opc-tweaks/<Dir>, fetched from Modrinth by slug.
type modPart struct{ Dir, Project string }

var tweakList = []Tweak{
	{ID: "journeymap", Title: "JourneyMap — карта и миникарта", Default: true, Glob: "journeymap-*.jar",
		Desc: "Входит в сборку. Если включаете Xaero, JourneyMap лучше выключить, иначе будет две карты."},
	{ID: "xaero", Title: "Xaero's Minimap + World Map", Legacy: []string{"xaero_mini", "xaero_world"},
		Parts: []modPart{{"xaero_mini", "xaeros-minimap"}, {"xaero_world", "xaeros-world-map"}},
		Desc:  "Миникарта и большая карта Xaero. Входят в сборку (если их нет — скачаются с Modrinth)."},
	{ID: "justzoom", Title: "Just Zoom — приближение", Default: true, Glob: "justzoom_neoforge_*.jar",
		Desc: "Приближение камеры по клавише, как подзорная труба."},
	{ID: "ipn", Title: "Inventory Profiles Next — сортировка инвентаря", Default: true, Glob: "InventoryProfilesNext-*.jar",
		Desc: "Кнопки сортировки и автопополнения в инвентаре и сундуках."},
	{ID: "blur", Title: "Blur — размытие фона в меню", Default: true, Glob: "blur-neoforge-*.jar",
		Desc: "Размывает игру за открытыми окнами. Выключите, если не нравится или не хватает FPS."},
}

func (c Config) tweakOn(t Tweak) bool {
	if v, ok := c.Tweaks[t.ID]; ok {
		return v
	}
	for _, k := range t.Legacy {
		if c.Tweaks[k] {
			return true
		}
	}
	return t.Default
}

const (
	tweaksDir = ".opc-tweaks" // inside the game folder: downloaded mods are cached here
	offExt    = ".off"
)

// modrinthVersion is the part of the Modrinth API answer that we need.
type modrinthVersion struct {
	VersionNumber string `json:"version_number"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
}

// modrinthAPI can be replaced in tests.
var modrinthAPI = "https://api.modrinth.com/v2"

// cachedTweakJar returns the cached jar of a downloadable tweak ("" if there is none).
func cachedTweakJar(game string, t modPart) string {
	m, _ := filepath.Glob(filepath.Join(game, tweaksDir, t.Dir, "*.jar"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// fetchTweakJar downloads the newest NeoForge 1.21.1 build of the tweak's mod into the cache.
func fetchTweakJar(game string, t modPart, prog *Progress) (string, error) {
	var vs []modrinthVersion
	q := url.Values{}
	q.Set("loaders", `["neoforge"]`)
	q.Set("game_versions", `["1.21.1"]`)
	if err := getJSON(modrinthAPI+"/project/"+t.Project+"/version?"+q.Encode(), &vs); err != nil {
		return "", fmt.Errorf("не удалось узнать версию %s: %w", t.Project, err)
	}
	for _, v := range vs {
		for _, f := range v.Files {
			if !strings.HasSuffix(strings.ToLower(f.Filename), ".jar") || (!f.Primary && len(v.Files) > 1) {
				continue
			}
			dir := filepath.Join(game, tweaksDir, t.Dir)
			os.RemoveAll(dir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
			dst := filepath.Join(dir, filepath.Base(f.Filename))
			if prog != nil {
				prog.set("Скачиваем "+f.Filename, f.Size)
			}
			d := dl{URL: f.URL, Dest: dst, Size: f.Size, SHA1: f.Hashes["sha1"]}
			if err := download(d, func(n int64) {
				if prog != nil {
					prog.add(n)
				}
			}); err != nil {
				os.RemoveAll(dir)
				return "", err
			}
			if want := f.Hashes["sha512"]; want != "" {
				if got := fileSHA512(dst); got != want {
					os.RemoveAll(dir)
					return "", errors.New("скачанный файл " + f.Filename + " повреждён (не сошлась контрольная сумма)")
				}
			}
			return dst, nil
		}
	}
	return "", errors.New("для " + t.Project + " на Modrinth нет сборки под NeoForge 1.21.1")
}

func fileSHA512(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// applyTweaks makes the mods folder match the player's choices. It is idempotent and runs before
// every game start (an update of the pack may bring the disabled jar back). The returned warnings are
// problems that did not stop the game from starting (for example no internet for a download).
func applyTweaks(root string, cfg Config, prog *Progress) []string {
	game := gameDir(root)
	mods := filepath.Join(game, "mods")
	var warns []string
	for _, t := range tweakList {
		on := cfg.tweakOn(t)
		if t.Glob != "" {
			warns = append(warns, applyBuiltin(mods, t, on)...)
			continue
		}
		if t.ID == "xaero" && on {
			if err := applyXaeroKeys(game); err != nil {
				warns = append(warns, t.Title+": клавиши: "+err.Error())
			}
		}
		for _, p := range t.Parts {
			jar := cachedTweakJar(game, p)
			if on && jar == "" {
				var err error
				if jar, err = fetchTweakJar(game, p, prog); err != nil {
					warns = append(warns, t.Title+": "+err.Error())
					continue
				}
			}
			if jar == "" {
				continue
			}
			dst := filepath.Join(mods, filepath.Base(jar))
			if on {
				if st, err := os.Stat(dst); err == nil {
					if si, _ := os.Stat(jar); si != nil && si.Size() == st.Size() {
						continue
					}
				}
				os.MkdirAll(mods, 0o755)
				if err := copyFile(jar, dst); err != nil {
					warns = append(warns, t.Title+": "+err.Error())
				}
			} else {
				os.Remove(dst)
			}
		}
	}
	return warns
}

func applyBuiltin(mods string, t Tweak, on bool) []string {
	var warns []string
	if on {
		offs, _ := filepath.Glob(filepath.Join(mods, t.Glob+offExt))
		jars, _ := filepath.Glob(filepath.Join(mods, t.Glob))
		for _, o := range offs {
			if len(jars) > 0 { // the pack brought a fresh copy: the old disabled one is not needed
				os.Remove(o)
				continue
			}
			if err := os.Rename(o, strings.TrimSuffix(o, offExt)); err != nil {
				warns = append(warns, t.Title+": "+err.Error())
			}
		}
		return warns
	}
	jars, _ := filepath.Glob(filepath.Join(mods, t.Glob))
	for _, j := range jars {
		os.Remove(j + offExt)
		if err := os.Rename(j, j+offExt); err != nil {
			warns = append(warns, t.Title+": "+err.Error())
		}
	}
	return warns
}

// xaeroKeys makes Xaero's maps use the same keys as JourneyMap (J — big map, Ctrl+J — hide the
// minimap, B — new waypoint, N — waypoint list, = / - — minimap zoom). Applied once; the player can
// change the keys afterwards in the game's controls.
var xaeroKeys = [][2]string{
	{"key_gui.xaero_open_map", "key.keyboard.j"},
	{"key_gui.xaero_open_settings", "key.keyboard.unknown"}, // was J: would clash with the map key
	{"key_gui.xaero_toggle_map", "key.keyboard.j:CONTROL"},
	{"key_gui.xaero_new_waypoint", "key.keyboard.b"},
	{"key_gui.xaero_waypoints_key", "key.keyboard.n"},
	{"key_gui.xaero_zoom_in", "key.keyboard.equal"},
	{"key_gui.xaero_zoom_out", "key.keyboard.minus"},
	{"key_gui.xaero_map_zoom_in", "key.keyboard.equal"}, // on the big map; was the left mouse button
	{"key_gui.xaero_map_zoom_out", "key.keyboard.minus"},
}

const xaeroKeysMarker = "xaero_keys.done"

func applyXaeroKeys(game string) error {
	marker := filepath.Join(game, tweaksDir, xaeroKeysMarker)
	if fileExists(marker) {
		return nil
	}
	opt := filepath.Join(game, "options.txt")
	b, err := os.ReadFile(opt)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, kv := range xaeroKeys {
		found := false
		for i, l := range lines {
			if strings.HasPrefix(l, kv[0]+":") {
				lines[i], found = kv[0]+":"+kv[1], true
			}
		}
		if !found {
			lines = append(lines, kv[0]+":"+kv[1])
		}
	}
	os.MkdirAll(game, 0o755)
	if err := os.WriteFile(opt, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(marker), 0o755)
	return os.WriteFile(marker, []byte("keys like JourneyMap applied; delete this file to apply them again\n"), 0o644)
}

// tweaksNeedDownload reports whether starting the game would have to download a mod first.
func tweaksNeedDownload(root string, cfg Config) bool {
	for _, t := range tweakList {
		if !cfg.tweakOn(t) {
			continue
		}
		for _, p := range t.Parts {
			if cachedTweakJar(gameDir(root), p) == "" {
				return true
			}
		}
	}
	return false
}

// tweaksSummary is a short status line, e.g. for the launcher.
func tweaksSummary(cfg Config) string {
	var on []string
	for _, t := range tweakList {
		if cfg.tweakOn(t) {
			on = append(on, strings.SplitN(t.Title, " — ", 2)[0])
		}
	}
	if len(on) == 0 {
		return "без карты"
	}
	return strings.Join(on, ", ")
}
