package temoin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Etat est ce que le module retient d'un Emplacement d'un passage à l'autre.
//
// Persisté sur disque : sans mémoire, le module ne saurait ni faire la
// différence entre une anomalie isolée et une panne installée, ni compter les
// bons passages nécessaires au dégel. Et surtout, il redémarrerait « sain »
// après chaque redémarrage du daemon — ce qui reviendrait à effacer l'alerte.
type Etat struct {
	Nom             string       `json:"nom"`
	Verdict         Verdict      `json:"verdict"`
	Raison          string       `json:"raison,omitempty"`
	Gel             bool         `json:"gel"`
	BonsConsecutifs int          `json:"bons_consecutifs"`
	DernierPassage  time.Time    `json:"dernier_passage"`
	DernierSain     time.Time    `json:"dernier_sain,omitempty"`
	Compteurs       CompteursNFS `json:"compteurs"`
	// SondeEnCours : posé avant de sonder, retiré après. S'il est encore là au
	// passage suivant, c'est que la sonde ne revient pas — le montage est figé.
	SondeEnCours bool `json:"sonde_en_cours"`
	// TotalBlocsNuls : cumul depuis toujours, pour la valeur de preuve.
	TotalBlocsNuls int `json:"total_blocs_nuls"`
	Passages       int `json:"passages"`
}

// dossierEtat range les états sous runtime/temoin/.
func dossierEtat(baseDir string) string {
	return filepath.Join(baseDir, "runtime", "temoin")
}

// nomFichier neutralise un nom d'Emplacement pour en faire un nom de fichier
// sûr : un nom contenant « / » ou « .. » écrirait ailleurs que prévu.
func nomFichier(nom string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", "..", "_", string(os.PathSeparator), "_")
	n := strings.TrimSpace(r.Replace(nom))
	if n == "" || n == "." {
		n = "sans-nom"
	}
	return n + ".json"
}

// ChargerEtat lit l'état d'un Emplacement. Un état absent n'est pas une erreur :
// c'est un Emplacement jamais encore vérifié, et son verdict est « inconnu » —
// jamais « sain ».
func ChargerEtat(baseDir, nom string) Etat {
	e := Etat{Nom: nom, Verdict: VerdictInconnu}
	b, err := os.ReadFile(filepath.Join(dossierEtat(baseDir), nomFichier(nom)))
	if err != nil {
		return e
	}
	var lu Etat
	if err := json.Unmarshal(b, &lu); err != nil {
		return e // fichier abîmé : on repart d'« inconnu », pas de « sain »
	}
	lu.Nom = nom
	if lu.Verdict == "" {
		lu.Verdict = VerdictInconnu
	}
	return lu
}

// EnregistrerEtat écrit l'état de façon atomique (fichier temporaire puis
// renommage) : une coupure au mauvais moment ne doit pas laisser un JSON
// tronqué qui serait relu comme « inconnu » à chaque démarrage.
func EnregistrerEtat(baseDir string, e Etat) error {
	dir := dossierEtat(baseDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(dir, nomFichier(e.Nom))
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// ListerEtats rend tous les états connus, pour l'affichage.
func ListerEtats(baseDir string) []Etat {
	var out []Etat
	entries, err := os.ReadDir(dossierEtat(baseDir))
	if err != nil {
		return out
	}
	for _, f := range entries {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		nom := strings.TrimSuffix(f.Name(), ".json")
		out = append(out, ChargerEtat(baseDir, nom))
	}
	return out
}

// Appliquer met à jour l'état à partir d'une décision, et tient les compteurs.
func (e *Etat) Appliquer(d Decision, c Constat, compteurs CompteursNFS, maintenant time.Time) {
	// Un passage irréprochable qui reste « suspect » uniquement parce qu'on sort
	// d'un incident compte comme bon : c'est lui qui fait avancer le dégel.
	// ⚠️ Le compte doit être fait EN UNE FOIS. Une première version remettait le
	// compteur à zéro puis le remontait à 1 dans la foulée : il ne dépassait
	// jamais 1 et le dégel n'arrivait jamais.
	bonPassage := d.Verdict == VerdictSain ||
		(d.Verdict == VerdictSuspect && strings.HasPrefix(d.Raison, "en rétablissement"))
	if bonPassage {
		e.BonsConsecutifs++
	} else {
		e.BonsConsecutifs = 0
	}
	if d.Verdict == VerdictSain {
		e.DernierSain = maintenant
	}

	e.Verdict = d.Verdict
	e.Raison = d.Raison
	e.Gel = d.Gel
	e.DernierPassage = maintenant
	e.Compteurs = compteurs
	e.TotalBlocsNuls += c.BlocsNuls
	e.Passages++
}

// Change dit si le verdict a changé, pour n'émettre un événement que dans ce
// cas — sinon le journal se remplirait d'une ligne par minute et personne ne
// lirait plus rien.
func Change(avant, apres Verdict) bool { return avant != apres }
