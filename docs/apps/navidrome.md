---
title: Navidrome
description: Serveur de musique compatible Subsonic
published: true
date: 2026-06-28
---

# Navidrome

Serveur de musique moderne et léger compatible avec l'API Subsonic. Streamez votre collection musicale depuis n'importe quel appareil avec des clients comme Symfonium, Substreamer, DSub ou Feishin.

## Installation

```bash
caleope install navidrome --domain music.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `NAVIDROME_PORT_WEB` | Port web | `4533` |
| `ND_LOGLEVEL` | Niveau de log | `info` |

## Accès

```
https://music.monserveur.fr/
```

Un wizard de premier accès guide la création du compte admin.

## Clients compatibles

| Client | Plateforme |
|--------|-----------|
| **Symfonium** | Android |
| **Substreamer** | iOS |
| **DSub** | Android |
| **Feishin** | Desktop |
| **Sonixd** | Desktop |

## Commandes utiles

```bash
caleope logs navidrome       # Logs
caleope restart navidrome    # Redémarrer
caleope backup navidrome     # Sauvegarder
```

## Structure des données

```
app-data/navidrome/
├── data/    ← base de données et cache
└── music/   ← bibliothèque musicale (point de montage)
```

## Notes

- Placer les fichiers musicaux dans `app-data/navidrome/music/` ou configurer un volume supplémentaire.
