package posteclient

import "testing"

// La règle de présence est le cœur du client : c'est elle qui a failli faire
// installer des doublons Homebrew par-dessus les outils système d'un Mac.
func TestManquantsIgnoreCeQueLeSystemeFournitDeja(t *testing.T) {
	p := Profil{Paquets: []string{"git", "outil-qui-nexiste-pas-du-tout"}}
	// « git » n'est PAS dans l'inventaire du gestionnaire, mais la commande
	// existe sur toute machine de développement : il ne doit pas être réclamé.
	m := Manquants(p, map[string]bool{})
	for _, x := range m {
		if x == "git" {
			t.Fatal("git réclamé alors que la commande répond déjà")
		}
	}
	if len(m) != 1 || m[0] != "outil-qui-nexiste-pas-du-tout" {
		t.Fatalf("manquants inattendus : %v", m)
	}
}

// Le préfixe « ! » sert quand on veut la version du gestionnaire malgré tout.
func TestManquantsForceLeGestionnaireAvecPointExclamation(t *testing.T) {
	p := Profil{Paquets: []string{"!git"}}
	m := Manquants(p, map[string]bool{})
	if len(m) != 1 || m[0] != "git" {
		t.Fatalf("« !git » aurait dû être réclamé : %v", m)
	}
	// …mais s'il est déjà connu du gestionnaire, il ne l'est plus.
	if len(Manquants(p, map[string]bool{"git": true})) != 0 {
		t.Fatal("« !git » réclamé alors que le gestionnaire le connaît")
	}
}

func TestDecoupeDesFormatsDeLigne(t *testing.T) {
	cas := []struct {
		ligne, nom, cmd string
		force           bool
	}{
		{"git", "git", "git", false},
		{"ripgrep=rg", "ripgrep", "rg", false},
		{"!coreutils=gls", "coreutils", "gls", true},
		{"  jq  ", "jq", "jq", false},
	}
	for _, c := range cas {
		n, cm, f := Decoupe(c.ligne)
		if n != c.nom || cm != c.cmd || f != c.force {
			t.Fatalf("%q → (%q,%q,%v), attendu (%q,%q,%v)", c.ligne, n, cm, f, c.nom, c.cmd, c.force)
		}
	}
}

// Les lignes vides et les commentaires ne doivent pas devenir des paquets.
func TestManquantsIgnoreVidesEtCommentaires(t *testing.T) {
	p := Profil{Paquets: []string{"", "   ", "# un commentaire", "outil-inexistant-xyz"}}
	if m := Manquants(p, map[string]bool{}); len(m) != 1 {
		t.Fatalf("lignes non pertinentes prises pour des paquets : %v", m)
	}
}

// L'invitation doit faire l'aller-retour sans perte : c'est la seule chose que
// l'utilisateur manipule.
func TestInvitationAllerRetour(t *testing.T) {
	inv := FabriquerInvitation("https://caleope.exemple.fr/", "45e2d3fcb0492ff28c")
	s, c, err := LireInvitation(inv)
	if err != nil {
		t.Fatal(err)
	}
	if s != "https://caleope.exemple.fr" || c != "45e2d3fcb0492ff28c" {
		t.Fatalf("aller-retour cassé : %q / %q", s, c)
	}
}

// Un presse-papiers ajoute volontiers des espaces et des sauts de ligne.
func TestInvitationToleranteAuPressePapiers(t *testing.T) {
	inv := FabriquerInvitation("https://x.fr", "abc123")
	for _, variante := range []string{" " + inv, inv + "\n", "\n\t" + inv + "  "} {
		if _, _, err := LireInvitation(variante); err != nil {
			t.Fatalf("variante refusée à tort (%q) : %v", variante, err)
		}
	}
}

// Repli sur ce qu'un humain écrit naturellement : adresse puis code.
func TestInvitationAccepteLesDeuxChampsSepares(t *testing.T) {
	s, c, err := LireInvitation("https://caleope.exemple.fr/  abc123")
	if err != nil {
		t.Fatal(err)
	}
	if s != "https://caleope.exemple.fr" || c != "abc123" {
		t.Fatalf("repli cassé : %q / %q", s, c)
	}
}

// Ce qui n'est pas exploitable doit produire un message utile, pas un plantage.
func TestInvitationRefuseCeQuiNEstPasExploitable(t *testing.T) {
	for _, mauvais := range []string{"", "   ", "CALEOPE1:pas-du-base64!!", "CALEOPE1:" + "aGVsbG8", "juste-un-mot"} {
		if _, _, err := LireInvitation(mauvais); err == nil {
			t.Fatalf("entrée acceptée à tort : %q", mauvais)
		}
	}
}
