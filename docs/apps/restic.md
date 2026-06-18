---
title: Restic
description: Outil de sauvegarde incrémentale avec déduplication
published: true
date: 2026-06-18
---

# Restic

Outil de sauvegarde incrémentale et déduplication. Sauvegarde les données des applications Caleope vers un dépôt local ou distant (SFTP, S3, Backblaze B2…).

> Restic est un **outil système** — il n'a pas de container Docker. `caleope start/stop restic` ne s'applique pas.

## Installation

```bash
caleope install restic
```

Restic est installé directement sur l'hôte (`/usr/bin/restic`). Après installation, il est disponible comme backend de backup pour toutes les applications.

## Utilisation via Caleope

```bash
# Backup vers un dépôt local
caleope backup jellyfin --restic --repo /mnt/backup/caleope --password <pass>

# Backup vers un serveur SFTP
caleope backup nextcloud \
  --restic \
  --repo sftp:user@backup-host:/backups/caleope \
  --password <pass>

# Mot de passe via variable d'environnement
RESTIC_PASSWORD=<pass> caleope backup jellyfin --restic --repo /mnt/backup/caleope
```

Le daemon Caleope initialise le dépôt automatiquement (`restic init`) s'il n'existe pas encore.

## Commandes restic directes

Le daemon tourne en root — le dépôt Restic appartient à root. Utiliser `sudo` pour les commandes directes :

```bash
# Lister les snapshots
sudo sh -c 'RESTIC_PASSWORD=<pass> restic -r /mnt/backup/caleope snapshots'

# Restaurer un snapshot manuellement
sudo sh -c 'RESTIC_PASSWORD=<pass> restic -r /mnt/backup/caleope restore latest --target /'

# Vérifier l'intégrité du dépôt
sudo sh -c 'RESTIC_PASSWORD=<pass> restic -r /mnt/backup/caleope check'

# Nettoyer les anciennes sauvegardes (garder 7 dernières)
sudo sh -c 'RESTIC_PASSWORD=<pass> restic -r /mnt/backup/caleope forget --keep-last 7 --prune'
```

## Automatisation

```bash
# Cron — backup quotidien à 3h du matin
echo "0 3 * * * root RESTIC_PASSWORD=<pass> caleope backup jellyfin --restic --repo /mnt/backup/caleope" \
  >> /etc/cron.d/caleope-restic
```

## Dépôts supportés

| Type | Format |
|------|--------|
| Local | `/chemin/absolu` |
| SFTP | `sftp:user@host:/path` |
| S3 | `s3:s3.amazonaws.com/bucket` |
| Backblaze B2 | `b2:bucket-name:/path` |
| REST Server | `rest:http://host:8000/` |

→ [Documentation Restic](https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html)
