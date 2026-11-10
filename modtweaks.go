package main

// Tweaks for any instance: a tweak is a feature ("Xaero's Maps", "Zoom", "Shaders"); the launcher asks
// Modrinth for the build that matches the instance's Minecraft version and loader, with its required
// dependencies, and keeps the mods folder in line with the switches before every start.

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ModTweak struct {
	ID       string              `json:"id"`
	Title    string              `json:"title"`
	Desc     string              `json:"desc"`
	Projects map[string][]string `json:"-"` // loader -> Modrinth slugs ("*" = any loader)
}

var modTweaks = []ModTweak{
	{ID: "xaero", Title: "Xaero's Minimap + World Map", Desc: "Миникарта и большая карта.",
		Projects: map[string][]string{"*": {"xaeros-minimap", "xaeros-world-map"}}},
	{ID: "journeymap", Title: "JourneyMap", Desc: "Карта и миникарта. С Xaero лучше не включать одновременно.",
		Projects: map[string][]string{"*": {"journeymap"}}},
	{ID: "sodium", Title: "Sodium — больше FPS", Desc: "Быстрая отрисовка, обычно в 2–3 раза больше кадров.",
		Projects: map[string][]string{LoaderFabric: {"sodium"}, LoaderQuilt: {"sodium"}, LoaderNeoForge: {"sodium"}, LoaderForge: {"embeddium"}}},
	{ID: "shaders", Title: "Шейдеры (Iris / Oculus)", Desc: "Поддержка шейдерпаков: положите их в папку shaderpacks.",
		Projects: map[string][]string{LoaderFabric: {"iris"}, LoaderQuilt: {"iris"}, LoaderNeoForge: {"iris"}, LoaderForge: {"oculus"}}},
	{ID: "zoom", Title: "Приближение (Zoom)", Desc: "Приближение камеры по клавише.",
		Projects: map[string][]string{LoaderFabric: {"zoomify"}, LoaderQuilt: {"zoomify"}, LoaderNeoForge: {"just-zoom"}, LoaderForge: {"just-zoom"}}},
	{ID: "jade", Title: "Jade — подсказки о блоках", Desc: "Плашка с названием блока или моба, на которого смотрите.",
		Projects: map[string][]string{"*": {"jade"}}},
	{ID: "appleskin", Title: "AppleSkin — сытость еды", Desc: "Показывает, сколько сытости даёт еда.",
		Projects: map[string][]string{"*": {"appleskin"}}},
	{ID: "ipn", Title: "Inventory Profiles Next", Desc: "Сортировка инвентаря и сундуков.",
		Projects: map[string][]string{"*": {"inventory-profiles-next"}}},
	{ID: "modmenu", Title: "Mod Menu — список модов", Desc: "Кнопка «Моды» в главном меню (Fabric/Quilt).",
		Projects: map[string][]string{LoaderFabric: {"modmenu"}, LoaderQuilt: {"modmenu"}}},
}

func (t ModTweak) slugs(loader string) []string {
	if s, ok := t.Projects[loader]; ok {
		return s
	}
	return t.Projects["*"]
}

// modrinthLoaders: Quilt runs Fabric mods too.
func modrinthLoaders(loader string) []string {
	if loader == LoaderQuilt {
		return []string{"quilt", "fabric"}
	}
	return []string{loader}
}

type mrVersion struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	VersionNum   string `json:"version_number"`
	Dependencies []struct {
		ProjectID string `json:"project_id"`
		VersionID string `json:"version_id"`
		Type      string `json:"dependency_type"`
	} `json:"dependencies"`
	Files []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
}

type mrFile struct {
	Project, Filename, URL, SHA1 string
	Size                         int64
}

// resolveMod finds the build of a project (slug or id) for the instance, plus its required dependencies.
func resolveMod(project, loader, mc string, seen map[string]bool) ([]mrFile, error) {
	if seen[project] {
		return nil, nil
	}
	seen[project] = true
	ls := ""
	for i, l := range modrinthLoaders(loader) {
		if i > 0 {
			ls += ","
		}
		ls += `"` + l + `"`
	}
	q := url.Values{}
	q.Set("loaders", "["+ls+"]")
	q.Set("game_versions", `["`+mc+`"]`)
	var vs []mrVersion
	if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(project)+"/version?"+q.Encode(), &vs); err != nil {
		return nil, fmt.Errorf("Modrinth: %w", err)
	}
	if len(vs) == 0 {
		return nil, errNoBuild
	}
	v := vs[0]
	var out []mrFile
	for _, f := range v.Files {
		if strings.HasSuffix(strings.ToLower(f.Filename), ".jar") && (f.Primary || len(v.Files) == 1) {
			out = append(out, mrFile{Project: v.ProjectID, Filename: f.Filename, URL: f.URL, SHA1: f.Hashes["sha1"], Size: f.Size})
			break
		}
	}
	if len(out) == 0 {
		return nil, errNoBuild
	}
	seen[v.ProjectID] = true
	for _, d := range v.Dependencies {
		if d.Type != "required" || d.ProjectID == "" {
			continue
		}
		dep, err := resolveMod(d.ProjectID, loader, mc, seen)
		if err != nil && !errors.Is(err, errNoBuild) {
			return nil, err
		}
		out = append(out, dep...)
	}
	return out, nil
}

var errNoBuild = errors.New("нет сборки под эту версию")

// tweak state: which files each tweak put into mods/ (so they can be removed again).
type tweakState map[string][]string

func tweakStatePath(gameDir string) string {
	return filepath.Join(gameDir, ".opc-tweaks", "state.json")
}

func loadTweakState(gameDir string) tweakState {
	st := tweakState{}
	readJSONFile(tweakStatePath(gameDir), &st)
	return st
}

// applyModTweaks brings the mods folder in line with the switches. Problems are returned as warnings.
func applyModTweaks(in *Instance, gameDir string, p *Progress) []string {
	if in.Loader == LoaderVanilla || in.Loader == "" {
		return nil
	}
	mods := filepath.Join(gameDir, "mods")
	cache := filepath.Join(gameDir, ".opc-tweaks", "cache")
	os.MkdirAll(mods, 0o755)
	st := loadTweakState(gameDir)
	var warns []string
	want := map[string]bool{} // file names that enabled tweaks need
	for _, t := range modTweaks {
		if !in.Tweaks[t.ID] {
			continue
		}
		if files, ok := st[t.ID]; ok && allExist(cache, files) {
			for _, f := range files {
				want[f] = true
			}
			continue
		}
		seen := map[string]bool{}
		var files []mrFile
		var failed error
		for _, slug := range t.slugs(in.Loader) {
			fs, err := resolveMod(slug, in.Loader, in.MC, seen)
			if err != nil {
				failed = err
				break
			}
			files = append(files, fs...)
		}
		if failed == nil && len(t.slugs(in.Loader)) == 0 {
			failed = errNoBuild
		}
		if failed != nil {
			warns = append(warns, t.Title+": "+failed.Error())
			continue
		}
		var list []dl
		var names []string
		for _, f := range files {
			list = append(list, dl{URL: f.URL, Dest: filepath.Join(cache, f.Filename), SHA1: f.SHA1, Size: f.Size})
			names = append(names, f.Filename)
		}
		if err := downloadAll(list, p, "Скачиваем "+t.Title); err != nil {
			warns = append(warns, t.Title+": "+err.Error())
			continue
		}
		st[t.ID] = names
		for _, n := range names {
			want[n] = true
		}
	}
	// remove files of switched-off tweaks (unless another enabled tweak needs the same file)
	for id, files := range st {
		if in.Tweaks[id] {
			continue
		}
		for _, f := range files {
			if !want[f] {
				os.Remove(filepath.Join(mods, f))
			}
		}
	}
	for f := range want {
		dst := filepath.Join(mods, f)
		if !fileExists(dst) {
			if err := copyFile(filepath.Join(cache, f), dst); err != nil {
				warns = append(warns, err.Error())
			}
		}
	}
	writeJSON(tweakStatePath(gameDir), st)
	return warns
}

func allExist(dir string, files []string) bool {
	for _, f := range files {
		if !fileExists(filepath.Join(dir, f)) {
			return false
		}
	}
	return len(files) > 0
}

// TweakView is what the UI shows.
type TweakView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Desc  string `json:"desc"`
	On    bool   `json:"on"`
	Note  string `json:"note,omitempty"`
}

func tweakViews(in *Instance, packRoot string) []TweakView {
	var out []TweakView
	if isAdventurePack(in) {
		cfg := loadConfig(packRoot)
		for _, t := range tweakList {
			out = append(out, TweakView{ID: t.ID, Title: t.Title, Desc: t.Desc, On: cfg.tweakOn(t)})
		}
		return out
	}
	for _, t := range modTweaks {
		v := TweakView{ID: t.ID, Title: t.Title, Desc: t.Desc, On: in.Tweaks[t.ID]}
		if in.Loader == LoaderVanilla || in.Loader == "" {
			v.Note = "нужен загрузчик модов"
		} else if len(t.slugs(in.Loader)) == 0 {
			v.Note = "не для " + loaderNames[in.Loader]
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Note == "" && out[j].Note != "" })
	return out
}
