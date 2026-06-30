// internal/metrics/metrics.go
//
// 📊 COLLECTE DE MÉTRIQUES
//
// Collecte les métriques des apps Caleope et du système hôte.
// Sources : docker stats (CPU/RAM par container), /proc/meminfo, df.
//
// STRATÉGIE DE GROUPEMENT :
// docker compose crée des containers nommés <app>, <app>-db, <app>-redis...
// On somme les métriques de tous les containers dont le nom commence par l'appID.

package metrics

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/internal/runtime"
	"github.com/gaiver-it/caleope/pkg/types"
)

// Collector collecte les métriques Caleope.
type Collector struct {
	rt      *runtime.Manager
	baseDir string
}

func NewCollector(rt *runtime.Manager, baseDir string) *Collector {
	return &Collector{rt: rt, baseDir: baseDir}
}

// ─────────────────────────────────────────────
// COLLECTE PRINCIPALE
// ─────────────────────────────────────────────

// Collect retourne un snapshot complet des métriques.
// withDisk=true calcule l'usage disque par app (plus lent).
func (c *Collector) Collect(withDisk bool) (*types.StatsSnapshot, error) {
	snap := &types.StatsSnapshot{Timestamp: time.Now()}

	// Métriques système
	snap.MemUsedMB, snap.MemTotalMB = systemMemory()
	snap.DiskUsedGB, snap.DiskTotalGB = systemDisk(c.baseDir)

	// Apps installées
	apps, err := c.rt.ListApps()
	if err != nil {
		return snap, nil
	}

	// Snapshot docker stats (une seule invocation pour toutes les apps)
	dockerStats := collectDockerStats()

	for _, app := range apps {
		stat := types.AppStats{
			AppID:  app.ID,
			Name:   app.Name,
			Status: string(app.Status),
			DiskMB: -1,
		}

		// Port principal
		if len(app.Ports) > 0 {
			stat.Port = app.Ports[0].Host
		}

		// Sommer CPU + RAM de tous les containers de cette app
		for name, ds := range dockerStats {
			if name == app.ID || strings.HasPrefix(name, app.ID+"-") {
				stat.CPUPercent += ds.cpu
				stat.MemoryMB += ds.memMB
			}
		}

		// Disk par app (seulement en mode avancé)
		if withDisk {
			stat.DiskMB = appDiskMB(c.baseDir, app.ID)
		}

		snap.Apps = append(snap.Apps, stat)
	}

	return snap, nil
}

// ─────────────────────────────────────────────
// DOCKER STATS
// ─────────────────────────────────────────────

type dockerStat struct {
	cpu   float64
	memMB float64
}

// collectDockerStats invoque docker stats --no-stream et retourne une map nom→stats.
func collectDockerStats() map[string]dockerStat {
	result := make(map[string]dockerStat)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		return result
	}

	// docker stats retourne 1 JSON par ligne
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var row struct {
			Name     string `json:"Name"`
			CPUPerc  string `json:"CPUPerc"`
			MemUsage string `json:"MemUsage"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}

		result[row.Name] = dockerStat{
			cpu:   parsePercent(row.CPUPerc),
			memMB: parseMemMB(row.MemUsage),
		}
	}

	return result
}

// parsePercent convertit "0.50%" → 0.5
func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// parseMemMB convertit "256MiB / 8GiB" → 256.0 (partie gauche en MB)
func parseMemMB(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 0 {
		return 0
	}
	return toMB(strings.TrimSpace(parts[0]))
}

// toMB convertit "256MiB", "1.5GiB", "512kB"... en MB float64
func toMB(s string) float64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		factor float64
	}{
		{"GiB", 1024},
		{"MiB", 1},
		{"KiB", 1.0 / 1024},
		{"GB", 1000},
		{"MB", 1},
		{"kB", 0.001},
		{"B", 1.0 / (1024 * 1024)},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			v, _ := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			return v * u.factor
		}
	}
	return 0
}

// ─────────────────────────────────────────────
// MÉTRIQUES SYSTÈME
// ─────────────────────────────────────────────

// systemMemory lit /proc/meminfo et retourne (usedMB, totalMB).
func systemMemory() (used, total float64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}

	vals := make(map[string]float64)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		v, _ := strconv.ParseFloat(parts[1], 64)
		vals[key] = v / 1024 // kB → MB
	}

	total = vals["MemTotal"]
	available := vals["MemAvailable"]
	used = total - available
	return used, total
}

// systemDisk retourne (usedGB, totalGB) du système de fichiers contenant baseDir.
func systemDisk(baseDir string) (used, total float64) {
	cmd := exec.Command("df", "-B1", baseDir)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return 0, 0
	}

	totalB, _ := strconv.ParseFloat(fields[1], 64)
	usedB, _ := strconv.ParseFloat(fields[2], 64)
	return usedB / 1e9, totalB / 1e9
}

// appDiskMB retourne l'usage disque en MB de app-data/<appID>.
func appDiskMB(baseDir, appID string) int64 {
	dir := fmt.Sprintf("%s/app-data/%s", baseDir, appID)
	cmd := exec.Command("du", "-sm", dir)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(fields[0], 10, 64)
	return v
}

// ─────────────────────────────────────────────
// FORMAT PROMETHEUS
// ─────────────────────────────────────────────

// PrometheusText génère le texte de métriques au format Prometheus 0.0.4.
func PrometheusText(snap *types.StatsSnapshot) string {
	var sb strings.Builder

	w := func(format string, args ...interface{}) {
		fmt.Fprintf(&sb, format+"\n", args...)
	}

	w("# HELP caleope_app_running App running status (1=running, 0=other)")
	w("# TYPE caleope_app_running gauge")
	for _, a := range snap.Apps {
		running := 0.0
		if a.Status == "running" {
			running = 1.0
		}
		w(`caleope_app_running{app="%s"} %g`, a.AppID, running)
	}

	w("# HELP caleope_app_cpu_percent App CPU usage percent")
	w("# TYPE caleope_app_cpu_percent gauge")
	for _, a := range snap.Apps {
		w(`caleope_app_cpu_percent{app="%s"} %g`, a.AppID, a.CPUPercent)
	}

	w("# HELP caleope_app_memory_megabytes App memory usage in MB")
	w("# TYPE caleope_app_memory_megabytes gauge")
	for _, a := range snap.Apps {
		w(`caleope_app_memory_megabytes{app="%s"} %g`, a.AppID, a.MemoryMB)
	}

	w("# HELP caleope_system_memory_used_megabytes System RAM used in MB")
	w("# TYPE caleope_system_memory_used_megabytes gauge")
	w("caleope_system_memory_used_megabytes %g", snap.MemUsedMB)

	w("# HELP caleope_system_memory_total_megabytes System RAM total in MB")
	w("# TYPE caleope_system_memory_total_megabytes gauge")
	w("caleope_system_memory_total_megabytes %g", snap.MemTotalMB)

	w("# HELP caleope_system_disk_used_gigabytes Caleope disk used in GB")
	w("# TYPE caleope_system_disk_used_gigabytes gauge")
	w("caleope_system_disk_used_gigabytes %g", snap.DiskUsedGB)

	w("# HELP caleope_system_disk_total_gigabytes Caleope disk total in GB")
	w("# TYPE caleope_system_disk_total_gigabytes gauge")
	w("caleope_system_disk_total_gigabytes %g", snap.DiskTotalGB)

	return sb.String()
}
