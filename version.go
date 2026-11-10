package main

// Minecraft version descriptions (Mojang's format, also used by Fabric, Quilt, Forge and NeoForge),
// merging of the inheritsFrom chain, and Mojang's library/argument rules.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Platform settings; the defaults are for 64-bit Windows.
var (
	ruleOSName   = "windows"
	javaGUIName  = "javaw.exe"
	javaCLIName  = "java.exe"
	classpathSep = ";"
)

type Rule struct {
	Action   string          `json:"action"`
	OS       *RuleOS         `json:"os"`
	Features map[string]bool `json:"features"`
}

type RuleOS struct {
	Name string `json:"name"`
	Arch string `json:"arch"`
}

type Artifact struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type Library struct {
	Name      string `json:"name"`
	URL       string `json:"url"` // Maven repository (Fabric, Quilt, old Forge)
	Downloads struct {
		Artifact    *Artifact           `json:"artifact"`
		Classifiers map[string]Artifact `json:"classifiers"`
	} `json:"downloads"`
	Natives map[string]string `json:"natives"` // old versions: os -> classifier
	Extract *struct {
		Exclude []string `json:"exclude"`
	} `json:"extract"`
	Rules []Rule `json:"rules"`
}

// Arg is either a plain string or {"rules": [...], "value": string|[]string}.
type Arg struct {
	Values []string
	Rules  []Rule
}

func (a *Arg) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		a.Values = []string{s}
		return nil
	}
	var obj struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	a.Rules = obj.Rules
	if json.Unmarshal(obj.Value, &s) == nil {
		a.Values = []string{s}
		return nil
	}
	return json.Unmarshal(obj.Value, &a.Values)
}

type AssetIndexRef struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type Version struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
	Jar          string `json:"jar"`
	MainClass    string `json:"mainClass"`
	Type         string `json:"type"`
	Arguments    *struct {
		Game []Arg `json:"game"`
		JVM  []Arg `json:"jvm"`
	} `json:"arguments"`
	MinecraftArguments string              `json:"minecraftArguments"`
	Libraries          []Library           `json:"libraries"`
	AssetIndex         *AssetIndexRef      `json:"assetIndex"`
	Assets             string              `json:"assets"`
	Downloads          map[string]Artifact `json:"downloads"`
	JavaVersion        *struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
}

// rulesAllow evaluates Mojang rules for this OS (64-bit) with no optional features enabled.
func rulesAllow(rules []Rule) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		match := true
		if r.OS != nil {
			if r.OS.Name != "" && r.OS.Name != ruleOSName {
				match = false
			}
			if a := r.OS.Arch; a != "" {
				arm := runtime.GOARCH == "arm64"
				if !(arm && (a == "arm64" || a == "aarch64")) && !(!arm && (a == "x86_64" || a == "amd64")) { // "x86" never
					match = false
				}
			}
		}
		for _, want := range r.Features {
			if want {
				match = false
			}
		}
		if match {
			allowed = r.Action == "allow"
		}
	}
	return allowed
}

// mavenPath turns "group:artifact:version[:classifier][@ext]" into a relative path.
func mavenPath(name string) string {
	ext := "jar"
	if i := strings.Index(name, "@"); i >= 0 {
		ext, name = name[i+1:], name[:i]
	}
	p := strings.Split(name, ":")
	if len(p) < 3 {
		return ""
	}
	file := p[1] + "-" + p[2]
	if len(p) > 3 {
		file += "-" + p[3]
	}
	return strings.ReplaceAll(p[0], ".", "/") + "/" + p[1] + "/" + p[2] + "/" + file + "." + ext
}

func libKey(name string) string { // group:artifact[:classifier] (version dropped)
	name = strings.Split(name, "@")[0]
	p := strings.Split(name, ":")
	if len(p) < 3 {
		return name
	}
	k := p[0] + ":" + p[1]
	if len(p) > 3 {
		k += ":" + p[3]
	}
	return k
}

const mojangLibs = "https://libraries.minecraft.net/"

// artifact returns the library's main jar (false for natives-only entries of old versions).
func (l Library) artifact(libDir string) (dl, bool) {
	if a := l.Downloads.Artifact; a != nil {
		p := a.Path
		if p == "" {
			p = mavenPath(l.Name)
		}
		return dl{URL: a.URL, Dest: filepath.Join(libDir, filepath.FromSlash(p)), SHA1: a.SHA1, Size: a.Size}, true
	}
	if len(l.Natives) > 0 && l.URL == "" {
		return dl{}, false
	}
	p := mavenPath(l.Name)
	if p == "" {
		return dl{}, false
	}
	base := l.URL
	if base == "" {
		base = mojangLibs
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return dl{URL: base + p, Dest: filepath.Join(libDir, filepath.FromSlash(p))}, true
}

// native returns the natives jar of an old-style library for this OS.
func (l Library) native(libDir string) (dl, bool) {
	cls, ok := l.Natives[ruleOSName]
	if !ok {
		return dl{}, false
	}
	cls = strings.ReplaceAll(cls, "${arch}", "64")
	if a, ok := l.Downloads.Classifiers[cls]; ok {
		p := a.Path
		if p == "" {
			p = mavenPath(l.Name + ":" + cls)
		}
		return dl{URL: a.URL, Dest: filepath.Join(libDir, filepath.FromSlash(p)), SHA1: a.SHA1, Size: a.Size}, true
	}
	p := mavenPath(l.Name + ":" + cls)
	base := l.URL
	if base == "" {
		base = mojangLibs
	}
	return dl{URL: strings.TrimSuffix(base, "/") + "/" + p, Dest: filepath.Join(libDir, filepath.FromSlash(p))}, true
}

func readVersion(root, id string) (*Version, error) {
	b, err := os.ReadFile(filepath.Join(root, "versions", id, id+".json"))
	if err != nil {
		return nil, err
	}
	v := &Version{}
	if err := json.Unmarshal(b, v); err != nil {
		return nil, fmt.Errorf("описание версии %s повреждено: %w", id, err)
	}
	return v, nil
}

// Resolved is a version with its whole inheritsFrom chain merged.
type Resolved struct {
	ID, Base           string // the launched version and the vanilla one at the bottom of the chain
	MainClass          string
	Libraries          []Library
	JVM, Game          []Arg
	HasModernArgs      bool
	MinecraftArguments string
	AssetIndex         *AssetIndexRef
	Assets             string
	JavaMajor          int
	ClientDL           *Artifact
	Type               string
}

func resolveVersion(root, id string) (*Resolved, error) {
	var chain []*Version
	seen := map[string]bool{}
	for cur := id; cur != ""; {
		if seen[cur] {
			return nil, errors.New("зацикленное наследование версий: " + cur)
		}
		seen[cur] = true
		v, err := readVersion(root, cur)
		if err != nil {
			return nil, err
		}
		chain = append(chain, v)
		cur = v.InheritsFrom
	}
	r := &Resolved{ID: id, Base: chain[len(chain)-1].ID}
	libSeen := map[string]bool{}
	// child first: its libraries override the parent's versions of the same artifact
	for _, v := range chain {
		if r.MainClass == "" {
			r.MainClass = v.MainClass
		}
		if r.MinecraftArguments == "" {
			r.MinecraftArguments = v.MinecraftArguments
		}
		if r.AssetIndex == nil {
			r.AssetIndex = v.AssetIndex
		}
		if r.Assets == "" {
			r.Assets = v.Assets
		}
		if r.JavaMajor == 0 && v.JavaVersion != nil {
			r.JavaMajor = v.JavaVersion.MajorVersion
		}
		if r.ClientDL == nil {
			if c, ok := v.Downloads["client"]; ok {
				c := c
				r.ClientDL = &c
			}
		}
		if r.Type == "" {
			r.Type = v.Type
		}
		for _, l := range v.Libraries {
			k := libKey(l.Name)
			if len(l.Natives) > 0 {
				k += "#natives"
			}
			if libSeen[k] {
				continue
			}
			libSeen[k] = true
			r.Libraries = append(r.Libraries, l)
		}
	}
	// arguments: parent first, then each child appends its own
	for i := len(chain) - 1; i >= 0; i-- {
		if a := chain[i].Arguments; a != nil {
			r.HasModernArgs = true
			r.JVM = append(r.JVM, a.JVM...)
			r.Game = append(r.Game, a.Game...)
		}
	}
	if r.JavaMajor == 0 {
		r.JavaMajor = 8
	}
	if r.Assets == "" && r.AssetIndex != nil {
		r.Assets = r.AssetIndex.ID
	}
	return r, nil
}

// supportsQuickPlay tells whether the version understands --quickPlayMultiplayer (1.20+).
func (r *Resolved) supportsQuickPlay() bool {
	for _, a := range r.Game {
		for _, v := range a.Values {
			if strings.Contains(v, "quickPlayMultiplayer") {
				return true
			}
		}
	}
	return false
}
