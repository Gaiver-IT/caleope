// internal/ufw/ufw.go
//
// 🔥 Gestion du pare-feu UFW
//
// Ouvre/ferme les ports UFW lors de l'installation et suppression d'apps.
// Si UFW n'est pas actif, toutes les opérations sont silencieusement ignorées.

package ufw

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PortSpec décrit un port à ouvrir ou fermer dans UFW.
type PortSpec struct {
	Name     string
	Host     int
	Protocol string // "tcp", "udp", "any"
	Firewall bool   // true = gérer dans UFW
}

// IsAvailable retourne true si UFW est installé et actif.
func IsAvailable() bool {
	out, err := exec.Command("ufw", "status").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Status: active")
}

// Open ouvre un port dans UFW.
// proto = "tcp", "udp" ou "any" (any = pas de suffixe, UFW autorise les deux)
func Open(port int, proto string) error {
	if !IsAvailable() {
		return nil
	}
	rule := strconv.Itoa(port)
	if proto != "any" && proto != "" {
		rule += "/" + proto
	}
	out, err := exec.Command("ufw", "allow", rule).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw allow %s: %w (%s)", rule, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Close ferme un port dans UFW (supprime la règle allow).
func Close(port int, proto string) error {
	if !IsAvailable() {
		return nil
	}
	rule := strconv.Itoa(port)
	if proto != "any" && proto != "" {
		rule += "/" + proto
	}
	out, err := exec.Command("ufw", "delete", "allow", rule).CombinedOutput()
	if err != nil {
		// Règle inexistante → pas une erreur critique
		if strings.Contains(string(out), "Could not delete non-existent rule") {
			return nil
		}
		return fmt.Errorf("ufw delete allow %s: %w (%s)", rule, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// OpenPorts ouvre tous les ports marqués Firewall:true dans la liste.
// Retourne les erreurs non-fatales sans interrompre le reste.
func OpenPorts(ports []PortSpec) []error {
	var errs []error
	for _, p := range ports {
		if !p.Firewall || p.Host <= 0 {
			continue
		}
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		if err := Open(p.Host, proto); err != nil {
			errs = append(errs, fmt.Errorf("port %s(%d): %w", p.Name, p.Host, err))
		}
	}
	return errs
}

// ClosePorts ferme tous les ports marqués Firewall:true dans la liste.
func ClosePorts(ports []PortSpec) []error {
	var errs []error
	for _, p := range ports {
		if !p.Firewall || p.Host <= 0 {
			continue
		}
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		if err := Close(p.Host, proto); err != nil {
			errs = append(errs, fmt.Errorf("port %s(%d): %w", p.Name, p.Host, err))
		}
	}
	return errs
}
