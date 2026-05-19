// internal/events/events.go
//
// 📋 LE SYSTÈME D'ÉVÉNEMENTS
//
// Les events sont stockés en JSONL (JSON Lines).
// Chaque ligne = 1 événement JSON.
// On n'écrase jamais le fichier : on ajoute à la fin (append-only).
//
// Exemple de fichier events.jsonl :
//   {"timestamp":"2026-01-01T10:00:00Z","event":"app.installed","app":"jellyfin"}
//   {"timestamp":"2026-01-01T10:05:00Z","event":"app.stopped","app":"jellyfin"}
//
// C'est parfait pour l'audit, le debug, et les futures automatisations.

package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// Emitter gère l'écriture des événements.
type Emitter struct {
	eventsDir string
	mu        sync.Mutex // Un seul écrivain à la fois sur le fichier
}

func NewEmitter(baseDir string) *Emitter {
	return &Emitter{
		eventsDir: filepath.Join(baseDir, "runtime", "events"),
	}
}

// Emit écrit un événement dans le fichier JSONL.
// C'est la fonction principale — toutes les autres sont des raccourcis.
func (e *Emitter) Emit(eventType string, app string, meta map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	event := types.Event{
		Timestamp: time.Now().UTC(),
		Type:      eventType,
		App:       app,
		Meta:      meta,
	}

	// Sérialiser l'event en JSON (une seule ligne, sans indentation)
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("erreur sérialisation event: %w", err)
	}

	// Ouvrir le fichier en mode APPEND (os.O_APPEND = ajouter à la fin)
	// os.O_CREATE = créer si n'existe pas
	// os.O_WRONLY = écriture seule
	file, err := os.OpenFile(
		filepath.Join(e.eventsDir, "events.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fmt.Errorf("erreur ouverture fichier events: %w", err)
	}
	defer file.Close()

	// Écrire la ligne JSON + saut de ligne
	_, err = fmt.Fprintf(file, "%s\n", line)
	return err
}

// ─── Raccourcis pour les events courants ───

func (e *Emitter) AppInstalled(appID string) error {
	return e.Emit("app.installed", appID, nil)
}

func (e *Emitter) AppRemoved(appID string) error {
	return e.Emit("app.removed", appID, nil)
}

func (e *Emitter) AppUpdated(appID, fromVersion, toVersion string) error {
	return e.Emit("app.updated", appID, map[string]string{
		"from": fromVersion,
		"to":   toVersion,
	})
}

func (e *Emitter) AppStarted(appID string) error {
	return e.Emit("app.started", appID, nil)
}

func (e *Emitter) AppStopped(appID string) error {
	return e.Emit("app.stopped", appID, nil)
}

func (e *Emitter) AppError(appID string, errMsg string) error {
	return e.Emit("app.error", appID, map[string]string{
		"error": errMsg,
	})
}

func (e *Emitter) BackupCreated(appID string, path string) error {
	return e.Emit("app.backup", appID, map[string]string{
		"path": path,
	})
}
