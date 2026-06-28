package main

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// ── Ports exposés par les conteneurs Docker ───────────────────────────────────

type containerPort struct {
	Container     string `json:"container"`
	HostPort      string `json:"host_port"`
	ContainerPort string `json:"container_port"`
	State         string `json:"state"`
}

func handleSysPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	// docker ps -a --format '{{.Names}}\t{{.Ports}}\t{{.State}}'
	out, err := exec.Command("docker", "ps", "-a",
		"--format", "{{.Names}}\t{{.Ports}}\t{{.State}}").Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ports": []containerPort{}, "success": true})
		return
	}

	var ports []containerPort
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		name, portsStr, state := parts[0], parts[1], parts[2]
		if portsStr == "" {
			ports = append(ports, containerPort{Container: name, HostPort: "—", ContainerPort: "—", State: state})
			continue
		}
		// Format: "0.0.0.0:8080->80/tcp, 0.0.0.0:4430->443/tcp"
		for _, mapping := range strings.Split(portsStr, ", ") {
			mapping = strings.TrimSpace(mapping)
			if mapping == "" {
				continue
			}
			hostPort, containerPort_ := "", ""
			if idx := strings.Index(mapping, "->"); idx >= 0 {
				left := mapping[:idx]
				right := strings.Split(mapping[idx+2:], "/")[0]
				// left may be "0.0.0.0:8080" or ":::8080"
				if colonIdx := strings.LastIndex(left, ":"); colonIdx >= 0 {
					hostPort = left[colonIdx+1:]
				} else {
					hostPort = left
				}
				containerPort_ = right
			} else {
				containerPort_ = strings.Split(mapping, "/")[0]
			}
			ports = append(ports, containerPort{Container: name, HostPort: hostPort, ContainerPort: containerPort_, State: state})
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ports": ports, "success": true})
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

// ── Stats réseau (/proc/net/dev) ──────────────────────────────────────────────

type ifaceStats struct {
	Name    string `json:"name"`
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
	RxPkts  uint64 `json:"rx_pkts"`
	TxPkts  uint64 `json:"tx_pkts"`
}

func handleSysNetstat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ifaces": []ifaceStats{}, "success": false, "error": err.Error()})
		return
	}

	var ifaces []ifaceStats
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// Skip header lines
		if !strings.Contains(line, ":") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		name := strings.TrimSpace(line[:colonIdx])
		if !isPhysicalIface(name) {
			continue
		}
		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 9 {
			continue
		}
		var rxB, txB, rxP, txP uint64
		fmt.Sscan(fields[0], &rxB)
		fmt.Sscan(fields[1], &rxP)
		fmt.Sscan(fields[8], &txB)
		fmt.Sscan(fields[9], &txP)
		ifaces = append(ifaces, ifaceStats{Name: name, RxBytes: rxB, TxBytes: txB, RxPkts: rxP, TxPkts: txP})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ifaces": ifaces, "success": true})
}

// ── Top processus ──────────────────────────────────────────────────────────────

type procInfo struct {
	PID    string `json:"pid"`
	User   string `json:"user"`
	CPU    string `json:"cpu"`
	Mem    string `json:"mem"`
	VSZ    string `json:"vsz"`
	RSS    string `json:"rss"`
	Cmd    string `json:"cmd"`
}

func handleSysProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "cpu"
	}
	limit := 20
	if sortBy != "cpu" && sortBy != "mem" {
		sortBy = "cpu"
	}

	out, err := exec.Command("ps", "aux", "--sort=-"+sortBy, "--no-headers").Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"processes": []procInfo{}, "success": false})
		return
	}

	var procs []procInfo
	sc := bufio.NewScanner(bytes.NewReader(out))
	count := 0
	for sc.Scan() {
		if count >= limit {
			break
		}
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		cmd := strings.Join(fields[10:], " ")
		if len(cmd) > 60 {
			cmd = cmd[:60] + "…"
		}
		procs = append(procs, procInfo{
			PID:  fields[1],
			User: fields[0],
			CPU:  fields[2],
			Mem:  fields[3],
			VSZ:  fields[4],
			RSS:  fields[5],
			Cmd:  cmd,
		})
		count++
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"processes": procs, "success": true})
}

// ── Docker inspect ────────────────────────────────────────────────────────────

func handleDockerInspect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/sys/docker-inspect/")
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid name"})
		return
	}

	out, err := exec.Command("docker", "inspect", name).Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "success": false})
		return
	}

	var result []json.RawMessage
	if err := json.Unmarshal(out, &result); err != nil || len(result) == 0 {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "parse error", "success": false})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"container": result[0], "success": true})
}

// ── Firewall UFW ──────────────────────────────────────────────────────────────

type fwRule struct {
	To     string `json:"to"`
	Action string `json:"action"`
	From   string `json:"from"`
	Proto  string `json:"proto"`
	Note   string `json:"note"`
}

func handleSysFirewall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	out, err := exec.Command("sudo", "ufw", "status", "verbose").Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unavailable", "rules": []fwRule{}, "success": false})
		return
	}

	lines := strings.Split(string(out), "\n")
	status := "unknown"
	var rules []fwRule
	inRules := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Status:") {
			status = strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
		}
		if strings.HasPrefix(line, "To ") && strings.Contains(line, "Action") {
			inRules = true
			continue
		}
		if strings.HasPrefix(line, "--") {
			continue
		}
		if !inRules || line == "" {
			continue
		}
		// Parse rule line: "22/tcp   ALLOW IN   Anywhere  # SSH"
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		rule := fwRule{To: fields[0]}
		// Find action (ALLOW/DENY/LIMIT/REJECT)
		for i := 1; i < len(fields); i++ {
			up := strings.ToUpper(fields[i])
			if up == "ALLOW" || up == "DENY" || up == "LIMIT" || up == "REJECT" {
				action := fields[i]
				direction := ""
				if i+1 < len(fields) {
					d := strings.ToUpper(fields[i+1])
					if d == "IN" || d == "OUT" || d == "FWD" {
						direction = " " + fields[i+1]
						i++
					}
				}
				rule.Action = action + direction
				// Everything after is From + note
				rest := strings.Join(fields[i+1:], " ")
				// Split note
				if idx := strings.Index(rest, "#"); idx >= 0 {
					rule.Note = strings.TrimSpace(rest[idx+1:])
					rule.From = strings.TrimSpace(rest[:idx])
				} else {
					rule.From = strings.TrimSpace(rest)
				}
				break
			}
		}
		if rule.Action != "" {
			rules = append(rules, rule)
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": status, "rules": rules, "success": true})
}

// ── Docker prune ──────────────────────────────────────────────────────────────

func handleDockerPrune(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Pruner les images, volumes et networks non utilisés
	imgOut, _ := exec.Command("docker", "image", "prune", "-f").Output()
	volOut, _ := exec.Command("docker", "volume", "prune", "-f").Output()
	netOut, _ := exec.Command("docker", "network", "prune", "-f").Output()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"images":  string(imgOut),
		"volumes": string(volOut),
		"networks": string(netOut),
		"success": true,
	})
}

type dockerStatEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CPU       string `json:"cpu"`
	Mem       string `json:"mem"`
	MemPerc   string `json:"mem_perc"`
	NetIO     string `json:"net_io"`
	BlockIO   string `json:"block_io"`
	StartedAt string `json:"started_at,omitempty"`
}

func handleDockerStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	out, err := exec.Command("docker", "stats", "--no-stream",
		"--format", `{"id":"{{.ID}}","name":"{{.Name}}","cpu":"{{.CPUPerc}}","mem":"{{.MemUsage}}","mem_perc":"{{.MemPerc}}","net_io":"{{.NetIO}}","block_io":"{{.BlockIO}}"}`).Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "stats": []interface{}{}})
		return
	}
	var stats []dockerStatEntry
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry dockerStatEntry
		if err := json.Unmarshal(line, &entry); err == nil {
			stats = append(stats, entry)
		}
	}
	if stats == nil {
		stats = []dockerStatEntry{}
	}
	// Enrich with start time from docker inspect
	if len(stats) > 0 {
		names := make([]string, len(stats))
		for i, s := range stats {
			names[i] = s.Name
		}
		inspectOut, err := exec.Command("docker", append([]string{"inspect", "--format", `{{.Name}}|{{.State.StartedAt}}`}, names...)...).Output()
		if err == nil {
			inspectSc := bufio.NewScanner(bytes.NewReader(inspectOut))
			startMap := map[string]string{}
			for inspectSc.Scan() {
				parts := strings.SplitN(inspectSc.Text(), "|", 2)
				if len(parts) == 2 {
					startMap[strings.TrimPrefix(parts[0], "/")] = parts[1]
				}
			}
			for i := range stats {
				cleanName := strings.TrimPrefix(stats[i].Name, "/")
				stats[i].StartedAt = startMap[cleanName]
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"stats": stats})
}

type certInfo struct {
	File      string `json:"file"`
	Subject   string `json:"subject"`
	Issuer    string `json:"issuer"`
	NotBefore string `json:"not_before"`
	NotAfter  string `json:"not_after"`
	DaysLeft  int    `json:"days_left"`
	Expired   bool   `json:"expired"`
	SelfSigned bool  `json:"self_signed"`
}

func handleSysCerts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	certDir := "/opt/gaiver-it/caleope/data/traefik/certs"
	entries, err := os.ReadDir(certDir)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "certs": []interface{}{}})
		return
	}
	now := time.Now()
	var certs []certInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".crt") {
			continue
		}
		path := filepath.Join(certDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		block, _ := pem.Decode(data)
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		daysLeft := int(cert.NotAfter.Sub(now).Hours() / 24)
		certs = append(certs, certInfo{
			File:       e.Name(),
			Subject:    cert.Subject.CommonName,
			Issuer:     cert.Issuer.CommonName,
			NotBefore:  cert.NotBefore.Format("2006-01-02"),
			NotAfter:   cert.NotAfter.Format("2006-01-02"),
			DaysLeft:   daysLeft,
			Expired:    now.After(cert.NotAfter),
			SelfSigned: cert.Subject.CommonName == cert.Issuer.CommonName,
		})
	}
	if certs == nil {
		certs = []certInfo{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"certs": certs})
}

func handleNetTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	toolType := r.URL.Query().Get("type")
	host     := r.URL.Query().Get("host")

	// Validate host: no spaces, no shell metacharacters
	for _, ch := range host {
		if ch == ' ' || ch == ';' || ch == '&' || ch == '|' || ch == '$' || ch == '`' || ch == '\'' || ch == '"' || ch == '\\' || ch == '>' || ch == '<' {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "hôte invalide"})
			return
		}
	}
	if host == "" {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "hôte requis"})
		return
	}

	var cmd *exec.Cmd
	switch toolType {
	case "ping":
		cmd = exec.Command("ping", "-c", "4", "-W", "2", host)
	case "dns":
		cmd = exec.Command("dig", "+short", host)
	case "trace":
		cmd = exec.Command("traceroute", "-m", "15", "-w", "2", host)
	default:
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "type invalide"})
		return
	}

	out, _ := cmd.CombinedOutput()
	_ = json.NewEncoder(w).Encode(map[string]string{"output": string(out)})
}

func handleTraefikRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Traefik dashboard API runs on port 8080 by default
	traefikURL := "http://localhost:8080/api/http/routers?per_page=100"
	resp, err := http.Get(traefikURL)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "routers": []interface{}{}})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// Proxy the raw JSON array as-is
	w.Write(body)
}

func handleTraefikServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	traefikURL := "http://localhost:8080/api/http/services?per_page=100"
	resp, err := http.Get(traefikURL)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "services": []interface{}{}})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Write(body)
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

// ── Docker volumes ────────────────────────────────────────────────────────────

type dockerVolumeEntry struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	Scope      string `json:"scope"`
	Labels     string `json:"labels"`
}

func handleDockerVolumes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	out, err := exec.Command("docker", "volume", "ls", "--format",
		"{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}\t{{.Scope}}\t{{.Labels}}").Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"volumes": []interface{}{}, "error": err.Error()})
		return
	}

	var volumes []dockerVolumeEntry
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		v := dockerVolumeEntry{Name: parts[0], Driver: parts[1]}
		if len(parts) > 2 {
			v.Mountpoint = parts[2]
		}
		if len(parts) > 3 {
			v.Scope = parts[3]
		}
		if len(parts) > 4 {
			v.Labels = parts[4]
		}
		volumes = append(volumes, v)
	}
	if volumes == nil {
		volumes = []dockerVolumeEntry{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"volumes": volumes})
}

type appSizeEntry struct {
	AppID   string `json:"app_id"`
	DataDir string `json:"data_dir"`
	SizeStr string `json:"size_str"`
	Bytes   int64  `json:"bytes"`
}

func handleAppSizes(w http.ResponseWriter, r *http.Request, baseDir string) {
	w.Header().Set("Content-Type", "application/json")
	appDataDir := filepath.Join(baseDir, "app-data")
	entries, err := os.ReadDir(appDataDir)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"sizes": []appSizeEntry{}})
		return
	}
	var sizes []appSizeEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(appDataDir, e.Name())
		out, err := exec.Command("du", "-sb", dir).Output()
		if err != nil {
			continue
		}
		parts := strings.Fields(string(out))
		if len(parts) < 1 {
			continue
		}
		var byteCount int64
		fmt.Sscanf(parts[0], "%d", &byteCount)
		sizes = append(sizes, appSizeEntry{
			AppID:   e.Name(),
			DataDir: dir,
			SizeStr: formatBytes(byteCount),
			Bytes:   byteCount,
		})
	}
	if sizes == nil {
		sizes = []appSizeEntry{}
	}
	// Sort by size descending
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].Bytes > sizes[j].Bytes })
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sizes": sizes})
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
