package install

import (
	"os"
	"path/filepath"
	"testing"
)

func ecrire(t *testing.T, dir, nom, contenu string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, nom), []byte(contenu), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lire(t *testing.T, dir, nom string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, nom))
	if err != nil {
		t.Fatalf("lecture %s: %v", nom, err)
	}
	return string(b)
}

// Le cas qui compte : la montée réécrit compose.yml, elle échoue, on doit
// retrouver EXACTEMENT la description d'avant.
func TestInstantaneRestaureLaDescriptionPrecedente(t *testing.T) {
	dir := t.TempDir()
	ecrire(t, dir, "compose.yml", "image: postgres:14-ancienne\n")
	ecrire(t, dir, "app.env", "PORT=8006\n")

	repli := prendreInstantane(dir)
	if repli == nil {
		t.Fatal("aucun instantané pris alors que les fichiers existent")
	}

	// la montée écrase tout
	ecrire(t, dir, "compose.yml", "image: postgres:16-nouvelle\n")
	ecrire(t, dir, "app.env", "PORT=9999\n")

	if err := repli.restaurer(dir); err != nil {
		t.Fatalf("restauration: %v", err)
	}
	if got := lire(t, dir, "compose.yml"); got != "image: postgres:14-ancienne\n" {
		t.Fatalf("compose.yml non restauré : %q", got)
	}
	if got := lire(t, dir, "app.env"); got != "PORT=8006\n" {
		t.Fatalf("app.env non restauré : %q", got)
	}
}

// Installation neuve : rien à reprendre. L'appelant s'en sert pour distinguer
// « montée ratée » de « installation ratée », qui n'appellent pas le même geste.
func TestInstantaneVideSurRepertoireNeuf(t *testing.T) {
	if repli := prendreInstantane(t.TempDir()); repli != nil {
		t.Fatalf("instantané non vide sur un répertoire neuf : %d fichier(s)", repli.nombreFichiers())
	}
}

// Une surcouche absente ne doit pas empêcher la reprise des autres fichiers.
func TestInstantanePartielSansSurcouche(t *testing.T) {
	dir := t.TempDir()
	ecrire(t, dir, "compose.yml", "a\n")

	repli := prendreInstantane(dir)
	if repli.nombreFichiers() != 1 {
		t.Fatalf("attendu 1 fichier, obtenu %d", repli.nombreFichiers())
	}
	ecrire(t, dir, "compose.yml", "b\n")
	if err := repli.restaurer(dir); err != nil {
		t.Fatalf("restauration: %v", err)
	}
	if got := lire(t, dir, "compose.yml"); got != "a\n" {
		t.Fatalf("compose.yml non restauré : %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.override.yml")); !os.IsNotExist(err) {
		t.Fatal("une surcouche inexistante a été fabriquée par la restauration")
	}
}

// La surcouche (GPU, montages maison) fait partie de ce qui décrit la stack :
// elle doit être reprise comme le reste.
func TestInstantaneRepriseDeLaSurcouche(t *testing.T) {
	dir := t.TempDir()
	ecrire(t, dir, "compose.yml", "base\n")
	ecrire(t, dir, "compose.override.yml", "thumbs-local\n")

	repli := prendreInstantane(dir)
	ecrire(t, dir, "compose.override.yml", "écrasée\n")
	if err := repli.restaurer(dir); err != nil {
		t.Fatalf("restauration: %v", err)
	}
	if got := lire(t, dir, "compose.override.yml"); got != "thumbs-local\n" {
		t.Fatalf("surcouche non restaurée : %q", got)
	}
}

// Un instantané nil (installation neuve) ne doit rien faire, pas paniquer.
func TestInstantaneNilInoffensif(t *testing.T) {
	var repli *instantane
	if err := repli.restaurer(t.TempDir()); err != nil {
		t.Fatalf("restauration d'un instantané nil: %v", err)
	}
	if repli.nombreFichiers() != 0 {
		t.Fatal("nombreFichiers sur nil devrait valoir 0")
	}
}
