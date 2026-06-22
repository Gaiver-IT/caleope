package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// Le CLI tâches passe par le socket UNIX (callDaemon), pas par HTTP directement.
// Les commandes socket correspondantes sont gérées dans internal/api/api.go :
//   task-list, task-add, task-remove, task-run, task-toggle

// cmdTask gère les sous-commandes :
//
//	caleope task list
//	caleope task add <id> --type backup [--app jellyfin] [--scope all|config|data] --at HH:MM [--days lun,mer,ven]
//	caleope task add <id> --type upgrade --at 01:00
//	caleope task remove <id>
//	caleope task run <id>
//	caleope task enable <id>
//	caleope task disable <id>
func cmdTask(args []string) {
	if len(args) == 0 {
		printTaskHelp()
		return
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		taskList()
	case "add":
		taskAdd(rest)
	case "remove", "rm", "delete":
		if len(rest) == 0 {
			die("Usage : caleope task remove <id>")
		}
		taskRemove(rest[0])
	case "run":
		if len(rest) == 0 {
			die("Usage : caleope task run <id>")
		}
		taskRun(rest[0])
	case "enable":
		if len(rest) == 0 {
			die("Usage : caleope task enable <id>")
		}
		taskToggle(rest[0], true)
	case "disable":
		if len(rest) == 0 {
			die("Usage : caleope task disable <id>")
		}
		taskToggle(rest[0], false)
	default:
		fmt.Fprintf(os.Stderr, "❌ Sous-commande inconnue : %s\n\n", sub)
		printTaskHelp()
		os.Exit(1)
	}
}

// ── sous-commandes ────────────────────────────────────────────────────────────

func taskList() {
	resp := callDaemon("task-list", map[string]string{})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var tasks []types.Task
	if err := json.Unmarshal(raw, &tasks); err != nil || len(tasks) == 0 {
		fmt.Println("Aucune tâche planifiée.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTYPE\tAPP\tSCOPE\tHORAIRE\tÉTAT\tDERNIÈRE EXÉC\tSTATUT")
	for _, t := range tasks {
		state := "✓ actif"
		if !t.Enabled {
			state = "⏸ pausé"
		}

		app := t.App
		if app == "" && t.Type == types.TaskTypeBackup {
			app = "(toutes)"
		}

		scope := string(t.Scope)
		if scope == "" {
			scope = "-"
		}

		schedule := formatSchedule(t.Schedule)

		lastRun := "-"
		if !t.LastRun.IsZero() {
			lastRun = t.LastRun.Format("2006-01-02 15:04")
		}

		lastStatus := t.LastStatus
		if lastStatus == "" {
			lastStatus = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Type, app, scope, schedule, state, lastRun, lastStatus)
	}
	w.Flush()
}

func taskAdd(args []string) {
	if len(args) == 0 {
		die("Usage : caleope task add <id> --type <backup|upgrade|update> --at HH:MM [options]")
	}

	id := args[0]
	t := types.Task{ID: id, Enabled: true}

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--type":
			i++
			if i >= len(args) {
				die("--type requiert une valeur : backup, upgrade, update")
			}
			t.Type = types.TaskType(args[i])
		case "--app":
			i++
			if i < len(args) {
				t.App = args[i]
			}
		case "--scope":
			i++
			if i < len(args) {
				t.Scope = types.BackupScope(args[i])
			}
		case "--at":
			i++
			if i >= len(args) {
				die("--at requiert HH:MM")
			}
			h, m, err := parseTime(args[i])
			if err != nil {
				die("Format d'heure invalide : " + args[i] + " (attendu HH:MM)")
			}
			t.Schedule.Hour = h
			t.Schedule.Minute = m
		case "--days":
			i++
			if i >= len(args) {
				die("--days requiert une liste : lun,mer,ven ou 1,3,5")
			}
			days, err := parseDays(args[i])
			if err != nil {
				die(err.Error())
			}
			t.Schedule.Days = days
		}
	}

	if t.Type == "" {
		die("--type requis (backup, upgrade, update)")
	}
	if t.Schedule.Hour == 0 && t.Schedule.Minute == 0 {
		// 0:00 est valide mais inhabituel — on avertit seulement si --at n'a pas été passé
		// On ne peut pas distinguer "non fourni" de "00:00" sans flag booléen.
		// Par simplicité, on accepte 00:00 comme heure valide.
	}

	t.CreatedAt = time.Now()

	taskJSON, _ := json.Marshal(t)
	resp := callDaemon("task-add", map[string]string{"task": string(taskJSON)})
	if !resp.Success {
		die("❌ " + resp.Error)
	}

	fmt.Printf("✅ Tâche '%s' créée — %s tous les %s à %02d:%02d\n",
		id, t.Type, formatDays(t.Schedule.Days), t.Schedule.Hour, t.Schedule.Minute)
}

func taskRemove(id string) {
	resp := callDaemon("task-remove", map[string]string{"id": id})
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	fmt.Printf("✅ Tâche '%s' supprimée\n", id)
}

func taskRun(id string) {
	fmt.Printf("▶ Exécution immédiate de '%s'...\n", id)
	resp := callDaemon("task-run", map[string]string{"id": id})
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	fmt.Printf("✅ Tâche '%s' exécutée\n", id)
}

func taskToggle(id string, enabled bool) {
	val := "false"
	if enabled {
		val = "true"
	}
	resp := callDaemon("task-toggle", map[string]string{"id": id, "enabled": val})
	if !resp.Success {
		die("❌ " + resp.Error)
	}
	state := "activée"
	if !enabled {
		state = "désactivée"
	}
	fmt.Printf("✅ Tâche '%s' %s\n", id, state)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func parseTime(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("format invalide")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("heure invalide")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("minutes invalides")
	}
	return h, m, nil
}

var dayNames = map[string]int{
	"dim": 0, "sun": 0, "0": 0,
	"lun": 1, "mon": 1, "1": 1,
	"mar": 2, "tue": 2, "2": 2,
	"mer": 3, "wed": 3, "3": 3,
	"jeu": 4, "thu": 4, "4": 4,
	"ven": 5, "fri": 5, "5": 5,
	"sam": 6, "sat": 6, "6": 6,
}

var dayLabels = []string{"dim", "lun", "mar", "mer", "jeu", "ven", "sam"}

func parseDays(s string) ([]int, error) {
	var days []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		d, ok := dayNames[part]
		if !ok {
			return nil, fmt.Errorf("jour invalide : '%s' (attendu : lun,mar,mer,jeu,ven,sam,dim ou 0-6)", part)
		}
		days = append(days, d)
	}
	return days, nil
}

func formatSchedule(s types.Schedule) string {
	days := formatDays(s.Days)
	return fmt.Sprintf("%s à %02d:%02d", days, s.Hour, s.Minute)
}

func formatDays(days []int) string {
	if len(days) == 0 {
		return "tous les jours"
	}
	labels := make([]string, len(days))
	for i, d := range days {
		if d >= 0 && d <= 6 {
			labels[i] = dayLabels[d]
		}
	}
	return strings.Join(labels, ",")
}

func printTaskHelp() {
	fmt.Print(`Gestion des tâches planifiées

Usage:
  caleope task list
  caleope task add <id> --type <type> --at HH:MM [--days <jours>] [options]
  caleope task remove <id>
  caleope task run <id>
  caleope task enable <id>
  caleope task disable <id>

Types de tâches:
  backup    Sauvegarder une ou toutes les applications
  upgrade   Mettre à jour Caleope
  update    Synchroniser le catalogue d'apps

Options pour --type backup:
  --app <id>              Application à sauvegarder (vide = toutes)
  --scope all|config|data Contenu à sauvegarder (défaut: all)

Planification:
  --at HH:MM              Heure d'exécution (ex: 03:00)
  --days <jours>          Jours séparés par des virgules (défaut: tous les jours)
                          Valeurs : lun,mar,mer,jeu,ven,sam,dim ou 0-6

Exemples:
  caleope task add backup-nuit --type backup --at 03:00
  caleope task add backup-jellyfin --type backup --app jellyfin --scope data --at 02:00 --days lun,mer,ven
  caleope task add backup-configs --type backup --scope config --at 01:00
  caleope task add upgrade-auto --type upgrade --at 01:00 --days dim
  caleope task add sync-store --type update --at 06:00
  caleope task run backup-nuit
  caleope task disable upgrade-auto
`)
}
