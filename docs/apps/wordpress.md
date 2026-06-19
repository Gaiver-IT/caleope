---
title: WordPress
description: CMS et plateforme de publication la plus utilisée au monde
published: true
date: 2026-06-19
---

# WordPress

CMS open-source — blog, site vitrine, e-commerce, portfolio. Extensible via des milliers de plugins et thèmes.

## Installation

```bash
caleope install wordpress --domain site.monserveur.fr
```

## Accès

- **Site public** : `https://site.monserveur.fr`
- **Admin** : `https://site.monserveur.fr/wp-admin/`
- **Login** : `admin`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/wordpress/secrets.env`

## Configuration email

WordPress nécessite un plugin SMTP pour envoyer des emails (confirmation inscription, reset mot de passe) :

1. Installer le plugin **WP Mail SMTP** ou **FluentSMTP**
2. Configurer avec le SMTP global Caleope (voir `app-config/wordpress/secrets.env` pour les variables)

## Commandes utiles

```bash
caleope logs wordpress
caleope backup wordpress        # DB + fichiers wp-content/
caleope restart wordpress
```

## Structure des données

```
app-data/wordpress/
├── html/        ← fichiers WordPress (wp-content/, plugins, thèmes)
└── db/          ← base MySQL
```
