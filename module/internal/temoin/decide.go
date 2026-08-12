// Package temoin surveille l'intégrité des Emplacements réseau de Caleope.
//
// Un « témoin », en maçonnerie, est le repère de plâtre qu'on colle en travers
// d'une fissure : il ne répare rien, mais c'est le seul moyen de savoir si le
// mur a bougé. Le contrat de ce module est le même — il constate, il prouve, il
// prévient. Il ne corrige pas.
//
// # POURQUOI IL EXISTE
//
// Entre le 18/07 et le 03/08/2026, chez un utilisateur, 30 à 60 Go de fichiers
// ont été troués de blocs de 1 Mio entièrement nuls, écrits à travers un montage
// NFS instable. Aucune erreur n'a été journalisée nulle part. Le produit a
// continué d'afficher tout en vert pendant six semaines.
//
// La leçon tient en une phrase, et c'est la thèse de ce module :
//
//	une écriture qui n'est pas relue est une écriture dont on ignore si elle a eu lieu.
//
// Ce fichier ne contient que la DÉCISION : une fonction pure, sans horloge, sans
// système de fichiers, sans réseau. Tout ce qui touche au monde réel vit
// ailleurs et lui passe un Constat.
package temoin

import "fmt"

// Verdict est l'état d'un Emplacement au terme d'un passage.
type Verdict string

const (
	// VerdictSain : l'écriture a été relue et retrouvée identique. Prouvé.
	VerdictSain Verdict = "sain"
	// VerdictSuspect : une anomalie isolée, pas encore une condamnation.
	VerdictSuspect Verdict = "suspect"
	// VerdictRompu : l'Emplacement perd ou abîme des données. Caleope n'y écrit plus.
	VerdictRompu Verdict = "rompu"
	// VerdictInconnu : la vérification n'a pas pu aboutir. Surtout pas « sain ».
	VerdictInconnu Verdict = "inconnu"
)

// SHA1BlocNul est l'empreinte d'un bloc de 1 Mio entièrement nul.
//
// C'est la signature de l'incident fondateur : deux zones différentes d'un même
// fichier qui rendent cette empreinte sont deux trous. Constante documentaire,
// citée telle quelle dans les rapports pour que le constat soit rejouable.
const SHA1BlocNul = "3b71f43ff30f4b15b5cd85dd9e95ebc7e84eb5a3"

// BonsPassagesPourDegeler : combien de passages irréprochables consécutifs il
// faut pour lever un gel. Asymétrie volontaire — on gèle sur un doute, on
// dégèle sur une preuve répétée.
const BonsPassagesPourDegeler = 3

// Constat est ce que la sonde a réellement observé. Aucun jugement ici, que des
// faits.
type Constat struct {
	// Monte : le chemin est un vrai point de montage (et pas le dossier local nu).
	Monte bool
	// Sentinelle : le fichier témoin déposé sur le partage est lisible et conforme.
	// Distingue « le partage est là » de « un dossier vide se fait passer pour lui ».
	Sentinelle bool
	// SondeEnCours : la sonde du passage précédent n'est jamais revenue.
	SondeEnCours bool
	// SondeFaite : un aller-retour écriture/relecture a été mené à son terme.
	SondeFaite bool
	// RelectureDirecte : la relecture a bien contourné le cache (O_DIRECT).
	// Sans elle, on relit sa propre mémoire et on ne prouve rien.
	RelectureDirecte bool
	// BlocsNuls : blocs relus entièrement nuls là où on avait écrit. Écriture perdue.
	BlocsNuls int
	// BlocsDivergents : blocs relus différents mais NON nuls. Corruption en transit.
	BlocsDivergents int
	// DeltaErrWrite / DeltaErrRead : erreurs NFS depuis le passage précédent.
	DeltaErrWrite int64
	DeltaErrRead  int64
	// OptionSoft : le montage porte « soft » — une erreur d'écriture ne remontera
	// nulle part. Signalé, mais ne gèle pas (sinon une mise à jour gèlerait tout
	// le parc d'un coup).
	OptionSoft bool
}

// Decision est ce que le module conclut et ce qu'il faut faire.
type Decision struct {
	Verdict Verdict
	// Gel : Caleope cesse d'écrire lui-même sur cet Emplacement. Ne coupe JAMAIS
	// une application déjà en marche — on retire des droits à Caleope, pas un
	// service à l'utilisateur.
	Gel bool
	// Raison : une phrase, en français, destinée à être lue par un humain.
	Raison string
}

// Decide applique les règles dans un ordre strict : la première qui s'applique
// gagne. L'ordre est le cœur du module — il place la preuve avant l'optimisme.
//
// prec est le verdict du passage précédent, bons le nombre de passages
// irréprochables consécutifs déjà accumulés.
func Decide(c Constat, prec Verdict, bons int) Decision {
	// 1. Pas de montage : écrire ici remplirait le disque système sans prévenir.
	//    C'est le trou le plus bête et le plus fréquent.
	if !c.Monte {
		return Decision{VerdictRompu, true,
			"point de montage absent — écrire ici remplirait le disque système"}
	}

	// 2. La sonde précédente n'est jamais revenue : le montage est figé. Relancer
	//    une sonde par-dessus ne ferait qu'empiler des goroutines bloquées.
	if c.SondeEnCours {
		return Decision{VerdictInconnu, true,
			"sonde précédente encore bloquée — montage figé"}
	}

	// 3. Sentinelle absente : le chemin est monté, lisible… mais ce n'est pas le
	//    partage attendu. Typiquement le dossier local nu après un démontage.
	if !c.Sentinelle {
		return Decision{VerdictRompu, true,
			"sentinelle absente — ce dossier n'est pas le partage attendu"}
	}

	// 4. Des blocs relus NULS : c'est l'incident fondateur, mot pour mot.
	if c.BlocsNuls > 0 {
		v := VerdictSuspect
		if prec == VerdictSuspect || prec == VerdictRompu {
			v = VerdictRompu
		}
		return Decision{v, true, fmt.Sprintf(
			"%d Mio écrits puis relus NULS (sha1 %s…) — écriture perdue",
			c.BlocsNuls, SHA1BlocNul[:8])}
	}

	// 5. Des blocs différents mais non nuls : la donnée s'est abîmée en chemin.
	if c.BlocsDivergents > 0 {
		v := VerdictSuspect
		if prec == VerdictSuspect || prec == VerdictRompu {
			v = VerdictRompu
		}
		return Decision{v, true, fmt.Sprintf(
			"%d bloc(s) relus différents de ce qui a été écrit — corruption en transit",
			c.BlocsDivergents)}
	}

	// 6. LE CŒUR DU MODULE : sans aller-retour mené à terme et sans relecture
	//    directe, on n'a rien prouvé. On ne déclare jamais « sain » par défaut —
	//    c'est précisément ce silence-là qui a coûté six semaines.
	if !c.SondeFaite || !c.RelectureDirecte {
		return Decision{VerdictInconnu, false,
			"relecture directe indisponible — rien n'est prouvé pour ce passage"}
	}

	// 7. Le noyau a compté des erreurs d'écriture : la donnée est peut-être déjà
	//    partie en fumée ailleurs, sur un fichier qu'on ne surveille pas.
	if c.DeltaErrWrite > 0 {
		return Decision{VerdictSuspect, true, fmt.Sprintf(
			"%d erreur(s) d'écriture NFS depuis le dernier passage", c.DeltaErrWrite)}
	}

	// 8. Erreurs de lecture : gênant, mais ça ne détruit rien. Pas de gel.
	if c.DeltaErrRead > 0 {
		return Decision{VerdictSuspect, false, fmt.Sprintf(
			"%d erreur(s) de lecture NFS depuis le dernier passage", c.DeltaErrRead)}
	}

	// 9. Montage en « soft » : la configuration elle-même autorise la perte
	//    silencieuse. On le dit, on ne gèle pas.
	if c.OptionSoft {
		return Decision{VerdictSuspect, false,
			"montage en mode soft : une erreur d'écriture ne remontera nulle part"}
	}

	// 10. Ce passage est bon, mais on sort d'un incident : il en faut plusieurs
	//     d'affilée avant de rendre sa confiance.
	//
	//     `bons` compte les passages irréprochables DÉJÀ accumulés ; celui-ci
	//     s'y ajoute, d'où le `bons+1`. Sans ce détail, « trois passages
	//     dégèlent » en demandait quatre — un décalage qu'on ne voit pas en
	//     lisant le code, seulement en comptant sur ses doigts.
	if (prec == VerdictRompu || prec == VerdictSuspect) && bons+1 < BonsPassagesPourDegeler {
		return Decision{VerdictSuspect, true, fmt.Sprintf(
			"en rétablissement (%d/%d passages irréprochables)", bons+1, BonsPassagesPourDegeler)}
	}

	// 11. Écrit, relu, identique. Prouvé.
	return Decision{VerdictSain, false, ""}
}
