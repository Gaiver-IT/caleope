---
title: Mealie
description: Gestionnaire de recettes self-hosted
published: true
date: 2026-06-28
---

# Mealie

Gestionnaire de recettes self-hosted avec import automatique depuis URLs, planificateur de repas hebdomadaire et liste de courses générée automatiquement.

## Installation

```bash
caleope install mealie --domain mealie.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `MEALIE_PORT_WEB` | Port web | `9000` |

## Accès

```
https://mealie.monserveur.fr/
```

- **Login** : `changeme@exemple.fr`
- **Mot de passe** : `MyPassword`

> Changer les identifiants immédiatement après la première connexion dans **Profile → Settings**.

## Commandes utiles

```bash
caleope logs mealie       # Logs
caleope restart mealie    # Redémarrer
caleope backup mealie     # Sauvegarder
```

## Structure des données

```
app-data/mealie/
└── data/    ← recettes, images, base de données
```
