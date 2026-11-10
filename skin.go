package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var skinURLRe = regexp.MustCompile(`^https?://[^\s"'\\]+$`)

// normalizeSkin turns user input into a canonical value: "" (none), a Minecraft nick,
// or "<http(s) URL> [classic|slim]" (classic = wide arms is the default).
func normalizeSkin(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return "", true
	}
	if validNick(s) {
		return s, true
	}
	f := strings.Fields(s)
	variant := "classic"
	if len(f) == 2 {
		v := strings.ToLower(f[1])
		if v != "classic" && v != "slim" {
			return "", false
		}
		variant = v
	} else if len(f) != 1 {
		return "", false
	}
	if len(f[0]) <= 400 && skinURLRe.MatchString(f[0]) {
		return f[0] + " " + variant, true
	}
	return "", false
}

// skinCommand is the chat command (without the leading slash) that applies the chosen skin
// through the server-side SkinRestorer mod.
func skinCommand(skin string) string {
	switch {
	case skin == "":
		return ""
	case strings.HasPrefix(skin, "http"):
		f := strings.Fields(skin)
		variant := "classic"
		if len(f) == 2 {
			variant = f[1]
		}
		return "skin set web " + variant + ` "` + f[0] + `"`
	default:
		return "skin set mojang " + skin
	}
}

const skinScriptName = "opc_skin.js"

// writeSkinScript writes (or removes) a KubeJS client script that, after joining a server,
// sends the skin command a few times (the first attempts may fall before /login).
func writeSkinScript(gameDir, skin string, enable bool) error {
	dir := filepath.Join(gameDir, "kubejs", "client_scripts")
	path := filepath.Join(dir, skinScriptName)
	cmd := skinCommand(skin)
	if !enable || cmd == "" {
		os.Remove(path)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	js := fmt.Sprintf(`// Создаётся лаунчером Our Place Craft. Не редактируйте вручную.
var OPC_SKIN_CMD = %q
var opcTicks = -1
var opcSent = 0
var opcDone = false
var opcDelay = -1
var OPC_TIMES = [600, 1500, 3000, 6000]

function opcSend(cmd) {
  try { Client.player.connection.sendCommand(cmd); return true } catch (e) {}
  try { Client.runCommand(cmd); return true } catch (e) {}
  return false
}

ClientEvents.loggedIn(function (event) {
  opcTicks = 0
  opcSent = 0
  opcDone = false
  opcDelay = -1
})

// Сразу после успешного входа/регистрации на сервере (мод NeoLogin пишет это в чат).
// В этой версии KubeJS нет своего события чата, поэтому слушаем событие NeoForge.
try {
  NativeEvents.onEvent('net.neoforged.neoforge.client.event.ClientChatReceivedEvent', function (event) {
    if (opcDone || opcDelay >= 0) return
    var t = ''
    try { t = String(event.getMessage().getString()) } catch (e) { try { t = String(event.message) } catch (e2) {} }
    t = t.toLowerCase()
    if (t.indexOf('logged in successfully') >= 0 || t.indexOf('registered successfully') >= 0) {
      opcDelay = 20
    }
  })
} catch (e) {
  console.error('opc_skin: не удалось подписаться на чат, будут запасные попытки: ' + e)
}

ClientEvents.tick(function (event) {
  if (opcTicks < 0 || opcDone || !Client.player) return
  opcTicks++
  if (opcDelay > 0) {
    opcDelay--
    if (opcDelay === 0) { opcSend(OPC_SKIN_CMD); opcDone = true }
    return
  }
  if (opcDelay < 0 && opcSent < OPC_TIMES.length && opcTicks >= OPC_TIMES[opcSent]) {
    opcSend(OPC_SKIN_CMD)
    opcSent++
  }
})
`, cmd)
	return os.WriteFile(path, []byte(js), 0o644)
}
