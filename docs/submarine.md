---
title: Mode submarine (installation hors-ligne)
description: Installer et mettre à jour Caleope sans accès internet, depuis un support amovible
published: true
date: 2026-06-22
---

# Mode submarine

Le **mode submarine** permet d'installer et de mettre à jour Caleope **sans aucun accès internet**.  
Il est conçu pour les réseaux isolés (air-gap), les environnements tactiques ou militaires, les activistes et les personnes fonctionnant en autonomie totale.

Le principe : sur une machine connectée, tu crées un **bundle** (un répertoire autonome contenant les binaires, le store et les images Docker). Tu copies ce bundle sur une clé USB ou un disque externe, puis tu l'utilises pour installer ou mettre à jour n'importe quel serveur hors-ligne.

---

## Vue d'ensemble

```
Machine connectée                         Serveur hors-ligne
─────────────────                         ──────────────────
caleope offline-pack /media/usb/    →    bash install.sh --offline /media/usb/caleope-bundle-YYYY-MM-DD
                                    →    caleope offline-update /media/usb/caleope-bundle-YYYY-MM-DD
```

---

## Créer un bundle (sur une machine connectée)

```bash
caleope offline-pack /media/usb/
```

Crée automatiquement un répertoire `caleope-bundle-YYYY-MM-DD/` à l'emplacement spécifié.

### Structure du bundle

```
caleope-bundle-2026-06-22/
├── binaries/
│   ├── caleoped              # daemon
│   ├── caleope               # CLI
│   └── caleope-ui            # interface web
├── store.tar.gz              # catalogue complet des apps (sans .git)
├── images/
│   ├── traefik_v3.0.tar      # images Docker sauvegardées
│   ├── portainer_ce.tar
│   ├── jellyfin_latest.tar
│   └── ...
├── caleope-completion.bash   # autocomplétion bash (si présent)
└── pack-info.json            # métadonnées (version, date, architecture)
```

### Contenu de `pack-info.json`

```json
{
  "caleope_version": "v0.4.18",
  "packed_at": "2026-06-22 14:30:00",
  "arch": "x86_64",
  "hostname": "serveur-principal",
  "bundle_dir": "/media/usb/caleope-bundle-2026-06-22"
}
```

> Le bundle contient toutes les images Docker actuellement présentes sur la machine source. Assure-toi que les apps que tu veux déployer hors-ligne sont bien installées sur la machine source avant de créer le bundle.

---

## Installer depuis un bundle

### Prérequis

- Debian 12 ou Ubuntu 22.04+
- **Docker doit être installé** sur le serveur cible avant de lancer l'installation offline  
  *(Docker nécessite un dépôt réseau — installe-le via un dépôt local ou dpkg si nécessaire)*
- Le bundle copié sur un support accessible (`/media/usb/`, `/mnt/bundle/`, etc.)

### Installation

```bash
sudo bash install.sh --offline /media/usb/caleope-bundle-2026-06-22
```

L'installateur détecte le mode submarine et :

1. Valide le bundle (binaires + store.tar.gz requis)
2. Charge les images Docker depuis `images/*.tar`
3. Copie les binaires depuis `binaries/`
4. Extrait le store depuis `store.tar.gz`
5. Configure les services systemd normalement
6. Saute `apt-get update` et tout accès réseau

> L'installation interactive (domaine, mode proxy) fonctionne normalement en mode offline.  
> Pour une installation 100% non-interactive :

```bash
CALEOPE_DOMAIN=caleope.home.local \
CALEOPE_PROXY_MODE=standalone \
CALEOPE_CHANNEL=stable \
sudo bash install.sh --offline /media/usb/caleope-bundle-2026-06-22
```

### Options de mode proxy en hors-ligne

| Mode | Usage |
|------|-------|
| `standalone` | Réseau local, HTTP uniquement — recommandé pour le hors-ligne |
| `npm` | Derrière un reverse proxy existant sur le réseau local |
| `traefik` | HTTPS avec Let's Encrypt — nécessite une résolution DNS, déconseillé hors-ligne |

---

## Mettre à jour une installation existante

```bash
caleope offline-update /media/usb/caleope-bundle-2026-06-22
```

Met à jour :
- Les binaires (`caleoped`, `caleope`, `caleope-ui`)
- Le store (catalogue d'apps)
- Les images Docker (charge les nouvelles versions)

Puis redémarre les services :

```bash
sudo systemctl restart caleoped caleope-ui
```

---

## Workflow complet

### Première installation

```bash
# 1. Sur la machine connectée — créer le bundle
caleope offline-pack /media/usb/

# 2. Transférer la clé USB sur le serveur hors-ligne

# 3. Sur le serveur hors-ligne — installer Docker si nécessaire
# (via dépôt local, dpkg, ou réseau temporaire)

# 4. Lancer l'installation
sudo bash /media/usb/caleope-bundle-2026-06-22/install.sh \
    --offline /media/usb/caleope-bundle-2026-06-22

# (optionnel) copier install.sh depuis le bundle directement
sudo bash install.sh --offline /media/usb/caleope-bundle-2026-06-22
```

> L'`install.sh` n'est pas inclus automatiquement dans le bundle — utilise le script local ou copie-le manuellement dans le répertoire bundle.

### Mise à jour périodique

```bash
# 1. Sur la machine connectée — mettre à jour Caleope
caleope upgrade

# 2. Re-créer un bundle à jour
caleope offline-pack /media/usb/

# 3. Sur le serveur hors-ligne — appliquer
caleope offline-update /media/usb/caleope-bundle-2026-06-22-nouveau

# 4. Redémarrer
sudo systemctl restart caleoped caleope-ui
```

---

## Installer des apps hors-ligne

Une fois Caleope installé, les apps dont les images sont dans le bundle sont directement installables :

```bash
caleope list          # voir ce qui est disponible
caleope install jellyfin --domain media.local
```

Si une image n'est pas dans le bundle, `caleope install` essaiera de la télécharger — ce qui échouera sans réseau.  
**Solution :** ajouter l'app sur la machine connectée avant de créer le bundle :

```bash
# Sur la machine connectée :
docker pull jellyfin/jellyfin:latest
caleope offline-pack /media/usb/
```

---

## Référence des commandes

| Commande | Description |
|----------|-------------|
| `caleope offline-pack <dest>` | Créer un bundle dans `<dest>/caleope-bundle-YYYY-MM-DD/` |
| `caleope offline-update <bundle>` | Appliquer un bundle sur une installation existante |
| `bash install.sh --offline <bundle>` | Installer Caleope depuis un bundle (sans internet) |
| `bash install.sh --offline <bundle> --debug` | Même chose avec logs détaillés |
