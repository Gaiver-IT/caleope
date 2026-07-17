package shares

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gaiver-it/caleope/pkg/types"
)

// TestValidateACL couvre les valeurs telles qu'elles arrivent de l'API (JSON),
// et pas via les constantes Go — c'est précisément là qu'est le piège :
// TestRenderSMBConf utilise types.AccessWrite et ne peut donc jamais voir
// passer un « write » littéral.
//
// Un accès invalide DOIT être refusé : stocké tel quel, le groupe atterrit dans
// « valid users » mais jamais dans « write list », et le partage reste en
// lecture seule alors qu'il semble configuré en écriture.
func TestValidateACL(t *testing.T) {
	cas := []struct {
		nom  string
		body string
		ok   bool
	}{
		{"ro/rw valides", `[{"group":"creators","access":"rw"},{"group":"invites","access":"ro"}]`, true},
		{"ACL vide", `[]`, true},
		{"write au lieu de rw", `[{"group":"creators","access":"write"}]`, false},
		{"read au lieu de ro", `[{"group":"invites","access":"read"}]`, false},
		{"accès absent", `[{"group":"creators"}]`, false},
		{"groupe vide", `[{"group":"  ","access":"rw"}]`, false},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			var acl []types.ShareGroupACL
			if err := json.Unmarshal([]byte(c.body), &acl); err != nil {
				t.Fatalf("JSON invalide dans le cas de test: %v", err)
			}
			err := validateACL(acl)
			if c.ok && err != nil {
				t.Errorf("devait être accepté, refusé avec: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("devait être refusé, a été accepté")
			}
		})
	}
}

// TestValidateACLRefusExplicite : le message doit nommer le groupe et les
// valeurs attendues, sinon l'utilisateur ne sait pas quoi corriger.
func TestValidateACLRefusExplicite(t *testing.T) {
	acl := []types.ShareGroupACL{{Group: "creators", Access: "write"}}
	err := validateACL(acl)
	if err == nil {
		t.Fatal("« write » doit être refusé")
	}
	for _, attendu := range []string{"creators", "write", "ro", "rw"} {
		if !strings.Contains(err.Error(), attendu) {
			t.Errorf("le message doit contenir %q, obtenu: %v", attendu, err)
		}
	}
}
