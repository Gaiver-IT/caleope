---
title: Uptime Kuma
description: Monitoring de disponibilité des services
published: true
date: 2026-06-28
---

# Uptime Kuma

Outil de monitoring de disponibilité auto-hébergé. Surveille tes services (HTTP, TCP, DNS, Docker, etc.) et envoie des alertes par email, Telegram, Gotify, ntfy, Slack et bien d'autres.

## Installation

```bash
caleope install uptime-kuma --domain status.monserveur.fr
```

## Accès

```
https://status.monserveur.fr/
```

Un wizard de premier accès guide la création du compte admin.

## Commandes utiles

```bash
caleope logs uptime-kuma       # Logs
caleope restart uptime-kuma    # Redémarrer
caleope backup uptime-kuma     # Sauvegarder
```

## Structure des données

```
app-data/uptime-kuma/
└── data/    ← base de données SQLite et configuration
```
