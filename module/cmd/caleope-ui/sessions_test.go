// cmd/caleope-ui/sessions_test.go
//
// Tests de la persistance des sessions (survie au redémarrage de caleope-ui).

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionsCreateValidDelete(t *testing.T) {
	s := newSessions("") // pas de persistance
	tok := s.create()
	if !s.valid(tok) {
		t.Fatal("session créée devrait être valide")
	}
	if s.valid("jeton-inexistant") {
		t.Fatal("un jeton inconnu ne devrait pas être valide")
	}
	s.delete(tok)
	if s.valid(tok) {
		t.Fatal("session supprimée ne devrait plus être valide")
	}
}

func TestSessionsPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "ui", "sessions.json")

	// "Instance 1" crée une session
	s1 := newSessions(path)
	tok := s1.create()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sessions.json devrait être écrit: %v", err)
	}

	// "Instance 2" (redémarrage) recharge depuis le disque
	s2 := newSessions(path)
	if !s2.valid(tok) {
		t.Fatal("la session devrait survivre au redémarrage (rechargée du disque)")
	}
}

func TestSessionsDeletePersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	s1 := newSessions(path)
	tok := s1.create()
	s1.delete(tok)

	// Après redémarrage, la session supprimée ne doit pas réapparaître
	s2 := newSessions(path)
	if s2.valid(tok) {
		t.Fatal("une session supprimée ne doit pas être rechargée")
	}
}

func TestSessionsExpiredNotLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	// Écrire à la main un fichier avec un jeton déjà expiré
	expired := map[string]int64{"vieux-jeton": time.Now().Add(-1 * time.Hour).Unix()}
	raw, _ := json.Marshal(expired)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := newSessions(path)
	if s.valid("vieux-jeton") {
		t.Fatal("une session expirée ne doit pas être chargée")
	}
}

func TestSessionsCorruptFileIsFailsafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("{ pas du json valide"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ne doit pas paniquer ; démarre avec un store vide, puis fonctionne normalement.
	s := newSessions(path)
	tok := s.create()
	if !s.valid(tok) {
		t.Fatal("après un fichier corrompu, la création de session doit quand même marcher")
	}
}

func TestSessionsNoPathNoFile(t *testing.T) {
	dir := t.TempDir()
	s := newSessions("") // persistance désactivée
	s.create()
	// Aucun fichier ne doit être créé quand path est vide.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("aucun fichier attendu avec path vide, trouvé %d", len(entries))
	}
}

func TestSessionsFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	s := newSessions(path)
	s.create()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("sessions.json devrait être 0600 (jetons sensibles), got %o", perm)
	}
}
