package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// handleTerminal ouvre un terminal PTY en user-caleope via WebSocket.
// Protocole :
//
//	client → serveur : bytes bruts (input) ou JSON {"r":{"c":cols,"r":rows}} (resize)
//	serveur → client : bytes bruts (output PTY)
func handleTerminal(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Résoudre user-caleope
	u, err := user.Lookup("user-caleope")
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\x1b[31m❌ user-caleope introuvable\x1b[0m\r\n"))
		return
	}
	uid64, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid64, _ := strconv.ParseUint(u.Gid, 10, 32)

	cmd := exec.Command("/bin/bash", "--login")
	cmd.Env = []string{
		"TERM=xterm-256color",
		"HOME=" + u.HomeDir,
		"USER=user-caleope",
		"LOGNAME=user-caleope",
		"SHELL=/bin/bash",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=fr_FR.UTF-8",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid64),
			Gid: uint32(gid64),
		},
		Setsid: true,
	}
	cmd.Dir = u.HomeDir

	ptmx, err := pty.Start(cmd)
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte("\x1b[31m❌ Impossible de démarrer le terminal : "+err.Error()+"\x1b[0m\r\n"))
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	// WebSocket → PTY (input / resize)
	go func() {
		var resizeMsg struct {
			R *struct {
				C uint16 `json:"c"`
				R uint16 `json:"r"`
			} `json:"r"`
		}
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if len(msg) > 0 && msg[0] == '{' {
				if json.Unmarshal(msg, &resizeMsg) == nil && resizeMsg.R != nil {
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Rows: resizeMsg.R.R,
						Cols: resizeMsg.R.C,
					})
					continue
				}
			}
			if _, err := ptmx.Write(msg); err != nil {
				return
			}
		}
	}()

	// PTY → WebSocket (output)
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				_, _ = os.Stderr.WriteString("pty: " + err.Error() + "\n")
			}
			return
		}
	}
}
