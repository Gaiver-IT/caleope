package sso

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeResolver struct{ domain string }

func (f fakeResolver) AppDomain(string) string { return f.domain }

// ── HasKey : la nuance « valeur non vide » est le cœur du sujet ──────────────

func TestHasKey(t *testing.T) {
	cas := []struct {
		nom     string
		contenu string
		attendu bool
	}{
		{"clé présente et remplie", "A=1\nAUTHENTIK_DOMAIN=auth.example.org\nB=2", true},
		{"clé absente", "A=1\nB=2", false},

		// Ce cas est LA raison d'être de la fonction : un script qui substitue
		// une variable vide laisse `AUTHENTIK_DOMAIN=` derrière lui. Un `grep`
		// naïf le trouve, l'application qui le lit n'en tire rien.
		{"clé présente mais VIDE", "AUTHENTIK_DOMAIN=\nB=2", false},
		{"clé présente mais espaces", "AUTHENTIK_DOMAIN=   \n", false},

		{"ligne commentée ne compte pas", "#AUTHENTIK_DOMAIN=auth.example.org\n", false},
		{"préfixe voisin ne compte pas", "AUTHENTIK_DOMAIN_EXTRA=x\n", false},
		{"espaces autour du nom", "  AUTHENTIK_DOMAIN = auth.example.org\n", false},
		{"fichier vide", "", false},
	}
	for _, c := range cas {
		if got := HasKey(c.contenu, "AUTHENTIK_DOMAIN"); got != c.attendu {
			t.Errorf("%s : attendu %v, obtenu %v", c.nom, c.attendu, got)
		}
	}
}

// ── EnsureAuthentikDomain ───────────────────────────────────────────────────

func prepare(t *testing.T, secrets string, installed bool) string {
	t.Helper()
	base := t.TempDir()
	if installed {
		if err := os.MkdirAll(filepath.Join(base, "apps-installed", "authentik"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if secrets != "" || installed {
		dir := filepath.Join(base, "app-config", "authentik")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if secrets != "" {
			if err := os.WriteFile(filepath.Join(dir, "secrets.env"), []byte(secrets), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return base
}

func lire(t *testing.T, base string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(base, "app-config", "authentik", "secrets.env"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAjouteLaCleManquante(t *testing.T) {
	base := prepare(t, "AUTHENTIK_BOOTSTRAP_TOKEN=secret123\nPOSTGRES_PASSWORD=pg\n", true)
	changed, err := EnsureAuthentikDomain(base, fakeResolver{"auth.exemple.org"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("le fichier aurait dû être modifié")
	}
	out := lire(t, base)
	if !strings.Contains(out, "AUTHENTIK_DOMAIN=auth.exemple.org") {
		t.Errorf("clé absente après ajout :\n%s", out)
	}
	// LA garantie qui compte : on n'a pas touché aux secrets existants.
	for _, must := range []string{"AUTHENTIK_BOOTSTRAP_TOKEN=secret123", "POSTGRES_PASSWORD=pg"} {
		if !strings.Contains(out, must) {
			t.Errorf("secret existant perdu : %s", must)
		}
	}
}

func TestNeTouchePasSiDejaPresente(t *testing.T) {
	orig := "AUTHENTIK_DOMAIN=deja.exemple.org\nPOSTGRES_PASSWORD=pg\n"
	base := prepare(t, orig, true)
	changed, err := EnsureAuthentikDomain(base, fakeResolver{"autre.exemple.org"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("le fichier ne devait pas être modifié")
	}
	if lire(t, base) != orig {
		t.Errorf("le fichier a été altéré :\n%s", lire(t, base))
	}
}

// Une valeur vide doit être COMPLÉTÉE : c'est exactement l'état que laisse un
// script ayant substitué une variable absente.
func TestCompleteUneValeurVide(t *testing.T) {
	base := prepare(t, "AUTHENTIK_DOMAIN=\nPOSTGRES_PASSWORD=pg\n", true)
	changed, err := EnsureAuthentikDomain(base, fakeResolver{"auth.exemple.org"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("une valeur vide aurait dû être complétée")
	}
	if !strings.Contains(lire(t, base), "AUTHENTIK_DOMAIN=auth.exemple.org") {
		t.Error("la valeur n'a pas été ajoutée")
	}
}

func TestAuthentikNonInstalleNeFaitRien(t *testing.T) {
	base := prepare(t, "", false)
	changed, err := EnsureAuthentikDomain(base, fakeResolver{"auth.exemple.org"}, nil)
	if err != nil {
		t.Fatalf("ne doit pas être une erreur : %v", err)
	}
	if changed {
		t.Fatal("rien ne devait être fait sans Authentik installé")
	}
}

// Domaine inconnu : on préfère ne rien écrire plutôt qu'inscrire une valeur
// fausse que 18 apps utiliseraient ensuite.
func TestDomaineInconnuNecritRien(t *testing.T) {
	base := prepare(t, "POSTGRES_PASSWORD=pg\n", true)
	var msgs []string
	changed, err := EnsureAuthentikDomain(base, fakeResolver{"  "}, func(f string, a ...any) {
		msgs = append(msgs, f)
	})
	if err != nil || changed {
		t.Fatalf("attendu aucun changement, obtenu changed=%v err=%v", changed, err)
	}
	if strings.Contains(lire(t, base), "AUTHENTIK_DOMAIN") {
		t.Error("une clé a été écrite alors que le domaine est inconnu")
	}
	if len(msgs) == 0 {
		t.Error("le cas aurait dû être journalisé")
	}
}

func TestFichierSansSautDeLigneFinal(t *testing.T) {
	base := prepare(t, "POSTGRES_PASSWORD=pg", true) // pas de \n final
	if _, err := EnsureAuthentikDomain(base, fakeResolver{"auth.exemple.org"}, nil); err != nil {
		t.Fatal(err)
	}
	out := lire(t, base)
	if strings.Contains(out, "POSTGRES_PASSWORD=pg#") || strings.Contains(out, "pg\n#") == false {
		// on veut que la dernière ligne existante reste intacte et séparée
		if !strings.Contains(out, "POSTGRES_PASSWORD=pg\n") {
			t.Errorf("la dernière ligne a été collée à l'ajout :\n%s", out)
		}
	}
	if !HasKey(out, "AUTHENTIK_DOMAIN") {
		t.Error("clé non ajoutée")
	}
}

// Rejouer doit être sans effet : le daemon appelle ceci à CHAQUE démarrage.
func TestIdempotence(t *testing.T) {
	base := prepare(t, "POSTGRES_PASSWORD=pg\n", true)
	r := fakeResolver{"auth.exemple.org"}
	if _, err := EnsureAuthentikDomain(base, r, nil); err != nil {
		t.Fatal(err)
	}
	apres1 := lire(t, base)
	for i := 0; i < 3; i++ {
		changed, err := EnsureAuthentikDomain(base, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Fatalf("passage %d : le fichier a été modifié une seconde fois", i+2)
		}
	}
	if lire(t, base) != apres1 {
		t.Error("le fichier a dérivé entre deux passages")
	}
}

func TestDroitsPreserves(t *testing.T) {
	base := prepare(t, "POSTGRES_PASSWORD=pg\n", true)
	if _, err := EnsureAuthentikDomain(base, fakeResolver{"auth.exemple.org"}, nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(base, "app-config", "authentik", "secrets.env"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("droits attendus 600, obtenus %o — un secrets.env ne doit pas s'ouvrir", fi.Mode().Perm())
	}
}
