package temoin

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// Ce fichier lit ce que le NOYAU sait déjà, et que personne ne regardait.
//
// /proc/self/mountstats tient, pour chaque montage NFS, un compteur d'erreurs
// par opération. Sur la production où l'incident s'est produit, il affichait
// 106 erreurs d'ÉCRITURE et 66 050 erreurs de LOOKUP — la signature du
// « fileid changed » de mergerfs. Cette information était disponible en
// permanence, gratuite, et jamais consultée.
//
// Aucun fork : ni nfsiostat, ni journalctl. Ce sont des lecteurs de ces mêmes
// fichiers, et un fork qui se bloque sur un montage figé bloque l'appelant.

// CompteursNFS agrège les compteurs d'un montage à un instant donné.
type CompteursNFS struct {
	ReadOps, ReadErrs     int64
	WriteOps, WriteErrs   int64
	LookupOps, LookupErrs int64
	Timeouts              int64 // READ + WRITE + LOOKUP
}

// Delta soustrait un relevé précédent. Plancher à 0 : au remontage, les
// compteurs repartent de zéro, et un delta négatif signifierait « moins
// d'erreurs qu'avant », ce qui n'a pas de sens — on préfère ne rien signaler
// plutôt que signaler à tort.
func (c CompteursNFS) Delta(prec CompteursNFS) CompteursNFS {
	pos := func(a, b int64) int64 {
		if d := a - b; d > 0 {
			return d
		}
		return 0
	}
	return CompteursNFS{
		ReadOps:    pos(c.ReadOps, prec.ReadOps),
		ReadErrs:   pos(c.ReadErrs, prec.ReadErrs),
		WriteOps:   pos(c.WriteOps, prec.WriteOps),
		WriteErrs:  pos(c.WriteErrs, prec.WriteErrs),
		LookupOps:  pos(c.LookupOps, prec.LookupOps),
		LookupErrs: pos(c.LookupErrs, prec.LookupErrs),
		Timeouts:   pos(c.Timeouts, prec.Timeouts),
	}
}

// ParseMountstats lit le format de /proc/self/mountstats et rend les compteurs
// par point de montage. Fonction PURE : elle prend un io.Reader, pas un chemin.
//
// Format d'une ligne per-op, après l'étiquette :
//
//	ops trans timeouts bytes_sent bytes_recv queue rtt exec errors
//
// Soit 9 nombres. Certains noyaux anciens n'en publient que 8 (pas d'errors) —
// on ne lit donc « errors » que s'il est réellement là, plutôt que de prendre
// le dernier champ au hasard et de compter des microsecondes comme des erreurs.
func ParseMountstats(r io.Reader) map[string]CompteursNFS {
	out := map[string]CompteursNFS{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var mp string
	var nfs bool
	var cur CompteursNFS

	valider := func() {
		if mp != "" && nfs {
			out[mp] = cur
		}
		mp, nfs, cur = "", false, CompteursNFS{}
	}

	for sc.Scan() {
		ligne := sc.Text()

		if strings.HasPrefix(ligne, "device ") {
			valider()
			// device X mounted on <point de montage> with fstype <type> ...
			i := strings.Index(ligne, " mounted on ")
			j := strings.Index(ligne, " with fstype ")
			if i < 0 || j < 0 || j <= i {
				continue
			}
			mp = ligne[i+len(" mounted on ") : j]
			reste := strings.Fields(ligne[j+len(" with fstype "):])
			// « nfs » et « nfs4 » comptent ; « fuse.rclone », « overlay », non.
			nfs = len(reste) > 0 && (reste[0] == "nfs" || reste[0] == "nfs4")
			continue
		}
		if !nfs {
			continue
		}

		champs := strings.Fields(ligne)
		if len(champs) < 4 || !strings.HasSuffix(champs[0], ":") {
			continue
		}
		op := strings.TrimSuffix(champs[0], ":")
		if op != "READ" && op != "WRITE" && op != "LOOKUP" {
			continue
		}
		n := func(i int) int64 {
			if i >= len(champs) {
				return 0
			}
			v, err := strconv.ParseInt(champs[i], 10, 64)
			if err != nil {
				return 0
			}
			return v
		}
		ops := n(1)
		timeouts := n(3)
		var errs int64
		if len(champs) >= 10 { // étiquette + 9 nombres
			errs = n(9)
		}
		switch op {
		case "READ":
			cur.ReadOps, cur.ReadErrs = ops, errs
		case "WRITE":
			cur.WriteOps, cur.WriteErrs = ops, errs
		case "LOOKUP":
			cur.LookupOps, cur.LookupErrs = ops, errs
		}
		cur.Timeouts += timeouts
	}
	valider()
	return out
}

// LireMountstats ouvre /proc/self/mountstats. Sur un système sans /proc (macOS
// pendant les tests), rend une carte vide sans erreur bloquante.
func LireMountstats() map[string]CompteursNFS {
	f, err := os.Open("/proc/self/mountstats")
	if err != nil {
		return map[string]CompteursNFS{}
	}
	defer f.Close()
	return ParseMountstats(f)
}

// ParseMounts rend les options de montage par point de montage, au format de
// /proc/self/mounts. PURE.
//
// Le noyau échappe les espaces des chemins en « \040 » : sans le décodage, un
// partage nommé « Mes documents » ne serait jamais retrouvé.
func ParseMounts(r io.Reader) map[string][]string {
	out := map[string][]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		ch := strings.Fields(sc.Text())
		if len(ch) < 4 {
			continue
		}
		mp := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(ch[1])
		out[mp] = strings.Split(ch[3], ",")
	}
	return out
}

// OptionsMontage rend les options réelles d'un point de montage.
func OptionsMontage(mp string) []string {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	return ParseMounts(f)[mp]
}

// ContientOption cherche une option exacte (« soft » ne doit pas correspondre à
// « nosoft », ni « hard » à « hardlink »).
func ContientOption(opts []string, o string) bool {
	for _, x := range opts {
		if x == o {
			return true
		}
	}
	return false
}
