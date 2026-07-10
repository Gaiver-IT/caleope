---
title: Guide utilisateur
description: Référence complète des commandes Caleope
published: true
date: 2026-06-18
---

# Guide utilisateur

## Commandes essentielles

| Commande | Description |
|----------|-------------|
| `caleope ping` | Vérifier que le daemon est actif |
| `caleope version` | Afficher la version installée |
| `caleope list` | Lister les applications installées |
| `caleope top` | Supervision live (CPU, RAM, état) |
| `caleope search <terme>` | Rechercher une app dans le store |
| `caleope upgrade` | Mettre à jour Caleope |
| `caleope update` | Synchroniser le catalogue d'apps |

---

## Gérer les applications

### Installer

```bash
caleope install jellyfin
# Domaine personnalisé :
caleope install jellyfin --domain media.mon-domaine.fr
# Données sur NAS :
caleope install jellyfin --storage mon-nas
# Paramètre interactif (ex: apps avec config requise) :
caleope install pterodactyl-wings --param NODE_FQDN=1.2.3.4
# Activer le passthrough GPU (si l'app le supporte) :
caleope install jellyfin --gpu
# Variable d'environnement équivalente à --param :
CALEOPE_PARAM_NODE_FQDN=1.2.3.4 caleope install pterodactyl-wings
```

> Si l'application génère des identifiants (Nextcloud, Grafana…), ils sont affichés à la fin de l'installation et sauvegardés dans `/opt/gaiver-it/caleope/app-config/<app>/secrets.env`.

### Lister

```bash
caleope list

# APP                  ÉTAT          PORT   DOMAINE
# jellyfin             ● actif       8234   media.home.local
# nextcloud            ● actif       8456   cloud.home.local

caleope list --json   # sortie JSON
```

### Démarrer / Arrêter / Redémarrer

```bash
caleope stop jellyfin
caleope start jellyfin
caleope restart jellyfin
```

### Informations et logs

```bash
caleope info jellyfin          # état, port, domaine, date, taille
caleope logs jellyfin          # 100 dernières lignes
caleope logs jellyfin --tail 50
```

### Désinstaller

```bash
caleope remove jellyfin             # supprime l'app ET les données
caleope remove jellyfin --keep-data # supprime l'app, conserve les données
```

### Rechercher dans le store

```bash
caleope search media    # → jellyfin, plex...
caleope search cloud    # → nextcloud...
```

---

## Sauvegardes

### Backup local (tar)

```bash
# Créer une sauvegarde
caleope backup nextcloud

# Lister les sauvegardes disponibles
caleope backups nextcloud
# TIMESTAMP             DATA   CONFIG   VERSION
# 2026-05-23T12-09-14   ✓      ✓        v0.4.7

# Restaurer la plus récente
caleope restore nextcloud

# Restaurer un backup précis
caleope restore nextcloud --backup 2026-05-23T12-09-14
```

Les sauvegardes sont stockées dans `/opt/gaiver-it/caleope/backups/<app>/<timestamp>/`.

> **Astuce** — automatiser avec cron :
> ```bash
> echo "0 3 * * * root caleope backup nextcloud" >> /etc/cron.d/caleope-backup
> ```

### Backup Restic (déduplication)

[Restic](https://restic.net) est un outil de backup incrémental avec déduplication. Il permet des sauvegardes vers un dépôt distant (SFTP, S3, Backblaze…) en ne transférant que les blocs modifiés.

```bash
# Prérequis : installer restic
caleope install restic

# Backup vers un dépôt local (test)
caleope backup jellyfin --restic --repo /mnt/backup/caleope --password <pass>

# Backup vers un serveur SFTP
caleope backup jellyfin \
  --restic \
  --repo sftp:user@backup-host:/backups/caleope \
  --password <pass>

# Lister les snapshots existants
sudo sh -c 'RESTIC_PASSWORD=<pass> restic -r /mnt/backup/caleope snapshots'
```

> Le daemon `caleoped` tourne en root — le dépôt Restic appartient à root. Utiliser `sudo` pour les commandes `restic` directes.

> Le mot de passe peut aussi être transmis via `RESTIC_PASSWORD` dans le shell appelant :
> ```bash
> RESTIC_PASSWORD=<pass> caleope backup jellyfin --restic --repo /mnt/backup/caleope
> ```

---

## Supervision

### Temps réel

```bash
caleope top

# Caleope — Supervision        18:42:15    Ctrl+C pour quitter
# Système   RAM 1842/3941 MB (46%)   Disk 12.5/50.0 GB (25%)
#
# APP                  ÉTAT          CPU      RAM
# jellyfin             ● actif       1.2%     256 MB
# nextcloud            ● actif       0.5%     512 MB

caleope top --advanced  # affiche aussi disk et port
```

### Grafana (graphiques historiques)

```bash
caleope install prometheus-grafana --domain metrics.mon-domaine.fr
```

Le dashboard **Caleope Overview** est préconfiguré avec CPU, RAM, disque et état de toutes les apps.

---

## Historique des événements

```bash
caleope events                      # 50 derniers événements
caleope events --limit 20
caleope events --app nextcloud      # filtrer par app
caleope events --type app_stopped   # filtrer par type
```

**Types disponibles :** `app_installed`, `app_removed`, `app_started`, `app_stopped`, `app_restarted`, `backup_created`, `backup_restored`

---

## Emplacements réseau (NAS)

### Ajouter un NAS

```bash
# Partage SMB/CIFS
caleope location add mon-nas \
  --type smb \
  --host 192.168.1.10 \
  --share nas \
  --user admin

# Serveur SFTP
caleope location add backup-sftp \
  --type sftp \
  --host backup.mon-domaine.fr \
  --share /data/backups \
  --user deploy
```

Après l'ajout, Caleope monte automatiquement le partage et crée la structure `caleope/app-data/` sur le NAS.

### Gérer les emplacements

```bash
caleope location list           # lister
caleope location mount mon-nas  # monter
caleope location unmount mon-nas
caleope location remove mon-nas
```

### Stocker les données d'une app sur le NAS

```bash
# À l'installation
caleope install jellyfin --storage mon-nas

# Migrer une app existante
caleope location storage jellyfin mon-nas

# Rapatrier en local
caleope location storage jellyfin local

# Vérifier le stockage actuel
caleope location storage jellyfin
# 💾 'jellyfin' : NAS 'mon-nas'
```

---

## GPU — Passthrough matériel

Certaines applications (Jellyfin…) peuvent utiliser le GPU de l'hôte pour l'accélération matérielle du transcodage vidéo.

```bash
caleope install jellyfin --gpu
```

Caleope détecte automatiquement le type de GPU :
- **NVIDIA** — injecte `deploy.resources.reservations.devices` (driver nvidia)
- **Intel / AMD** — monte `/dev/dri` avec les bons groupes système

Un fichier `compose.override.yml` est généré dans le répertoire de l'app. Il est chargé automatiquement à chaque `docker compose up`.

> L'option `--gpu` n'a effet que si l'app déclare `"gpu": true` dans son `app.json`. Consulter la page de chaque app pour vérifier.

Apps avec support GPU : [Jellyfin](/docs/apps/jellyfin)

---

## Outils système

Certains paquets du store n'ont pas de container Docker — ils installent un outil directement sur le système hôte.

```bash
caleope install restic   # installe le binaire restic
caleope list             # apparaît dans la liste avec port "-"
```

Ces apps sont visibles dans `caleope list` mais `caleope start/stop` ne s'applique pas.

→ [Restic](/docs/apps/restic)

---

## Mode submarine (hors-ligne)

Le mode submarine permet d'installer ou mettre à jour Caleope sans accès internet.

### Créer un bundle

```bash
caleope offline-pack /media/usb/
# → crée /media/usb/caleope-bundle-2026-06-22/ avec binaires, store et images Docker
```

### Mettre à jour une installation existante

```bash
caleope offline-update /media/usb/caleope-bundle-2026-06-22
sudo systemctl restart caleoped caleope-ui
```

| Commande | Description |
|----------|-------------|
| `caleope offline-pack <dest>` | Créer un bundle d'installation offline |
| `caleope offline-update <bundle>` | Appliquer un bundle sur une installation existante |

→ [Documentation complète du mode submarine](/docs/submarine)

---

## API REST

L'API REST écoute sur le port **8765**.

```bash
# Récupérer le token
caleope token

# Utilisation
TOKEN=$(caleope token | grep Token | awk '{print $NF}')
curl -H "Authorization: Bearer $TOKEN" http://localhost:8765/api/v1/apps
```

> **Rate limiting** : 60 requêtes par minute par adresse IP. Au-delà, l'API répond `429 Too Many Requests` avec le header `Retry-After: 60`.

→ [Référence complète de l'API](/docs/api)
