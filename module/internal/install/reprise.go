package install

import (
	"os"
	"path/filepath"
)

// Reprise après une montée de version ratée.
//
// POURQUOI : `caleope install <app> --force` EST la montée de version. Si elle
// échoue à mi-parcours, le rollback historique faisait le ménage d'une
// installation NEUVE — `compose down`, `RemoveAll(apps-installed/<app>)`,
// libération du port, désinscription. Sur une app qui existait déjà, ça détruit
// une installation qui marchait, pour la « protéger ». Les données
// (`app-data/`, `app-config/`) survivaient, mais l'app disparaissait de Caleope
// et ne redémarrait plus.
//
// On prend donc, avant de régénérer quoi que ce soit, une copie des deux
// fichiers qui décrivent la version EN PLACE. Ils sont minuscules (quelques
// kio) et suffisent à faire repartir l'ancienne version.

// fichiersRepris : ce qui décrit la stack telle qu'elle tourne. Les données de
// l'app n'y sont pas — elles ne sont jamais touchées par une réinstallation.
var fichiersRepris = []string{
	"compose.yml",
	"app.env",
	"compose.override.yml", // surcouches GPU / montages spécifiques
}

// instantane retient le contenu des fichiers de description d'une app.
type instantane struct {
	fichiers map[string][]byte
}

// prendreInstantane copie en mémoire les fichiers de description présents.
// Renvoie nil s'il n'y a rien à reprendre (installation neuve) : l'appelant
// distingue ainsi « rien à restaurer » de « restauration possible ».
func prendreInstantane(composeDir string) *instantane {
	inst := &instantane{fichiers: map[string][]byte{}}
	for _, nom := range fichiersRepris {
		contenu, err := os.ReadFile(filepath.Join(composeDir, nom))
		if err != nil {
			continue // absent : normal pour compose.override.yml
		}
		inst.fichiers[nom] = contenu
	}
	if len(inst.fichiers) == 0 {
		return nil
	}
	return inst
}

// restaurer réécrit les fichiers repris. Un fichier qui n'existait pas au
// moment de l'instantané n'est PAS supprimé : on remet l'ancien état sans
// prétendre effacer ce qu'on n'a pas observé.
func (in *instantane) restaurer(composeDir string) error {
	if in == nil {
		return nil
	}
	if err := os.MkdirAll(composeDir, 0o755); err != nil {
		return err
	}
	for nom, contenu := range in.fichiers {
		if err := os.WriteFile(filepath.Join(composeDir, nom), contenu, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// nombreFichiers sert aux messages et aux tests.
func (in *instantane) nombreFichiers() int {
	if in == nil {
		return 0
	}
	return len(in.fichiers)
}
