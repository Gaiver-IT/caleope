//go:build !linux

package temoin

import "errors"

// TailleBloc est l'unité de comparaison : 1 Mio.
const TailleBloc = 1 << 20

// LireDirect n'existe pas hors Linux (O_DIRECT est propre à Linux). On rend
// explicitement « pas de lecture directe » plutôt qu'une relecture depuis le
// cache : le module préfère dire « je n'ai rien prouvé » que « tout va bien ».
//
// Caleope ne tourne que sur Debian ; cette variante sert à garder le paquet
// compilable et testable sur le poste de développement (macOS).
func LireDirect(chemin string, taille int) ([]byte, bool, error) {
	return nil, false, errors.New("lecture directe (O_DIRECT) indisponible sur cette plateforme")
}
