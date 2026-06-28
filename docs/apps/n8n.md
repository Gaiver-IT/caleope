---
title: n8n
description: Plateforme d'automatisation de workflows
published: true
date: 2026-06-28
---

# n8n

Plateforme d'automatisation de workflows open-source, alternative à Zapier. Connecte des centaines de services avec une interface visuelle et supporte le code custom (JavaScript/Python).

## Installation

```bash
caleope install n8n --domain n8n.monserveur.fr
```

## Accès

```
https://n8n.monserveur.fr/
```

Un wizard de premier accès guide la création du compte admin.

## Commandes utiles

```bash
caleope logs n8n       # Logs
caleope restart n8n    # Redémarrer
caleope backup n8n     # Sauvegarder
```

## Structure des données

```
app-data/n8n/
└── data/    ← workflows, credentials, executions
```
