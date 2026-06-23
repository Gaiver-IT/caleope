// internal/audit/audit.go
//
// 📋 Journal d'audit — toutes les actions sensibles loguées dans /var/log/caleope/audit.log

package audit

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const logPath = "/var/log/caleope/audit.log"

// Action représente le type d'action auditée.
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
	ActionSecretsShow Action = "SECRETS_SHOW"
	ActionStartup   Action = "STARTUP"
	ActionError     Action = "ERROR"
)

// Log ajoute une ligne au journal d'audit.
// Format : 2006-01-02T15:04:05Z07:00 ACTION app=<id> result=<result>
// Silencieux si le fichier n'est pas accessible.
func Log(action Action, appID, result string) {
	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s %s app=%s result=%s\n", ts, action, appID, result)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return // silencieux — audit ne doit jamais bloquer une opération
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// Read retourne les n dernières lignes du journal.
// n <= 0 retourne toutes les lignes.
func Read(n int) ([]string, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("lecture audit log: %w", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
