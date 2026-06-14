// internal/audit/audit.go
//
// 📋 AUDIT LOG — traçabilité des actions daemon
//
// Toutes les actions critiques (install, remove, configure, start, stop)
// sont loggées dans /var/log/caleope/audit.log au format :
//   2026-06-14T15:04:05Z  INSTALL  jellyfin  user-caleope  OK
//
// Le fichier est en append-only. Root peut le lire via `caleope audit`.

package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const logPath = "/var/log/caleope/audit.log"

// Action représente une action auditée.
type Action string

const (
	ActionInstall   Action = "INSTALL"
	ActionRemove    Action = "REMOVE"
	ActionStart     Action = "START"
	ActionStop      Action = "STOP"
	ActionRestart   Action = "RESTART"
	ActionConfigure Action = "CONFIGURE"
	ActionUpgrade   Action = "UPGRADE"
	ActionBackup    Action = "BACKUP"
	ActionRestore   Action = "RESTORE"
	ActionSecrets   Action = "SECRETS_SHOW"
)

// Log enregistre une action dans le fichier d'audit.
// result = "OK" ou message d'erreur.
// Non-bloquant : si le log échoue (ex: /var/log non accessible), on ignore silencieusement.
func Log(action Action, appID, result string) {
	_ = os.MkdirAll(filepath.Dir(logPath), 0750)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s\t%-12s\t%-30s\t%s\n", ts, string(action), appID, result)
	_, _ = f.WriteString(line)
}

// Read retourne les N dernières lignes du log d'audit.
func Read(n int) ([]string, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	lines := splitLines(string(data))
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
