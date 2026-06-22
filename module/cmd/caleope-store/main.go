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
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

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
	case "top":
		cmdTop(args)
	case "stop":
		cmdStopStart("stop", args)
	case "start":
		cmdStopStart("start", args)
	case "restart":
		cmdStopStart("restart", args)
	case "backup":
		cmdBackup(args)
	case "restore":
		cmdRestore(args)
	case "backups":
		cmdBackupList(args)
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

	// Récupérer les params interactifs définis dans params.json de l'app.
	// Si l'app n'en a pas, fetchStoreParams retourne nil et on passe directement à l'install.
	paramDefs := fetchStoreParams(args[0])
	if len(paramDefs) > 0 {
		needHeader := true
		for _, p := range paramDefs {
			// 1. Lire CALEOPE_PARAM_<ID> depuis l'environnement (installs non-interactives)
			envKey := "CALEOPE_PARAM_" + strings.ToUpper(p.ID)
			if envVal := os.Getenv(envKey); envVal != "" {
				apiArgs["param_"+p.ID] = envVal
				continue
			}
			// 2. Vérifier qu'un terminal est disponible avant de prompter
			tty, ttyErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
			if ttyErr == nil {
				tty.Close()
			}
			if ttyErr != nil {
				if p.Required {
					die(fmt.Sprintf("Param requis '%s' manquant. Utiliser CALEOPE_PARAM_%s=<val> ou --param %s=<val>", p.ID, strings.ToUpper(p.ID), p.ID))
				}
				continue
			}
			// 3. Prompt interactif
			if needHeader {
				fmt.Printf("\n⚙️  Configuration de '%s'\n", args[0])
				fmt.Printf("   (les champs marqués * sont obligatoires)\n\n")
				needHeader = false
			}
			val := promptParam(p)
			// Boucle jusqu'à saisie valide pour les champs requis
			for p.Required && val == "" {
				fmt.Printf("  ⚠  Ce champ est requis.\n")
				val = promptParam(p)
			}
			if val != "" {
				apiArgs["param_"+p.ID] = val
			}
		}
		if !needHeader {
			fmt.Println()
		}
	}

	fmt.Printf("📦 Installation de '%s'...\n", args[0])
	resp := callDaemon("install", apiArgs)

	if !resp.Success {
		die("❌ " + resp.Error)
	}

	if m, ok := resp.Data.(map[string]interface{}); ok {
		if notes, ok := m["notes"].(string); ok && notes != "" {
			fmt.Println(notes)
		}
	}
}

// fetchStoreParams interroge le daemon pour obtenir les params interactifs d'une app.
// Retourne nil si l'app n'a pas de params.json ou en cas d'erreur.
func fetchStoreParams(appID string) []types.ParamDef {
	resp := callDaemon("store-params", map[string]string{"app": appID})
	if !resp.Success || resp.Data == nil {
		return nil
	}
	// resp.Data est interface{} après décodage JSON — re-sérialiser pour typer correctement
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return nil
	}
	var params []types.ParamDef
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	return params
}

// promptParam affiche un prompt interactif pour un param et retourne la valeur saisie.
// - Type "secret" : masque la saisie (stty -echo)
// - Valeur vide + default non vide → retourne le default
func promptParam(p types.ParamDef) string {
	// Ouvrir /dev/tty pour garantir l'interactivité même si stdin est redirigé
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		tty = os.Stdin // fallback
	} else {
		defer tty.Close()
	}

	// Description en gris (optionnelle)
	if p.Description != "" {
		fmt.Fprintf(tty, "  \033[2m%s\033[0m\n", p.Description)
	}

	// Ligne de prompt : label + indication required/default
	switch {
	case p.Default != "":
		fmt.Fprintf(tty, "  %s [%s] : ", p.Label, p.Default)
	case p.Required:
		fmt.Fprintf(tty, "  %s * : ", p.Label)
	default:
		fmt.Fprintf(tty, "  %s : ", p.Label)
	}

	// Masquer l'écho pour les secrets
	if p.Type == "secret" {
		exec.Command("stty", "-F", "/dev/tty", "-echo").Run() //nolint
	}

	reader := bufio.NewReader(tty)
	val, _ := reader.ReadString('\n')
	val = strings.TrimRight(val, "\r\n")

	// Restaurer l'écho et sauter une ligne (le curseur est resté sur la même ligne)
	if p.Type == "secret" {
		exec.Command("stty", "-F", "/dev/tty", "echo").Run() //nolint
		fmt.Fprintln(tty)
	}

	// Utiliser le default si l'utilisateur a laissé vide
	if val == "" && p.Default != "" {
		return p.Default
	}
	return val
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
		die("Usage: caleope-store logs <app> [--tail <n>]")
	}

	apiArgs := map[string]string{"app": args[0]}
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--tail" {
			apiArgs["tail"] = args[i+1]
			i++
		}
	}

	resp := callDaemon("logs", apiArgs)
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	if m, ok := resp.Data.(map[string]interface{}); ok {
		if logs, ok := m["logs"].(string); ok {
			fmt.Print(logs)
		}
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

func cmdTop(args []string) {
	advanced := contains(args, "--advanced") || contains(args, "-a")

	// Intercepter Ctrl+C pour quitter proprement
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Premier affichage immédiat
	printTop(advanced)

	for {
		select {
		case <-sigChan:
			fmt.Print("\033[?25h") // réafficher le curseur
			fmt.Println()
			return
		case <-ticker.C:
			printTop(advanced)
		}
	}
}

func printTop(advanced bool) {
	apiArgs := map[string]string{}
	if advanced {
		apiArgs["disk"] = "true"
	}

	resp := callDaemon("stats", apiArgs)
	if !resp.Success {
		fmt.Printf("\033[2J\033[H❌ %s\n", resp.Error)
		return
	}

	// Désérialiser le snapshot
	raw, _ := json.Marshal(resp.Data)
	var snap types.StatsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		fmt.Printf("\033[2J\033[H❌ Erreur parsing: %s\n", err)
		return
	}

	// Effacer l'écran et revenir en haut
	fmt.Print("\033[2J\033[H\033[?25l")

	// ── En-tête ──
	now := time.Now().Format("15:04:05")
	fmt.Printf("\033[1;36m  Caleope — Supervision\033[0m          \033[90m%s   Ctrl+C pour quitter\033[0m\n", now)
	fmt.Println()

	// ── Système ──
	memPct := 0.0
	if snap.MemTotalMB > 0 {
		memPct = snap.MemUsedMB / snap.MemTotalMB * 100
	}
	diskPct := 0.0
	if snap.DiskTotalGB > 0 {
		diskPct = snap.DiskUsedGB / snap.DiskTotalGB * 100
	}
	fmt.Printf("  \033[1mSystème\033[0m   RAM \033[33m%.0f/%.0f MB\033[0m (%s)   Disk \033[33m%.1f/%.1f GB\033[0m (%s)\n",
		snap.MemUsedMB, snap.MemTotalMB, colorPct(memPct),
		snap.DiskUsedGB, snap.DiskTotalGB, colorPct(diskPct),
	)
	fmt.Println()

	// ── Tableau apps ──
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if advanced {
		fmt.Fprintln(w, "  APP\tÉTAT\tCPU\tRAM\tDISK\tPORT")
		fmt.Fprintln(w, "  ───\t────\t───\t───\t────\t────")
	} else {
		fmt.Fprintln(w, "  APP\tÉTAT\tCPU\tRAM\tPORT")
		fmt.Fprintln(w, "  ───\t────\t───\t───\t────")
	}

	for _, a := range snap.Apps {
		status := formatStatusTop(a.Status)
		cpu := fmt.Sprintf("%.1f%%", a.CPUPercent)
		ram := fmt.Sprintf("%.0f MB", a.MemoryMB)
		port := "-"
		if a.Port > 0 {
			port = fmt.Sprintf("%d", a.Port)
		}

		if advanced {
			disk := "-"
			if a.DiskMB >= 0 {
				if a.DiskMB > 1024 {
					disk = fmt.Sprintf("%.1f GB", float64(a.DiskMB)/1024)
				} else {
					disk = fmt.Sprintf("%d MB", a.DiskMB)
				}
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n", a.Name, status, cpu, ram, disk, port)
		} else {
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", a.Name, status, cpu, ram, port)
		}
	}
	w.Flush()

	if len(snap.Apps) == 0 {
		fmt.Println("  Aucune application installée.")
	}

	if advanced {
		fmt.Println()
		fmt.Printf("\033[90m  Mode avancé — métriques Prometheus sur :9100/metrics\033[0m\n")
	}
}

func formatStatusTop(status string) string {
	switch status {
	case "running":
		return "\033[32m● actif\033[0m"
	case "stopped":
		return "\033[33m● arrêté\033[0m"
	case "installing":
		return "\033[36m● install...\033[0m"
	case "error":
		return "\033[31m● erreur\033[0m"
	default:
		return "\033[90m● " + status + "\033[0m"
	}
}

func colorPct(pct float64) string {
	s := fmt.Sprintf("%.0f%%", pct)
	if pct >= 90 {
		return "\033[31m" + s + "\033[0m"
	} else if pct >= 70 {
		return "\033[33m" + s + "\033[0m"
	}
	return "\033[32m" + s + "\033[0m"
}

func cmdStopStart(action string, args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store " + action + " <app>")
	}
	icons := map[string]string{"stop": "⏹", "start": "▶️", "restart": "🔄"}
	labels := map[string]string{"stop": "Arrêt", "start": "Démarrage", "restart": "Redémarrage"}
	done := map[string]string{"stop": "arrêté", "start": "démarré", "restart": "redémarré"}
	fmt.Printf("%s  %s de '%s'...\n", icons[action], labels[action], args[0])

	resp := callDaemon(action, map[string]string{"app": args[0]})
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	fmt.Printf("✅ '%s' %s\n", args[0], done[action])
}

func cmdBackup(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store backup <app>")
	}

	fmt.Printf("💾 Sauvegarde de '%s'...\n", args[0])
	resp := callDaemon("backup", map[string]string{"app": args[0]})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	if m, ok := resp.Data.(map[string]interface{}); ok {
		fmt.Printf("✅ %s\n", m["message"])
	}
}

func cmdRestore(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store restore <app> [--backup <timestamp>]")
	}

	apiArgs := map[string]string{"app": args[0]}
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--backup" {
			apiArgs["backup"] = args[i+1]
			i++
		}
	}

	fmt.Printf("⚠️  Restaurer '%s' ? Les données actuelles seront écrasées. [o/N] ", args[0])
	var resp string
	fmt.Scanln(&resp)
	if strings.ToLower(resp) != "o" {
		fmt.Println("Annulé.")
		return
	}

	fmt.Printf("♻️  Restauration de '%s'...\n", args[0])
	apiResp := callDaemon("restore", apiArgs)
	if !apiResp.Success {
		die("❌ " + apiResp.Error)
	}
	fmt.Println("✅ Restauration terminée")
}

func cmdBackupList(args []string) {
	if len(args) == 0 {
		die("Usage: caleope-store backups <app>")
	}

	resp := callDaemon("backup-list", map[string]string{"app": args[0]})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	backups, ok := resp.Data.([]interface{})
	if !ok || len(backups) == 0 {
		fmt.Printf("Aucun backup trouvé pour '%s'\n", args[0])
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "TIMESTAMP\tDATA\tCONFIG\tVERSION")
	fmt.Fprintln(w, "─────────\t────\t──────\t───────")

	for _, b := range backups {
		bm, _ := b.(map[string]interface{})
		ts := strField(bm, "timestamp")
		if len(ts) > 19 {
			ts = ts[:19]
		}
		hasData := "✓"
		if bm["has_data"] != true {
			hasData = "-"
		}
		hasConfig := "✓"
		if bm["has_config"] != true {
			hasConfig = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			ts, hasData, hasConfig, strField(bm, "caleope_version"))
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

  top               Supervision live (refresh 2s, Ctrl+C pour quitter)
    --advanced      Mode avancé : disk, port
  stop <app>        Arrêter une application
  start <app>       Démarrer une application arrêtée
  restart <app>     Redémarrer une application

  remove <app>      Désinstaller une application
    --keep-data     Conserver les données

  list              Lister les applications installées
    --json          Sortie JSON (mode machine)

  info <app>        Détails d'une application installée
  logs <app>        Afficher les logs d'une application (100 dernières lignes)
    --tail <n>      Nombre de lignes à afficher
  search <terme>    Rechercher une application dans le store
  backup <app>      Sauvegarder une application (données + config)
  restore <app>     Restaurer une sauvegarde
    --backup <ts>   Timestamp du backup (défaut : le plus récent)
  backups <app>     Lister les sauvegardes disponibles

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
