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
	"bufio"
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
	case "configure":
		cmdConfigure(args)
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
	case "license":
		cmdLicense(args)
	case "token":
		cmdToken()
	case "offline-pack":
		cmdOfflinePack(args)
	case "offline-update":
		cmdOfflineUpdate(args)
	case "task", "tasks":
		cmdTask(args)
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
		die("Usage: caleope install <app> [--domain <domaine>] [--channel stable|latest|nightly] [--alpha] [--storage <location>] [--param KEY=VALUE] [--gpu]")
	}

	apiArgs := map[string]string{
		"app": args[0],
	}

	// Parser les flags optionnels
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--domain":
			if i+1 < len(args) {
				apiArgs["domain"] = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				apiArgs["channel"] = args[i+1]
				i++
			}
		case "--alpha":
			// Raccourci équivalent à --channel alpha : installe depuis la branche
			// alpha du store (le daemon synchronise le cache sur cette branche).
			apiArgs["channel"] = "alpha"
		case "--force":
			apiArgs["force"] = "true"
		case "--storage":
			if i+1 < len(args) {
				apiArgs["storage"] = args[i+1]
				i++
			}
		case "--param":
			if i+1 < len(args) {
				kv := args[i+1]
				if idx := strings.IndexByte(kv, '='); idx > 0 {
					apiArgs["param_"+strings.ToLower(kv[:idx])] = kv[idx+1:]
				}
				i++
			}
		case "--gpu":
			apiArgs["gpu"] = "true"
		}
	}

	// Lire CALEOPE_PARAM_* depuis l'environnement (priorité basse : surchargeable par --param)
	for _, envLine := range os.Environ() {
		if strings.HasPrefix(envLine, "CALEOPE_PARAM_") {
			if parts := strings.SplitN(envLine, "=", 2); len(parts) == 2 {
				key := "param_" + strings.ToLower(strings.TrimPrefix(parts[0], "CALEOPE_PARAM_"))
				if _, exists := apiArgs[key]; !exists {
					apiArgs[key] = parts[1]
				}
			}
		}
	}

	// Collecter les params requis manquants via prompt interactif (terminal seulement)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		paramsResp := callDaemon("store-params", map[string]string{"app": args[0]})
		if paramsResp.Success {
			if paramDefs, ok := paramsResp.Data.([]interface{}); ok && len(paramDefs) > 0 {
				r := bufio.NewReader(os.Stdin)
				needHeader := true
				for _, raw := range paramDefs {
					def, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					id, _ := def["id"].(string)
					if id == "" {
						continue
					}
					paramKey := "param_" + strings.ToLower(id)
					if _, alreadySet := apiArgs[paramKey]; alreadySet {
						continue
					}
					required, _ := def["required"].(bool)
					defaultVal, _ := def["default"].(string)
					if !required && defaultVal != "" {
						continue
					}
					if needHeader {
						fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
						fmt.Printf("  Paramètres de configuration pour '%s'\n", args[0])
						fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
						needHeader = false
					}
					label, _ := def["label"].(string)
					desc, _ := def["description"].(string)
					ptype, _ := def["type"].(string)
					if desc != "" {
						fmt.Printf("\n  %s\n  (%s)\n", label, desc)
					} else {
						fmt.Printf("\n  %s\n", label)
					}
					var val string
					for {
						if defaultVal != "" {
							fmt.Printf("  Valeur [%s] : ", defaultVal)
						} else {
							fmt.Printf("  Valeur : ")
						}
						var line string
						if ptype == "secret" {
							b, err := term.ReadPassword(int(os.Stdin.Fd()))
							fmt.Println()
							if err == nil {
								line = strings.TrimSpace(string(b))
							}
						} else {
							line, _ = r.ReadString('\n')
							line = strings.TrimSpace(line)
						}
						if line == "" {
							line = defaultVal
						}
						if line != "" || !required {
							val = line
							break
						}
						fmt.Println("  ❌ Ce paramètre est obligatoire.")
					}
					if val != "" {
						apiArgs[paramKey] = val
					}
				}
			}
		}
	}

	if storage, ok := apiArgs["storage"]; ok && storage != "" {
		fmt.Printf("📦 Installation de '%s' (données sur NAS '%s')...\n", args[0], storage)
	} else {
		fmt.Printf("📦 Installation de '%s'...\n", args[0])
	}
	resp := callDaemon("install", apiArgs)

	if !resp.Success {
		die("❌ " + resp.Error)
	}

	// Mémoriser les notes post-install : on les affiche EN DERNIER,
	// après les wizards interactifs, pour que l'utilisateur les voie à la fin.
	postInstallNotes := ""
	if m, ok := resp.Data.(map[string]interface{}); ok {
		if notes, ok := m["notes"].(string); ok {
			postInstallNotes = notes
		}
	}

	// Apps avec wizard post-install interactif
	// (le daemon n'a pas de terminal → on lance le wizard ici, dans le CLI)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		switch args[0] {
		case "arr-stack":
			fmt.Println()
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("  Voulez-vous configurer le VPN maintenant ?")
			fmt.Println("  (ou plus tard : caleope configure arr-stack)")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			r := bufio.NewReader(os.Stdin)
			fmt.Print("  Configurer le VPN ? [o/N] : ")
			line, _ := r.ReadString('\n')
			line = strings.TrimSpace(strings.ToLower(line))
			if line == "o" || line == "oui" || line == "y" || line == "yes" {
				// Le wizard retourne les notes mises à jour (post VPN patch)
				if updated := cmdConfigureArrStack(); updated != "" {
					postInstallNotes = updated
				}
			}
		}
	}

	// Afficher les notes à la toute fin (après les wizards)
	if postInstallNotes != "" {
		fmt.Println(postInstallNotes)
	}
}

// ─────────────────────────────────────────────
// CONFIGURE — wizard interactif par app
// ─────────────────────────────────────────────

func cmdConfigure(args []string) {
	if len(args) == 0 {
		die("Usage: caleope configure <app>\n  Ex:    caleope configure arr-stack")
	}


	switch args[0] {
	case "arr-stack":
		if notes := cmdConfigureArrStack(); notes != "" {
			fmt.Println(notes)
		}
	default:
		die(fmt.Sprintf("❌ configure: pas de wizard disponible pour '%s'\n   Apps supportées: arr-stack", args[0]))
	}
}

// cmdConfigureArrStack — wizard interactif pour reconfigurer le VPN de arr-stack.
// S'exécute dans le processus CLI (terminal interactif), puis envoie les updates au daemon.
// Retourne les notes post-install mises à jour (depuis la réponse daemon), ou "" si indisponibles.
func cmdConfigureArrStack() string {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		die("❌ caleope configure requiert un terminal interactif.\n   Pour une config non-interactive, utilisez l'API REST.")
	}

	r := bufio.NewReader(os.Stdin)
	ask := func(prompt, defaultVal string) string {
		if defaultVal != "" {
			fmt.Printf("  %s [%s] : ", prompt, defaultVal)
		} else {
			fmt.Printf("  %s : ", prompt)
		}
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultVal
		}
		return line
	}
	askPassword := func(prompt string) string {
		fmt.Printf("  %s : ", prompt)
		pwd, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return ""
		}
		return string(pwd)
	}

	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────┐")
	fmt.Println("│  🔒 Arr Stack — Reconfiguration VPN                             │")
	fmt.Println("│                                                                 │")
	fmt.Println("│  Reconfigure le VPN pour qBittorrent (Gluetun).                │")
	fmt.Println("│  La stack sera redémarrée automatiquement.                      │")
	fmt.Println("└─────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	updates := map[string]string{"app": "arr-stack"}

	// ── Activer/désactiver le VPN ──
	vpnAnswer := ask("Activer un VPN ? [o/N]", "N")
	vpnEnabled := vpnAnswer == "o" || vpnAnswer == "oui" || vpnAnswer == "y" || vpnAnswer == "yes"

	if !vpnEnabled {
		updates["COMPOSE_PROFILES"] = "novpn,jellyfin"
		updates["ARR_QBT_HOST"] = "qbittorrent"
		updates["ARR_VPN_PROVIDER"] = ""
		updates["ARR_VPN_TYPE"] = ""
		updates["ARR_VPN_WG_PRIVATE_KEY"] = ""
		updates["ARR_VPN_WG_ADDRESSES"] = ""
		updates["ARR_VPN_OPENVPN_USER"] = ""
		updates["ARR_VPN_OPENVPN_PASSWORD"] = ""
		updates["ARR_VPN_SERVER_COUNTRIES"] = ""
		fmt.Println("  ✓ VPN désactivé")
	} else {
		fmt.Println()
		fmt.Println("  Fournisseur VPN :")
		fmt.Println("    1) ProtonVPN  (recommandé)")
		fmt.Println("    2) Mullvad")
		fmt.Println("    3) NordVPN")
		fmt.Println("    4) Private Internet Access (PIA)")
		fmt.Println("    5) Surfshark")
		fmt.Println("    6) ExpressVPN")
		fmt.Println("    7) Autre (compatible Gluetun)")

		providerChoice := ask("Choix [1-7]", "1")
		var provider string
		switch providerChoice {
		case "1":
			provider = "protonvpn"
		case "2":
			provider = "mullvad"
		case "3":
			provider = "nordvpn"
		case "4":
			provider = "private internet access"
		case "5":
			provider = "surfshark"
		case "6":
			provider = "expressvpn"
		case "7":
			provider = ask("Nom du fournisseur Gluetun (ex: ivpn)", "")
		default:
			provider = "protonvpn"
		}

		fmt.Println()
		fmt.Println("  Protocole :")
		fmt.Println("    1) WireGuard  (recommandé — plus rapide)")
		fmt.Println("    2) OpenVPN    (plus compatible)")
		protoChoice := ask("Choix [1/2]", "1")

		var vpnType, wgKey, wgAddr, ovpnUser, ovpnPass string
		if protoChoice == "2" {
			vpnType = "openvpn"
			fmt.Println()
			fmt.Println("  ── Identifiants OpenVPN ──────────────────────────────────────")
			ovpnUser = ask("Nom d'utilisateur", "")
			ovpnPass = askPassword("Mot de passe")
		} else {
			vpnType = "wireguard"
			fmt.Println()
			fmt.Println("  ── Clé WireGuard ─────────────────────────────────────────────")
			switch provider {
			case "protonvpn":
				fmt.Println("  → account.proton.me → VPN → Télécharger → WireGuard")
				fmt.Println("    Sélectionne le serveur, copie PrivateKey + Address depuis [Interface]")
				fmt.Println("    PublicKey et Endpoint sont inutiles — Gluetun les récupère automatiquement")
				fmt.Println("    Address : copie la ligne complète (IPv4 ou IPv4+IPv6 séparés par une virgule)")
			case "mullvad":
				fmt.Println("  → mullvad.net/account/wireguard-config")
			}
			fmt.Println()
			wgKey = ask("Clé privée WireGuard (PrivateKey)", "")
			// Accepter IPv4 seul ou IPv4+IPv6 séparés par une virgule
			// ex: "10.2.0.2/32" ou "10.2.0.2/32, 2a07:b944::2:2/128"
			wgAddr = ask("Adresse WireGuard (Address, ex: 10.2.0.2/32)", "")
		}

		fmt.Println()
		fmt.Println("  Pays de sortie VPN — nom complet en anglais (ex: Germany, France)")
		if provider == "protonvpn" {
			fmt.Println("  → SecureCore IS→DE : entrer 'Germany'  (pays de sortie uniquement)")
		}
		country := ask("Pays du serveur VPN (Entrée pour ignorer)", "")

		updates["COMPOSE_PROFILES"] = "vpn,jellyfin"
		updates["ARR_QBT_HOST"] = "arr-gluetun"
		updates["ARR_VPN_PROVIDER"] = provider
		updates["ARR_VPN_TYPE"] = vpnType
		updates["ARR_VPN_WG_PRIVATE_KEY"] = wgKey
		updates["ARR_VPN_WG_ADDRESSES"] = wgAddr
		updates["ARR_VPN_OPENVPN_USER"] = ovpnUser
		updates["ARR_VPN_OPENVPN_PASSWORD"] = ovpnPass
		updates["ARR_VPN_SERVER_COUNTRIES"] = country

		fmt.Printf("  ✓ VPN configuré : %s / %s\n", provider, vpnType)
	}

	fmt.Println()
	fmt.Println("→ Application de la configuration et redémarrage de la stack...")
	resp := callDaemon("configure", updates)
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	fmt.Println("✅ arr-stack reconfiguré — stack redémarrée")

	// Retourner les notes post-install mises à jour si le daemon les a incluses
	if m, ok := resp.Data.(map[string]interface{}); ok {
		if notes, ok := m["notes"].(string); ok && notes != "" {
			return notes
		}
	}
	return ""
}

func cmdRemove(args []string) {
	if len(args) == 0 {
		die("Usage: caleope remove <app> [--keep-data] [-y]")
	}

	apiArgs := map[string]string{"app": args[0]}
	skipConfirm := false

	for _, arg := range args[1:] {
		switch arg {
		case "--keep-data":
			apiArgs["keep_data"] = "true"
		case "-y", "--yes":
			skipConfirm = true
		}
	}

	if !skipConfirm {
		fmt.Printf("⚠️  Supprimer '%s' ? [o/N] ", args[0])
		var resp string
		fmt.Scanln(&resp)
		if strings.ToLower(resp) != "o" {
			fmt.Println("Annulé.")
			return
		}
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
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	// Le daemon retourne {apps: [...], services: [...]}
	var appsRaw []interface{}
	var servicesRaw []interface{}
	if dataMap, ok := resp.Data.(map[string]interface{}); ok {
		if a, ok := dataMap["apps"].([]interface{}); ok {
			appsRaw = a
		}
		if s, ok := dataMap["services"].([]interface{}); ok {
			servicesRaw = s
		}
	} else {
		// Compat descendante : ancien daemon retourne []interface{} directement
		appsRaw, _ = resp.Data.([]interface{})
	}

	if len(appsRaw) == 0 {
		fmt.Println("Aucune application installée.")
	} else {
		fmt.Fprintln(w, "NOM\tSTATUT\tVERSION\tPORT\tDÉPÔT")
		fmt.Fprintln(w, "───\t──────\t───────\t────\t─────")
		for _, a := range appsRaw {
			app, _ := a.(map[string]interface{})
			name := strField(app, "id")
			status := formatStatus(strField(app, "status"))
			version := strField(app, "version")
			repo := strField(app, "repository")
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
	}

	if len(servicesRaw) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "SERVICES PLATEFORME")
		fmt.Fprintln(w, "NOM\tSTATUT")
		fmt.Fprintln(w, "───\t──────")
		for _, s := range servicesRaw {
			svc, _ := s.(map[string]interface{})
			fmt.Fprintf(w, "%s\t%s\n", strField(svc, "id"), formatStatus(strField(svc, "status")))
		}
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
	for i := 1; i < len(args); i++ {
		if args[i] == "--tail" && i+1 < len(args) {
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

	case "storage":
		// caleope location storage <app>                → affiche le stockage actuel
		// caleope location storage <app> <location>     → migre vers le NAS
		// caleope location storage <app> local          → rapatrie en local
		if len(rest) == 0 {
			die("Usage: caleope location storage <app> [<location>|local]")
		}
		apiArgs := map[string]string{"app": rest[0]}
		if len(rest) >= 2 {
			apiArgs["location"] = rest[1]
		}
		resp := callDaemon("location-storage", apiArgs)
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		if m, ok := resp.Data.(map[string]interface{}); ok {
			if msg, ok := m["message"].(string); ok {
				// Migration effectuée
				fmt.Printf("✅ %s\n", msg)
			} else {
				// Affichage info
				storage := m["storage"]
				dataDir := m["data_dir"]
				if storage == "local" {
					fmt.Printf("💾 '%s' : stockage local\n   Données : %s\n", rest[0], dataDir)
				} else {
					fmt.Printf("💾 '%s' : NAS '%s'\n   Données : %s\n", rest[0], storage, dataDir)
				}
			}
		}

	default:
		die("Sous-commande inconnue: " + sub + "\n  Utilisez: add, remove, mount, unmount, list, storage")
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
		die("Usage: caleope backup <app> [--restic --repo <url> [--password <pass>]]")
	}

	apiArgs := map[string]string{"app": args[0]}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--restic":
			apiArgs["restic"] = "true"
		case "--repo":
			if i+1 < len(args) {
				apiArgs["repo"] = args[i+1]
				i++
			}
		case "--password":
			if i+1 < len(args) {
				apiArgs["restic_password"] = args[i+1]
				i++
			}
		}
	}
	// Lire depuis RESTIC_PASSWORD si non fourni via --password
	if apiArgs["restic"] == "true" && apiArgs["restic_password"] == "" {
		if p := os.Getenv("RESTIC_PASSWORD"); p != "" {
			apiArgs["restic_password"] = p
		}
	}

	fmt.Printf("💾 Sauvegarde de '%s'...\n", args[0])
	resp := callDaemon("backup", apiArgs)
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
	for i := 1; i < len(args); i++ {
		if args[i] == "--backup" && i+1 < len(args) {
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
	apiArgs := map[string]string{}
	for _, a := range args {
		if a == "--alpha" {
			apiArgs["channel"] = "alpha"
		}
	}
	if apiArgs["channel"] == "alpha" {
		fmt.Println("🔄 Synchronisation des dépôts (canal alpha)...")
	} else {
		fmt.Println("🔄 Synchronisation des dépôts...")
	}
	resp := callDaemon("update", apiArgs)
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	fmt.Println("✅ Dépôts synchronisés")
}

func cmdUpgrade(args []string) {
	checkOnly := contains(args, "--check")
	useAlpha := contains(args, "--alpha")

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
	if useAlpha {
		apiArgs["channel"] = "alpha"
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

	// Afficher le dossier caleope/ créé sur le NAS
	if caleopeDir, ok := m["caleope_dir"].(string); ok {
		fmt.Printf("   📁 Dossier Caleope créé sur le NAS : %s\n", caleopeDir)
		fmt.Printf("      Utilisez --storage <nom> lors de l'installation d'une app\n")
		fmt.Printf("      pour y stocker ses données.\n")
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
// Réessaie jusqu'à 10 fois (3 s) si le socket n'est pas encore disponible
// — absorbe la race condition entre `systemctl restart` et la première commande.
func callDaemon(command string, args map[string]string) types.APIResponse {
	var conn net.Conn
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		conn, err = net.Dial("unix", SOCKET_PATH)
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
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
    --param K=V     Paramètre d'installation (répétable ; ou via env CALEOPE_PARAM_K=V)

  configure <app>   Reconfigurer une application (wizard interactif)
                    Exemples : caleope configure arr-stack (VPN)

  top               Supervision live (refresh 2s, Ctrl+C pour quitter)
    --advanced      Mode avancé : disk, port
  stop <app>        Arrêter une application
  start <app>       Démarrer une application arrêtée
  restart <app>     Redémarrer une application

  remove <app>      Désinstaller une application
    --keep-data     Conserver les données
    -y, --yes       Ne pas demander de confirmation (mode non-interactif)

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

  update            Synchroniser les dépôts Git (branche main)
    --alpha         Synchroniser depuis la branche alpha du store
  upgrade           Mettre à jour Caleope vers la dernière version
    --check         Vérifier sans installer
    --alpha         Installer la dernière pré-release alpha
  token             Afficher le token d'accès à l'API REST (:8765)
  version           Afficher la version installée
  ping              Vérifier que le daemon est actif

Tâches planifiées :
  task list                         Lister les tâches planifiées
  task add <id> --type <type> --at HH:MM [--days j1,j2] [--app <app>] [--scope all|config|data]
                                    Créer une tâche (type: backup, upgrade, update)
  task remove <id>                  Supprimer une tâche
  task run <id>                     Exécuter une tâche immédiatement
  task enable/disable <id>          Activer ou désactiver une tâche

Mode submarine (installation hors-ligne) :
  offline-pack <dest>              Créer un bundle dans <dest>/ (binaires + store + images Docker)
  offline-pack <dest> --no-images  Bundle sans images Docker (binaires + store uniquement)
  offline-update <bundle>          Appliquer un bundle sur une installation existante
  Installation offline :           sudo bash install.sh --offline <bundle-path>

Exemples:
  caleope install jellyfin --domain media.home.local
  caleope task add backup-nuit --type backup --at 03:00
  caleope task add backup-configs --type backup --scope config --at 01:00 --days lun,mer,ven
  caleope task add upgrade-auto --type upgrade --at 01:00 --days dim
  caleope task list
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

// ─────────────────────────────────────────────
// LICENCE
// ─────────────────────────────────────────────

func cmdLicense(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  caleope license activate <CALP-XXXX-XXXX-XXXX>")
		fmt.Println("  caleope license status")
		os.Exit(1)
	}

	switch args[0] {
	case "activate":
		if len(args) < 2 {
			die("❌ Usage: caleope license activate <CALP-XXXX-XXXX-XXXX>")
		}
		resp := callDaemon("license.activate", map[string]string{"license_key": args[1]})
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		data, _ := resp.Data.(map[string]interface{})
		edition := ""
		if data != nil {
			edition, _ = data["edition"].(string)
		}
		fmt.Printf("✓ Licence %s activée avec succès\n", strings.ToUpper(edition))

	case "status":
		resp := callDaemon("license.status", nil)
		if !resp.Success {
			die("❌ " + resp.Error)
		}
		data, _ := resp.Data.(map[string]interface{})
		if data == nil {
			die("❌ Réponse invalide")
		}
		activated, _ := data["activated"].(bool)
		if !activated {
			fmt.Println("⚠️  Licence non activée")
			fmt.Println("   Activez avec : caleope license activate <CALP-XXXX-XXXX-XXXX>")
			return
		}
		edition, _ := data["edition"].(string)
		key, _ := data["license_key"].(string)
		fmt.Printf("✓ Licence active\n")
		fmt.Printf("  Édition     : %s\n", strings.ToUpper(edition))
		fmt.Printf("  Clé         : %s\n", key)

	default:
		die("❌ Sous-commande inconnue: caleope license " + args[0])
	}
}
