---
title: Plex
description: Serveur multimédia populaire
published: true
date: 2026-06-28
---

# Plex

Serveur multimédia populaire — streamez films, séries et musique sur tous vos appareils avec des apps natives iOS, Android, TV, console et web.

## Installation

```bash
caleope install plex --domain plex.monserveur.fr

# Avec claim token Plex (recommandé pour associer au compte)
caleope install plex --domain plex.monserveur.fr \
  --param PLEX_CLAIM=claim-xxxxxx
```

> Le claim token est obtenu sur [plex.tv/claim](https://www.plex.tv/claim) (valide 4 minutes). Il permet d'associer le serveur automatiquement à ton compte Plex.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `PLEX_PORT_WEB` | Port web | `32400` |
| `PLEX_CLAIM` | Token de claim Plex | optionnel |

## Accès

```
https://plex.monserveur.fr/web     ← Interface web Plex
```

Ou depuis l'application Plex sur n'importe quel appareil.

## Commandes utiles

```bash
caleope logs plex       # Logs
caleope restart plex    # Redémarrer
caleope backup plex     # Sauvegarder
```

## Structure des données

```
app-data/plex/
└── config/    ← configuration, base de données, miniatures
```

## Notes

- Sans claim token, associer le serveur manuellement depuis l'interface web au premier accès.
- Le port DLNA 1900 (UDP) est exposé pour la découverte réseau locale.
