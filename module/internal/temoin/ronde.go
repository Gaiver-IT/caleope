package temoin

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// La ronde : relire les fichiers DÉJÀ LÀ.
//
// # POURQUOI ELLE MANQUAIT
//
// La sonde prouve que le lien fonctionne À CET INSTANT : on écrit 4 Mio, on les
// relit, ils sont identiques. Elle ne dit RIEN des fichiers écrits hier.
//
// Or l'incident fondateur n'a pas empêché les écritures : il les a trouées
// après coup, en silence. Un Emplacement pouvait donc rendre « sain » à chaque
// passage pendant que la bibliothèque pourrissait dessous. Le jour où on ouvre
// cette bibliothèque à d'autres personnes, « le témoin de 4 Mio va bien » n'est
// plus une réponse acceptable.
//
// # CE QU'ELLE CHERCHE
//
// La signature de l'incident : des blocs de 1 Mio ENTIÈREMENT NULS à
// l'intérieur d'un fichier. Aucune référence extérieure n'est nécessaire — ni
// somme de contrôle enregistrée, ni copie témoin — donc ça marche sur n'importe
// quel Emplacement, pour n'importe quelle application.
//
// # CE QU'ELLE NE FAIT PAS
//
//   - Elle ne répare rien : c'est le contrat du module.
//   - Elle ne condamne JAMAIS un Emplacement à elle seule. Un bloc nul n'est pas
//     toujours un dégât, et une ronde qui déclencherait un gel sur un faux
//     positif ferait plus de mal que l'incident qu'elle surveille. Elle compte,
//     elle nomme les fichiers, et elle laisse l'humain trancher.
//   - Elle ne relit pas tout à chaque passage : sur un lien lent, lire 40 Go
//     par heure rendrait le partage inutilisable. Elle avance par tranches, avec
//     un curseur, et fait le tour en plusieurs jours.

// TailleBloc (1 Mio, la granularité des trous de l'incident) est défini
// à côté de la lecture directe : la ronde réutilise la même unité.

// BudgetRondeDefaut : ce qu'une ronde s'autorise à lire en un passage.
// 256 Mio à 8 Mo/s (débit mesuré sur le NAS de la production) ≈ 30 s de lien
// occupé par heure. Assez pour faire le tour d'une bibliothèque de 40 Go en une
// semaine, assez peu pour que personne ne le remarque.
const BudgetRondeDefaut int64 = 256 << 20

// MaxTrouvaillesRetenues borne ce qu'on garde en mémoire et sur disque : si
// 10 000 fichiers sont abîmés, en nommer 50 suffit largement à donner l'alerte,
// et l'état ne doit pas devenir un fichier de plusieurs mégaoctets.
const MaxTrouvaillesRetenues = 50

// Trouvaille : un fichier dont le contenu porte la signature de l'incident.
type Trouvaille struct {
	Chemin    string `json:"chemin"`
	Octets    int64  `json:"octets"`
	BlocsNuls int    `json:"blocs_nuls"`
	Blocs     int    `json:"blocs"`
}

// ResultatRonde : ce qu'un passage de ronde a constaté.
type ResultatRonde struct {
	Fichiers    int
	Octets      int64
	Trouvailles []Trouvaille
	// Curseur : dernier chemin examiné. Le passage suivant reprend APRÈS lui.
	// Vide = le tour est bouclé, on repartira du début.
	Curseur string
	Termine bool
	Duree   time.Duration
	Erreur  error
}

// AnalyserContenu compte les blocs de TailleBloc entièrement nuls.
//
// Fonction PURE : elle prend un io.Reader, pas un chemin. Seuls les blocs
// COMPLETS comptent — un fichier de trois octets nuls n'a rien à voir avec la
// signature qu'on cherche, et le compter ferait hurler la ronde sur des
// fichiers parfaitement sains.
func AnalyserContenu(r io.Reader) (blocs int, nuls int, err error) {
	tampon := make([]byte, TailleBloc)
	zeros := make([]byte, TailleBloc)
	for {
		n, errLecture := io.ReadFull(r, tampon)
		if n == TailleBloc {
			blocs++
			if bytes.Equal(tampon, zeros) {
				nuls++
			}
		}
		if errLecture != nil {
			if errLecture == io.EOF || errLecture == io.ErrUnexpectedEOF {
				return blocs, nuls, nil
			}
			return blocs, nuls, errLecture
		}
	}
}

// AnalyserFichier ouvre puis analyse. Rend nil, nil pour un fichier qu'on a
// décidé de ne pas juger (trop petit, ou creux).
func AnalyserFichier(chemin string, info fs.FileInfo) (*Trouvaille, error) {
	// Sous un bloc complet, la signature ne peut pas apparaître.
	if info.Size() < TailleBloc {
		return nil, nil
	}
	// Un fichier CREUX rend des zéros à la lecture sans qu'aucune donnée n'ait
	// été perdue : c'est le faux positif évident, on l'écarte avant de lire.
	if estCreux(info) {
		return nil, nil
	}
	f, err := os.Open(chemin)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	blocs, nuls, err := AnalyserContenu(f)
	if err != nil {
		return nil, err
	}
	if nuls == 0 {
		return nil, nil
	}
	return &Trouvaille{Chemin: chemin, Octets: info.Size(), BlocsNuls: nuls, Blocs: blocs}, nil
}

// Ronde parcourt `racine` par ordre lexical, reprend après `curseur`, et
// s'arrête dès que `budget` octets ont été lus.
//
// L'ordre lexical n'est pas un détail : c'est lui qui rend le curseur fiable.
// Un parcours dont l'ordre change d'un passage à l'autre ferait relire toujours
// les mêmes fichiers et n'en atteindrait jamais la fin. WalkDir trie déjà les
// entrées de chaque répertoire, on hérite donc de cet ordre gratuitement.
//
// ⚠️ Le parcours est traité EN FLUX, et non collecté puis trié : sur le
// montage que ce module surveille, lister 25 fichiers coûtait 4 secondes
// mesurées. Énumérer l'arbre entier à chaque passage — plusieurs minutes de
// métadonnées sur le lien — pour n'en lire ensuite que 256 Mio serait une
// dépense absurde, et la ronde deviendrait elle-même une nuisance.
func Ronde(racine, curseur string, budget int64, maintenant func() time.Time) ResultatRonde {
	if maintenant == nil {
		maintenant = time.Now
	}
	debut := maintenant()
	res := ResultatRonde{}

	err := filepath.WalkDir(racine, func(chemin string, d fs.DirEntry, err error) error {
		if err != nil {
			// Un sous-dossier illisible ne doit pas interrompre la ronde : on
			// passe. S'arrêter ici reviendrait à ne jamais examiner ce qui suit
			// dans l'ordre alphabétique.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Le dossier de travail du module n'a pas à s'auto-surveiller.
			if strings.Contains(filepath.ToSlash(chemin), "/"+DossierTemoin) {
				return fs.SkipDir
			}
			// Élagage : un répertoire entièrement situé AVANT le curseur ne
			// contient que du déjà-vu. On ne descend pas dedans — c'est ce qui
			// rend la reprise bon marché au lieu de re-parcourir tout l'arbre.
			if curseur != "" && chemin != racine &&
				chemin < curseur && !strings.HasPrefix(curseur, chemin+string(filepath.Separator)) {
				return fs.SkipDir
			}
			return nil
		}
		if curseur != "" && chemin <= curseur {
			return nil
		}

		info, errInfo := d.Info()
		if errInfo != nil {
			return nil
		}
		t, errAnalyse := AnalyserFichier(chemin, info)
		res.Fichiers++
		res.Curseur = chemin
		if errAnalyse == nil && info.Size() >= TailleBloc {
			res.Octets += info.Size()
		}
		if t != nil && len(res.Trouvailles) < MaxTrouvaillesRetenues {
			res.Trouvailles = append(res.Trouvailles, *t)
		}
		if res.Octets >= budget {
			return errBudgetEpuise
		}
		return nil
	})

	switch {
	case err == errBudgetEpuise:
		// Arrêt volontaire : le curseur retient où reprendre.
	case err != nil:
		res.Erreur = err
	default:
		// Fin de l'arbre : le tour est bouclé, le prochain passage repart du début.
		res.Termine = true
		res.Curseur = ""
	}
	res.Duree = maintenant().Sub(debut)
	return res
}

// errBudgetEpuise n'est pas une erreur : c'est le signal d'arrêt volontaire que
// WalkDir sait faire remonter. On le distingue soigneusement d'un vrai échec —
// confondre les deux ferait passer une ronde normale pour une panne.
var errBudgetEpuise = errors.New("budget de ronde épuisé")
