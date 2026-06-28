---
title: Monica
description: CRM personnel — gérez vos relations et contacts
published: true
date: 2026-06-28
---

# Monica

CRM personnel pour gérer les relations humaines : contacts, anniversaires, interactions, notes et rappels. Idéal pour rester en contact avec sa famille et ses amis.

## Installation

```bash
caleope install monica --domain monica.monserveur.fr
```

La base de données est initialisée automatiquement à l'installation.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `MYSQL_PASSWORD` | Mot de passe base de données | généré automatiquement |
| `MYSQL_ROOT_PASSWORD` | Mot de passe root MySQL | généré automatiquement |

## Accès

```
https://monica.monserveur.fr/
```

Créer un compte depuis la page d'accueil au premier accès (Register).

## Commandes utiles

```bash
caleope logs monica       # Logs
caleope restart monica    # Redémarrer
caleope backup monica     # Sauvegarder
```

## Structure des données

```
app-data/monica/
└── storage/    ← fichiers et pièces jointes
```

## Notes

- Monica inclut une base de données MariaDB dédiée démarrée automatiquement.
- Les migrations de la base de données prennent ~90 secondes au premier démarrage — attendre avant d'accéder à l'interface.
