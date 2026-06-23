---
title: Ghost
description: Plateforme de publication et newsletter moderne
published: true
date: 2026-06-19
---

# Ghost

Plateforme de publication moderne — blog, newsletter, membership. Simple, rapide, et axée sur la monétisation de contenu.

## Installation

```bash
caleope install ghost --domain blog.monserveur.fr
```

## Accès

- **Site public** : `https://blog.monserveur.fr`
- **Admin** : `https://blog.monserveur.fr/ghost/`
- **Login** : `admin@blog.monserveur.fr`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/ghost/secrets.env`

## Configuration email (newsletters)

Ghost a besoin d'un serveur SMTP pour envoyer les newsletters. Caleope injecte automatiquement la config SMTP globale si configurée :

```bash
# Configurer le SMTP global Caleope (une seule fois pour toutes les apps)
caleope config smtp --host smtp.monserveur.fr --port 587 \
  --user no-reply@monserveur.fr --pass <pass>
```

## Membership et monétisation

Ghost supporte nativement :
- Abonnements payants (via Stripe)
- Newsletter gratuite avec wall de contenu
- Pages membres privées

Configurer dans **Admin → Membership → Portail**.

## Commandes utiles

```bash
caleope logs ghost
caleope backup ghost
caleope restart ghost
```

## Structure des données

```
app-data/ghost/
├── content/     ← médias, thèmes, fichiers
└── db/          ← base MySQL
```
