package postes

import (
	"testing"
	"time"
)

func gest(t *testing.T) *Gestionnaire {
	t.Helper()
	g := Nouveau(t.TempDir())
	g.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return g
}

func profilDeBase(t *testing.T, g *Gestionnaire) {
	t.Helper()
	if err := g.EnregistrerProfil(Profil{
		Nom:      "portable",
		Paquets:  []string{"git", "ripgrep=rg"},
		Dossiers: []Dossier{{Nom: "fonds", Chemin: "~/Images/Fonds", Sens: SensDeuxSens}},
	}); err != nil {
		t.Fatal(err)
	}
}

// Le parcours complet : je crée un profil, je fabrique un code, je le tape sur
// la machine, je reçois ma configuration.
func TestParcoursAppairageComplet(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)

	code, exp, err := g.CreerJeton("portable")
	if err != nil {
		t.Fatal(err)
	}
	if len(code) < 12 {
		t.Fatalf("jeton trop court pour être sûr : %q", code)
	}
	if !exp.After(time.Unix(1_700_000_000, 0)) {
		t.Fatal("le jeton devrait expirer dans le futur")
	}

	m, err := g.Appairer(code, "mac-de-ewen", "macos")
	if err != nil {
		t.Fatal(err)
	}
	if m.Cle == "" || m.Profil != "portable" {
		t.Fatalf("appairage incomplet : %+v", m)
	}

	_, p, err := g.ProfilDeLaMachine(m.Cle)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Paquets) != 2 || len(p.Dossiers) != 1 {
		t.Fatalf("profil mal rendu : %+v", p)
	}
}

// Un jeton ne sert qu'UNE fois : sinon un code recopié dans un salon de
// discussion appaire autant de machines qu'on veut, avec les mêmes droits.
func TestJetonNeServQuUneFois(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)
	code, _, _ := g.CreerJeton("portable")

	if _, err := g.Appairer(code, "poste-1", "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Appairer(code, "poste-2", "linux"); err == nil {
		t.Fatal("le même jeton a appairé une deuxième machine")
	}
}

// Un jeton périmé est refusé.
func TestJetonPerime(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)
	code, _, _ := g.CreerJeton("portable")

	g.now = func() time.Time { return time.Unix(1_700_000_000, 0).Add(DureeJeton + time.Minute) }
	if _, err := g.Appairer(code, "poste", "linux"); err == nil {
		t.Fatal("un jeton périmé a été accepté")
	}
}

// Une clé inconnue ne doit rien obtenir : c'est la seule barrière entre une
// machine appairée et n'importe qui d'autre.
func TestCleInconnueNObtientRien(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)
	if _, _, err := g.ProfilDeLaMachine("clé-inventée"); err == nil {
		t.Fatal("une clé inventée a obtenu un profil")
	}
}

// La clé ne doit JAMAIS sortir par la liste des machines.
func TestLaCleNeFuitPasDansLaListe(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)
	code, _, _ := g.CreerJeton("portable")
	m, _ := g.Appairer(code, "poste", "linux")

	for _, vue := range g.ListerMachines() {
		if vue.Cle != "" {
			t.Fatal("la clé de la machine est exposée par ListerMachines")
		}
		if vue.Empreinte == "" || len(vue.Empreinte) >= len(m.Cle) {
			t.Fatalf("empreinte inutilisable : %q", vue.Empreinte)
		}
	}
}

// Supprimer un profil encore utilisé viderait la configuration des machines qui
// s'en servent, sans prévenir.
func TestProfilUtiliseNeSeSupprimePas(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)
	code, _, _ := g.CreerJeton("portable")
	if _, err := g.Appairer(code, "poste", "linux"); err != nil {
		t.Fatal(err)
	}
	if err := g.SupprimerProfil("portable"); err == nil {
		t.Fatal("un profil encore utilisé a été supprimé")
	}
}

// Oublier une machine, c'est révoquer sa clé.
func TestOublierUneMachineRevoqueSaCle(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)
	code, _, _ := g.CreerJeton("portable")
	m, _ := g.Appairer(code, "poste", "linux")

	if err := g.OublierMachine(m.Empreinte); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.ProfilDeLaMachine(m.Cle); err == nil {
		t.Fatal("la clé d'une machine oubliée fonctionne encore")
	}
}

// Un profil sans nom, ou avec un nom qui casserait une URL, est refusé.
func TestProfilsInvalidesRefuses(t *testing.T) {
	g := gest(t)
	for _, nom := range []string{"", "  ", "avec/slash", "avec espace"} {
		if err := g.EnregistrerProfil(Profil{Nom: nom}); err == nil {
			t.Fatalf("nom de profil accepté à tort : %q", nom)
		}
	}
}

// Un sens de dossier non renseigné prend une valeur sûre plutôt que de rester vide.
func TestSensParDefautSurUnDossier(t *testing.T) {
	p := Profil{Nom: "x", Dossiers: []Dossier{{Nom: "docs", Chemin: "~/Documents"}}}
	if err := ValiderProfil(&p); err != nil {
		t.Fatal(err)
	}
	if p.Dossiers[0].Sens != SensDeuxSens {
		t.Fatalf("sens par défaut inattendu : %q", p.Dossiers[0].Sens)
	}
}

// Deux machines appairées doivent recevoir des clés DIFFÉRENTES.
func TestDeuxMachinesDeuxCles(t *testing.T) {
	g := gest(t)
	profilDeBase(t, g)
	c1, _, _ := g.CreerJeton("portable")
	m1, _ := g.Appairer(c1, "a", "linux")
	c2, _, _ := g.CreerJeton("portable")
	m2, _ := g.Appairer(c2, "b", "macos")
	if m1.Cle == m2.Cle {
		t.Fatal("deux machines partagent la même clé")
	}
}
