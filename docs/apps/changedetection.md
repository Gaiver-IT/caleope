---
title: Changedetection.io
description: Surveillance de changements de pages web avec alertes
published: true
date: 2026-06-28
---

# Changedetection.io

Surveille des pages web et envoie des alertes quand leur contenu change. Supporte le rendu JavaScript, les sélecteurs CSS, les filtres XPath et les notifications (email, Gotify, ntfy, etc.).

## Installation

```bash
caleope install changedetection --domain changedetection.monserveur.fr
```

## Accès

```
https://changedetection.monserveur.fr/
```

Aucun compte requis — accès direct à l'interface. Configurer un mot de passe dans **Settings → Security** si nécessaire.

## Commandes utiles

```bash
caleope logs changedetection       # Logs
caleope restart changedetection    # Redémarrer
caleope backup changedetection     # Sauvegarder
```

## Structure des données

```
app-data/changedetection/
└── data/    ← configuration, historique et snapshots
```
