# Caleope — Distribution ISO

Objectif v0.7 : distribuer Caleope sous forme d'**ISO d'installation** (Debian 13
Trixie + Docker + Caleope), avec un **wizard de premier démarrage** (web ou CLI).

## Deux modes de distribution

### 0. Le plus simple — `make-iso.sh` (assistant guidé)
Un seul point d'entrée qui pose 3-4 questions (online/offline, version, canal,
registre/apps) puis appelle ce qu'il faut. Rien à retenir :
```bash
cd iso && ./make-iso.sh
```
Il vérifie/installe les prérequis, et pour l'offline il construit le bundle
(via `offline-builder`) avant d'assembler l'ISO. Les sections ci-dessous
décrivent les briques qu'il orchestre.

### 1. ISO online (ce dossier — `build.sh`)
ISO légère (~1-2 Go). Installe Debian 13 minimal + Caleope. Les **images Docker
des apps sont pull depuis le registre miroir Caleope** (`registry.gaiver-it.fr`, à
monter). Cible : serveurs avec accès internet (95 % des cas).

```bash
sudo apt install -y xorriso wget cpio gzip
CALEOPE_VERSION=v0.6.6 CHANNEL=stable ./build.sh
# → build/caleope-installer-v0.6.6.iso

# ISO online qui pull depuis le registre miroir plutôt que Docker Hub :
CALEOPE_REGISTRY=caleope-registry.gaiver-it.fr \
CALEOPE_REGISTRY_USER=caleope CALEOPE_REGISTRY_PASS=… ./build.sh
```

Ce que fait `build.sh` :
1. récupère la netinst Debian 13 ;
2. injecte `preseed.cfg` dans l'initrd (install 100 % auto) ;
3. assemble le **payload** au format bundle (voir plus bas) dans l'ISO ;
4. patche les bootloaders (BIOS+UEFI) pour booter direct sur l'install auto ;
5. repack une ISO hybride.

### Déroulé à l'installation (deux démarrages)
1. **d-i (auto, preseed)** — installe Debian minimal, copie le payload dans
   `/opt/caleope-install`, active `caleope-install.service`. *Rien de lourd
   n'est fait dans le chroot d-i* (pas de systemd/Docker fiables).
2. **1er boot (headless)** — `caleope-install.service` lance
   `install.sh --iso` : Docker + binaires + daemon + store, active le wizard,
   puis reboote.
3. **2e boot (console)** — `caleope-firstboot.service` lance le wizard
   `install.sh --iso-finalize` sur `tty1` : domaine, reverse proxy, e-mail,
   mots de passe → écrit la config, déploie Traefik + apps par défaut, se
   désactive et rend la main à la session de login.

### Layout du payload (`/caleope/` sur l'ISO)
```
binaries/{caleoped,caleope,caleope-ui}   binaires Linux amd64
store.tar.gz                             store (wrappé, --strip-components=1)
install.sh                               l'installeur (--iso / --iso-finalize)
caleope-completion.bash                  autocomplétion
caleope-install.service                  unité systemd du 1er boot
pack-info.json                           version, canal, registre, mode
images/*.tar                             (offline uniquement)
```
C'est le **même format** que `install.sh --offline <bundle>` : une ISO offline
n'est qu'un bundle (produit par `offline-builder/`) embarqué dans l'image.

> ⚠️ **À tester sur un hôte Linux** (xorriso). Points à valider au 1er build réel :
> chemins de l'initrd Trixie (`install.amd/initrd.gz`), boot UEFI (`boot/grub/efi.img`),
> et le flux des deux démarrages.

### 2. ISO offline — outil builder côté utilisateur (à venir : `offline-builder/`)
Plutôt qu'une énorme ISO « tout inclus » (~40 Go pour les 42 apps), un **binaire Go
multi-plateforme** que l'utilisateur lance sur un PC connecté :
- il **choisit ses apps** → l'outil pull **seulement ces images** (via
  `go-containerregistry`, sans dépendance Docker) → les `docker save`/injecte dans
  un template d'ISO offline → sort un `caleope-offline-<sélection>.iso` taillé juste.
- Chaque cible air-gapped a une ISO sur mesure, à jour, sans héberger 40 Go.

## Prérequis côté produit (pour que l'ISO soit complète)
- [x] **Registre miroir** debout (`docker-registry` sur CT113 `172.16.51.9`, public via NPM `caleope-registry.gaiver-it.fr`).
- [x] `install.sh` : mode **`--iso`** (base, payload local) + **`--iso-finalize`** (wizard). Réutilise la machinerie `--offline`.
- [x] **`caleope-firstboot.service`** + wizard CLI (`install.sh --iso-finalize` sur tty1). *Mode setup web : à venir (optionnel).*
- [x] `offline-builder/` : outil Go qui produit un bundle offline (images incluses) → ISO air-gap.
- [ ] **Test de build réel** sur hôte Linux (xorriso) — chemins initrd Trixie, boot UEFI, flux 2 boots.
- [ ] Corriger le store : `lscr.io/linuxserver/readarr:develop` n'existe plus (Readarr déprécié).
