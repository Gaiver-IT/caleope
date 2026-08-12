package supervisor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gaiver-it/caleope/pkg/types"
)

// TestSansConteneur : la distinction qui compte est entre « cette app n'a pas de
// conteneur PAR CONCEPTION » et « le dossier de cette app a disparu ». La
// première ne doit rien déclencher ; la seconde est une vraie anomalie et doit
// suivre le chemin normal.
func TestSansConteneur(t *testing.T) {
	base := t.TempDir()

	avecYml := filepath.Join(base, "avec-yml")
	os.MkdirAll(avecYml, 0o750)
	os.WriteFile(filepath.Join(avecYml, "docker-compose.yml"), []byte("services: {}\n"), 0o640)

	avecYaml := filepath.Join(base, "avec-yaml")
	os.MkdirAll(avecYaml, 0o750)
	os.WriteFile(filepath.Join(avecYaml, "docker-compose.yaml"), []byte("services: {}\n"), 0o640)

	// Le cas restic : le dossier existe, il porte setup.sh et app.json, mais
	// aucun compose — l'app installe un binaire système.
	outilSysteme := filepath.Join(base, "restic")
	os.MkdirAll(outilSysteme, 0o750)
	os.WriteFile(filepath.Join(outilSysteme, "app.json"), []byte(`{"no_container":true}`), 0o640)

	cas := []struct {
		nom     string
		chemin  string
		attendu bool
	}{
		{"compose .yml présent", avecYml, false},
		{"compose .yaml présent", avecYaml, false},
		{"outil système sans compose", outilSysteme, true},
		{"dossier absent = anomalie, pas conception", filepath.Join(base, "jamais-vu"), false},
	}
	for _, c := range cas {
		if got := sansConteneur(c.chemin); got != c.attendu {
			t.Errorf("%s : sansConteneur = %v, attendu %v", c.nom, got, c.attendu)
		}
	}
}

// TestOutilSystemeJamaisRelance reproduit la panne observée sur le banc d'essai
// le 12/08/2026 : `restic` s'installe très bien, aucun conteneur ne tourne — et
// le superviseur le passait EN ERREUR après une relance vouée à l'échec.
// L'utilisateur voyait une croix rouge sur un outil parfaitement fonctionnel.
func TestOutilSystemeJamaisRelance(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "restic")
	os.MkdirAll(dir, 0o750)
	os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{"no_container":true}`), 0o640)

	a := &types.RuntimeApp{ID: "restic", Status: types.StatusRunning, ComposeDir: dir}
	store := &fakeStore{apps: []*types.RuntimeApp{a}}
	comp := &fakeCompose{running: map[string]bool{dir: false}}

	rep := newSup(store, comp, &fakeMounts{}).Check()

	if len(comp.startCalls) != 0 {
		t.Errorf("relance tentée sur un outil sans conteneur : %v", comp.startCalls)
	}
	if len(rep) != 0 {
		t.Errorf("compte rendu produit alors qu'il n'y avait rien à faire : %+v", rep)
	}
	if a.Status != types.StatusRunning {
		t.Errorf("statut passé à %q — l'outil est pourtant installé et fonctionnel", a.Status)
	}
}
