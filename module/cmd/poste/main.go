// poste — la même chose que la fenêtre, mais en ligne de commande.
//
//	poste connexion https://caleope.exemple.fr CODE   une seule fois, par machine
//	poste etat                                        ce qui manque ici
//	poste appliquer                                   essai à blanc
//	poste appliquer --pour-de-vrai                    on y va
//
// Tout le raisonnement vit dans internal/posteclient, partagé avec l'interface
// graphique : deux copies divergent au premier correctif, et l'utilisateur se
// retrouve avec une fenêtre qui dit « à jour » quand le terminal dit
// « 3 manquants ». Ce fichier ne fait que de l'affichage.
package main

import (
	"fmt"
	"os"

	"github.com/gaiver-it/caleope/internal/posteclient"
)

func afficherDossiers(etats []posteclient.EtatDossier) {
	if len(etats) == 0 {
		return
	}
	fmt.Println("\nDossiers du profil :")
	for _, d := range etats {
		detail := d.Etat
		if d.Detail != "" {
			detail += " — " + d.Detail
		}
		fmt.Printf("  %-22s %-40s %s (%s)\n", d.Nom, d.Chemin, d.Sens, detail)
	}
}

func cmdConnexion(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage : poste connexion <url-du-serveur> <code>")
	}
	c, err := posteclient.Connecter(args[0], args[1])
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s connectée au profil « %s ».\n", c.Machine, c.Profil)
	fmt.Println("  Le code d'appairage vient d'être consommé : il ne resservira pas.")
	fmt.Println("  Ensuite :  poste etat")
	return nil
}

func cmdEtat() error {
	c, err := posteclient.LireConfig()
	if err != nil {
		return err
	}
	p, err := posteclient.TirerProfil(c)
	if err != nil {
		return err
	}
	sys, gest := posteclient.Systeme()
	fmt.Printf("Machine : %s (%s, %s)\nProfil  : %s\n", c.Machine, sys, gest, p.Nom)

	inst, err := posteclient.PaquetsInstalles(gest)
	if err != nil {
		return err
	}
	m := posteclient.Manquants(p, inst)
	fmt.Printf("Logiciels demandés : %d   manquants ici : %d\n", len(p.Paquets), len(m))
	for _, x := range m {
		fmt.Printf("  + %s\n", x)
	}
	afficherDossiers(posteclient.PreparerDossiers(p))
	posteclient.Rapporter(c, len(m))
	return nil
}

func cmdAppliquer(pourDeVrai bool) error {
	c, err := posteclient.LireConfig()
	if err != nil {
		return err
	}
	p, err := posteclient.TirerProfil(c)
	if err != nil {
		return err
	}
	_, gest := posteclient.Systeme()
	inst, err := posteclient.PaquetsInstalles(gest)
	if err != nil {
		return err
	}
	m := posteclient.Manquants(p, inst)
	afficherDossiers(posteclient.PreparerDossiers(p))

	if len(m) == 0 {
		fmt.Println("\nRien à installer : cette machine a déjà tout ce que son profil demande.")
		posteclient.Rapporter(c, 0)
		return nil
	}
	fmt.Printf("\n%d logiciel(s) manquant(s) :\n", len(m))
	for _, x := range m {
		fmt.Printf("  + %s\n", x)
	}
	if !pourDeVrai {
		fmt.Println("\n→ Essai à blanc. Rien n'a été installé.")
		fmt.Println("  Pour agir :  poste appliquer --pour-de-vrai")
		posteclient.Rapporter(c, len(m))
		return nil
	}
	echecs := 0
	for _, x := range m {
		fmt.Printf("\n→ installation de %s…\n", x)
		if err := posteclient.Installer(gest, x); err != nil {
			// Un nom peut ne pas exister sur cette distribution : on continue,
			// sinon un seul paquet exotique bloque tous les autres.
			fmt.Fprintf(os.Stderr, "✗ échec sur %s : %v — on continue\n", x, err)
			echecs++
		}
	}
	// On RE-MESURE au lieu d'annoncer : un rattrapage qui ne se relit pas
	// transforme une petite panne en longue panne.
	inst2, _ := posteclient.PaquetsInstalles(gest)
	reste := posteclient.Manquants(p, inst2)
	fmt.Printf("\nTerminé : %d échec(s), %d encore manquant(s).\n", echecs, len(reste))
	posteclient.Rapporter(c, len(reste))
	return nil
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"etat"}
	}
	var err error
	switch args[0] {
	case "connexion":
		err = cmdConnexion(args[1:])
	case "etat":
		err = cmdEtat()
	case "appliquer":
		err = cmdAppliquer(len(args) > 1 && args[1] == "--pour-de-vrai")
	case "version":
		fmt.Println("poste (Caleope) — client de poste nomade")
	default:
		fmt.Println("usage : poste {connexion <url> <code> | etat | appliquer [--pour-de-vrai]}")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}
