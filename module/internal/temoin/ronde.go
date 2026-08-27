package temoin

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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

// MaxFichiersParPasse : seconde borne du passage. Le budget en octets ne
// protège de rien face à une arborescence de vignettes — des centaines de
// milliers de fichiers de 8 Kio pèsent trois fois rien mais coûtent une
// métadonnée chacune, et sur le montage surveillé une métadonnée se paie en
// dizaines de millisecondes.
const MaxFichiersParPasse = 3000

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
	// Positions : l'offset EN OCTETS de chaque bloc nul, depuis le début du
	// fichier.
	//
	// Sans elles, un signalement est invérifiable : la ronde lit par tranches,
	// donc « 2 blocs nuls sur 256 lus » ne dit pas OÙ regarder. Constaté en
	// vrai — une archive de 45 Go signalée, et une relecture des 256 premiers
	// mégaoctets qui ne retrouve rien, sans qu'on puisse dire si le défaut est
	// ailleurs dans le fichier ou n'a jamais existé. Un constat qu'on ne peut
	// pas rejouer ne vaut rien.
	Positions []int64 `json:"positions,omitempty"`
}

// MaxPositionsRetenues borne la liste : un fichier entièrement nul en
// produirait des milliers, et l'état sur disque doit rester lisible.
const MaxPositionsRetenues = 20

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
	tr, _, _, err := AnalyserTranche(chemin, info, 0, 1<<62)
	return tr, err
}

// AnalyserTranche lit AU PLUS `budget` octets d'un fichier, à partir de
// `depuis`, et rend ce qu'elle a vu ainsi que la position d'arrêt.
//
// POURQUOI la tranche existe : la première version lisait chaque fichier en
// ENTIER puis regardait si le budget du passage était dépassé. Sur une
// bibliothèque média, un seul film de plusieurs gigaoctets occupait donc le
// lien pendant des dizaines de minutes — mesuré sur la production, la ronde
// lisait encore sans discontinuer 23 minutes après son démarrage, en
// concurrence directe avec la galerie de l'utilisateur. Le budget doit être
// respecté PENDANT la lecture, pas après.
//
// `depuis` est toujours un multiple de TailleBloc : c'est ce qui garantit que
// les blocs restent alignés d'un passage à l'autre, et donc qu'un trou à cheval
// sur deux passages soit quand même vu.
func AnalyserTranche(chemin string, info fs.FileInfo, depuis, budget int64) (tr *Trouvaille, lus int64, fini bool, err error) {
	// Sous un bloc complet, la signature ne peut pas apparaître.
	if info.Size() < TailleBloc {
		return nil, 0, true, nil
	}
	// Un fichier CREUX rend des zéros à la lecture sans qu'aucune donnée n'ait
	// été perdue : c'est le faux positif évident, on l'écarte avant de lire.
	if estCreux(info) {
		return nil, 0, true, nil
	}
	f, err := os.Open(chemin)
	if err != nil {
		return nil, 0, true, err
	}
	defer f.Close()
	if depuis > 0 {
		if _, err := f.Seek(depuis, io.SeekStart); err != nil {
			return nil, 0, true, err
		}
	}

	tampon := make([]byte, TailleBloc)
	zeros := make([]byte, TailleBloc)
	var blocs, nuls int
	var positions []int64
	for lus < budget {
		n, errLecture := io.ReadFull(f, tampon)
		if n == TailleBloc {
			// Position AVANT d'incrémenter : c'est l'offset du bloc qu'on vient
			// de lire, celui qu'on veut pouvoir rejouer.
			position := depuis + lus
			blocs++
			lus += int64(n)
			if bytes.Equal(tampon, zeros) {
				nuls++
				if len(positions) < MaxPositionsRetenues {
					positions = append(positions, position)
				}
			}
		}
		if errLecture != nil {
			if errLecture == io.EOF || errLecture == io.ErrUnexpectedEOF {
				fini = true
				break
			}
			return nil, lus, true, errLecture
		}
	}
	// ⚠️ Un fichier dont la taille tombe pile sur le budget sort de la boucle
	// SANS avoir vu la fin de fichier. Sans ce rattrapage, la ronde le
	// marquerait « à reprendre », dépenserait un passage entier à découvrir
	// qu'il n'y a plus rien, et le compterait deux fois.
	if depuis+lus >= info.Size() {
		fini = true
	}
	if nuls > 0 {
		tr = &Trouvaille{Chemin: chemin, Octets: info.Size(), BlocsNuls: nuls, Blocs: blocs, Positions: positions}
	}
	return tr, lus, fini, nil
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
	pos := decoderCurseur(curseur)

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
			if pos.chemin != "" && chemin != racine &&
				chemin < pos.chemin && !strings.HasPrefix(pos.chemin, chemin+string(filepath.Separator)) {
				return fs.SkipDir
			}
			return nil
		}
		var depuis int64
		switch {
		case pos.chemin == "":
			// pas de curseur : on prend tout
		case chemin == pos.chemin && pos.offset > 0:
			// Fichier laissé à mi-chemin au passage précédent : on le reprend
			// où on l'avait laissé plutôt que de le relire depuis le début.
			depuis = pos.offset
		case chemin <= pos.chemin:
			// ⚠️ « <= » et non « < » : un curseur SANS offset désigne un fichier
			// entièrement examiné. Le traiter comme un point de reprise le
			// ferait relire à chaque passage, et la ronde piétinerait sur place.
			return nil
		}

		info, errInfo := d.Info()
		if errInfo != nil {
			return nil
		}
		t, lus, fini, errAnalyse := AnalyserTranche(chemin, info, depuis, budget-res.Octets)
		res.Fichiers++
		res.Octets += lus
		if errAnalyse == nil && !fini {
			// Arrêt en plein fichier : le curseur retient l'endroit exact.
			res.Curseur = encoderCurseur(positionRonde{chemin: chemin, offset: depuis + lus})
		} else {
			res.Curseur = chemin
		}
		if t != nil && len(res.Trouvailles) < MaxTrouvaillesRetenues {
			res.Trouvailles = append(res.Trouvailles, *t)
		}
		// Deux bornes, pas une : les octets protègent le lien d'un gros fichier,
		// le nombre de fichiers protège d'une arborescence de millions de
		// vignettes qui ne pèsent rien mais coûtent une métadonnée chacune.
		if res.Octets >= budget || res.Fichiers >= MaxFichiersParPasse {
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

// positionRonde : où la ronde s'est arrêtée. L'offset permet de reprendre un
// gros fichier en plein milieu au lieu de le relire entièrement à chaque fois —
// sans lui, un film de 8 Go ne serait jamais examiné en entier avec un budget
// de 256 Mio, ou monopoliserait le lien pendant une demi-heure.
type positionRonde struct {
	chemin string
	offset int64
}

// separateurCurseur : un octet nul ne peut pas apparaître dans un chemin, donc
// il ne peut pas être confondu avec le nom du fichier.
const separateurCurseur = "\x00"

func encoderCurseur(p positionRonde) string {
	if p.offset <= 0 {
		return p.chemin
	}
	return p.chemin + separateurCurseur + strconv.FormatInt(p.offset, 10)
}

func decoderCurseur(s string) positionRonde {
	i := strings.Index(s, separateurCurseur)
	if i < 0 {
		return positionRonde{chemin: s}
	}
	off, err := strconv.ParseInt(s[i+1:], 10, 64)
	if err != nil || off < 0 {
		// Curseur abîmé : on repart du fichier depuis le début plutôt que de
		// sauter au hasard. Relire coûte ; sauter laisserait un trou non vu.
		return positionRonde{chemin: s[:i]}
	}
	return positionRonde{chemin: s[:i], offset: off}
}

// errBudgetEpuise n'est pas une erreur : c'est le signal d'arrêt volontaire que
// WalkDir sait faire remonter. On le distingue soigneusement d'un vrai échec —
// confondre les deux ferait passer une ronde normale pour une panne.
var errBudgetEpuise = errors.New("budget de ronde épuisé")
