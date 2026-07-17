# Caleope — Distribution ISO

Objectif v0.7 : distribuer Caleope sous forme d'**ISO d'installation** (Debian 13
Trixie + Docker + Caleope), avec un **wizard de premier démarrage** (web ou CLI).

> ## ⚠️ Source de vérité — à lire avant tout build
>
> **`iso/` n'existe que sur la branche `main`.** Il n'est PAS sur `alpha` ni sur les
> branches de feature. Avant de builder :
> ```bash
> git fetch origin && git checkout origin/main -- iso/ install.sh
> ```
> Ne builde **jamais** depuis une copie de travail dont tu n'as pas vérifié la
> fraîcheur : un `iso/preseed.cfg` périmé produit une ISO qui *semble* correcte
> (elle boote, le menu s'affiche) mais qui **s'arrête à l'écran de partitionnement**
> et attend un humain. Vérification en une commande :
> ```bash
> git diff --stat origin/main -- iso/ install.sh    # doit être vide
> ```
> *Incident 16/07 : une ISO buildée depuis un dossier figé au 10/07 embarquait un
> preseed antérieur à v0.7.6 (le commit `68f8605` du 11/07, qui ajoute le choix
> automatique du disque). Raté d'une journée. Deux boot-tests à l'aveugle et un
> soupçon infondé sur la version de Debian avant d'ouvrir la console et de voir
> l'écran « Méthode de partitionnement » qui attendait `<Entrée>`.*

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
   `install.sh --iso` : Docker + binaires + daemon + store, active le setup
   web, puis se désactive (`Removed .../caleope-install.service`) et reboote.
3. **2e boot — setup WEB sur `:8766`** (`Caleope — Premier démarrage`), à faire
   depuis un autre poste. `caleope-banner.service` affiche l'URL sur la console.
   Le formulaire couvre domaine, mode proxy, e-mail, mots de passe UI/secrets
   **et les disques de données** (`/setup/disks` liste les disques hors système
   → aucun/simple/RAID1 + ext4|btrfs|xfs). `POST /setup` répond
   `{"status":"started"}` et travaille en détaché ; `GET /setup/status` renvoie
   `{"firstboot":false}` une fois fini. Le mot de passe UI devient **aussi** le
   mot de passe console de `user-caleope`.

> Le wizard **console** sur tty1 (`caleope-firstboot.service` /
> `install.sh --iso-finalize`) n'existe plus : il a été remplacé par le setup web
> (commit `a477109`). L'unité `caleope-firstboot.service` est absente d'une
> install à jour — si tu la cherches, c'est normal de ne pas la trouver.

**Vérifié de bout en bout le 17/07** sur une install ISO réelle (3 disques) :
install auto ~4 min 25 sans une seule question, OS sur le SSD, `sdb`/`sdc`
laissés vierges puis assemblés par le wizard en `md0 : active raid1 [2/2] [UU]`
monté sur `app-data`, puis Authentik/CrowdSec/Traefik `healthy`.

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

## Contrôle qualité de l'ISO produite

Une ISO qui se construit sans erreur n'est pas une ISO qui marche. Ces quatre
contrôles se font **sans booter** et attrapent l'essentiel :

```bash
ISO=build/caleope-installer-vX.Y.Z.iso

# 1. Le preseed réellement embarqué est-il le bon ? (le piège n°1)
xorriso -osirrox on -indev $ISO -extract /install.amd/initrd.gz /tmp/i.gz
mkdir -p /tmp/ir && (cd /tmp/ir && gunzip -c /tmp/i.gz | cpio -idm --quiet)
diff iso/preseed.cfg /tmp/ir/preseed.cfg && echo "preseed OK"
grep -c partman/early_command /tmp/ir/preseed.cfg   # doit être ≥ 1, sinon install NON auto

# 2. Le payload contient-il les bons binaires ?
xorriso -osirrox on -indev $ISO -extract /caleope /tmp/pl
md5sum /tmp/pl/binaries/*                            # à comparer aux binaires attendus
cat /tmp/pl/pack-info.json                           # version + canal

# 3. Le store embarqué a-t-il les apps attendues ?
tar tzf /tmp/pl/store.tar.gz | grep -c '^[^/]*/apps/'

# 4. L'ISO est-elle réellement amorçable BIOS **et** UEFI ?
fdisk -l $ISO | tail -3     # doit montrer une partition bootable (*) + une partition EFI (type ef)
```

## Pannes connues (et pourquoi elles sont sournoises)

| Symptôme | Cause | Correctif |
|---|---|---|
| L'install s'arrête sur « **[!!] Partitionner les disques — Méthode de partitionnement** » | preseed sans `partman/early_command` → d-i ne sait pas choisir parmi plusieurs disques et retombe sur le menu. Sur une machine **mono-disque ça passe quand même** : le bug ne se voit qu'en multi-disques. | preseed ≥ v0.7.6 (`git checkout origin/main -- iso/`) |
| L'ISO se construit mais n'amorce pas en BIOS | `/usr/lib/ISOLINUX/isohdpfx.bin` absent → l'`xorriso` final échoue, **mais `build.sh` masque l'erreur avec `2>/dev/null`** | `apt install -y isolinux syslinux-common` |
| `caleope-completion.bash` fait 0 octet dans l'ISO | il est `wget` depuis le **tag** `${CALEOPE_VERSION}` ; si le tag n'existe pas encore (build pré-release), 404 → le `\|\| true` laisse un fichier vide | builder depuis un tag existant, ou ignorer (cosmétique) |
| La version affichée n'est pas celle de `version.go` | le Makefile injecte `git describe --tags --abbrev=0`, pas `version.go` | poser le tag avant de builder |

**Le fil rouge : `2>/dev/null` et `|| true`.** Trois des quatre pannes ci-dessus sont
des erreurs réelles converties en silence. Quand un build « réussit » mais que le
résultat est faux, cherche d'abord ce qui a été mis en sourdine.

> Piège d'outillage, hors ISO mais de la même famille : en **zsh**, `$var:a` applique
> le modificateur `:a` (chemin absolu). Un `git show "origin/$b:apps/x.yml"` devient
> `origin//tmp/mainpps/x.yml` → erreur fatale, et avec `2>/dev/null` un joli `0` bien
> trompeur. Utiliser `origin/${b}:apps/x.yml`.

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
