---
title: Vaultwarden
description: Gestionnaire de mots de passe compatible Bitwarden
published: true
date: 2026-06-19
---

# Vaultwarden

Implémentation légère et open-source de Bitwarden. Stocke et synchronise tes mots de passe, notes, et identités sur ton propre serveur.

## Installation

```bash
caleope install vaultwarden --domain vault.monserveur.fr
```

## Accès

- **Application** : `https://vault.monserveur.fr`
- **Panel admin** : `https://vault.monserveur.fr/admin`
- **Token admin** : généré à l'installation, affiché dans le terminal et dans `app-config/vaultwarden/secrets.env`

> Créer ton compte via l'interface web avant de désactiver les inscriptions en production.

## Clients compatibles

Utilise les applications officielles Bitwarden (elles se connectent à ton instance Vaultwarden) :

- **iOS / Android** : app Bitwarden officielle
- **Desktop** : Bitwarden pour Windows, Mac, Linux
- **Navigateur** : extension Bitwarden (Chrome, Firefox, Safari…)
- **CLI** : `bw` (Bitwarden CLI)

Dans chaque client, pointer vers `https://vault.monserveur.fr` comme serveur.

## Commandes utiles

```bash
caleope logs vaultwarden
caleope backup vaultwarden
caleope restart vaultwarden
```

## Structure des données

```
app-data/vaultwarden/
└── data/    ← base SQLite + attachments
```
