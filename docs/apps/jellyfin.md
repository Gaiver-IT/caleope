---
title: Jellyfin
description: Serveur multimédia libre
published: true
date: 2026-05-25
---

# Jellyfin

Serveur multimédia libre et open-source. Diffuse films, séries, musique et photos depuis ton serveur vers n'importe quel appareil.

## Installation

```bash
caleope install jellyfin --domain media.monserveur.fr
```

Jellyfin ne génère pas d'identifiants à l'installation — un wizard de premier démarrage te guide pour créer le compte admin.

## Première configuration

1. Ouvre `https://media.monserveur.fr` → wizard de démarrage
2. Crée ton compte administrateur
3. Ajoute tes bibliothèques médias (si Jellyfin standalone)

> **Avec l'arr-stack** : pas besoin de configurer manuellement. Le bootstrap de l'arr-stack ajoute automatiquement les bibliothèques Films, Séries et Musique — que Jellyfin soit inclus dans la stack ou installé séparément.

## Intégrations

| Intégration | Description |
|-------------|-------------|
| **Jellyseerr** | Interface de demande de contenu (arr-stack) |
| **Jellyfin Vue** | Interface de lecture alternative épurée (arr-stack) |
| **Prometheus + Grafana** | Supervision des ressources |
| **Bazarr** | Sous-titres automatiques |

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
├── config/   ← configuration, base de données
├── cache/    ← miniatures, cache de transcodage
└── media/    ← point de montage bibliothèque (optionnel)
```
