// internal/api/vms.go
//
// Handlers des machines virtuelles (KVM) — socket (CLI) + HTTP (UI).
// Pro-only pour les opérations d'écriture ; la lecture (capacité + liste)
// est ouverte pour que l'UI puisse afficher l'état.

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gaiver-it/caleope/pkg/types"
)

// vmRequirePro refuse l'opération si la licence n'est pas Pro.
func (s *Server) vmRequirePro() error {
	if strings.EqualFold(s.lic.Status().Edition, "pro") {
		return nil
	}
	return fmt.Errorf("les machines virtuelles nécessitent une licence Pro")
}

// ─── SOCKET (CLI) ───

func (s *Server) handleVMsList() (interface{}, error) {
	cap := s.vm.Capability()
	if !cap.Available {
		return map[string]interface{}{"capability": cap, "vms": []types.VM{}}, nil
	}
	vms, err := s.vm.List()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"capability": cap, "vms": vms}, nil
}

func (s *Server) handleVMCreate(args map[string]string) error {
	if err := s.vmRequirePro(); err != nil {
		return err
	}
	var spec types.VMSpec
	if err := json.Unmarshal([]byte(args["spec"]), &spec); err != nil {
		return fmt.Errorf("champ 'spec' JSON invalide: %w", err)
	}
	return s.vm.Create(spec)
}

func (s *Server) handleVMAction(args map[string]string, action string) error {
	if err := s.vmRequirePro(); err != nil {
		return err
	}
	name := args["name"]
	if name == "" {
		return fmt.Errorf("argument 'name' manquant")
	}
	switch action {
	case "start":
		return s.vm.Start(name)
	case "stop":
		return s.vm.Stop(name)
	case "force-stop":
		return s.vm.ForceStop(name)
	case "delete":
		return s.vm.Delete(name)
	}
	return fmt.Errorf("action VM inconnue: %s", action)
}

// ─── HTTP (UI) ───

// routeVMs : GET /api/v1/vms (capacité + liste) · POST /api/v1/vms (créer)
func (s *Server) routeVMs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := s.handleVMsList()
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
	case http.MethodPost:
		if err := s.vmRequirePro(); err != nil {
			s.httpError(w, err.Error(), http.StatusForbidden)
			return
		}
		var spec types.VMSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			s.httpError(w, "body JSON invalide", http.StatusBadRequest)
			return
		}
		if err := s.vm.Create(spec); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"message": "VM créée", "name": spec.Name})
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeVM : /api/v1/vms/{name}[/action]
//
//	POST   /api/v1/vms/{name}/start|stop|force-stop
//	DELETE /api/v1/vms/{name}
func (s *Server) routeVM(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/vms/")
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if name == "" {
		s.httpError(w, "nom de VM manquant", http.StatusBadRequest)
		return
	}
	if err := s.vmRequirePro(); err != nil {
		s.httpError(w, err.Error(), http.StatusForbidden)
		return
	}
	var err error
	if r.Method == http.MethodDelete {
		err = s.vm.Delete(name)
	} else if r.Method == http.MethodPost {
		switch action {
		case "start":
			err = s.vm.Start(name)
		case "stop":
			err = s.vm.Stop(name)
		case "force-stop":
			err = s.vm.ForceStop(name)
		default:
			s.httpError(w, "action inconnue", http.StatusBadRequest)
			return
		}
	} else {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, nil)
}
