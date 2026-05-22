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
	"strings"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// EventFilter filtre les events à lire.
type EventFilter struct {
	App   string // filtrer par app (vide = toutes)
	Type  string // filtrer par type d'event (vide = tous)
	Limit int    // nombre max de résultats (0 = 50 par défaut)
}

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

// ─── Lecture des événements ───

// Read lit les derniers events depuis events.jsonl, avec filtres optionnels.
// Retourne les events les plus récents en premier.
func (e *Emitter) Read(filter EventFilter) ([]types.Event, error) {
	filePath := filepath.Join(e.eventsDir, "events.jsonl")

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []types.Event{}, nil
		}
		return nil, fmt.Errorf("lecture events: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	// Parser chaque ligne JSONL
	var all []types.Event
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var ev types.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		// Filtrer par app
		if filter.App != "" && ev.App != filter.App {
			continue
		}
		// Filtrer par type
		if filter.Type != "" && ev.Type != filter.Type {
			continue
		}
		all = append(all, ev)
	}

	// Garder les N derniers (les plus récents)
	if len(all) > limit {
		all = all[len(all)-limit:]
	}

	// Inverser : plus récent en premier
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}

	return all, nil
}
