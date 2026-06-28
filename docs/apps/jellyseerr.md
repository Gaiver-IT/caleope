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

## Accès

```
https://media.monserveur.fr/
```

Un wizard de premier accès guide la connexion à Jellyfin, Radarr et Sonarr.

## SSO Authentik (OIDC natif)

Si Authentik est installé, Jellyseerr est automatiquement enregistré comme application OIDC (v2+).

- Callback URI : `https://media.monserveur.fr/auth/oidc/callback`
- Connexion : bouton **"Se connecter avec Authentik"** sur la page de login

Les credentials OIDC sont dans `app-config/jellyseerr/secrets.env`.

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
