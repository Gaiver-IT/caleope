// Package instances : installer plusieurs fois la même application.
//
// # LE PROBLÈME
//
// Caleope identifie une application par son nom : « minecraft ». Tout en
// découle — le dossier de compose, les données, la configuration, les ports,
// les sauvegardes. Installer un deuxième serveur Minecraft écrasait donc le
// premier, monde compris.
//
// # LA FORME RETENUE
//
// Une instance EST une application, dont l'identifiant porte un suffixe :
//
//	minecraft            l'instance par défaut
//	minecraft@homestead  une autre, indépendante
//
// Tout ce qui est indexé par identifiant devient distinct sans qu'une seule
// ligne de l'installateur, du superviseur ou des sauvegardes ait à savoir que
// les instances existent. C'est le point de cette conception : un seul endroit
// comprend le suffixe — la résolution du paquet dans le magasin.
//
// # CE QUE ÇA N'AUTORISE PAS
//
// Toutes les applications ne s'y prêtent pas : celles qui prennent un port fixe
// obligatoire, qui écrivent dans un chemin absolu partagé, ou qui n'existent
// qu'en un exemplaire sur une machine (un pare-feu, un serveur DNS). Le paquet
// doit donc le déclarer explicitement. Par défaut, c'est NON — se tromper dans
// ce sens ne casse rien, l'inverse détruit des données.
package instances

import (
	"fmt"
	"strings"
)

// Separateur entre le nom du paquet et celui de l'instance.
//
// « @ » et pas « - » : un tiret est déjà présent dans des noms d'applications
// (arr-stack, pterodactyl-panel) et on ne saurait plus où coupe le paquet.
const Separateur = "@"

// Decouper sépare « minecraft@homestead » en paquet et instance.
// Une application sans suffixe rend son nom et une instance vide.
func Decouper(id string) (paquet, instance string) {
	i := strings.Index(id, Separateur)
	if i < 0 {
		return id, ""
	}
	return id[:i], id[i+1:]
}

// Composer fabrique l'identifiant complet.
func Composer(paquet, instance string) string {
	if instance == "" {
		return paquet
	}
	return paquet + Separateur + instance
}

// EstInstance dit si cet identifiant désigne une instance nommée.
func EstInstance(id string) bool {
	_, inst := Decouper(id)
	return inst != ""
}

// ValiderNom refuse ce qui ferait des dégâts plus loin.
//
// Le nom d'instance devient un nom de dossier, une clé de port et un morceau
// d'URL. Un « .. » ou un « / » écrirait ailleurs que prévu ; une majuscule
// donnerait deux instances qui se ressemblent sur un système de fichiers
// insensible à la casse, et l'une écraserait l'autre sans prévenir.
func ValiderNom(instance string) error {
	if instance == "" {
		return fmt.Errorf("le nom d'instance ne peut pas être vide")
	}
	if len(instance) > 32 {
		return fmt.Errorf("nom d'instance trop long (32 caractères au plus)")
	}
	for _, r := range instance {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return fmt.Errorf("nom d'instance invalide : « %s » — minuscules, chiffres et tirets seulement", instance)
		}
	}
	if strings.HasPrefix(instance, "-") || strings.HasSuffix(instance, "-") {
		return fmt.Errorf("le nom d'instance ne peut pas commencer ni finir par un tiret")
	}
	return nil
}

// Verifier contrôle une demande d'installation d'instance.
// `multiInstance` vient du manifeste du paquet.
func Verifier(id string, multiInstance bool) error {
	paquet, instance := Decouper(id)
	if instance == "" {
		return nil // installation ordinaire
	}
	if paquet == "" {
		return fmt.Errorf("identifiant invalide : « %s »", id)
	}
	if err := ValiderNom(instance); err != nil {
		return err
	}
	if !multiInstance {
		// Message explicite : l'utilisateur doit comprendre que ce n'est pas un
		// oubli de sa part mais une limite du paquet, et laquelle.
		return fmt.Errorf("l'application « %s » ne peut être installée qu'une fois "+
			"(elle ne déclare pas « multi_instance »)", paquet)
	}
	return nil
}
