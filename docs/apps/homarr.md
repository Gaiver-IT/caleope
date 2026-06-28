---
title: Homarr
description: Dashboard de liens et widgets pour centraliser vos applications
published: true
date: 2026-06-28
---

# Homarr

Dashboard de liens et widgets pour centraliser toutes vos applications self-hosted. Supporte les intégrations Docker pour afficher l'état des containers, et des widgets pour Sonarr, Radarr, Jellyfin, etc.

## Installation

```bash
caleope install homarr --domain home.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `HOMARR_PORT_WEB` | Port web | `7575` |

## Accès

```
https://home.monserveur.fr/
```

Aucun compte requis pour la première utilisation. Ajouter vos applications depuis le mode édition (icône crayon).

## Commandes utiles

```bash
caleope logs homarr       # Logs
caleope restart homarr    # Redémarrer
caleope backup homarr     # Sauvegarder
```

## Structure des données

```
app-data/homarr/
├── data/     ← configuration et tableaux de bord
└── configs/  ← icônes personnalisées
```

## Notes

- Homarr a accès au socket Docker pour détecter automatiquement les containers en cours et afficher leur statut.
