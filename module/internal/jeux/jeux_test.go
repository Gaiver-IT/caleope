package jeux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gest(t *testing.T) (*Gestionnaire, string) {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "app-data", "minecraft@homestead", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Nouveau(base), base
}

// Le « @ » est refusé par Docker : se tromper ici, c'est parler au mauvais
// conteneur — ou à aucun.
func TestNomConteneur(t *testing.T) {
	if got := NomConteneur("minecraft"); got != "minecraft" {
		t.Fatalf("app simple altérée : %q", got)
	}
	if got := NomConteneur("minecraft@homestead"); got != "minecraft-homestead" {
		t.Fatalf("instance mal traduite : %q", got)
	}
}

// Écrire une propriété ne doit PAS réécrire le fichier : les commentaires et
// les clés inconnues d'une version future doivent survivre.
func TestEcrireProprietesPreserveLeReste(t *testing.T) {
	g, base := gest(t)
	p := filepath.Join(base, "app-data", "minecraft@homestead", "data", "server.properties")
	depart := "#Minecraft server properties\n#Tue Aug 19\ndifficulty=easy\nmotd=A Minecraft Server\nune-cle-du-futur=42\n"
	if err := os.WriteFile(p, []byte(depart), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := g.EcrireProprietes("minecraft@homestead", map[string]string{"difficulty": "hard"}); err != nil {
		t.Fatal(err)
	}
	apres, _ := os.ReadFile(p)
	s := string(apres)
	if !strings.Contains(s, "difficulty=hard") {
		t.Fatal("la modification n'a pas été appliquée")
	}
	if !strings.Contains(s, "#Minecraft server properties") {
		t.Fatal("les commentaires ont été perdus")
	}
	if !strings.Contains(s, "une-cle-du-futur=42") {
		t.Fatal("une clé inconnue a été effacée")
	}
	if !strings.Contains(s, "motd=A Minecraft Server") {
		t.Fatal("une clé non modifiée a été perdue")
	}
}

// Une propriété absente du fichier doit être AJOUTÉE : l'utilisateur qui la
// fixe attend qu'elle prenne effet.
func TestUnePropriteAbsenteEstAjoutee(t *testing.T) {
	g, base := gest(t)
	p := filepath.Join(base, "app-data", "minecraft@homestead", "data", "server.properties")
	os.WriteFile(p, []byte("difficulty=easy\n"), 0o644)

	if err := g.EcrireProprietes("minecraft@homestead", map[string]string{"view-distance": "8"}); err != nil {
		t.Fatal(err)
	}
	apres, _ := os.ReadFile(p)
	if !strings.Contains(string(apres), "view-distance=8") {
		t.Fatal("la propriété absente n'a pas été ajoutée")
	}
}

// Un saut de ligne dans une valeur écrirait une propriété supplémentaire :
// c'est l'injection du monde des fichiers de configuration.
func TestUneValeurAvecSautDeLigneEstRefusee(t *testing.T) {
	if err := ValiderProprieteAppliquee("motd", "coucou\nop-permission-level=4"); err == nil {
		t.Fatal("saut de ligne accepté dans une valeur")
	}
	if err := ValiderProprieteAppliquee("mauvaise=cle", "x"); err == nil {
		t.Fatal("nom de propriété avec « = » accepté")
	}
	if err := ValiderProprieteAppliquee("motd", "Bienvenue chez le chef"); err != nil {
		t.Fatalf("valeur normale refusée : %v", err)
	}
}

// La liste de mods vit dans la CONFIGURATION, pas dans secrets.env que setup.sh
// réécrit : sans ça, une montée de version effacerait les mods ajoutés depuis
// l'interface.
func TestAjouterEtRetirerUnMod(t *testing.T) {
	g, _ := gest(t)
	const app = "minecraft@homestead"

	if l, _ := g.Mods(app); len(l) != 0 {
		t.Fatal("liste non vide au départ")
	}
	if err := g.AjouterMod(app, "lithium"); err != nil {
		t.Fatal(err)
	}
	if err := g.AjouterMod(app, "fabric-api"); err != nil {
		t.Fatal(err)
	}
	l, _ := g.Mods(app)
	if len(l) != 2 {
		t.Fatalf("attendu 2 mods, obtenu %v", l)
	}
	// Deux fois le même ferait échouer le démarrage du serveur.
	if err := g.AjouterMod(app, "LITHIUM"); err == nil {
		t.Fatal("doublon accepté")
	}
	if err := g.RetirerMod(app, "lithium"); err != nil {
		t.Fatal(err)
	}
	l, _ = g.Mods(app)
	if len(l) != 1 || l[0] != "fabric-api" {
		t.Fatalf("retrait incorrect : %v", l)
	}
	if err := g.RetirerMod(app, "inexistant"); err == nil {
		t.Fatal("retrait d'un mod absent accepté en silence")
	}
}

// L'identifiant part dans une variable d'environnement lue par l'image : une
// virgule ou un espace ferait dérailler la liste entière.
func TestUnIdentifiantDeProjetDouteuxEstRefuse(t *testing.T) {
	for _, mauvais := range []string{"", "  ", "deux mots", "a,b", "../../etc", "http://x/y", "projet;rm"} {
		if err := ValiderNomProjet(mauvais); err == nil {
			t.Fatalf("identifiant accepté à tort : %q", mauvais)
		}
	}
	for _, bon := range []string{"lithium", "fabric-api", "Chunky", "no_chat_reports"} {
		if err := ValiderNomProjet(bon); err != nil {
			t.Fatalf("identifiant refusé à tort : %q (%v)", bon, err)
		}
	}
}

func TestLireListeModsIgnoreCommentairesEtVides(t *testing.T) {
	l := LireListeMods("# un commentaire\n\nlithium\n   \nfabric-api\n")
	if len(l) != 2 || l[0] != "lithium" || l[1] != "fabric-api" {
		t.Fatalf("liste mal lue : %v", l)
	}
}
