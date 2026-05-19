// cmd/caleope-store/main.go
//
// 💻 LE CLI — interface utilisateur en ligne de commande
//
// caleope-store est le client du daemon.
// Il traduit les commandes humaines en requêtes API JSON sur le socket UNIX.
//
// FLUX :
//   $ caleope-store install jellyfin
//   → CLI construit {"command":"install","args":{"app":"jellyfin"}}
//   → CLI envoie sur /run/caleoped.sock
//   → daemon traite et répond
//   → CLI affiche le résultat
//
// CONCEPT : os.Args et sous-commandes
// os.Args = liste des arguments passés au programme
// os.Args[0] = nom du programme
// os.Args[1] = première sous-commande (ex: "install")
// os.Args[2:] = reste des arguments

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/gaiver-it/caleope/pkg/types"
	"github.com/gaiver-it/caleope/pkg/version"
)

const SOCKET_PATH = "/run/caleoped.sock"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	// Router vers la bonne commande
	switch command {
	case "install":
		cmdInstall(args)
	case "remove", "rm":
		cmdRemove(args)
	case "list", "ls":
		cmdList(args)
	case "info":
		cmdInfo(args)
	case "logs":
		cmdLogs(args)
	case "search":
		cmdSearch(args)
	case "update":
		cmdUpdate(args)
	case "upgrade":
		cmdUpgrade(args)
	case "version", "--version", "-v":
		fmt.Printf("caleope-store %s (commit: %s)\n", version.Version, version.Commit)
	case "ping":
		cmdPing()
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "❌ Commande inconnue: %s\n\n", command)
		printHelp()
		os.Exit(1)
	}
}

// ─────────────────────────────────────────────
// COMMANDES
// ─────────────────────────────────────────────

func cmdInstall(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store install <app> [--domain <domaine>] [--channel stable|latest|nightly]")
	}

	apiArgs := map[string]string{
		"app": args[0],
	}

	// Parser les flags optionnels
	// Ex: caleope-store install jellyfin --domain media.home.local --channel latest
	for i := 1; i < len(args)-1; i++ {
		switch args[i] {
		case "--domain":
			apiArgs["domain"] = args[i+1]
			i++
		case "--channel":
			apiArgs["channel"] = args[i+1]
			i++
		case "--force":
			apiArgs["force"] = "true"
		}
	}

	fmt.Printf("📦 Installation de '%s'...\n", args[0])
	resp := callDaemon("install", apiArgs)

	if !resp.Success {
		die("❌ " + resp.Error)
	}
	// Le daemon affiche sa propre progression, pas besoin d'afficher Data ici
}

func cmdRemove(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store remove <app> [--keep-data]")
	}

	apiArgs := map[string]string{"app": args[0]}

	for _, arg := range args[1:] {
		if arg == "--keep-data" {
			apiArgs["keep_data"] = "true"
		}
	}

	// Demander confirmation avant de supprimer
	fmt.Printf("⚠️  Supprimer '%s' ? [o/N] ", args[0])
	var resp string
	fmt.Scanln(&resp)
	if strings.ToLower(resp) != "o" {
		fmt.Println("Annulé.")
		return
	}

	apiResp := callDaemon("remove", apiArgs)
	if !apiResp.Success {
		die("❌ " + apiResp.Error)
	}
}

func cmdList(args []string) {
	jsonMode := contains(args, "-json") || contains(args, "--json")

	resp := callDaemon("list", nil)
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	if jsonMode {
		// Mode machine : afficher le JSON brut
		data, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Println(string(data))
		return
	}

	// Mode humain : tableau formaté
	// tabwriter = bibliothèque Go pour aligner les colonnes
	apps, ok := resp.Data.([]interface{})
	if !ok || len(apps) == 0 {
		fmt.Println("Aucune application installée.")
		return
	}

	// tabwriter.NewWriter(os.Stdout, minWidth, tabWidth, padding, padChar, flags)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NOM\tSTATUT\tVERSION\tPORT\tDÉPÔT")
	fmt.Fprintln(w, "───\t──────\t───────\t────\t─────")

	for _, a := range apps {
		app, _ := a.(map[string]interface{})
		name := strField(app, "id")
		status := formatStatus(strField(app, "status"))
		version := strField(app, "version")
		repo := strField(app, "repository")

		// Extraire le premier port
		port := "-"
		if ports, ok := app["ports"].([]interface{}); ok && len(ports) > 0 {
			if p, ok := ports[0].(map[string]interface{}); ok {
				if h, ok := p["host"].(float64); ok && h > 0 {
					port = fmt.Sprintf("%d", int(h))
				}
			}
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, status, version, port, repo)
	}
	w.Flush()
}

func cmdInfo(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store info <app>")
	}

	resp := callDaemon("info", map[string]string{"app": args[0]})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	// Afficher le JSON formaté pour l'info détaillée
	data, _ := json.MarshalIndent(resp.Data, "", "  ")
	fmt.Println(string(data))
}

func cmdLogs(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store logs <app>")
	}

	resp := callDaemon("logs", map[string]string{"app": args[0]})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	if m, ok := resp.Data.(map[string]interface{}); ok {
		fmt.Printf("→ %s\n", m["message"])
		fmt.Printf("  docker compose -f %s/compose.yml logs -f\n", m["compose_dir"])
	}
}

func cmdSearch(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store search <terme>")
	}

	resp := callDaemon("search", map[string]string{"term": args[0]})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	apps, ok := resp.Data.([]interface{})
	if !ok || len(apps) == 0 {
		fmt.Printf("Aucun résultat pour '%s'\n", args[0])
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tNOM\tCATÉGORIE\tDÉPÔT")
	fmt.Fprintln(w, "──\t───\t─────────\t─────")

	for _, a := range apps {
		app, _ := a.(map[string]interface{})
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			strField(app, "id"),
			strField(app, "name"),
			strField(app, "category"),
			strField(app, "repository"),
		)
	}
	w.Flush()
}

func cmdUpdate(args []string) {
	fmt.Println("🔄 Synchronisation des dépôts...")
	resp := callDaemon("update", nil)
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	fmt.Println("✅ Dépôts synchronisés")
}

func cmdUpgrade(args []string) {
	checkOnly := contains(args, "--check")

	if checkOnly {
		fmt.Println("🔍 Vérification des mises à jour...")
	} else {
		fmt.Println("⬆️  Mise à jour de Caleope...")
		fmt.Println("   (le daemon va redémarrer automatiquement)")
	}

	apiArgs := map[string]string{}
	if checkOnly {
		apiArgs["check"] = "true"
	}

	resp := callDaemon("upgrade", apiArgs)
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	if m, ok := resp.Data.(map[string]interface{}); ok {
		fmt.Printf("✅ %s\n", m["message"])
		if url, ok := m["url"].(string); ok && url != "" {
			fmt.Printf("   Détails : %s\n", url)
		}
	}
}

func cmdPing() {
	resp := callDaemon("ping", nil)
	if !resp.Success {
		fmt.Println("❌ Daemon non disponible")
		os.Exit(1)
	}
	if m, ok := resp.Data.(map[string]interface{}); ok {
		fmt.Printf("✓ Daemon actif — version %s\n", m["version"])
		if domain, ok := m["domain"].(string); ok && domain != "" {
			fmt.Printf("  Domaine    : %s\n", domain)
			fmt.Printf("  Proxy mode : %s\n", m["proxy_mode"])
		} else {
			fmt.Printf("  ⚠️  Aucun domaine configuré (caleope.conf manquant ?)\n")
		}
	}
}

// ─────────────────────────────────────────────
// COMMUNICATION AVEC LE DAEMON
// ─────────────────────────────────────────────

// callDaemon envoie une requête au daemon et retourne la réponse.
func callDaemon(command string, args map[string]string) types.APIResponse {
	// Se connecter au socket UNIX
	conn, err := net.Dial("unix", SOCKET_PATH)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Impossible de contacter le daemon.\n")
		fmt.Fprintf(os.Stderr, "   Vérifiez que caleoped tourne : systemctl status caleoped\n")
		os.Exit(1)
	}
	defer conn.Close()

	// Envoyer la requête JSON
	req := types.APIRequest{
		Command: command,
		Args:    args,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		die("Erreur envoi requête: " + err.Error())
	}

	// Lire la réponse JSON
	var resp types.APIResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		die("Erreur lecture réponse: " + err.Error())
	}

	return resp
}

// ─────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────

func printHelp() {
	fmt.Println(`Caleope Store — Gestionnaire d'applications self-hosted

Usage:
  caleope-store <commande> [options]

Commandes:
  install <app>     Installer une application
    --domain <dom>  Domaine (optionnel — auto: <app>.<domaine_base>)
    --channel       Canal: stable (défaut), latest, nightly
    --force         Forcer la réinstallation

  remove <app>      Désinstaller une application
    --keep-data     Conserver les données

  list              Lister les applications installées
    --json          Sortie JSON (mode machine)

  info <app>        Détails d'une application installée
  logs <app>        Afficher les logs d'une application
  search <terme>    Rechercher une application dans le store
  update            Synchroniser les dépôts Git
  upgrade           Mettre à jour Caleope vers la dernière version
    --check         Vérifier sans installer
  version           Afficher la version installée
  ping              Vérifier que le daemon est actif

Exemples:
  caleope-store install jellyfin --domain media.home.local
  caleope-store install nextcloud --domain cloud.home.local
  caleope-store list
  caleope-store remove jellyfin
  caleope-store search media`)
}

func formatStatus(status string) string {
	switch status {
	case "running":
		return "✅ actif"
	case "stopped":
		return "⏹ arrêté"
	case "installing":
		return "⏳ installation"
	case "error":
		return "❌ erreur"
	default:
		return status
	}
}

func strField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return "-"
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// die affiche une erreur et quitte le programme.
// os.Exit(1) = code de retour non-zéro = erreur (convention Unix)
func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
