package main

// The page and its API are served on 127.0.0.1 with a random token; the window (WebView2 on
// Windows) just shows that page. POST /api/<Method> with a JSON array of arguments calls
// the App method of that name.

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"reflect"
	"strings"
)

//go:embed web
var webFS embed.FS

var allowedMethods = map[string]bool{
	"State": true, "Job": true, "SaveSettings": true, "Select": true, "Versions": true, "LoaderVersions": true,
	"Create": true, "Update": true, "Delete": true, "OpenFolder": true, "Tweaks": true, "SetTweak": true, "Play": true, "Servers": true, "HideServer": true,
	"ImportPack": true, "SetCFKey": true, "OpenURL": true, "InstallPack": true, "PackInfo": true,
	"SetupInfo": true, "SetLoader": true, "Repair": true, "MigrateLegacy": true, "Unlock": true, "CheckUpdate": true, "SelfUpdate": true, "PickFolder": true, "Install": true, "StartLauncher": true,
}

func serve(a *App, addr string) (string, error) {
	tok := make([]byte, 16)
	rand.Read(tok)
	token := hex.EncodeToString(tok)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Token") != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id, err := saveUpload(a.store.Root, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"result": id})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Token") != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/")
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		res, err := callMethod(a, name, body)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"result": res})
	})
	go http.Serve(ln, mux)
	page := "index.html"
	if a.setup {
		page = "setup.html"
	}
	return "http://" + ln.Addr().String() + "/" + page + "#" + token, nil
}

func callMethod(a *App, name string, body []byte) (any, error) {
	if !allowedMethods[name] {
		return nil, errors.New("нет такого действия: " + name)
	}
	m := reflect.ValueOf(a).MethodByName(name)
	if !m.IsValid() {
		return nil, errors.New("нет такого действия: " + name)
	}
	var raw []json.RawMessage
	if len(body) > 0 {
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, err
		}
	}
	t := m.Type()
	if len(raw) != t.NumIn() {
		return nil, errors.New("неверное число аргументов для " + name)
	}
	args := make([]reflect.Value, len(raw))
	for i := range raw {
		v := reflect.New(t.In(i))
		if err := json.Unmarshal(raw[i], v.Interface()); err != nil {
			return nil, err
		}
		args[i] = v.Elem()
	}
	out := m.Call(args)
	errT := reflect.TypeOf((*error)(nil)).Elem()
	var res any
	for _, o := range out {
		if o.Type().Implements(errT) {
			if !o.IsNil() {
				return nil, o.Interface().(error)
			}
			continue
		}
		res = o.Interface()
	}
	return res, nil
}
