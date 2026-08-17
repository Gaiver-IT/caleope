//go:build !linux

package temoin

import "io/fs"

// Hors Linux (poste de développement), on ne dispose pas de st_blocks de façon
// portable. On répond « non creux » plutôt que de deviner : la ronde examinera
// donc le fichier, ce qui est le comportement prudent — au pire elle signale un
// fichier creux à un humain, jamais l'inverse.
func estCreux(info fs.FileInfo) bool { return false }
