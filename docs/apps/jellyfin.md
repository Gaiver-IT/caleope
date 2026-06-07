---
title: Jellyfin
description: Serveur multimédia libre
published: true
date: 2026-06-07
---

# Jellyfin

Serveur multimédia libre et open-source. Diffuse films, séries, musique et photos depuis ton serveur vers n'importe quel appareil.

## Installation

```bash
caleope install jellyfin
```

L'installation est **entièrement automatique** :
- Compte admin créé automatiquement (identifiants affichés et sauvegardés)
- Wizard de démarrage complété sans interaction
- Langue française configurée (métadonnées + interface)

Les identifiants sont affichés à la fin de l'installation et sauvegardés dans :
```
/opt/gaiver-it/caleope/app-config/jellyfin/secrets.env
```

## Avec l'arr-stack (recommandé)

Installer Jellyfin **séparément** avant l'arr-stack est la configuration recommandée :

```bash
caleope install jellyfin    # 1. Jellyfin standalone
caleope install arr-stack   # 2. arr-stack détecte Jellyfin automatiquement
```

L'arr-stack :
- Détecte Jellyfin via le runtime Caleope
- Lit les credentials depuis `app-config/jellyfin/secrets.env`
- Monte le dossier de médias de l'arr-stack dans Jellyfin (`/arr-media`)
- Crée les bibliothèques Films, Séries, Musique automatiquement
- Configure Jellyseerr pour se connecter à Jellyfin sans aucune action manuelle

## Bibliothèques médias

Si Jellyfin est installé avec l'arr-stack, les bibliothèques sont créées automatiquement et pointent vers les médias téléchargés par l'arr-stack :

| Bibliothèque | Chemin dans le container |
|-------------|--------------------------|
| Films | `/arr-media/movies` |
| Séries | `/arr-media/tv` |
| Musique | `/arr-media/music` |

Pour un Jellyfin standalone (sans arr-stack), ajoute tes bibliothèques manuellement dans :
`Tableau de bord → Bibliothèques → Ajouter une bibliothèque`

## Intégrations

| Intégration | Description |
|-------------|-------------|
| **arr-stack** | Jellyseerr, Radarr, Sonarr — gestion et téléchargement automatique |
| **Jellyfin Vue** | Interface de lecture alternative épurée (incluse dans arr-stack) |
| **Prometheus + Grafana** | Supervision des ressources |
| **Bazarr** | Sous-titres automatiques |
| **Authentik** | SSO OIDC — connexion unique avec tes autres apps |

## Commandes utiles

```bash
caleope logs jellyfin        # Voir les logs
caleope restart jellyfin     # Redémarrer
caleope backup jellyfin      # Sauvegarder la config
```

## Accès mobile

- **iOS/Android** : application officielle Jellyfin
- **TV** : Jellyfin pour Android TV, Apple TV, Fire TV
- **Web** : n'importe quel navigateur

## Structure des données

```
app-data/jellyfin/
├── config/   ← configuration, base de données, system.xml
├── cache/    ← miniatures, cache de transcodage
└── media/    ← point de montage bibliothèque locale (optionnel)
```

Les données de l'arr-stack (films, séries téléchargés) sont dans :
```
app-data/arr-stack/data/media/
```
et montées en lecture seule dans Jellyfin sous `/arr-media`.
