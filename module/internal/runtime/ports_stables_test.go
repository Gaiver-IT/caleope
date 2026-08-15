package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

// managerDeTest : un Manager sur une base vierge, avec le répertoire runtime/
// que le gestionnaire de ports attend.
func managerDeTest(t *testing.T) *Manager {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewManager(base)
}

// Le port d'une app ne doit PAS bouger d'une montée de version à l'autre.
// Avant correctif, AllocatePort comptait la réservation de l'app parmi les
// ports « occupés » et lui en donnait un autre : le port publié changeait à
// chaque `install --force`.
func TestAllocatePortRendLeMemePortALaReinstallation(t *testing.T) {
	m := managerDeTest(t)

	premier, err := m.AllocatePort("immich-web", 8000, 8100)
	if err != nil {
		t.Fatalf("première allocation: %v", err)
	}

	second, err := m.AllocatePort("immich-web", 8000, 8100)
	if err != nil {
		t.Fatalf("seconde allocation: %v", err)
	}

	if second != premier {
		t.Fatalf("le port a dérivé à la réinstallation : %d puis %d", premier, second)
	}
}

// Deux apps distinctes gardent des ports distincts : la réutilisation ne doit
// pas dégénérer en partage.
func TestAllocatePortDonneDesPortsDistinctsADesAppsDistinctes(t *testing.T) {
	m := managerDeTest(t)

	a, err := m.AllocatePort("app-a", 8000, 8100)
	if err != nil {
		t.Fatalf("app-a: %v", err)
	}
	b, err := m.AllocatePort("app-b", 8000, 8100)
	if err != nil {
		t.Fatalf("app-b: %v", err)
	}
	if a == b {
		t.Fatalf("deux apps ont reçu le même port %d", a)
	}
}

// Une réservation à 0 (fichier abîmé) ne doit pas être rendue telle quelle :
// on réalloue un vrai port.
func TestAllocatePortIgnoreUneReservationVide(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(base)
	if err := m.writePorts(map[string]int{"cassee-web": 0}); err != nil {
		t.Fatalf("écriture: %v", err)
	}

	p, err := m.AllocatePort("cassee-web", 8000, 8100)
	if err != nil {
		t.Fatalf("allocation: %v", err)
	}
	if p == 0 {
		t.Fatal("un port nul a été rendu au lieu d'être réalloué")
	}
}
