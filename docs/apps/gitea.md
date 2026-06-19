---
title: Gitea
description: Forge Git légère et auto-hébergée
published: true
date: 2026-06-19
---

# Gitea

Forge Git légère : dépôts, issues, pull requests, CI/CD (Gitea Actions), wiki — sans la lourdeur de GitLab.

## Installation

```bash
caleope install gitea --domain git.monserveur.fr
```

## Accès

- **Interface web** : `https://git.monserveur.fr`
- **SSH Git** : `git@git.monserveur.fr:2222` (port fixe 2222)
- **Login admin** : `gitea-admin`
- **Mot de passe admin** : généré à l'installation, affiché dans le terminal et dans `app-config/gitea/secrets.env`

> Le port SSH 2222 est ouvert dans UFW automatiquement. Configurer ton `~/.ssh/config` :
> ```
> Host git.monserveur.fr
>   Port 2222
> ```

## Cloner un dépôt

```bash
# HTTPS
git clone https://git.monserveur.fr/<user>/<repo>.git

# SSH
git clone ssh://git@git.monserveur.fr:2222/<user>/<repo>.git
```

## SSO Authentik

Si Authentik est installé, Gitea est configuré automatiquement avec OAuth2 Authentik. Les utilisateurs peuvent se connecter avec leur compte Authentik via le bouton "Connexion avec Authentik".

## Commandes utiles

```bash
caleope logs gitea
caleope backup gitea        # DB + dépôts
caleope restart gitea
```

## Structure des données

```
app-data/gitea/
├── git/         ← dépôts Git
├── conf/        ← configuration app.ini
├── data/        ← base SQLite, attachments
└── ssh/         ← clés SSH du serveur
```
