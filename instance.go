package main

// Instances ("сборки"): each one has its own game folder, Minecraft version, loader, server and tweaks.
// Shared files (versions, libraries, assets, Java) live once in the launcher root.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	KindCustom    = "custom"
	KindAdventure = "adventure" // the Our Place Craft pack installed by the old launcher
)

type Instance struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	MC            string          `json:"mc"`
	Loader        string          `json:"loader"`
	LoaderVersion string          `json:"loader_version,omitempty"`
	VersionID     string          `json:"version_id,omitempty"` // installed version to launch
	Server        string          `json:"server,omitempty"`
	RamMB         int             `json:"ram_mb"`
	Tweaks        map[string]bool `json:"tweaks,omitempty"`
	Created       int64           `json:"created"`
	LastPlayed    int64           `json:"last_played,omitempty"`
	// Only for KindAdventure: the old install keeps its own files.
	Root    string `json:"root,omitempty"`
	GameDir string `json:"game_dir,omitempty"`
}

type Settings struct {
	Nick     string `json:"nick"`
	Skin     string `json:"skin,omitempty"`
	Selected string `json:"selected,omitempty"`
	// servers hidden from the home page (they stay in «Сборки»)
	HiddenServers []string `json:"hidden_servers,omitempty"`
	CFKey         string   `json:"cf_key,omitempty"` // CurseForge API key for importing CurseForge packs
	Code          string   `json:"code,omitempty"`   // access code: unlocks our servers and the Adventure pack
}

type Store struct {
	Root string // launcher root: versions, libraries, assets, runtimes, instances
}

func (s *Store) instDir(id string) string { return filepath.Join(s.Root, "instances", id) }

// FilesRoot is where the instance's versions/libraries/assets are.
func (s *Store) FilesRoot(in *Instance) string {
	if in.Kind == KindAdventure && in.Root != "" {
		return in.Root
	}
	return s.Root
}

func (s *Store) GameDir(in *Instance) string {
	if in.GameDir != "" {
		return in.GameDir
	}
	return filepath.Join(s.instDir(in.ID), "minecraft")
}

func (s *Store) settingsPath() string { return filepath.Join(s.Root, "launcher-settings.json") }

func (s *Store) Settings() Settings {
	var st Settings
	if b, err := os.ReadFile(s.settingsPath()); err == nil {
		json.Unmarshal(b, &st)
	}
	return st
}

func (s *Store) SaveSettings(st Settings) error { return writeJSON(s.settingsPath(), st) }

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (s *Store) List() []*Instance {
	es, _ := os.ReadDir(filepath.Join(s.Root, "instances"))
	var out []*Instance
	for _, e := range es {
		if in, err := s.Get(e.Name()); err == nil {
			out = append(out, in)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Kind == KindAdventure) != (out[j].Kind == KindAdventure) {
			return out[i].Kind == KindAdventure
		}
		return out[i].Created < out[j].Created
	})
	return out
}

func (s *Store) Get(id string) (*Instance, error) {
	b, err := os.ReadFile(filepath.Join(s.instDir(id), "instance.json"))
	if err != nil {
		return nil, err
	}
	in := &Instance{}
	if err := json.Unmarshal(b, in); err != nil {
		return nil, err
	}
	in.ID = id
	return in, nil
}

func (s *Store) Save(in *Instance) error {
	return writeJSON(filepath.Join(s.instDir(in.ID), "instance.json"), in)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Store) newID(name string) string {
	base := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(translit(name)), "-"), "-")
	if base == "" {
		base = "instance"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	id := base
	for i := 2; fileExists(s.instDir(id)); i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}

// translit keeps folder names latin (Java cannot start from paths with Cyrillic letters on Windows).
func translit(s string) string {
	m := map[rune]string{'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y",
		'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h",
		'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ы': "y", 'э': "e", 'ю': "yu", 'я': "ya"}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if t, ok := m[r]; ok {
			b.WriteString(t)
		} else if r < 128 {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

type NewInstance struct {
	Name          string `json:"name"`
	MC            string `json:"mc"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loader_version"`
	Server        string `json:"server"`
	RamMB         int    `json:"ram_mb"`
}

func (s *Store) Create(n NewInstance) (*Instance, error) {
	n.Name = strings.TrimSpace(n.Name)
	if n.Name == "" {
		n.Name = "Minecraft " + n.MC
	}
	if n.MC == "" {
		return nil, errors.New("выберите версию Minecraft")
	}
	if n.Loader == "" {
		n.Loader = LoaderVanilla
	}
	if n.Loader != LoaderVanilla && n.LoaderVersion == "" {
		return nil, errors.New("выберите версию " + loaderNames[n.Loader])
	}
	if n.RamMB <= 0 {
		n.RamMB = 4096
		if n.Loader != LoaderVanilla {
			n.RamMB = 6144
		}
	}
	in := &Instance{ID: s.newID(n.Name), Name: n.Name, Kind: KindCustom, MC: n.MC, Loader: n.Loader,
		LoaderVersion: n.LoaderVersion, Server: strings.TrimSpace(n.Server), RamMB: n.RamMB, Created: time.Now().Unix()}
	if err := os.MkdirAll(filepath.Join(s.GameDir(in), "mods"), 0o755); err != nil {
		return nil, err
	}
	return in, s.Save(in)
}

// Delete removes an instance folder (worlds included). The old Adventure install is only unlinked.
func (s *Store) Delete(id string) error {
	in, err := s.Get(id)
	if err != nil {
		return err
	}
	if in.Kind == KindAdventure {
		return os.Remove(filepath.Join(s.instDir(id), "instance.json"))
	}
	return os.RemoveAll(s.instDir(id))
}

// Prepare installs whatever the instance still needs (Java, Minecraft, loader, libraries, assets)
// and returns the resolved version ready to launch.
func (s *Store) Prepare(in *Instance, p *Progress, logw io.Writer) (*Resolved, error) {
	root := s.FilesRoot(in)
	id := in.VersionID
	if id == "" {
		id = loaderVersionID(in.Loader, in.MC, in.LoaderVersion)
	}
	if r, err := resolveVersion(root, id); err == nil && len(missingFiles(root, r)) == 0 && s.javaPath(in, r) != "" {
		return r, nil
	}
	if in.Kind == KindAdventure {
		return nil, errors.New("файлы сборки повреждены — запустите установщик Our Place Craft и выберите «Обновить сборку»")
	}
	p.set("Получаем данные Minecraft "+in.MC, 0)
	if err := ensureVersionJSON(root, in.MC); err != nil {
		return nil, err
	}
	base, err := resolveVersion(root, in.MC)
	if err != nil {
		return nil, err
	}
	if err := ensureJavaMajor(root, base.JavaMajor, p); err != nil {
		return nil, err
	}
	if err := installGameFiles(root, base, p); err != nil {
		return nil, err
	}
	java := javaFor(root, base.JavaMajor, javaCLIName, "")
	vid, err := installLoader(root, in.Loader, in.MC, in.LoaderVersion, java, p, logw)
	if err != nil {
		return nil, err
	}
	r, err := resolveVersion(root, vid)
	if err != nil {
		return nil, err
	}
	if r.JavaMajor != base.JavaMajor {
		if err := ensureJavaMajor(root, r.JavaMajor, p); err != nil {
			return nil, err
		}
	}
	if err := installGameFiles(root, r, p); err != nil {
		return nil, err
	}
	if in.VersionID != vid {
		in.VersionID = vid
		s.Save(in)
	}
	return r, nil
}

func (s *Store) javaPath(in *Instance, r *Resolved) string {
	legacy := ""
	if in.Kind == KindAdventure {
		legacy = in.Root
	}
	return javaFor(s.FilesRoot(in), r.JavaMajor, javaGUIName, legacy)
}
