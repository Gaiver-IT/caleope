// internal/api/files.go
//
// Gestionnaire de fichiers natif — API HTTP pour l'explorateur web.
// Scopé à la racine des Partages (app-data/_shares), avec garde-fou
// anti-traversal. L'upload est repris/par morceaux (offset) pour les gros
// fichiers : un transfert interrompu reprend là où il s'est arrêté.

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// safeSharePath résout un chemin relatif DANS la racine des Partages.
// Le préfixe "/" + Clean neutralise les ".." ; la vérification de préfixe
// est une double sécurité contre l'évasion hors de la racine.
func (s *Server) safeSharePath(rel string) (string, error) {
	root := s.sh.SharesRoot()
	clean := filepath.Clean("/" + strings.TrimPrefix(rel, "/"))
	full := filepath.Join(root, clean)
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("chemin hors des partages")
	}
	return full, nil
}

type fileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

// routeFiles : GET (liste un dossier) ou DELETE (supprime un fichier/dossier)
//
//	GET    /api/v1/files?path=rushs
//	DELETE /api/v1/files?path=rushs/vieux.mov
func (s *Server) routeFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		full, err := s.safeSharePath(r.URL.Query().Get("path"))
		if err != nil {
			s.httpError(w, err.Error(), http.StatusBadRequest)
			return
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			s.httpError(w, err.Error(), http.StatusNotFound)
			return
		}
		out := []fileEntry{}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, fileEntry{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size(), MTime: info.ModTime().Unix()})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].IsDir != out[j].IsDir {
				return out[i].IsDir // dossiers d'abord
			}
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
		s.httpOK(w, out)
	case http.MethodDelete:
		full, err := s.safeSharePath(r.URL.Query().Get("path"))
		if err != nil {
			s.httpError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if full == s.sh.SharesRoot() {
			s.httpError(w, "impossible de supprimer la racine des partages", http.StatusBadRequest)
			return
		}
		if err := os.RemoveAll(full); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, nil)
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeFilesMkdir : POST /api/v1/files/mkdir {path, name}
func (s *Server) routeFilesMkdir(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || strings.TrimSpace(b.Name) == "" {
		s.httpError(w, "champs 'path' et 'name' requis", http.StatusBadRequest)
		return
	}
	full, err := s.safeSharePath(filepath.Join(b.Path, filepath.Base(b.Name)))
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(full, 0o777); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, nil)
}

// routeFilesRename : POST /api/v1/files/rename {from, to}
func (s *Server) routeFilesRename(w http.ResponseWriter, r *http.Request) {
	var b struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		s.httpError(w, "body invalide", http.StatusBadRequest)
		return
	}
	src, err := s.safeSharePath(b.From)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	dst, err := s.safeSharePath(b.To)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.Rename(src, dst); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, nil)
}

// routeFilesDownload : GET /api/v1/files/download?path=... (flux, supporte Range)
func (s *Server) routeFilesDownload(w http.ResponseWriter, r *http.Request) {
	full, err := s.safeSharePath(r.URL.Query().Get("path"))
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		s.httpError(w, "fichier introuvable", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(full)))
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f) // gère Range → reprise download
}

// routeFilesUpload : upload repris par morceaux.
//
//	GET  /api/v1/files/upload?path=dir&name=fichier   → {"offset": <taille actuelle>}
//	POST /api/v1/files/upload?path=dir&name=fichier&offset=N   body=morceau
//
// Le client envoie les morceaux séquentiellement à partir de l'offset ; après
// interruption il relit l'offset (GET) et reprend. offset doit == taille courante.
func (s *Server) routeFilesUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dir, err := s.safeSharePath(q.Get("path"))
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := filepath.Base(q.Get("name")) // neutralise tout chemin dans le nom
	if name == "" || name == "." || name == string(os.PathSeparator) {
		s.httpError(w, "nom de fichier invalide", http.StatusBadRequest)
		return
	}
	full := filepath.Join(dir, name)

	if r.Method == http.MethodGet {
		var size int64
		if st, err := os.Stat(full); err == nil {
			size = st.Size()
		}
		s.httpOK(w, map[string]int64{"offset": size})
		return
	}
	if r.Method != http.MethodPost {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}

	offset, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY, 0o664)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	cur, _ := f.Seek(0, io.SeekEnd)
	if offset != cur {
		// désynchronisation → le client doit relire l'offset et reprendre
		w.Header().Set("X-Expected-Offset", strconv.FormatInt(cur, 10))
		s.httpError(w, fmt.Sprintf("offset attendu %d, reçu %d", cur, offset), http.StatusConflict)
		return
	}
	written, err := io.Copy(f, r.Body)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, map[string]int64{"offset": cur + written})
}
