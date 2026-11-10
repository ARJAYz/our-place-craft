package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"testing"

	"opcl/packcrypt"
)

const testServers = `[
 {"id":"adventure","name":"Our Place Craft: Adventure","subtitle":"Сборка Adventure · NeoForge 1.21.1","desc":"x","address":"127.0.0.1:1","instance":"our-place-craft","art":"a/art_modded.png"},
 {"id":"transnistria","name":"Our Place Craft: Transnistria","subtitle":"Ванильный сервер · Minecraft 1.21.11","desc":"y","address":"127.0.0.1:2","instance":"transnistria","art":"a/art_vanilla.png","mc":"1.21.11","loader":"vanilla"}
]`

// Tests use their own sealed server list (code "CODE"), built into the "installer" like a stamped one.
func TestMain(m *testing.M) {
	var b bytes.Buffer
	packcrypt.Encrypt(&b, bytes.NewReader([]byte(testServers)), "CODE")
	sealedServers = base64.StdEncoding.EncodeToString(b.Bytes())
	stampedCode = "CODE"
	os.Exit(m.Run())
}
