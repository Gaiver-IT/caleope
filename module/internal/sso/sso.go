// Package sso garantit les prérequis du branchement SSO entre Authentik et les
// applications du magasin.
//
// ── POURQUOI CE PAQUET EXISTE ────────────────────────────────────────────────
// 18 applications du magasin câblent leur SSO en lisant `AUTHENTIK_DOMAIN` dans
// `app-config/authentik/secrets.env`. Ce fichier est écrit UNE FOIS, à
// l'installation d'Authentik, et n'est jamais remis à jour ensuite.
//
// Conséquence observée en production (08/08) : Authentik avait été installé
// AVANT que son setup.sh n'écrive cette clé. Toutes les installations d'apps
// postérieures cherchaient donc une clé absente. Avant durcissement des scripts,
// cela TUAIT l'installation (`grep` sans résultat sous `set -e`) ; depuis, cela
// la laisse simplement aboutir SANS SSO — silencieusement.
//
// Le correctif de fond n'est pas dans les scripts : c'est que le daemon
// GARANTISSE la présence de cette clé, comme il garantit déjà la configuration
// de l'interface web ou les priorités de ressources.
//
// ── PRINCIPE ────────────────────────────────────────────────────────────────
// On n'écrase JAMAIS une valeur existante : si la clé est là, on ne touche à
// rien. On ne fait qu'ajouter ce qui manque, et on le journalise. Un secrets.env
// contient des mots de passe qu'on ne réécrit pas sur un malentendu.
package sso

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DomainResolver fournit le domaine public réel d'une application installée.
type DomainResolver interface {
	AppDomain(appID string) string
}

const (
	authentikApp = "authentik"
	domainKey    = "AUTHENTIK_DOMAIN"
)

// HasKey indique si le contenu d'un fichier d'environnement définit déjà la clé
// avec une valeur NON VIDE.
//
// La nuance « non vide » compte : un `AUTHENTIK_DOMAIN=` orphelin (laissé par un
// script qui a substitué une variable absente) satisfait un `grep` naïf mais ne
// vaut rien pour l'app qui le lit. On le traite comme manquant.
func HasKey(content, key string) bool {
	prefix := key + "="
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := sc.Text()
		// On imite EXACTEMENT la lecture des apps : `grep "^KEY=" | cut -d= -f2-`.
		// Surtout PAS de TrimSpace sur la ligne : « KEY = valeur » ou une ligne
		// indentée n'est pas une affectation shell valide, les apps ne la
		// verraient pas — la considérer comme présente nous ferait sauter
		// l'ajout et laisserait le SSO cassé.
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if strings.TrimSpace(strings.TrimPrefix(line, prefix)) != "" {
			return true
		}
	}
	return false
}

// EnsureAuthentikDomain ajoute AUTHENTIK_DOMAIN au secrets.env d'Authentik s'il
// manque. Renvoie true si le fichier a été modifié.
//
// Ne fait rien — sans erreur — si Authentik n'est pas installé : le SSO n'est
// pas un prérequis, c'est une commodité quand Authentik est là.
func EnsureAuthentikDomain(baseDir string, dr DomainResolver, logf func(string, ...any)) (bool, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	if _, err := os.Stat(filepath.Join(baseDir, "apps-installed", authentikApp)); err != nil {
		return false, nil // Authentik n'est pas installé : rien à garantir.
	}

	secrets := filepath.Join(baseDir, "app-config", authentikApp, "secrets.env")
	data, err := os.ReadFile(secrets)
	if err != nil {
		// Authentik est installé mais sans fichier de secrets : anomalie qu'on
		// signale sans y toucher — le réparer à l'aveugle serait pire.
		logf("sso : %s introuvable alors qu'Authentik est installé (%v)", secrets, err)
		return false, nil
	}

	if HasKey(string(data), domainKey) {
		return false, nil
	}

	domain := strings.TrimSpace(dr.AppDomain(authentikApp))
	if domain == "" {
		logf("sso : domaine d'Authentik inconnu — %s non ajouté", domainKey)
		return false, nil
	}

	// Ajout en fin de fichier, sans relire ni réécrire les lignes existantes :
	// elles contiennent des mots de passe et un jeton d'API.
	f, err := os.OpenFile(secrets, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("ouverture de %s : %w", secrets, err)
	}
	defer f.Close()

	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	block := prefix + "\n# Domaine public d'Authentik — ajouté automatiquement par le daemon.\n" +
		"# Lu par les applications du magasin pour câbler leur SSO ; son absence\n" +
		"# les laissait s'installer sans authentification unique, en silence.\n" +
		domainKey + "=" + domain + "\n"

	if _, err := f.WriteString(block); err != nil {
		return false, fmt.Errorf("écriture dans %s : %w", secrets, err)
	}

	logf("sso : %s=%s ajouté au secrets.env d'Authentik", domainKey, domain)
	return true, nil
}
