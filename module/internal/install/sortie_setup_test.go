package install

import (
	"strings"
	"testing"
)

// TestTamponNeGardeQueLaFin : un setup.sh qui construit une image crache des
// mégaoctets de journal. On ne doit pas les accumuler pour n'en garder que la
// queue au moment de l'erreur.
func TestTamponNeGardeQueLaFin(t *testing.T) {
	var tp tampon
	for i := 0; i < 500; i++ {
		if _, err := tp.Write([]byte(strings.Repeat("x", 1000) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if len(tp.octets) > tailleTampon {
		t.Errorf("tampon de %d octets, plafond %d — la mémoire n'est pas bornée",
			len(tp.octets), tailleTampon)
	}

	// La FIN doit survivre : c'est elle qui porte le message d'erreur.
	tp.Write([]byte("DERNIER MOT\n"))
	if !strings.Contains(string(tp.octets), "DERNIER MOT") {
		t.Error("la dernière ligne écrite a été perdue")
	}
}

// TestTamponEcritureGeante : une seule écriture plus grosse que le tampon ne
// doit pas le faire déborder ni perdre sa fin.
func TestTamponEcritureGeante(t *testing.T) {
	var tp tampon
	geant := strings.Repeat("a", tailleTampon*3) + "FIN"
	n, err := tp.Write([]byte(geant))
	if err != nil || n != len(geant) {
		t.Fatalf("Write a rendu (%d, %v), attendu (%d, nil)", n, err, len(geant))
	}
	if len(tp.octets) > tailleTampon {
		t.Errorf("tampon débordé : %d octets", len(tp.octets))
	}
	if !strings.HasSuffix(string(tp.octets), "FIN") {
		t.Error("la fin de l'écriture géante a été perdue")
	}
}

// TestDernieresLignes : c'est le message que l'utilisateur va lire. Il doit être
// dans l'ordre, sans lignes vides, et limité.
func TestDernieresLignes(t *testing.T) {
	sortie := `→ Préparation de Woodpecker CI...

  ✗ Aucune forge (Gitea ou Forgejo) trouvée sur ce serveur.

    Installe d'abord une forge, puis relance :
        caleope install gitea
        caleope install woodpecker
`
	got := dernieresLignes(sortie, 12)

	if !strings.Contains(got, "Aucune forge") {
		t.Errorf("le motif du refus a disparu :\n%s", got)
	}
	// Ordre de lecture préservé : la cause avant le remède.
	if strings.Index(got, "Aucune forge") > strings.Index(got, "caleope install gitea") {
		t.Errorf("lignes rendues à l'envers :\n%s", got)
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.TrimSpace(l) == "" {
			t.Errorf("ligne vide conservée — le diagnostic a l'air tronqué :\n%s", got)
		}
	}
}

func TestDernieresLignesPlafonne(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("ligne\n")
	}
	got := dernieresLignes(b.String(), 5)
	if n := len(strings.Split(got, "\n")); n != 5 {
		t.Errorf("%d lignes rendues, 5 demandées", n)
	}
}

// TestDernieresLignesSortieVide : un setup.sh muet ne doit pas produire un
// message d'erreur avec une queue vide collée dessous.
func TestDernieresLignesSortieVide(t *testing.T) {
	for _, vide := range []string{"", "\n\n\n", "   \n\t\n"} {
		if got := dernieresLignes(vide, 12); got != "" {
			t.Errorf("dernieresLignes(%q) = %q, attendu vide", vide, got)
		}
	}
}
