package main

// "Мои серверы" on the home page: the owner's servers with a live status (Minecraft Server List Ping).
// A server can be hidden from the home page (not deleted) and brought back in the settings.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"opcl/packcrypt"
)

type FeaturedServer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Subtitle string `json:"subtitle"`
	Desc     string `json:"desc"`
	Address  string `json:"address"`
	Instance string `json:"instance"` // instance used to play on it
	Art      string `json:"art"`
	MC       string `json:"mc,omitempty"`
	Loader   string `json:"loader,omitempty"`
}

// featuredServers is empty until the access code is known: the list (with the addresses) is
// stored encrypted in the binary (servers_sealed.go), so the public build does not reveal it.
var (
	featuredMu      sync.RWMutex
	featuredServers []FeaturedServer
)

func featured() []FeaturedServer {
	featuredMu.RLock()
	defer featuredMu.RUnlock()
	return append([]FeaturedServer(nil), featuredServers...)
}

// openServers decrypts the sealed server list; a wrong code fails.
func openServers(code string) ([]FeaturedServer, error) {
	if code == "" {
		return nil, errors.New("нет кода")
	}
	raw, err := base64.StdEncoding.DecodeString(sealedServers)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := packcrypt.Decrypt(&out, bytes.NewReader(raw), code, nil); err != nil {
		return nil, errors.New("неверный код доступа")
	}
	var list []FeaturedServer
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		return nil, err
	}
	return list, nil
}

// unlockServers tries the codes the launcher knows (saved, built into the installer, the old
// launcher's Adventure install) and fills featuredServers. Returns the working code.
func (s *Store) unlockServers(extra ...string) string {
	cands := append([]string{}, extra...)
	cands = append(cands, s.Settings().Code, stampedCode)
	if in, err := s.Get(adventureID); err == nil {
		cands = append(cands, loadConfig(s.packCfgRoot(in)).PackCode)
	}
	for _, r := range adventureRoots() {
		cands = append(cands, loadConfig(r).PackCode)
	}
	for _, c := range cands {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if list, err := openServers(c); err == nil {
			featuredMu.Lock()
			featuredServers = list
			featuredMu.Unlock()
			if st := s.Settings(); st.Code != c {
				st.Code = c
				s.SaveSettings(st)
			}
			return c
		}
	}
	return ""
}

// adventureServer is the Adventure server address ("" while the servers are locked).
func adventureServer() string {
	for _, f := range featured() {
		if f.ID == "adventure" {
			return f.Address
		}
	}
	return ""
}

// ensureFeaturedInstances creates the instances for vanilla servers once.
func (s *Store) ensureFeaturedInstances() {
	for _, f := range featured() {
		if f.MC == "" {
			continue
		}
		if _, err := s.Get(f.Instance); err == nil {
			continue
		}
		s.Save(&Instance{ID: f.Instance, Name: f.Name, Kind: KindCustom, MC: f.MC, Loader: f.Loader,
			Server: f.Address, RamMB: 4096, Created: time.Now().Unix()})
	}
}

// ---------------------------------------------------------------- status

type ServerStatus struct {
	Online  bool   `json:"online"`
	Players int    `json:"players"`
	Max     int    `json:"max"`
	Version string `json:"version,omitempty"`
	Checked int64  `json:"checked"`
}

type ServerView struct {
	FeaturedServer
	Hidden    bool          `json:"hidden"`
	Status    *ServerStatus `json:"status,omitempty"`
	Installed bool          `json:"installed"`
}

var (
	statusMu    sync.Mutex
	statusCache = map[string]*ServerStatus{}
	statusBusy  = map[string]bool{}
)

// serverStatus returns the last known status and refreshes it in the background every 30 s.
func serverStatus(addr string) *ServerStatus {
	statusMu.Lock()
	st := statusCache[addr]
	stale := st == nil || time.Since(time.Unix(st.Checked, 0)) > 30*time.Second
	if stale && !statusBusy[addr] {
		statusBusy[addr] = true
		go func() {
			res := pingServer(addr, 4*time.Second)
			statusMu.Lock()
			statusCache[addr], statusBusy[addr] = res, false
			statusMu.Unlock()
		}()
	}
	statusMu.Unlock()
	return st
}

func pingServer(addr string, timeout time.Duration) *ServerStatus {
	st := &ServerStatus{Checked: time.Now().Unix()}
	host, portS, err := net.SplitHostPort(addr)
	if err != nil {
		host, portS = addr, "25565"
	}
	port, _ := strconv.Atoi(portS)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, portS), timeout)
	if err != nil {
		return st
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))
	var hs bytes.Buffer
	writeVarInt(&hs, 0x00)
	writeVarInt(&hs, 767) // protocol version: any value works for a status request
	writeVarInt(&hs, len(host))
	hs.WriteString(host)
	binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1)
	var out bytes.Buffer
	writePacket(&out, hs.Bytes())
	writePacket(&out, []byte{0x00})
	if _, err := conn.Write(out.Bytes()); err != nil {
		return st
	}
	r := bufio.NewReader(conn)
	if _, err := readVarInt(r); err != nil { // packet length
		return st
	}
	if id, err := readVarInt(r); err != nil || id != 0 {
		return st
	}
	n, err := readVarInt(r)
	if err != nil || n <= 0 || n > 1<<20 {
		return st
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return st
	}
	var resp struct {
		Version struct {
			Name string `json:"name"`
		} `json:"version"`
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
	}
	if json.Unmarshal(buf, &resp) != nil {
		return st
	}
	st.Online, st.Players, st.Max, st.Version = true, resp.Players.Online, resp.Players.Max, resp.Version.Name
	return st
}

func writePacket(w *bytes.Buffer, data []byte) {
	writeVarInt(w, len(data))
	w.Write(data)
}

func writeVarInt(w *bytes.Buffer, v int) {
	u := uint32(v)
	for {
		if u&^0x7F == 0 {
			w.WriteByte(byte(u))
			return
		}
		w.WriteByte(byte(u&0x7F | 0x80))
		u >>= 7
	}
}

func readVarInt(r io.ByteReader) (int, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int(int32(v)), nil
		}
	}
	return 0, errors.New("varint too long")
}

// ---------------------------------------------------------------- API

func (a *App) Servers() []ServerView {
	st := a.store.Settings()
	hidden := map[string]bool{}
	for _, h := range st.HiddenServers {
		hidden[h] = true
	}
	out := []ServerView{}
	for _, f := range featured() {
		v := ServerView{FeaturedServer: f, Hidden: hidden[f.ID], Status: serverStatus(f.Address)}
		if in, err := a.store.Get(f.Instance); err == nil {
			v.Installed = in.VersionID != ""
			if in.Kind == KindPack {
				v.Installed = loadConfig(a.store.packCfgRoot(in)).PackVersion != ""
			} else if in.Kind == KindAdventure {
				v.Installed = true
			}
		} else if f.ID != "adventure" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (a *App) HideServer(id string, hide bool) error {
	st := a.store.Settings()
	var list []string
	for _, h := range st.HiddenServers {
		if h != id {
			list = append(list, h)
		}
	}
	if hide {
		list = append(list, id)
	}
	st.HiddenServers = list
	return a.store.SaveSettings(st)
}
