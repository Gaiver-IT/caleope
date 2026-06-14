// internal/ufw/ufw.go
//
// 🔥 UFW MANAGER — gestion dynamique du pare-feu
//
// Ouvre/ferme les ports UFW automatiquement lors de l'installation
// et de la suppression d'apps. Seuls les ports marqués "firewall: true"
// dans app.json sont concernés — les ports internes Traefik (8000-9999)
// ne sont PAS exposés directement.

package ufw

import (
	"fmt"
	"os/exec"
	"strings"
)

// IsAvailable vérifie si UFW est installé et actif sur le système.
func IsAvailable() bool {
	cmd := exec.Command("ufw", "status")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return !strings.Contains(string(out), "inactive")
}

// Open ouvre un port dans UFW.
// proto = "tcp", "udp" ou "any" (pour les deux).
// Idempotent : si la règle existe déjà, UFW l'ignore silencieusement.
func Open(port int, proto string) error {
	if !IsAvailable() {
		return nil // UFW absent ou inactif → pas d'erreur fatale
	}
	rule := fmt.Sprintf("%d/%s", port, proto)
	if proto == "any" {
		rule = fmt.Sprintf("%d", port)
	}
	out, err := exec.Command("ufw", "allow", rule).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw allow %s: %w (%s)", rule, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Close supprime une règle UFW pour un port.
// Idempotent : si la règle n'existe pas, UFW l'ignore silencieusement.
func Close(port int, proto string) error {
	if !IsAvailable() {
		return nil
	}
	rule := fmt.Sprintf("%d/%s", port, proto)
	if proto == "any" {
		rule = fmt.Sprintf("%d", port)
	}
	// "delete allow" supprime la règle d'autorisation
	out, err := exec.Command("ufw", "delete", "allow", rule).CombinedOutput()
	if err != nil {
		// UFW retourne une erreur si la règle n'existe pas — on l'ignore
		if strings.Contains(string(out), "Could not delete non-existent rule") ||
			strings.Contains(string(out), "could not find") {
			return nil
		}
		return fmt.Errorf("ufw delete allow %s: %w (%s)", rule, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// OpenPorts ouvre tous les ports d'une app qui ont Firewall=true.
// Appelé après l'installation réussie.
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
			errs = append(errs, fmt.Errorf("port %d/%s: %w", p.Host, proto, err))
		}
	}
	return errs
}

// ClosePorts ferme tous les ports d'une app qui ont Firewall=true.
// Appelé lors de la suppression.
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
			errs = append(errs, fmt.Errorf("port %d/%s: %w", p.Host, proto, err))
		}
	}
	return errs
}

// PortSpec décrit un port à ouvrir/fermer dans UFW.
// Miroir simplifié de types.AppPort pour éviter la dépendance circulaire.
type PortSpec struct {
	Name     string
	Host     int
	Protocol string // "tcp", "udp", "any"
	Firewall bool   // true = doit être ouvert dans UFW
}
