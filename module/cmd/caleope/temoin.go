package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// cmdTemoin affiche ce que le module Témoin a constaté sur les Emplacements
// réseau à son dernier passage.
//
// Le module écrit un motif connu sur le partage, le relit en contournant le
// cache, et compare. Un Emplacement n'est déclaré « sain » que si cet
// aller-retour a réellement abouti — jamais par défaut. C'est toute la
// différence entre un voyant vert et une preuve.
func cmdTemoin(args []string) {
	resp := callDaemon("temoin", map[string]string{})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	etats, ok := resp.Data.([]interface{})
	if !ok || len(etats) == 0 {
		fmt.Println("Aucun Emplacement réseau surveillé.")
		fmt.Println()
		fmt.Println("Le Témoin vérifie les Emplacements de type réseau (NFS, SMB, SFTP).")
		fmt.Println("Ajoutez-en un avec : caleope location add <nom> --type nfs --host … --share …")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EMPLACEMENT\tVERDICT\tDERNIER PASSAGE\tPASSAGES\tCONSTAT")
	fmt.Fprintln(w, "────────────────\t──────────\t────────────────\t────────\t───────")
	for _, e := range etats {
		m, _ := e.(map[string]interface{})
		verdict := strField(m, "verdict")
		raison := strField(m, "raison")
		passage := strField(m, "dernier_passage")
		if len(passage) > 16 {
			passage = passage[:16]
		}
		// L'horodatage ISO garde un « T » entre la date et l'heure : lisible par
		// une machine, pas par un humain qui lit un tableau.
		passage = strings.Replace(passage, "T", " ", 1)
		// strField rend « - » quand le champ est absent : un verdict « sain »
		// n'a pas de raison, et « - » ne dit rien à personne.
		if raison == "" || raison == "-" {
			raison = "écrit, relu, identique"
		}
		fmt.Fprintf(w, "%s\t%s %s\t%s\t%s\t%s\n",
			strField(m, "nom"), pastilleVerdict(verdict), verdict,
			passage, numField(m, "passages"), raison)
	}
	w.Flush()

	// Ce que le module a vu passer depuis toujours : c'est le chiffre qui
	// raconte l'histoire, pas le verdict de l'instant.
	for _, e := range etats {
		m, _ := e.(map[string]interface{})
		if n := numField(m, "total_blocs_nuls"); n != "0" && n != "" {
			fmt.Printf("\n⚠️  %s : %s Mio écrits puis relus NULS depuis la mise en service.\n",
				strField(m, "nom"), n)
			fmt.Println("   Des écritures ont été perdues en silence. Vérifiez le NAS avant d'y écrire davantage.")
		}
	}
	fmt.Println()
	fmt.Println("Le Témoin constate et prévient — il ne répare rien et ne bloque rien.")
}

func pastilleVerdict(v string) string {
	switch v {
	case "sain":
		return "🟢"
	case "suspect":
		return "🟠"
	case "rompu":
		return "🔴"
	default:
		return "⚪"
	}
}

func numField(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok {
		if f, ok := v.(float64); ok {
			return fmt.Sprintf("%.0f", f)
		}
	}
	return "0"
}
