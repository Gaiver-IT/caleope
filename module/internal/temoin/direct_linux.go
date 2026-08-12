//go:build linux

package temoin

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// TailleBloc est l'unité de comparaison : 1 Mio, comme les trous observés lors
// de l'incident (leurs frontières étaient alignées au Mio).
const TailleBloc = 1 << 20

// LireDirect relit un fichier en contournant le cache du client (O_DIRECT).
//
// C'est LE point qui fait la valeur du module. Une relecture ordinaire rend ce
// que le client a gardé en mémoire : on relit sa propre écriture, jamais ce que
// le serveur a réellement enregistré. Autrement dit, elle ne prouve rien — et
// c'est exactement le piège dans lequel je suis tombé pendant le diagnostic de
// l'incident, en croyant un fichier réparé parce qu'il se relisait bien.
//
// O_DIRECT impose que le tampon soit aligné sur la taille d'un bloc logique ;
// on aligne sur 4 Kio, ce qui convient partout. Renvoie (données, true) si la
// lecture directe a réellement eu lieu.
func LireDirect(chemin string, taille int) ([]byte, bool, error) {
	f, err := os.OpenFile(chemin, os.O_RDONLY|syscall.O_DIRECT, 0)
	if err != nil {
		// O_DIRECT non supporté (certains systèmes de fichiers le refusent) :
		// on le dit franchement plutôt que de relire depuis le cache et de
		// prétendre avoir prouvé quelque chose.
		return nil, false, err
	}
	defer f.Close()

	brut := make([]byte, taille+4096)
	décalage := 0
	if a := uintptr(unsafe.Pointer(&brut[0])) % 4096; a != 0 {
		décalage = int(4096 - a)
	}
	buf := brut[décalage : décalage+taille]

	lu := 0
	for lu < taille {
		n, err := f.Read(buf[lu:])
		if n > 0 {
			lu += n
			continue
		}
		if err != nil {
			return buf[:lu], true, fmt.Errorf("lecture directe interrompue à %d octets : %w", lu, err)
		}
		break
	}
	return buf[:lu], true, nil
}
