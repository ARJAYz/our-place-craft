package main

// The Our Place Craft: Adventure pack installed by the first launcher (C:\OurPlaceCraft).
// The new launcher shows it as an instance and shares its settings file (launcher.json),
// so both launchers stay in sync.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	adventureID       = "our-place-craft"
	adventureNeoForge = "neoforge-21.1.264"
)

// Config is the old launcher's launcher.json (only the fields used here; unknown ones are kept).
type Config struct {
	Nick        string          `json:"nick"`
	UUID        string          `json:"uuid"`
	UUIDFor     string          `json:"uuid_for"`
	RamMB       int             `json:"ram_mb"`
	Server      string          `json:"server"`
	Skin        string          `json:"skin"`
	PackCode    string          `json:"pack_code,omitempty"`
	PackVersion string          `json:"pack_version,omitempty"`
	Tweaks      map[string]bool `json:"tweaks,omitempty"`
}

func (c Config) uuid() string {
	if c.UUID != "" && c.UUIDFor == c.Nick {
		return c.UUID
	}
	return offlineUUID(c.Nick)
}

func configPath(root string) string { return filepath.Join(root, "launcher.json") }
func gameDir(root string) string    { return filepath.Join(root, "game") }

func loadConfig(root string) Config {
	c := Config{}
	if b, err := os.ReadFile(configPath(root)); err == nil {
		json.Unmarshal(b, &c)
	}
	if strings.TrimSpace(c.Server) == "" {
		c.Server = adventureServer()
	}
	return c
}

// saveConfig merges the known fields into launcher.json without dropping fields this build does not know.
func saveConfig(root string, c Config) error {
	m := map[string]any{}
	if b, err := os.ReadFile(configPath(root)); err == nil {
		json.Unmarshal(b, &m)
	}
	b, _ := json.Marshal(c)
	var known map[string]any
	json.Unmarshal(b, &known)
	for k, v := range known {
		m[k] = v
	}
	if len(c.Tweaks) == 0 {
		delete(m, "tweaks")
	}
	return writeJSON(configPath(root), m)
}

// adventureRoots are the places the first launcher installs to.
func adventureRoots() []string {
	r := []string{`C:\OurPlaceCraft`}
	if p := os.Getenv("PUBLIC"); p != "" {
		r = append(r, filepath.Join(p, "OurPlaceCraft"))
	}
	if h, _ := os.UserHomeDir(); h != "" {
		r = append(r, filepath.Join(h, "OurPlaceCraft"))
	}
	return r
}

// linkAdventure adds the old install as an instance (once) and imports the nick.
func (s *Store) linkAdventure() {
	for _, root := range adventureRoots() {
		if !fileExists(configPath(root)) || !fileExists(filepath.Join(root, "versions", adventureNeoForge)) {
			continue
		}
		cfg := loadConfig(root)
		if in, err := s.Get(adventureID); err == nil {
			if in.Kind == KindAdventure { // a pack installed by this launcher stays as it is
				in.Root, in.GameDir = root, gameDir(root)
				s.Save(in)
			}
		} else {
			ram := cfg.RamMB
			if ram == 0 {
				ram = 6144
			}
			s.Save(&Instance{ID: adventureID, Name: "Our Place Craft: Adventure", Kind: KindAdventure, MC: "1.21.1",
				Loader: LoaderNeoForge, LoaderVersion: "21.1.264", VersionID: adventureNeoForge, Server: cfg.Server,
				RamMB: ram, Created: time.Now().Unix(), Root: root, GameDir: gameDir(root)})
		}
		st := s.Settings()
		if st.Nick == "" && cfg.Nick != "" {
			st.Nick, st.Skin = cfg.Nick, cfg.Skin
			if st.Selected == "" {
				st.Selected = adventureID
			}
			s.SaveSettings(st)
		}
		return
	}
}
