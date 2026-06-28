---
title: Grocy
description: ERP self-hosted pour la maison — stock, courses, recettes, équipement
published: true
date: 2026-06-28
---

# Grocy

ERP self-hosted pour la maison : gestion de stock alimentaire, liste de courses, recettes, gestion des équipements et des tâches récurrentes.

## Installation

```bash
caleope install grocy --domain grocy.monserveur.fr
```

## Accès

```
https://grocy.monserveur.fr/
```

- **Login** : `admin`
- **Mot de passe** : `admin`

> Changer le mot de passe dans **Administration → Manage Users**.

## Commandes utiles

```bash
caleope logs grocy       # Logs
caleope restart grocy    # Redémarrer
caleope backup grocy     # Sauvegarder
```

## Structure des données

```
app-data/grocy/
└── data/    ← base de données et configuration
```
