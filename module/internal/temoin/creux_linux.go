//go:build linux

package temoin

import (
	"io/fs"
	"syscall"
)

// estCreux reconnaît un fichier « à trous » — un fichier dont certaines zones
// n'ont jamais été allouées et que le noyau rend sous forme de zéros.
//
// POURQUOI c'est indispensable ici : la ronde cherche des blocs entièrement
// nuls. Un fichier creux en rend par construction, sans qu'aucune donnée n'ait
// été perdue. Sans ce filtre, la ronde crierait au dégât sur des fichiers
// parfaitement sains — et une alerte qui se trompe finit par ne plus être lue.
//
// La mesure : st_blocks compte des secteurs de 512 octets RÉELLEMENT alloués.
// S'il y en a nettement moins que ce que la taille annoncée exigerait, des
// zones ne sont pas allouées. On garde une marge de 10 % : un système de
// fichiers peut compresser ou empaqueter les petites queues de fichier, et on
// préfère écarter un fichier douteux plutôt que le dénoncer à tort.
func estCreux(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return false // renseignement indisponible : on ne prétend pas savoir
	}
	alloue := int64(st.Blocks) * 512
	if info.Size() <= 0 {
		return false
	}
	return alloue < info.Size()*9/10
}
