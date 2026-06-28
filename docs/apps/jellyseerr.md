---
title: Jellyseerr
description: Gestionnaire de demandes de médias pour Jellyfin
published: true
date: 2026-06-28
---

# Jellyseerr

Interface de demande de contenus multimédia pour Jellyfin. Permet aux utilisateurs de demander des films et séries qui seront automatiquement téléchargés via Radarr et Sonarr.

## Installation

```bash
caleope install jellyseerr --domain media.monserveur.fr
```

> Jellyseerr est inclus dans l'[arr-stack](/apps/arr-stack). L'installer séparément uniquement si tu utilises Jellyfin sans l'arr-stack.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `JELLYSEERR_PORT_WEB` | Port web | `5099` |

## Accès

```
https://media.monserveur.fr/
```

Un wizard de premier accès guide la connexion à Jellyfin, Radarr et Sonarr.

## Commandes utiles

```bash
caleope logs jellyseerr       # Logs
caleope restart jellyseerr    # Redémarrer
caleope backup jellyseerr     # Sauvegarder
```

## Structure des données

```
app-data/jellyseerr/
└── config/    ← configuration et base de données
```
