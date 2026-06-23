---
title: Authentik
description: Gestionnaire d'identités et SSO open-source
published: true
date: 2026-06-19
---

# Authentik

Gestionnaire d'identités open-source (SSO/OIDC/LDAP). Centralise l'authentification de toutes tes apps Caleope avec un seul compte.

## Installation

```bash
caleope install authentik --domain auth.monserveur.fr
```

> Installer Authentik **avant** les autres apps pour que le SSO soit configuré automatiquement pendant leur installation.

## Accès

| Interface | URL |
|-----------|-----|
| Admin | `https://auth.monserveur.fr/if/admin/` |
| Login utilisateur | `https://auth.monserveur.fr/` |

- **Login** : `akadmin`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/authentik/secrets.env`

> Authentik met 1–2 minutes à initialiser sa base de données au premier démarrage.

## SSO avec les autres apps

Authentik configure automatiquement le SSO pour les apps installées après lui. Le middleware Traefik `authentik@docker` est disponible pour protéger n'importe quelle app.

### Apps avec intégration SSO automatique

| App | Type d'intégration |
|-----|--------------------|
| **Jellyfin** | OIDC natif (plugin SSO) |
| **Nextcloud** | OIDC via Social Login |
| **Gitea** | OAuth2 |
| **Grafana** | OAuth2 |

### Protéger une app manuellement

Pour ajouter la protection Authentik à une app déjà installée, ajouter dans son `app-config/<app>/secrets.env` :

```bash
CALEOPE_AUTH_MIDDLEWARE=authentik@docker
```

Puis redémarrer : `caleope restart <app>`

## Commandes utiles

```bash
caleope logs authentik          # Logs du serveur Authentik
caleope restart authentik       # Redémarrer
caleope backup authentik        # Sauvegarder (DB + config)
```

## Structure des données

```
app-data/authentik/
├── db/          ← base PostgreSQL
├── media/       ← avatars, fichiers uploadés
├── redis/       ← cache sessions
└── custom-templates/
```
