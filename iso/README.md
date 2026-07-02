# Caleope — Distribution ISO

Objectif v0.7 : distribuer Caleope sous forme d'**ISO d'installation** (Debian 13
Trixie + Docker + Caleope), avec un **wizard de premier démarrage** (web ou CLI).

## Deux modes de distribution

### 1. ISO online (ce dossier — `build.sh`)
ISO légère (~1-2 Go). Installe Debian 13 minimal + Caleope. Les **images Docker
des apps sont pull depuis le registre miroir Caleope** (`registry.gaiver-it.fr`, à
monter). Cible : serveurs avec accès internet (95 % des cas).

```bash
sudo apt install -y xorriso wget cpio gzip
CALEOPE_VERSION=v0.6.3 ./build.sh
# → build/caleope-installer-v0.6.3.iso
```

Ce que fait `build.sh` :
1. récupère la netinst Debian 13 ;
2. injecte `preseed.cfg` dans l'initrd (install 100 % auto) ;
3. copie le payload Caleope (binaires + `install.sh` + cache store) dans l'ISO ;
4. patche les bootloaders (BIOS+UEFI) pour booter direct sur l'install auto ;
5. repack une ISO hybride.

> ⚠️ **À tester sur un hôte Linux** (xorriso). Points à valider au 1er build réel :
> chemins de l'initrd Trixie (`install.amd/initrd.gz`), boot UEFI (`boot/grub/efi.img`),
> et le `late_command` (mode `install.sh --iso`, qui reste à implémenter côté install.sh).

### 2. ISO offline — outil builder côté utilisateur (à venir : `offline-builder/`)
Plutôt qu'une énorme ISO « tout inclus » (~40 Go pour les 42 apps), un **binaire Go
multi-plateforme** que l'utilisateur lance sur un PC connecté :
- il **choisit ses apps** → l'outil pull **seulement ces images** (via
  `go-containerregistry`, sans dépendance Docker) → les `docker save`/injecte dans
  un template d'ISO offline → sort un `caleope-offline-<sélection>.iso` taillé juste.
- Chaque cible air-gapped a une ISO sur mesure, à jour, sans héberger 40 Go.

## Prérequis côté produit (pour que l'ISO soit complète)
- [ ] **Registre miroir** debout (`registry:2` sur host dédié ~50 Go — mesuré : ~25 Go compressé).
- [ ] `install.sh` : supporter le **mode `--iso`** (offline : binaires/store depuis le payload local, pas de `curl | bash`).
- [ ] **`caleope-firstboot.service`** + wizard : web (`caleope-ui` mode setup) et CLI (`caleope setup`).
- [ ] Corriger le store : `lscr.io/linuxserver/readarr:develop` n'existe plus (Readarr déprécié).
