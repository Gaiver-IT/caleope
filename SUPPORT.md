# Support

Caleope est développé et maintenu par **une seule personne**, sur son temps libre.
Cette page dit précisément ce que cela implique — ce qui est fourni, ce qui ne
l'est pas, et où trouver une réponse. Elle est volontairement explicite : mieux
vaut une attente juste qu'une promesse tenue à moitié.

## En résumé

| | |
|---|---|
| **Documentation** | Première et principale ressource. Elle est faite pour répondre sans intermédiaire. |
| **Signalements** | GitHub Issues, en public. |
| **Rythme de traitement** | Une fenêtre de maintenance **par mois**. Pas de délai garanti. |
| **Astreinte, SLA, support téléphonique** | **Non fournis**, à aucun niveau, y compris Pro. |
| **Licence Pro** | Donne accès à des fonctionnalités et soutient le projet. **Elle n'achète pas du support.** |

## Avant de demander de l'aide

Ces quatre réflexes résolvent la grande majorité des situations, et bien plus
vite qu'une réponse qui peut mettre des semaines à venir.

1. **Le guide de dépannage** — il est organisé par symptôme, pas par composant.
2. **L'état réel du système** : `caleope status`, puis les journaux du service
   concerné (`journalctl -u caleoped -n 100`).
3. **Les journaux de l'application fautive** : `docker logs <nom-du-conteneur>`.
   Une application du catalogue est un logiciel **tiers** : si le problème vient
   d'elle, sa propre documentation et sa communauté seront plus compétentes que
   moi.
4. **Le fichier `LIENS.md`** créé à l'installation, qui récapitule vos accès.

## Ce qui est pris en charge

- Les **anomalies reproductibles** de Caleope lui-même : le daemon, la ligne de
  commande, l'interface web, l'installateur, l'ISO.
- Les **régressions** : quelque chose qui fonctionnait dans une version
  précédente et ne fonctionne plus.
- Les **erreurs de documentation** — un signalement de ce type est toujours le
  bienvenu et sera traité en priorité, parce qu'il profite à tout le monde.
- Les **failles de sécurité**, traitées à part : voir [SECURITY.md](SECURITY.md).

## Ce qui n'est pas pris en charge

Ce n'est pas de la mauvaise volonté : c'est ce qui rend le reste tenable.

- **L'infogérance de votre serveur** : dimensionnement, réseau, pare-feu,
  sauvegardes, nom de domaine, certificats.
- **Le fonctionnement interne des applications du catalogue.** Caleope les
  installe et les câble ; il ne les édite pas. Un souci propre à Nextcloud,
  Immich ou Jellyfin relève de leurs projets respectifs.
- **Les installations modifiées à la main** (conteneurs édités hors Caleope,
  fichiers compose retouchés, paquets système remplacés). Signalez-le d'emblée si
  c'est le cas — sinon on cherche à deux dans la mauvaise direction.
- **Les demandes de fonctionnalités sur mesure**, la formation, l'aide à la
  migration, l'accompagnement projet.
- **Les urgences.** Il n'existe aucun canal prioritaire. Si votre activité dépend
  d'une disponibilité garantie, Caleope n'est pas le bon choix : prenez un
  hébergeur avec un contrat.

## Comment signaler utilement

Un signalement complet est traité ; un signalement vague attend. Indiquez :

- **la version** : `caleope version` ;
- **le mode d'installation** : ISO, ou script `install.sh` ;
- **ce que vous attendiez** et **ce qui s'est produit** ;
- **les étapes exactes** pour reproduire, depuis une installation propre si
  possible ;
- **les journaux utiles**, en texte et non en capture d'écran.

⚠️ **Retirez vos secrets** avant de coller quoi que ce soit : mots de passe,
jetons, clés d'API, adresses IP publiques, noms de domaine que vous ne souhaitez
pas rendre publics. Les tickets sont visibles de tous.

## Rythme réel

Le projet est relu lors d'**une session de maintenance mensuelle**. Concrètement :

- un ticket ouvert juste après une session peut attendre plusieurs semaines ;
- il n'y a **pas** d'accusé de réception automatique ;
- l'absence de réponse ne signifie pas le rejet : elle signifie que la prochaine
  fenêtre n'est pas encore ouverte ;
- les correctifs de sécurité peuvent sortir hors de ce rythme, sans garantie
  de délai (voir [SECURITY.md](SECURITY.md)).

Si le projet devait cesser d'être maintenu, ce sera annoncé ici et dans le
README plutôt que laissé au silence.

## Le logiciel est libre — et fourni tel quel

Caleope est publié sous **AGPLv3**. Vous pouvez l'utiliser, l'étudier, le
modifier et le redistribuer. Cette liberté s'accompagne d'une **absence de
garantie**, dans les termes de la licence.

Cela ne retire rien à vos droits légaux : si vous avez acheté une licence Pro en
tant que consommateur dans l'Union européenne, les garanties légales
correspondantes continuent de s'appliquer, quoi que dise la licence logicielle.

## Contribuer

C'est la façon la plus efficace de faire avancer un point qui vous tient à cœur.
Les corrections de documentation et les recettes de dépannage issues de votre
propre expérience sont particulièrement utiles — elles évitent à la personne
suivante de rouvrir le même ticket.
