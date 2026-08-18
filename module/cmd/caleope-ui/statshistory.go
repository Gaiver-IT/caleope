// cmd/caleope-ui/statshistory.go
//
// Historique système (anneau circulaire) + health checks HTTP par app.
//
// Endpoints exposés :
//   GET /sys/stats/history   → tableau de {ts, cpu, ram, disk} sur les 60 dernières minutes
//   GET /sys/healthcheck     → état de santé Docker (status, health) par conteneur

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── Ring buffer ───────────────────────────────────────────────────────────────

// StatsSample est un point d'historique échantillonné toutes les 60 secondes.
type StatsSample struct {
	TS   int64   `json:"ts"`   // Unix timestamp
	CPU  float64 `json:"cpu"`  // CPU %
	RAM  float64 `json:"ram"`  // RAM %
	Disk float64 `json:"disk"` // Disk %
}

const histSize = 60 // 60 × 60s = 1 heure

type statsRing struct {
	mu  sync.RWMutex
	buf [histSize]StatsSample
	pos int
	cnt int
}

var statsHist = &statsRing{}

func (r *statsRing) push(s StatsSample) {
	r.mu.Lock()
	r.buf[r.pos] = s
	r.pos = (r.pos + 1) % histSize
	if r.cnt < histSize {
		r.cnt++
	}
	r.mu.Unlock()
}

func (r *statsRing) slice() []StatsSample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cnt == 0 {
		return nil
	}
	out := make([]StatsSample, r.cnt)
	start := (r.pos - r.cnt + histSize) % histSize
	for i := 0; i < r.cnt; i++ {
		out[i] = r.buf[(start+i)%histSize]
	}
	return out
}

// ── Sampler goroutine ─────────────────────────────────────────────────────────

// startStatsHistory lance la goroutine de collecte d'historique.
// Elle appelle le daemon Caleope (déjà authentifié) toutes les 60 secondes.
func startStatsHistory(daemonURL, token string) {
	go func() {
		// Premier échantillon immédiat (le daemon peut prendre quelques secondes à démarrer)
		time.Sleep(5 * time.Second)
		sampleDaemonStats(daemonURL, token)
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sampleDaemonStats(daemonURL, token)
		}
	}()
}

func sampleDaemonStats(daemonURL, token string) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, daemonURL+"/api/v1/stats", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var res struct {
		Data struct {
			CPUPct      float64 `json:"cpu_pct"`
			MemUsedMB   float64 `json:"mem_used_mb"`
			MemTotalMB  float64 `json:"mem_total_mb"`
			DiskUsedGB  float64 `json:"disk_used_gb"`
			DiskTotalGB float64 `json:"disk_total_gb"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return
	}
	d := res.Data
	ram, disk := 0.0, 0.0
	if d.MemTotalMB > 0 {
		ram = d.MemUsedMB / d.MemTotalMB * 100
	}
	if d.DiskTotalGB > 0 {
		disk = d.DiskUsedGB / d.DiskTotalGB * 100
	}
	statsHist.push(StatsSample{
		TS:   time.Now().Unix(),
		CPU:  d.CPUPct,
		RAM:  ram,
		Disk: disk,
	})
}

// ── Handler /sys/stats/history ───────────────────────────────────────────────

func handleStatsHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	samples := statsHist.slice()
	if samples == nil {
		samples = []StatsSample{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"samples":  samples,
		"interval": 60, // secondes entre chaque point
		"success":  true,
	})
}

// ── Handler /sys/healthcheck ──────────────────────────────────────────────────

type containerHealth struct {
	Name   string `json:"name"`
	Status string `json:"status"` // running, exited, etc.
	Health string `json:"health"` // healthy, unhealthy, starting, none
}

// healthCache évite de spammer docker ps/inspect trop souvent.
var healthCache struct {
	mu        sync.RWMutex
	data      []containerHealth
	fetchedAt time.Time
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	healthCache.mu.RLock()
	cached := healthCache.data
	age := time.Since(healthCache.fetchedAt)
	healthCache.mu.RUnlock()

	if cached != nil && age < 30*time.Second {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"containers": cached, "success": true})
		return
	}

	// docker ps avec toutes les colonnes utiles
	out, err := exec.Command("docker", "ps", "-a",
		"--format", `{{.Names}}|{{.Status}}|{{.RunningFor}}`).Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"containers": []containerHealth{}, "success": false, "error": err.Error()})
		return
	}

	// docker inspect pour le health status
	healthMap := map[string]string{}
	inspOut, err := exec.Command("docker", "ps", "-q").Output()
	if err == nil {
		ids := strings.Fields(string(inspOut))
		if len(ids) > 0 {
			args := append([]string{"inspect", "--format", `{{.Name}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}`}, ids...)
			inspData, err2 := exec.Command("docker", args...).Output()
			if err2 == nil {
				sc := bufio.NewScanner(bytes.NewReader(inspData))
				for sc.Scan() {
					parts := strings.SplitN(sc.Text(), "|", 2)
					if len(parts) == 2 {
						cleanName := strings.TrimPrefix(parts[0], "/")
						healthMap[cleanName] = parts[1]
					}
				}
			}
		}
	}

	var containers []containerHealth
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "|", 3)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		statusRaw := parts[1]
		status := "stopped"
		if strings.HasPrefix(strings.ToLower(statusRaw), "up") {
			status = "running"
		} else if strings.Contains(strings.ToLower(statusRaw), "exited") {
			status = "exited"
		}
		h := healthMap[name]
		if h == "" {
			h = "none"
		}
		containers = append(containers, containerHealth{Name: name, Status: status, Health: h})
	}
	if containers == nil {
		containers = []containerHealth{}
	}

	healthCache.mu.Lock()
	healthCache.data = containers
	healthCache.fetchedAt = time.Now()
	healthCache.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"containers": containers, "success": true})
}

// ── Update checker : comparer tag local vs registry ──────────────────────────

type updateInfo struct {
	Container  string `json:"container"`
	LocalImage string `json:"local_image"`
	LocalTag   string `json:"local_tag"`
	NeedsCheck bool   `json:"needs_check"` // toujours true — le vrai check est côté frontend via Docker Hub API
}

// handleUpdateCheck retourne les images locales avec leurs tags (le frontend fera le check Docker Hub).
func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	out, err := exec.Command("docker", "ps", "--format", `{{.Names}}|{{.Image}}`).Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"containers": []updateInfo{}, "success": false})
		return
	}
	var infos []updateInfo
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "|", 2)
		if len(parts) != 2 {
			continue
		}
		imgFull := parts[1]
		tag := "latest"
		img := imgFull
		if idx := strings.LastIndex(imgFull, ":"); idx > 0 && !strings.Contains(imgFull[idx:], "/") {
			img = imgFull[:idx]
			tag = imgFull[idx+1:]
		}
		infos = append(infos, updateInfo{Container: parts[0], LocalImage: img, LocalTag: tag, NeedsCheck: true})
	}
	if infos == nil {
		infos = []updateInfo{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"containers": infos, "success": true})
}
