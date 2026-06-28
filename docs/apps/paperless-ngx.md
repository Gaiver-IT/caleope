---
title: Paperless-ngx
description: Gestion de documents numérisés avec OCR
published: true
date: 2026-06-28
---

# Paperless-ngx

Système de gestion de documents auto-hébergé avec OCR automatique. Numérise, indexe et archive tes documents papier — contrats, factures, courriers — pour les retrouver instantanément.

## Installation

```bash
caleope install paperless-ngx --domain paperless.monserveur.fr
```

Les identifiants sont générés automatiquement et sauvegardés dans `app-config/paperless-ngx/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `PAPERLESS_ADMIN_USER` | Nom d'utilisateur admin | `admin` |
| `PAPERLESS_ADMIN_PASS` | Mot de passe admin | généré automatiquement |

```bash
caleope install paperless-ngx --domain paperless.monserveur.fr \
  --param PAPERLESS_ADMIN_USER=admin \
  --param PAPERLESS_ADMIN_PASS=monmotdepasse
```

## Accès

```
https://paperless.monserveur.fr/
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/paperless-ngx/secrets.env
```

## Importer des documents

Déposer les fichiers à numériser dans :

```
app-data/paperless-ngx/consume/
```

Paperless-ngx les importe, applique l'OCR et les indexe automatiquement.

## Commandes utiles

```bash
caleope logs paperless-ngx       # Logs
caleope restart paperless-ngx    # Redémarrer
caleope backup paperless-ngx     # Sauvegarder
```

## Structure des données

```
app-data/paperless-ngx/
├── data/       ← base de données et configuration
├── media/      ← documents archivés
├── consume/    ← dossier d'import automatique
└── export/     ← exports
```
