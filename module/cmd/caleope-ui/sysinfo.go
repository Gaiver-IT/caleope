package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

// ── Services systemd ──────────────────────────────────────────────────────────

type serviceInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Enabled     string `json:"enabled"`
}

// serviceList : services qu'on surveille par défaut + tous les services "actifs non-système"
var coreServices = []string{
	"caleoped", "caleope-ui",
	"docker", "traefik", "portainer",
	"crowdsec",
	"cockpit", "fail2ban", "ufw",
	"ssh", "unattended-upgrades",
}

func handleSysServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		svcs := queryCoreServices()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"services": svcs, "success": true})

	case http.MethodPost:
		// POST /sys/services/{name}/{action}
		rest := strings.TrimPrefix(r.URL.Path, "/sys/services/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "format: /sys/services/{name}/{action}"})
			return
		}
		svc, action := parts[0]+".service", parts[1]
		if action != "start" && action != "stop" && action != "restart" && action != "enable" && action != "disable" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "action invalide"})
			return
		}
		out, err := exec.Command("systemctl", action, svc).CombinedOutput()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": string(out)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": svc, "action": action})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func queryCoreServices() []serviceInfo {
	out, err := exec.Command("systemctl", "show", "--no-pager",
		"-p", "Id,Description,ActiveState,SubState,UnitFileState",
		"--", // sépare les options des noms de services
	).Output()
	// systemctl show ne prend pas de liste inline facilement, on fait par lot
	_ = out
	_ = err

	svcs := make([]serviceInfo, 0, len(coreServices))
	for _, name := range coreServices {
		svc := queryService(name + ".service")
		svcs = append(svcs, svc)
	}
	return svcs
}

func queryService(unit string) serviceInfo {
	out, err := exec.Command("systemctl", "show", "--no-pager",
		"-p", "Id,Description,ActiveState,SubState,UnitFileState",
		unit).Output()

	info := serviceInfo{Name: strings.TrimSuffix(unit, ".service")}
	if err != nil {
		info.Active = "unknown"
		info.Sub = "not-found"
		return info
	}

	for _, line := range strings.Split(string(out), "\n") {
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "Description":
			info.Description = kv[1]
		case "ActiveState":
			info.Active = kv[1]
		case "SubState":
			info.Sub = kv[1]
		case "UnitFileState":
			info.Enabled = kv[1]
		}
	}
	return info
}

// ── Réseau ───────────────────────────────────────────────────────────────────

// Préfixes d'interfaces virtuelles à masquer (Docker, veth, bridges, etc.)
var virtualIfacePrefixes = []string{
	"lo", "docker", "br-", "veth", "cali", "flannel",
	"dummy", "virbr", "ovs-", "tunl", "kube",
}

func isPhysicalIface(name string) bool {
	for _, pfx := range virtualIfacePrefixes {
		if strings.HasPrefix(name, pfx) {
			return false
		}
	}
	return true
}

func handleSysNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	addrOut, _ := exec.Command("ip", "-j", "addr").Output()
	routeOut, _ := exec.Command("ip", "-j", "route").Output()
	dnsOut, _ := exec.Command("resolvectl", "status", "--no-pager").Output()

	// Filtrer pour ne garder que les interfaces physiques
	var allIfaces []map[string]interface{}
	physIfaces := allIfaces // fallback : interface brute
	if json.Unmarshal(addrOut, &allIfaces) == nil {
		physIfaces = make([]map[string]interface{}, 0)
		for _, iface := range allIfaces {
			if name, _ := iface["ifname"].(string); isPhysicalIface(name) {
				physIfaces = append(physIfaces, iface)
			}
		}
	}

	// Filtrer les routes pour ne garder que celles des interfaces physiques
	var allRoutes []map[string]interface{}
	physRoutes := []map[string]interface{}{}
	if json.Unmarshal(routeOut, &allRoutes) == nil {
		for _, route := range allRoutes {
			if dev, _ := route["dev"].(string); isPhysicalIface(dev) {
				physRoutes = append(physRoutes, route)
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"interfaces": physIfaces,
		"routes":     physRoutes,
		"dns_raw":    string(dnsOut),
		"success":    true,
	})
}

// ── Stockage ─────────────────────────────────────────────────────────────────

type diskInfo struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Size   string `json:"size"`
	Used   string `json:"used"`
	Avail  string `json:"avail"`
	UsePct string `json:"use_pct"`
}

// blkDevice représente un périphérique bloc non monté (candidat au montage).
type blkDevice struct {
	Name   string `json:"name"`
	Size   string `json:"size"`
	Fstype string `json:"fstype"`
	Model  string `json:"model"`
	Path   string `json:"path"` // /dev/<name>
}

func handleSysStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	// df — partitions montées (hors tmpfs/devtmpfs/squashfs)
	dfOut, _ := exec.Command("df", "-h", "-x", "tmpfs", "-x", "devtmpfs",
		"-x", "squashfs", "-x", "overlay", "-x", "efivarfs",
		"--output=source,target,size,used,avail,pcent").Output()
	disks := parseDf(dfOut)

	// lsblk — vue des blocs (inclut les partitions non montées)
	lsblkOut, _ := exec.Command("lsblk", "-J",
		"-o", "name,size,type,mountpoint,fstype,model").Output()

	var lsblkResult struct {
		Blockdevices []struct {
			Name       string `json:"name"`
			Size       string `json:"size"`
			Type       string `json:"type"`
			Mountpoint string `json:"mountpoint"`
			Fstype     string `json:"fstype"`
			Model      string `json:"model"`
			Children   []struct {
				Name       string `json:"name"`
				Size       string `json:"size"`
				Type       string `json:"type"`
				Mountpoint string `json:"mountpoint"`
				Fstype     string `json:"fstype"`
			} `json:"children"`
		} `json:"blockdevices"`
	}

	var lsblkRaw interface{}
	_ = json.Unmarshal(lsblkOut, &lsblkRaw)

	// Extraire les périphériques non montés disponibles pour montage
	available := []blkDevice{}
	if json.Unmarshal(lsblkOut, &lsblkResult) == nil {
		for _, dev := range lsblkResult.Blockdevices {
			if dev.Type == "disk" {
				// Regarder les partitions enfants non montées
				for _, child := range dev.Children {
					if child.Mountpoint == "" && child.Mountpoint != "[SWAP]" && child.Fstype != "" {
						available = append(available, blkDevice{
							Name:   child.Name,
							Size:   child.Size,
							Fstype: child.Fstype,
							Model:  dev.Model,
							Path:   "/dev/" + child.Name,
						})
					}
				}
				// Disque entier sans partition et sans point de montage
				if len(dev.Children) == 0 && dev.Mountpoint == "" && dev.Fstype != "" {
					available = append(available, blkDevice{
						Name:   dev.Name,
						Size:   dev.Size,
						Fstype: dev.Fstype,
						Model:  dev.Model,
						Path:   "/dev/" + dev.Name,
					})
				}
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"disks":     disks,
		"lsblk":     lsblkRaw,
		"available": available,
		"success":   true,
	})
}

func parseDf(out []byte) []diskInfo {
	var disks []diskInfo
	sc := bufio.NewScanner(bytes.NewReader(out))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			continue // skip header
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		disks = append(disks, diskInfo{
			Source: fields[0], Target: fields[1],
			Size: fields[2], Used: fields[3],
			Avail: fields[4], UsePct: fields[5],
		})
	}
	return disks
}

// ── Journal système ───────────────────────────────────────────────────────────

type journalEntry struct {
	Time     string `json:"time"`
	Unit     string `json:"unit"`
	Priority string `json:"priority"`
	Message  string `json:"message"`
}

func handleSysJournal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	unit  := r.URL.Query().Get("unit")
	limit := r.URL.Query().Get("n")
	if limit == "" {
		limit = "200"
	}

	args := []string{"-n", limit, "--no-pager", "-o", "json"}
	if unit != "" && unit != "all" {
		args = append(args, "-u", unit)
	}

	out, _ := exec.Command("journalctl", args...).Output()
	entries := parseJournalJSON(out)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
		"success": true,
	})
}

// priorityToStr convertit les priorités syslog numériques en labels lisibles
func priorityToStr(p string) string {
	switch p {
	case "0":
		return "emerg"
	case "1":
		return "alert"
	case "2":
		return "crit"
	case "3":
		return "err"
	case "4":
		return "warning"
	case "5":
		return "notice"
	case "6":
		return "info"
	case "7":
		return "debug"
	default:
		return p
	}
}

func parseJournalJSON(raw []byte) []journalEntry {
	var entries []journalEntry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		entry := journalEntry{}
		// Timestamp : __REALTIME_TIMESTAMP en microsecondes
		if ts, ok := m["__REALTIME_TIMESTAMP"]; ok {
			var usec string
			_ = json.Unmarshal(ts, &usec)
			if len(usec) > 6 {
				entry.Time = fmt.Sprintf("%s.%s", usec[:len(usec)-6], usec[len(usec)-6:len(usec)-3])
			}
		}
		if v, ok := m["_SYSTEMD_UNIT"]; ok {
			var s string
			_ = json.Unmarshal(v, &s)
			entry.Unit = strings.TrimSuffix(s, ".service")
		} else if v, ok := m["SYSLOG_IDENTIFIER"]; ok {
			var s string
			_ = json.Unmarshal(v, &s)
			entry.Unit = s
		}
		if v, ok := m["PRIORITY"]; ok {
			var s string
			_ = json.Unmarshal(v, &s)
			entry.Priority = priorityToStr(s)
		}
		if v, ok := m["MESSAGE"]; ok {
			// MESSAGE peut être une string ou un tableau de bytes
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				entry.Message = s
			}
		}
		entries = append(entries, entry)
	}
	return entries
}
