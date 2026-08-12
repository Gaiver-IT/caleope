package network

import (
	"strings"
	"testing"

	"github.com/gaiver-it/caleope/pkg/types"
)

// TestNFSJamaisSoftParDefaut est un test de NON-RÉGRESSION d'un incident réel.
//
// Avec l'option `soft`, une écriture NFS qui expire rend une erreur — que la
// plupart des programmes (dont les copies d'import) ne vérifient jamais. La
// donnée est alors perdue EN SILENCE : le fichier conserve sa taille et porte
// des zéros à la place des octets manquants.
//
// Chez un utilisateur, du 18/07 au 03/08/2026, cela a troué 30 à 60 Go de
// fichiers sans produire une seule erreur visible ; l'affaire n'a été découverte
// que six semaines plus tard, parce qu'une vidéo gelait toujours au même endroit.
//
// Si ce test échoue, c'est que quelqu'un s'apprête à réintroduire cette panne.
func TestNFSJamaisSoftParDefaut(t *testing.T) {
	opts := optionsNFS()
	joint := strings.Join(opts, ",")

	for _, o := range opts {
		if o == "soft" {
			t.Fatalf("les options NFS par défaut contiennent « soft » : %s\n"+
				"→ une écriture expirée serait perdue en silence. Utiliser « hard ».", joint)
		}
	}
	if !contient(opts, "hard") {
		t.Errorf("« hard » absent des options NFS par défaut : %s", joint)
	}
}

// TestNFSOptionsDurciesPresentes verrouille le correctif v0.7.8, qui avait déjà
// été perdu une fois (il ne vivait que dans le montage à chaud).
func TestNFSOptionsDurciesPresentes(t *testing.T) {
	opts := optionsNFS()
	for _, attendu := range []string{"vers=3", "rw", "proto=tcp", "mountproto=tcp", "retry=0"} {
		if !contient(opts, attendu) {
			t.Errorf("option attendue absente : %s (options = %s)", attendu, strings.Join(opts, ","))
		}
	}
}

// TestFstabEtMontageAChaudNeDivergentPas est LE test qui aurait évité que le
// durcissement s'évapore au redémarrage : les deux chemins doivent porter les
// mêmes options, sans quoi le NAS revient monté « comme avant » après un reboot,
// silencieusement.
func TestFstabEtMontageAChaudNeDivergentPas(t *testing.T) {
	m := NewManager(t.TempDir())
	loc := types.NetworkLocation{
		Name:       "mon-nas",
		Type:       types.LocationNFS,
		Host:       "192.0.2.10",
		Share:      "/export/nas",
		MountPoint: "/opt/gaiver-it/caleope/mounts/mon-nas",
	}
	ligne := m.fstabLine(loc)
	if ligne == "" {
		t.Fatal("aucune ligne fstab produite pour un emplacement NFS")
	}
	for _, o := range optionsNFS() {
		if !strings.Contains(ligne, o) {
			t.Errorf("la ligne fstab ne porte pas l'option « %s » du montage à chaud\n  ligne : %s", o, ligne)
		}
	}
	if strings.Contains(ligne, ",soft") || strings.Contains(ligne, "=soft") {
		t.Errorf("la ligne fstab contient « soft » : %s", ligne)
	}
	if !strings.Contains(ligne, "_netdev") {
		t.Errorf("« _netdev » absent : le montage serait tenté avant que le réseau soit là\n  ligne : %s", ligne)
	}
}

// TestOptionsUtilisateurGagnent : on durcit par défaut, mais on n'enferme
// personne. Qui veut « soft » doit pouvoir le demander explicitement, et sa
// valeur doit être ajoutée APRÈS pour l'emporter côté mount(8).
func TestOptionsUtilisateurGagnent(t *testing.T) {
	m := NewManager(t.TempDir())
	loc := types.NetworkLocation{
		Name:       "nas-lent",
		Type:       types.LocationNFS,
		Host:       "192.0.2.10",
		Share:      "/export/nas",
		MountPoint: "/mnt/nas-lent",
		Options:    "soft,timeo=100",
	}
	ligne := m.fstabLine(loc)
	iHard := strings.Index(ligne, "hard")
	iSoft := strings.Index(ligne, "soft")
	if iSoft < 0 {
		t.Fatalf("l'option utilisateur « soft » n'apparaît pas dans la ligne : %s", ligne)
	}
	if iHard >= 0 && iSoft < iHard {
		t.Errorf("l'option utilisateur doit venir APRÈS le défaut pour l'emporter\n  ligne : %s", ligne)
	}
}

// TestModifierOptionsNFSCasseLeTest garde le test lui-même honnête : il vérifie
// que contient() ne rend pas vrai à tort (un test qui ne peut pas échouer ne
// prouve rien).
func TestContientEstFiable(t *testing.T) {
	if contient([]string{"hard", "rw"}, "soft") {
		t.Error("contient() a trouvé « soft » là où il n'y en a pas")
	}
	if !contient([]string{"hard", "rw"}, "hard") {
		t.Error("contient() n'a pas trouvé « hard » alors qu'il y est")
	}
	if contient([]string{"hardware"}, "hard") {
		t.Error("contient() fait une correspondance partielle : « hardware » ≠ « hard »")
	}
}

func contient(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
