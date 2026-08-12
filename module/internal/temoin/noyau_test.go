package temoin

import (
	"strings"
	"testing"
)

// mountstatsReel est un extrait VÉRITABLE, relevé le 2026-08-12 sur la machine
// où l'incident s'est produit. On teste sur la vraie chose, pas sur un format
// idéalisé : c'est ce relevé qui portait les 106 erreurs d'écriture que personne
// n'avait vues.
//
// Il contient volontairement du bruit : un montage fuse.rclone et un overlay
// Docker, qui ne doivent PAS être comptés comme du NFS.
const mountstatsReel = `device 172.16.51.7:/export/nas mounted on /opt/gaiver-it/caleope/mounts/mon-nas with fstype nfs statvers=1.1
	opts:	rw,vers=3,rsize=524288,wsize=524288,hard,nordirplus,proto=tcp,timeo=50,retrans=3,lookupcache=none
	age:	512666
	per-op statistics
	        NULL: 1 1 0 44 24 0 0 1 0
	     GETATTR: 164431 164432 0 22351972 18416272 6097 46508 58007 0
	      LOOKUP: 16942895 16942905 3 2399541400 4058103324 148556 137428993 138028241 66050
	        READ: 1463225 1463225 0 181975024 276267015060 45129 56967244 57073305 6
	       WRITE: 10927 10927 0 5572009288 1471656 2295784 486328 3985929 106
	      COMMIT: 270 272 0 33540 34176 15990 19883 68831 3

device sbox:NAS mounted on /opt/gaiver-it/caleope/mounts/sbox with fstype fuse.rclone
device overlay mounted on /var/lib/docker/rootfs/overlayfs/553a with fstype overlay
`

func TestParseMountstatsSurDonneesReelles(t *testing.T) {
	m := ParseMountstats(strings.NewReader(mountstatsReel))

	if len(m) != 1 {
		t.Fatalf("%d montage(s) retenu(s), attendu 1 (rclone et overlay ne sont pas du NFS) : %v",
			len(m), clefs(m))
	}
	c, ok := m["/opt/gaiver-it/caleope/mounts/mon-nas"]
	if !ok {
		t.Fatalf("le montage NFS n'a pas été trouvé ; clés = %v", clefs(m))
	}

	// Les nombres qui comptent : ce sont eux qui auraient dû alerter.
	if c.WriteErrs != 106 {
		t.Errorf("erreurs d'écriture = %d, attendu 106", c.WriteErrs)
	}
	if c.ReadErrs != 6 {
		t.Errorf("erreurs de lecture = %d, attendu 6", c.ReadErrs)
	}
	if c.LookupErrs != 66050 {
		t.Errorf("erreurs de LOOKUP = %d, attendu 66050 (signature « fileid changed »)", c.LookupErrs)
	}
	if c.WriteOps != 10927 {
		t.Errorf("opérations d'écriture = %d, attendu 10927", c.WriteOps)
	}
	if c.Timeouts != 3 {
		t.Errorf("timeouts cumulés = %d, attendu 3 (0 READ + 0 WRITE + 3 LOOKUP)", c.Timeouts)
	}
}

// TestNoyauSansColonneErreurs : sur les noyaux anciens la ligne n'a que 8
// nombres. Il ne faut alors PAS prendre le dernier champ pour des erreurs —
// sinon on compte des microsecondes d'exécution et on déclenche une alerte
// permanente sur une machine parfaitement saine.
func TestNoyauSansColonneErreurs(t *testing.T) {
	vieux := `device 10.0.0.1:/vol mounted on /mnt/vieux with fstype nfs statvers=1.0
	per-op statistics
	       WRITE: 500 500 0 1024 2048 7 900 4321
`
	c := ParseMountstats(strings.NewReader(vieux))["/mnt/vieux"]
	if c.WriteErrs != 0 {
		t.Errorf("erreurs d'écriture = %d, attendu 0 : 4321 est un temps d'exécution, pas des erreurs", c.WriteErrs)
	}
	if c.WriteOps != 500 {
		t.Errorf("opérations = %d, attendu 500", c.WriteOps)
	}
}

func TestDeltaNeDescendJamaisSousZero(t *testing.T) {
	// Cas réel : le montage a été refait, les compteurs sont repartis de zéro.
	apres := CompteursNFS{WriteErrs: 2, WriteOps: 10}
	avant := CompteursNFS{WriteErrs: 106, WriteOps: 10927}
	d := apres.Delta(avant)
	if d.WriteErrs != 0 || d.WriteOps != 0 {
		t.Errorf("après un remontage, le delta doit être nul, or WriteErrs=%d WriteOps=%d",
			d.WriteErrs, d.WriteOps)
	}
	// Cas normal : la différence honnête.
	d2 := CompteursNFS{WriteErrs: 110}.Delta(CompteursNFS{WriteErrs: 106})
	if d2.WriteErrs != 4 {
		t.Errorf("delta = %d, attendu 4", d2.WriteErrs)
	}
}

func TestParseMountsDecodeLesEspaces(t *testing.T) {
	// Le noyau échappe les espaces : un partage « Mes documents » apparaît en
	// « Mes\040documents ». Sans décodage, on ne le retrouve jamais.
	in := `172.16.51.7:/export/nas /opt/caleope/mounts/mon-nas nfs rw,vers=3,hard,proto=tcp 0 0
//serveur/partage /mnt/Mes\040documents cifs rw,soft 0 0
`
	m := ParseMounts(strings.NewReader(in))
	if _, ok := m["/mnt/Mes documents"]; !ok {
		t.Errorf("le point de montage avec espace n'a pas été décodé ; clés = %v", clefs2(m))
	}
	opts := m["/opt/caleope/mounts/mon-nas"]
	if !ContientOption(opts, "hard") {
		t.Errorf("« hard » non détecté dans %v", opts)
	}
	if ContientOption(opts, "soft") {
		t.Errorf("« soft » détecté à tort dans %v", opts)
	}
}

// TestContientOptionEstExact : « soft » ne doit pas correspondre à « nosoft »,
// sinon le module signalerait un montage dur comme dangereux.
func TestContientOptionEstExact(t *testing.T) {
	if ContientOption([]string{"nosoft", "hardlink"}, "soft") {
		t.Error("correspondance partielle : « nosoft » compté comme « soft »")
	}
	if ContientOption([]string{"nosoft", "hardlink"}, "hard") {
		t.Error("correspondance partielle : « hardlink » compté comme « hard »")
	}
}

func clefs(m map[string]CompteursNFS) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

func clefs2(m map[string][]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}
