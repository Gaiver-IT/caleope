# Politique de sécurité

Caleope installe et expose des services sur votre machine. Cette page dit
comment signaler une faille, ce à quoi vous pouvez vous attendre en retour, et
— tout aussi important — **quelles sont les hypothèses de sécurité du produit**.
Plusieurs choix d'architecture décrits ici ne sont pas des défauts à corriger :
ce sont des compromis assumés, qu'il vaut mieux connaître avant de déployer.

## Signaler une faille

**N'ouvrez pas de ticket public** pour une vulnérabilité.

Écrivez à **security@gaiver-it.fr**. Si vous n'obtenez aucune réponse sous
30 jours, vous êtes libre de publier.

Un signalement exploitable contient :

- le composant concerné (daemon `caleoped`, interface web, CLI, installateur,
  ISO, serveur de licences) et la **version** (`caleope version`) ;
- l'**impact** : ce qu'un attaquant obtient concrètement ;
- les **prérequis** : accès réseau local, compte existant, interaction de
  l'utilisateur, position d'intercepteur… ;
- les **étapes de reproduction**.

## Ce à quoi vous pouvez vous attendre

Ce projet est maintenu par **une seule personne** sur son temps libre, avec une
fenêtre de maintenance mensuelle. Les engagements ci-dessous sont volontairement
modestes, pour être tenus :

| | |
|---|---|
| Accusé de réception | sous **30 jours** |
| Analyse et qualification | au cas par cas, sans délai garanti |
| Correctif | **aucun engagement de délai**, y compris sur les failles critiques |
| Divulgation coordonnée | souhaitée, **90 jours** proposés par défaut, négociable |
| Prime / bug bounty | **aucune** |

Si une faille ne peut pas être corrigée dans un délai raisonnable, elle sera
**documentée publiquement** sur cette page comme limitation connue, plutôt que
laissée sous silence.

## Versions prises en charge

| Version | Prise en charge |
|---|---|
| Dernière version publiée | ✅ |
| Versions antérieures | ❌ — mettez à jour (`caleope upgrade`) |

Les correctifs sont publiés sur la dernière version uniquement. Il n'y a pas de
branche de maintenance à long terme.

## Périmètre

**Dans le périmètre** — le code de ce dépôt : daemon, CLI, interface web,
`install.sh`, construction de l'ISO, et le serveur de licences.

**Hors périmètre** :

- **Les applications du catalogue.** Ce sont des logiciels **tiers** (Nextcloud,
  Immich, Jellyfin, Authentik…). Leurs vulnérabilités se signalent à leurs
  projets. Caleope ne fait que les installer et les câbler.
- **Les images Docker en amont** et leurs mainteneurs.
- **Debian** et les paquets du système de base.
- Les installations **modifiées à la main** hors de Caleope.
- Les rapports issus d'un scanner automatique **sans démonstration d'impact**.

## Hypothèses de sécurité — à lire avant de déployer

Caleope est une **appliance d'auto-hébergement conçue pour un réseau local de
confiance**. Les points suivants sont des propriétés connues du produit :

- **N'exposez pas l'interface d'administration directement sur Internet.**
  Placez-la derrière un VPN (WireGuard, Tailscale) ou un accès restreint par IP.
  Publier des *applications* est un usage prévu ; publier le panneau
  d'administration ne l'est pas.
- **Le mot de passe de l'interface est aussi celui du compte système.** Choisir
  un mot de passe faible expose la machine entière, pas seulement l'interface.
- **Les scripts d'installation des applications du catalogue s'exécutent avec les
  privilèges root, sans bac à sable.** C'est inhérent au modèle : une appliance
  Docker orchestre des conteneurs privilégiés. **Installer une application, c'est
  accorder une confiance totale à son mainteneur.** Le dépôt officiel est relu ;
  les dépôts tiers que vous ajoutez ne le sont pas.
- **Les images Docker sont largement suivies par étiquette mobile**, et non
  épinglées par empreinte. Une image amont compromise se propagerait à
  l'installation ou à la mise à jour. Épingler par empreinte est un chantier en
  cours.
- **Les binaires téléchargés pendant l'installation et la mise à jour ne sont pas
  vérifiés par somme de contrôle.** Le transport est en HTTPS, mais il n'y a pas
  de second contrôle d'intégrité indépendant.
- **L'ISO efface le disque qu'elle sélectionne, sans demander de confirmation.**
  N'installez que sur une machine dédiée dont vous acceptez la perte totale des
  données.
- **Les sauvegardes ne sont pas chiffrées au repos** et contiennent les données
  et la configuration de vos applications. Traitez-les comme des secrets.

## Durcissement recommandé

1. Mot de passe long et unique pour l'interface, généré par un gestionnaire.
2. Interface d'administration accessible **uniquement** via VPN.
3. Pare-feu en refus par défaut ; n'ouvrez que les ports réellement nécessaires.
4. Les mises à jour de sécurité de Debian sont appliquées automatiquement
   (`unattended-upgrades`) — vérifiez que le service tourne toujours.
5. `caleope upgrade` régulièrement : les correctifs ne sont publiés que sur la
   dernière version.
6. Sauvegardes vérifiées **et testées en restauration**, stockées hors de la
   machine.
7. N'ajoutez de dépôt d'applications tiers que si vous en auditez le contenu.

## Avis de sécurité

Les avis publiés le sont dans `docs/securite/`. Aucun avis en vigueur à ce jour.

## Périmètre juridique

Caleope est distribué sous **AGPLv3**, **sans aucune garantie**, dans les termes
de la licence. Cette clause ne restreint pas les droits impératifs que vous
tenez de la loi — en particulier les garanties légales dues aux consommateurs
de l'Union européenne ayant acquis une licence Pro.
