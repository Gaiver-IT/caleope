// cmd/caleope/main.go
//
// 💻 LE CLI — interface utilisateur en ligne de commande
//
// caleope est le client du daemon.
// Il traduit les commandes humaines en requêtes API JSON sur le socket UNIX.
//
// FLUX :
//   $ caleope install jellyfin
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
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

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
	case "events":
		cmdEvents(args)
	case "location", "locations":
		cmdLocation(args)
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
		fmt.Printf("caleope %s (commit: %s)\n", version.Version, version.Commit)
	case "ping":
		cmdPing()
	case "token":
		cmdToken()
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
		die("Usage: caleope install <app> [--domain <domaine>] [--channel stable|latest|nightly]")
	}

	apiArgs := map[string]string{
		"app": args[0],
	}

	// Parser les flags optionnels
	// Ex: caleope install jellyfin --domain media.home.local --channel latest
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

	if m, ok := resp.Data.(map[string]interface{}); ok {
		if notes, ok := m["notes"].(string); ok && notes != "" {
			fmt.Println(notes)
		}
	}
}

func cmdRemove(args []string) {
	if len(args) == 0 {
		die("Usage: caleope remove <app> [--keep-data]")
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
		die("Usage: caleope info <app>")
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
		die("Usage: caleope logs <app> [--tail <n>]")
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
		die("Usage: caleope search <terme>")
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

func cmdEvents(args []string) {
	apiArgs := map[string]string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--app":
			if i+1 < len(args) {
				apiArgs["app"] = args[i+1]
				i++
			}
		case "--type":
			if i+1 < len(args) {
				apiArgs["type"] = args[i+1]
				i++
			}
		case "--limit", "-n":
			if i+1 < len(args) {
				apiArgs["limit"] = args[i+1]
				i++
			}
		}
	}

	resp := callDaemon("events", apiArgs)
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	evts, ok := resp.Data.([]interface{})
	if !ok || len(evts) == 0 {
		fmt.Println("Aucun événement.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIMESTAMP\tTYPE\tAPP\tDÉTAILS")
	fmt.Fprintln(w, "─────────────────────\t────────────────\t────────────\t───────")
	for _, e := range evts {
		ev, _ := e.(map[string]interface{})
		ts := strField(ev, "timestamp")
		if len(ts) > 19 {
			ts = ts[:19]
		}
		evType := strField(ev, "event")
		app := strField(ev, "app")
		meta := ""
		if m, ok := ev["meta"].(map[string]interface{}); ok {
			var parts []string
			for k, v := range m {
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
			meta = strings.Join(parts, " ")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ts, evType, app, meta)
	}
	w.Flush()
}

func cmdLocation(args []string) {
	if len(args) == 0 {
		// Liste par défaut
		resp := callDaemon("location-list", nil)
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		locs, ok := resp.Data.([]interface{})
		if !ok || len(locs) == 0 {
			fmt.Println("Aucun emplacement réseau configuré.")
			fmt.Println("  caleope location add <nom> --type smb --host <host> --share <partage> --user <user>")
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NOM\tTYPE\tHÔTE\tPARTAGE\tMONTÉ\tPOINT DE MONTAGE")
		fmt.Fprintln(w, "───\t────\t────\t───────\t──────\t────────────────")
		for _, l := range locs {
			loc, _ := l.(map[string]interface{})
			mounted := "non"
			if loc["mounted"] == true {
				mounted = "✓"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				strField(loc, "name"), strField(loc, "type"),
				strField(loc, "host"), strField(loc, "share"),
				mounted, strField(loc, "mount_point"))
		}
		w.Flush()
		return
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		cmdLocation(nil)

	case "add":
		if len(rest) == 0 {
			die("Usage: caleope location add <nom> --type smb|cifs|sftp --host <host> --share <partage> [--user <user>] [--password <pass>]")
		}
		apiArgs := map[string]string{"name": rest[0]}
		for i := 1; i < len(rest); i++ {
			switch rest[i] {
			case "--type":
				if i+1 < len(rest) {
					apiArgs["type"] = rest[i+1]
					i++
				}
			case "--host":
				if i+1 < len(rest) {
					apiArgs["host"] = rest[i+1]
					i++
				}
			case "--share", "--path":
				if i+1 < len(rest) {
					apiArgs["share"] = rest[i+1]
					i++
				}
			case "--user", "--username":
				if i+1 < len(rest) {
					apiArgs["username"] = rest[i+1]
					i++
				}
			case "--password":
				if i+1 < len(rest) {
					apiArgs["password"] = rest[i+1]
					i++
				}
			case "--options":
				if i+1 < len(rest) {
					apiArgs["options"] = rest[i+1]
					i++
				}
			}
		}
		// Demander le mot de passe interactivement si --user présent mais pas --password
		// (évite d'avoir le mot de passe dans l'historique bash, et évite les problèmes
		//  avec les caractères spéciaux comme ! qui déclenchent l'expansion d'historique)
		if apiArgs["username"] != "" && apiArgs["password"] == "" {
			fmt.Printf("🔑 Mot de passe pour %s@%s : ", apiArgs["username"], apiArgs["host"])
			password, err := readPassword()
			fmt.Println() // saut de ligne après la saisie masquée
			if err == nil && password != "" {
				apiArgs["password"] = password
			}
		}
		resp := callDaemon("location-add", apiArgs)
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		if m, ok := resp.Data.(map[string]interface{}); ok {
			fmt.Printf("✅ %s\n", m["message"])
			fmt.Printf("   Point de montage : %s\n", m["mount_point"])
			printLocationMountResult(m)
		}

	case "remove", "rm":
		if len(rest) == 0 {
			die("Usage: caleope location remove <nom>")
		}
		resp := callDaemon("location-remove", map[string]string{"name": rest[0]})
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		fmt.Printf("✅ Emplacement '%s' supprimé\n", rest[0])

	case "mount":
		if len(rest) == 0 {
			die("Usage: caleope location mount <nom>")
		}
		fmt.Printf("🔗 Montage de '%s'...\n", rest[0])
		resp := callDaemon("location-mount", map[string]string{"name": rest[0]})
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		if m, ok := resp.Data.(map[string]interface{}); ok {
			fmt.Printf("✅ '%s' monté sur %s\n", rest[0], m["mount_point"])
			printLocationMountResult(m)
		} else {
			fmt.Printf("✅ '%s' monté\n", rest[0])
		}

	case "unmount":
		if len(rest) == 0 {
			die("Usage: caleope location unmount <nom>")
		}
		resp := callDaemon("location-unmount", map[string]string{"name": rest[0]})
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		fmt.Printf("✅ '%s' démonté\n", rest[0])

	default:
		die("Sous-commande inconnue: " + sub + "\n  Utilisez: add, remove, mount, unmount, list")
	}
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
	// Largeurs des colonnes (en caractères visibles)
	const (
		wApp    = 16
		wStatus = 12
		wCPU    = 7
		wRAM    = 9
		wDisk   = 9
		wPort   = 6
	)

	if advanced {
		fmt.Printf("  %s%s%s%s%s%s\n",
			padR("APP", wApp), padR("ÉTAT", wStatus),
			padR("CPU", wCPU), padR("RAM", wRAM),
			padR("DISK", wDisk), "PORT")
		fmt.Printf("  %s%s%s%s%s%s\n",
			padR("───", wApp), padR("────", wStatus),
			padR("───", wCPU), padR("───", wRAM),
			padR("────", wDisk), "────")
	} else {
		fmt.Printf("  %s%s%s%s%s\n",
			padR("APP", wApp), padR("ÉTAT", wStatus),
			padR("CPU", wCPU), padR("RAM", wRAM), "PORT")
		fmt.Printf("  %s%s%s%s%s\n",
			padR("───", wApp), padR("────", wStatus),
			padR("───", wCPU), padR("───", wRAM), "────")
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
			fmt.Printf("  %s%s%s%s%s%s\n",
				padR(a.Name, wApp), padRANSI(status, wStatus),
				padR(cpu, wCPU), padR(ram, wRAM),
				padR(disk, wDisk), port)
		} else {
			fmt.Printf("  %s%s%s%s%s\n",
				padR(a.Name, wApp), padRANSI(status, wStatus),
				padR(cpu, wCPU), padR(ram, wRAM), port)
		}
	}

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

// padR pad un string à droite jusqu'à width caractères visibles.
func padR(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// padRANSI pad un string contenant des séquences ANSI.
// Calcule la largeur visible en ignorant les séquences \033[...m.
func padRANSI(s string, width int) string {
	visible := 0
	i := 0
	runes := []rune(s)
	for i < len(runes) {
		if runes[i] == '\033' && i+1 < len(runes) && runes[i+1] == '[' {
			for i < len(runes) && runes[i] != 'm' {
				i++
			}
			i++ // skip 'm'
		} else {
			visible++
			i++
		}
	}
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
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
		die("Usage: caleope " + action + " <app>")
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
		die("Usage: caleope backup <app>")
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
		die("Usage: caleope restore <app> [--backup <timestamp>]")
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
		die("Usage: caleope backups <app>")
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

func cmdToken() {
	resp := callDaemon("token", nil)
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	if m, ok := resp.Data.(map[string]interface{}); ok {
		fmt.Printf("🔑 Token API : %s\n", m["token"])
		fmt.Println()
		fmt.Println("Usage (curl) :")
		fmt.Printf("  curl -H 'Authorization: Bearer %s' http://localhost:8765/api/v1/apps\n", m["token"])
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

// printLocationMountResult affiche le résultat d'un montage (succès + fichiers, ou erreur).
// m est la map de la réponse API location-add ou location-mount.
func printLocationMountResult(m map[string]interface{}) {
	mounted, _ := m["mounted"].(bool)
	if !mounted {
		if mountErr, ok := m["mount_error"].(string); ok && mountErr != "" {
			fmt.Printf("\n   ⚠️  Montage automatique échoué :\n      %s\n", mountErr)
			fmt.Printf("   → Corrige le problème puis relance : caleope location mount <nom>\n")
		}
		return
	}

	// Montage réussi — afficher les fichiers
	files, hasFiles := m["files"].([]interface{})
	if !hasFiles || len(files) == 0 {
		fmt.Printf("   📂 Montage OK — dossier vide\n")
		return
	}

	fmt.Printf("\n   📂 Contenu (%d entrée(s)) :\n", len(files))
	for _, f := range files {
		fmt.Printf("      %s\n", f)
	}
}

// readPassword lit un mot de passe sans l'afficher (mode raw terminal).
// Utilise golang.org/x/term — cross-platform (Linux + macOS).
// Fallback sur lecture normale si le terminal n'est pas interactif (pipe).
func readPassword() (string, error) {
	pwd, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		// Pas un terminal (ex: pipe) — lire normalement
		var line string
		fmt.Scanln(&line)
		return line, nil
	}
	return string(pwd), nil
}

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
	fmt.Println(`Caleope — Gestionnaire d'applications self-hosted

Usage:
  caleope <commande> [options]

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

  events            Historique des événements système
    --app <app>     Filtrer par application
    --type <type>   Filtrer par type (app_started, app_stopped, install…)
    --limit <n>     Nombre d'événements (défaut : 50)

  location          Gérer les emplacements réseau (SMB, CIFS, SFTP)
  location list     Lister les emplacements configurés
  location add <n>  Ajouter un emplacement
    --type smb|cifs|sftp
    --host <host>   Adresse du serveur
    --share <part>  Nom du partage / chemin distant
    --user <user>   Nom d'utilisateur (optionnel)
    --password <pw> Mot de passe (optionnel)
    --options <opt> Options de montage supplémentaires
  location remove <n>   Supprimer un emplacement (démonte si monté)
  location mount <n>    Monter un emplacement
  location unmount <n>  Démonter un emplacement

  update            Synchroniser les dépôts Git
  upgrade           Mettre à jour Caleope vers la dernière version
    --check         Vérifier sans installer
  token             Afficher le token d'accès à l'API REST (:8765)
  version           Afficher la version installée
  ping              Vérifier que le daemon est actif

Exemples:
  caleope install jellyfin --domain media.home.local
  caleope install nextcloud --domain cloud.home.local
  caleope list
  caleope remove jellyfin
  caleope search media
  caleope events --app nextcloud --limit 20
  caleope location add nas --type smb --host 192.168.1.10 --share backup --user ewen
  caleope location mount nas`)
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
