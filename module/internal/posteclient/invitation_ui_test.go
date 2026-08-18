package posteclient

import "testing"

// L'invitation est fabriquée en JavaScript (btoa + base64url à la main) et lue
// en Go. Si les deux ne s'accordent pas sur le codage, l'appairage échoue au
// premier collage — et personne ne saura pourquoi.
func TestInvitationFabriqueeParLInterfaceEstLisible(t *testing.T) {
	// Exactement ce que produit app.js pour https://caleope.guernaham.bzh + un code.
	const venantDeLInterface = "CALEOPE1:aHR0cHM6Ly9jYWxlb3BlLmd1ZXJuYWhhbS5iemh8NDVlMmQzZmNiMDQ5MmZmMjhj"
	s, c, err := LireInvitation(venantDeLInterface)
	if err != nil {
		t.Fatalf("invitation de l'interface refusée : %v", err)
	}
	if c != "45e2d3fcb0492ff28c" {
		t.Fatalf("code mal décodé : %q", c)
	}
	if s == "" {
		t.Fatal("adresse vide")
	}
}
