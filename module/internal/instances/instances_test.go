package instances

import "testing"

func TestDecouperEtComposer(t *testing.T) {
	cas := []struct{ id, paquet, instance string }{
		{"minecraft", "minecraft", ""},
		{"minecraft@homestead", "minecraft", "homestead"},
		{"arr-stack", "arr-stack", ""},
		{"pterodactyl-panel@essai", "pterodactyl-panel", "essai"},
	}
	for _, c := range cas {
		p, i := Decouper(c.id)
		if p != c.paquet || i != c.instance {
			t.Fatalf("%q → (%q,%q), attendu (%q,%q)", c.id, p, i, c.paquet, c.instance)
		}
		if got := Composer(c.paquet, c.instance); got != c.id {
			t.Fatalf("recomposition : %q au lieu de %q", got, c.id)
		}
	}
}

// Le nom d'instance devient un nom de dossier : ce qui pourrait écrire ailleurs
// que prévu doit être refusé AVANT d'arriver au système de fichiers.
func TestValiderNomRefuseCeQuiSortiraitDuDossier(t *testing.T) {
	for _, mauvais := range []string{
		"", "..", "../../etc", "avec/slash", "avec espace", "MAJUSCULE",
		"-debut", "fin-", "accentué", "trop-long-nom-dinstance-qui-depasse-la-limite-fixee",
	} {
		if err := ValiderNom(mauvais); err == nil {
			t.Fatalf("nom accepté à tort : %q", mauvais)
		}
	}
	for _, bon := range []string{"homestead", "survie-2", "a", "serveur-des-copains"} {
		if err := ValiderNom(bon); err != nil {
			t.Fatalf("nom refusé à tort : %q (%v)", bon, err)
		}
	}
}

// Une application qui ne déclare pas le multi-instance ne doit PAS pouvoir être
// dupliquée : c'est le garde-fou qui empêche d'écraser des données.
func TestVerifierRefuseLeMultiPourUnPaquetQuiNeLeDeclarePas(t *testing.T) {
	if err := Verifier("immich@second", false); err == nil {
		t.Fatal("une app non compatible a été dupliquée")
	}
	if err := Verifier("minecraft@homestead", true); err != nil {
		t.Fatalf("une app compatible a été refusée : %v", err)
	}
	// Sans suffixe, l'installation ordinaire passe même sans la déclaration.
	if err := Verifier("immich", false); err != nil {
		t.Fatalf("installation ordinaire refusée : %v", err)
	}
}

func TestEstInstance(t *testing.T) {
	if EstInstance("minecraft") {
		t.Fatal("une app sans suffixe n'est pas une instance")
	}
	if !EstInstance("minecraft@homestead") {
		t.Fatal("suffixe non reconnu")
	}
}
