package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// OPCL_PREVIEW=1 go test -run TestPreview -timeout 0 — serves the UI on :8765 with fake servers.
func TestPreview(t *testing.T) {
	if os.Getenv("OPCL_PREVIEW") == "" {
		t.Skip()
	}
	srv := fakeWorld(t)
	javaWorks = func(string) bool { return true }
	{
		mod := []byte("modjar")
		idx := fmt.Sprintf(`{"formatVersion":1,"game":"minecraft","name":"Fabulously Optimized","versionId":"6.4","dependencies":{"minecraft":"1.21.11","fabric-loader":"0.16.9"},"files":[{"path":"mods/sodium.jar","hashes":{"sha1":"%s"},"downloads":["%s/mod.jar"],"fileSize":%d}]}`, sha(mod), srv.URL, len(mod))
		os.WriteFile("/tmp/Fabulously.mrpack", zipBytes(map[string]string{"modrinth.index.json": idx, "overrides/config/x.txt": "1"}), 0o644)
	}
	root := t.TempDir()
	adv := filepath.Join(root, "OurPlaceCraft")
	os.MkdirAll(filepath.Join(adv, "versions", adventureNeoForge), 0o755)
	os.WriteFile(filepath.Join(adv, "launcher.json"), []byte(`{"nick":"ARJAYz219","ram_mb":10240,"tweaks":{"journeymap":false,"xaero":true}}`), 0o644)
	os.WriteFile(filepath.Join(adv, "versions", adventureNeoForge, adventureNeoForge+".json"), []byte(`{"id":"x"}`), 0o644)
	os.Setenv("PUBLIC", root)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for i := 0; i < 2; i++ {
					n, _ := readVarInt(r)
					io.CopyN(io.Discard, r, int64(n))
				}
				js := `{"version":{"name":"1.21.1"},"players":{"online":3,"max":20}}`
				var body bytes.Buffer
				writeVarInt(&body, 0)
				writeVarInt(&body, len(js))
				body.WriteString(js)
				var out bytes.Buffer
				writePacket(&out, body.Bytes())
				c.Write(out.Bytes())
			}(c)
		}
	}()
	if os.Getenv("OPCL_LOCKED") != "" {
		stampedCode = ""
	}
	a := newApp(filepath.Join(root, "launcher"))
	if os.Getenv("OPCL_UPD") != "" {
		a.update = &UpdateInfo{Version: "20261030-1700", Notes: "серверы по коду, автообновление"}
	}
	name := "Fabric для тестов"
	if os.Getenv("OPCL_PROMO") != "" {
		name = "Выживание с модами"
	}
	a.Create(NewInstance{Name: name, MC: "1.21.11", Loader: LoaderFabric, LoaderVersion: "0.16.9", RamMB: 6144})
	featuredMu.Lock()
	if len(featuredServers) == 2 {
		featuredServers[0].Address = ln.Addr().String()
		featuredServers[1].Address = "127.0.0.1:1"
	}
	featuredMu.Unlock()
	a.Select(adventureID)
	url, err := serve(a, "127.0.0.1:8765")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile("/tmp/opcl_url", []byte(url), 0o644)
	fmt.Println(url)
	time.Sleep(time.Hour)
}
