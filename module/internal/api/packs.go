// internal/api/packs.go
//
// Packs — bundles d'apps par usage. Couche mince AU-DESSUS du catalogue :
// un pack ne fait qu'orchestrer l'installation d'apps déjà présentes dans le
// store, dans l'ordre déclaré. Le préflight indique ce qui est déjà installé,
// ce qu'il reste à installer, et ce qui manque au catalogue.

package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gaiver-it/caleope/pkg/types"
)

// installedSet retourne l'ensemble des ID d'apps installées.
func (s *Server) installedSet() (map[string]bool, error) {
	apps, err := s.rt.ListApps()
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(apps))
	for _, a := range apps {
		set[a.ID] = true
	}
	return set, nil
}

// buildPackStatus calcule le préflight d'un pack vis-à-vis de l'état courant.
func (s *Server) buildPackStatus(p types.Pack, installed map[string]bool) types.PackStatus {
	st := types.PackStatus{Pack: p, Apps: []types.PackAppState{}, ToInstall: []string{}, Missing: []string{}}
	for _, appID := range p.Apps {
		m := s.peekManifest(appID)
		as := types.PackAppState{ID: appID, Name: appID, InCatalog: m != nil, Installed: installed[appID]}
		if m != nil {
			as.Name = m.Name
		}
		st.Apps = append(st.Apps, as)
		switch {
		case as.Installed:
			st.Installed++
		case !as.InCatalog:
			st.Missing = append(st.Missing, appID)
		default:
			st.ToInstall = append(st.ToInstall, appID)
		}
	}
	st.Complete = len(st.ToInstall) == 0 && len(st.Missing) == 0
	return st
}

// ─── SOCKET (CLI) ───

func (s *Server) handlePacksList() (interface{}, error) {
	repos, err := s.rt.GetRepos()
	if err != nil {
		return nil, err
	}
	packs, err := s.st.ListPacks(repos)
	if err != nil {
		return nil, err
	}
	installed, err := s.installedSet()
	if err != nil {
		return nil, err
	}
	out := []types.PackStatus{}
	for _, p := range packs {
		out = append(out, s.buildPackStatus(p, installed))
	}
	return out, nil
}

func (s *Server) handlePackStatus(id string) (types.PackStatus, error) {
	repos, err := s.rt.GetRepos()
	if err != nil {
		return types.PackStatus{}, err
	}
	p, err := s.st.GetPack(id, repos)
	if err != nil {
		return types.PackStatus{}, err
	}
	installed, err := s.installedSet()
	if err != nil {
		return types.PackStatus{}, err
	}
	return s.buildPackStatus(*p, installed), nil
}

type packAppResult struct {
	App     string `json:"app"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// handlePackInstall installe, DANS L'ORDRE, les apps manquantes d'un pack.
// Chaque app est installée via le même chemin que `caleope install`. Une app
// qui échoue (ex : paramètre requis manquant) n'interrompt pas les suivantes ;
// son erreur est remontée pour que l'utilisateur la termine à la main.
func (s *Server) handlePackInstall(args map[string]string) (interface{}, error) {
	id := args["id"]
	if id == "" {
		return nil, fmt.Errorf("argument 'id' (pack) manquant")
	}
	status, err := s.handlePackStatus(id)
	if err != nil {
		return nil, err
	}
	results := []packAppResult{}
	okCount := 0
	for _, appID := range status.ToInstall {
		iargs := map[string]string{"app": appID}
		if c := args["channel"]; c != "" {
			iargs["channel"] = c
		}
		if _, err := s.handleInstall(iargs); err != nil {
			results = append(results, packAppResult{App: appID, OK: false, Message: err.Error()})
		} else {
			results = append(results, packAppResult{App: appID, OK: true, Message: "installée"})
			okCount++
		}
	}
	return map[string]interface{}{
		"pack":          status.Pack.Name,
		"already":       status.Installed,
		"installed_now": okCount,
		"results":       results,
		"missing":       status.Missing,
	}, nil
}

// ─── HTTP (UI) ───

// routePacks : GET /api/v1/packs (liste + préflight de chaque pack)
func (s *Server) routePacks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	data, err := s.handlePacksList()
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

// routePack : /api/v1/packs/{id}  (GET statut)  ·  /api/v1/packs/{id}/install (POST)
func (s *Server) routePack(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/packs/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if id == "" {
		s.httpError(w, "id de pack manquant", http.StatusBadRequest)
		return
	}
	if action == "install" {
		if r.Method != http.MethodPost {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		data, err := s.handlePackInstall(map[string]string{"id": id, "channel": r.URL.Query().Get("channel")})
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
		return
	}
	st, err := s.handlePackStatus(id)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusNotFound)
		return
	}
	s.httpOK(w, st)
}
