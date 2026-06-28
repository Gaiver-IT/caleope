---
title: FreshRSS
description: Agrégateur RSS self-hosted
published: true
date: 2026-06-28
---

# FreshRSS

Agrégateur RSS/Atom self-hosted, léger et complet. Compatible avec les clients mobiles via l'API Google Reader (Reeder, FeedMe, NetNewsWire, etc.).

## Installation

```bash
caleope install freshrss --domain rss.monserveur.fr
```

Les identifiants sont générés automatiquement et sauvegardés dans `app-config/freshrss/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `FRESHRSS_ADMIN_USER` | Nom d'utilisateur admin | `admin` |
| `FRESHRSS_ADMIN_PASS` | Mot de passe admin | généré automatiquement |
| `FRESHRSS_PORT_WEB` | Port web | `8065` |

```bash
caleope install freshrss --domain rss.monserveur.fr \
  --param FRESHRSS_ADMIN_USER=admin \
  --param FRESHRSS_ADMIN_PASS=monmotdepasse
```

## Accès

```
https://rss.monserveur.fr/
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/freshrss/secrets.env
```

## API mobile (Google Reader)

L'API Google Reader est activée automatiquement. URL de l'API :

```
https://rss.monserveur.fr/api/greader.php
```

Utiliser le nom d'utilisateur et mot de passe FreshRSS dans le client mobile.

## Commandes utiles

```bash
caleope logs freshrss       # Logs
caleope restart freshrss    # Redémarrer
caleope backup freshrss     # Sauvegarder
```

## Structure des données

```
app-data/freshrss/
├── data/        ← configuration et flux RSS
└── extensions/  ← extensions FreshRSS
```

## Notes

- L'API Google Reader est configurée automatiquement à l'installation pour permettre la connexion depuis les clients mobiles.
