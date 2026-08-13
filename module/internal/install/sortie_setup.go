package install

import "strings"

// ── Ce que l'utilisateur voit quand setup.sh refuse ──────────────────────────
//
// ⚠️ Constaté le 13/08/2026 au banc d'essai. Le paquet woodpecker refuse
// désormais de s'installer sans forge, et il l'explique : ce qu'il manque, les
// deux commandes à lancer. Rien de tout cela n'arrivait à l'utilisateur.
// `caleope install woodpecker` affichait, en tout et pour tout :
//
//	❌ setup.sh échoué: exit status 1
//
// La raison : la sortie de setup.sh part sur la sortie standard du DAEMON — donc
// dans le journal systemd — alors que la commande, elle, dialogue avec le daemon
// par l'API et ne reçoit que le message d'erreur. Le soin mis à écrire un
// message actionnable était perdu à l'endroit exact où il servait.
//
// On garde donc les derniers octets de la sortie pour les joindre à l'erreur.

// tailleTampon : de quoi porter une trentaine de lignes de message. On ne garde
// QUE la fin : un setup.sh qui construit une image peut cracher des mégaoctets
// de journal de build, et les tenir en mémoire pour les jeter ensuite serait
// absurde.
const tailleTampon = 8 << 10

// tampon retient les derniers octets écrits, et rien de plus.
type tampon struct {
	octets []byte
}

func (t *tampon) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= tailleTampon {
		t.octets = append(t.octets[:0], p[len(p)-tailleTampon:]...)
		return n, nil
	}
	t.octets = append(t.octets, p...)
	if len(t.octets) > tailleTampon {
		t.octets = t.octets[len(t.octets)-tailleTampon:]
	}
	return n, nil
}

// dernieresLignes rend les n dernières lignes non vides de la sortie, indentées,
// prêtes à être collées sous un message d'erreur.
//
// Les lignes vides sont écartées : les scripts du magasin aèrent leurs messages,
// et une erreur qui se termine par cinq lignes blanches donne l'impression que
// le diagnostic a été tronqué.
func dernieresLignes(sortie string, n int) string {
	var gardees []string
	lignes := strings.Split(sortie, "\n")
	for i := len(lignes) - 1; i >= 0 && len(gardees) < n; i-- {
		l := strings.TrimRight(lignes[i], " \t\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		gardees = append(gardees, "   "+l)
	}
	if len(gardees) == 0 {
		return ""
	}
	// On a parcouru à l'envers : on remet dans l'ordre de lecture.
	for i, j := 0, len(gardees)-1; i < j; i, j = i+1, j-1 {
		gardees[i], gardees[j] = gardees[j], gardees[i]
	}
	return strings.Join(gardees, "\n")
}
