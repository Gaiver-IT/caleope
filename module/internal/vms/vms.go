// internal/vms/vms.go
//
// 🖥️ MACHINES VIRTUELLES — KVM / libvirt (à la Unraid, Pro-only)
//
// Gère des VMs via libvirt (virsh / virt-install). Périmètre assumé « VM
// d'appoint » : hôte unique, pas de cluster / HA / migration à chaud.
//
// Garde-fou de capacité : /dev/kvm + vmx/svm + pas en LXC. Si Caleope tourne
// lui-même dans une VM, la virtualisation imbriquée doit être activée sur
// l'hyperviseur. Sans libvirt installé → état « à configurer ».
//
// Disques VM (qcow2) : <baseDir>/vms/<name>.qcow2

package vms

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gaiver-it/caleope/pkg/types"
)

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

type Manager struct{ baseDir string }

func NewManager(baseDir string) *Manager { return &Manager{baseDir: baseDir} }

func (m *Manager) vmsDir() string { return filepath.Join(m.baseDir, "vms") }

// Capability détecte si l'hôte peut faire tourner des VMs KVM.
func (m *Manager) Capability() types.VMCapability {
	if data, _ := os.ReadFile("/proc/1/environ"); strings.Contains(string(data), "container=lxc") {
		return types.VMCapability{Reason: "Caleope tourne en conteneur LXC — KVM n'y est pas disponible."}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return types.VMCapability{Reason: "Virtualisation matérielle absente (/dev/kvm). Si Caleope tourne dans une VM, active la virtualisation imbriquée sur l'hyperviseur."}
	}
	if data, _ := os.ReadFile("/proc/cpuinfo"); !strings.Contains(string(data), "vmx") && !strings.Contains(string(data), "svm") {
		return types.VMCapability{Reason: "Le CPU n'expose pas la virtualisation matérielle (vmx/svm)."}
	}
	if _, err := exec.LookPath("virsh"); err != nil {
		return types.VMCapability{NeedsSetup: true, Reason: "libvirt non installé — installe qemu-kvm et libvirt-daemon-system pour activer les VMs."}
	}
	return types.VMCapability{Available: true}
}

func (m *Manager) requireReady() error {
	c := m.Capability()
	if c.Available {
		return nil
	}
	if c.Reason != "" {
		return fmt.Errorf("%s", c.Reason)
	}
	return fmt.Errorf("VMs indisponibles sur cet hôte")
}

// List retourne toutes les VMs définies.
func (m *Manager) List() ([]types.VM, error) {
	if err := m.requireReady(); err != nil {
		return nil, err
	}
	out, err := exec.Command("virsh", "list", "--all", "--name").Output()
	if err != nil {
		return nil, fmt.Errorf("virsh list: %w", err)
	}
	vms := []types.VM{}
	for _, name := range strings.Fields(string(out)) {
		vm := types.VM{Name: name, State: m.state(name)}
		m.fillInfo(&vm)
		vms = append(vms, vm)
	}
	return vms, nil
}

func (m *Manager) state(name string) types.VMState {
	out, err := exec.Command("virsh", "domstate", name).Output()
	if err != nil {
		return types.VMStopped
	}
	s := strings.TrimSpace(string(out))
	switch {
	case strings.Contains(s, "running"):
		return types.VMRunning
	case strings.Contains(s, "paused"):
		return types.VMPaused
	default:
		return types.VMStopped
	}
}

func (m *Manager) fillInfo(vm *types.VM) {
	if out, err := exec.Command("virsh", "dominfo", vm.Name).Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch k {
			case "CPU(s)":
				vm.VCPUs, _ = strconv.Atoi(v)
			case "Max memory":
				if f := strings.Fields(v); len(f) > 0 {
					if kib, err := strconv.Atoi(f[0]); err == nil {
						vm.MemMB = kib / 1024
					}
				}
			}
		}
	}
	if out, err := exec.Command("virsh", "vncdisplay", vm.Name).Output(); err == nil {
		d := strings.TrimSpace(string(out))
		if strings.HasPrefix(d, ":") {
			if n, err := strconv.Atoi(strings.TrimPrefix(d, ":")); err == nil {
				vm.VNCPort = 5900 + n
			}
		}
	}
}

// Create crée et démarre une VM via virt-install (boot sur l'ISO fournie).
func (m *Manager) Create(spec types.VMSpec) error {
	if err := m.requireReady(); err != nil {
		return err
	}
	if !nameRe.MatchString(spec.Name) {
		return fmt.Errorf("nom de VM invalide (a-z, 0-9, - et _)")
	}
	if strings.TrimSpace(spec.ISO) == "" {
		return fmt.Errorf("une image ISO est requise pour installer la VM")
	}
	if spec.VCPUs <= 0 {
		spec.VCPUs = 2
	}
	if spec.MemMB <= 0 {
		spec.MemMB = 2048
	}
	if spec.DiskGB <= 0 {
		spec.DiskGB = 20
	}
	if err := os.MkdirAll(m.vmsDir(), 0o755); err != nil {
		return err
	}
	disk := filepath.Join(m.vmsDir(), spec.Name+".qcow2")
	if _, err := os.Stat(disk); err == nil {
		return fmt.Errorf("un disque pour '%s' existe déjà", spec.Name)
	}
	// Réseau : NAT par défaut (marche partout) ; bridge = avancé (nécessite un pont configuré).
	netArg := "network=default"
	if spec.Network == "bridge" {
		netArg = "bridge=br0"
	}
	args := []string{
		"--name", spec.Name,
		"--vcpus", strconv.Itoa(spec.VCPUs),
		"--memory", strconv.Itoa(spec.MemMB),
		"--disk", fmt.Sprintf("path=%s,size=%d,format=qcow2", disk, spec.DiskGB),
		"--cdrom", spec.ISO,
		"--osinfo", "detect=on,require=off",
		"--graphics", "vnc,listen=127.0.0.1",
		"--network", netArg,
		"--noautoconsole",
	}
	if out, err := exec.Command("virt-install", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("virt-install: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Start / Stop (arrêt propre) / ForceStop / Delete
func (m *Manager) Start(name string) error     { return m.virshDo("start", name) }
func (m *Manager) Stop(name string) error      { return m.virshDo("shutdown", name) }
func (m *Manager) ForceStop(name string) error { return m.virshDo("destroy", name) }

func (m *Manager) Delete(name string) error {
	if err := m.requireReady(); err != nil {
		return err
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("nom de VM invalide")
	}
	_ = exec.Command("virsh", "destroy", name).Run() // ignore si déjà arrêtée
	if out, err := exec.Command("virsh", "undefine", name, "--remove-all-storage").CombinedOutput(); err != nil {
		return fmt.Errorf("virsh undefine: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) virshDo(action, name string) error {
	if err := m.requireReady(); err != nil {
		return err
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("nom de VM invalide")
	}
	if out, err := exec.Command("virsh", action, name).CombinedOutput(); err != nil {
		return fmt.Errorf("virsh %s: %s", action, strings.TrimSpace(string(out)))
	}
	return nil
}
