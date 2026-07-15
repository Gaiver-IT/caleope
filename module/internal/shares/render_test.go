package shares

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gaiver-it/caleope/pkg/types"
)

// TestRenderSMBConf vérifie la génération du smb.conf depuis les Partages :
// l'ACL par groupe Authentik doit produire valid users / write list corrects.
func TestRenderSMBConf(t *testing.T) {
	shares := []types.Share{
		{
			Name:       "rushs",
			Path:       "/opt/gaiver-it/caleope/app-data/_shares/rushs",
			Comment:    "Rushs youtuber",
			SMBEnabled: true,
			ACL: []types.ShareGroupACL{
				{Group: "creators", Access: types.AccessWrite},
				{Group: "invites", Access: types.AccessRead},
			},
		},
		{Name: "off", SMBEnabled: false}, // ne doit PAS apparaître
	}
	pathFn := func(s types.Share) string { return "/shares/" + s.Name }
	conf := renderSMBConf(shares, pathFn)
	fmt.Println("─── smb.conf généré ───")
	fmt.Println(conf)

	must := []string{
		"[rushs]",
		"path = /shares/rushs",
		"valid users = @creators @invites", // RO + RW ont accès (lecture)
		"write list = @creators",           // seul RW peut écrire
		"read only = yes",
	}
	for _, m := range must {
		if !strings.Contains(conf, m) {
			t.Errorf("smb.conf ne contient pas: %q", m)
		}
	}
	if strings.Contains(conf, "[off]") {
		t.Error("un partage SMB désactivé ne doit pas apparaître")
	}
}
