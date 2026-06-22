---
title: Test du mode submarine (protocole)
description: Protocole de test complet pour valider l'installation offline Caleope
published: true
date: 2026-06-22
---

# Protocole de test — Mode submarine

Ce document décrit les étapes à suivre pour valider le mode submarine (installation et mise à jour hors-ligne).  
Il couvre deux scénarios : test en VM isolée et test sur réseau air-gap réel.

---

## Prérequis

- **Machine source** : serveur Caleope installé et fonctionnel (accès internet)
- **Machine cible** : VM ou serveur Debian 12 vierge (sans accès internet)
- **Support** : clé USB ou répertoire partagé simulé

---

## Scénario A — Test rapide en VM

### Étape 1 : Créer le bundle sur la machine source

```bash
# S'assurer que Caleope est installé et opérationnel
caleope ping

# Créer le bundle (remplacer /tmp/submarine par le chemin de la clé USB)
caleope offline-pack /tmp/submarine

# Vérifier le contenu du bundle
ls -lh /tmp/submarine/caleope-bundle-*/
# Attendu :
#   binaries/caleoped      (binaire daemon)
#   binaries/caleope       (binaire CLI)
#   binaries/caleope-ui    (binaire UI)
#   store.tar.gz           (catalogue des apps)
#   images/                (images Docker — peut être volumineux)
#   pack-info.json         (métadonnées)

# Vérifier le pack-info
cat /tmp/submarine/caleope-bundle-*/pack-info.json
```

**Résultat attendu** : répertoire bundle créé avec tous les fichiers listés, `pack-info.json` contient la version et la date.

### Étape 2 : Préparer la machine cible (VM isolée)

```bash
# Sur la VM cible (Debian 12 vierge) :
# Installer Docker AVANT de couper le réseau (Docker nécessite un dépôt internet)
apt-get update && apt-get install -y docker-ce docker-ce-cli containerd.io

# Vérifier Docker
docker --version   # → Docker version 26.x.x

# Copier le bundle sur la VM (via scp ou montage USB simulé)
scp -r /tmp/submarine/caleope-bundle-YYYY-MM-DD/ user@vm-cible:/tmp/bundle/

# COUPER LE RÉSEAU sur la VM (simuler l'air-gap)
# Via la VM : désactiver l'interface réseau dans les paramètres hyperviseur
# Ou : ip link set ens18 down  (pour tester seulement)
```

### Étape 3 : Installer depuis le bundle

```bash
# Sur la VM cible, sans réseau :

# Télécharger install.sh AVANT de couper le réseau, ou utiliser celui du bundle si présent
# Alternative : copier install.sh depuis la machine source
scp install.sh user@vm-cible:/tmp/

# Lancer l'installation offline
sudo bash /tmp/install.sh --offline /tmp/bundle/caleope-bundle-YYYY-MM-DD

# Réponses attendues aux questions interactives :
#   Domaine : test.local
#   Mode proxy : standalone
#   Canal : stable
```

**Points de vérification pendant l'installation :**

| Étape | Message attendu |
|-------|----------------|
| Validation bundle | `✔ Bundle valide — version vX.X.X` |
| Prérequis | `⚠ Mode submarine : apt-get update ignoré` |
| Docker | `⚠ Docker déjà installé` |
| Chargement images | `✔ N image(s) Docker chargée(s) depuis le bundle` |
| Binaires | `✔ Binaires installés depuis le bundle (version vX.X.X)` |
| Store | `✔ Store extrait — N application(s) disponible(s)` |
| Services | `✔ Daemon actif` |

### Étape 4 : Vérifier l'installation

```bash
# Sur la VM cible :
caleope ping
# → ✓ Daemon actif — version vX.X.X

caleope store
# → liste des apps du bundle (depuis store.tar.gz)

# Vérifier que les images sont bien présentes
docker images | grep -E "traefik|portainer"
# → les images core doivent être listées

# Tester l'interface web
curl -s http://localhost:8766/ui/ping
# → {"status":"ok"}
```

### Étape 5 : Installer une app depuis le bundle

```bash
# L'app doit avoir son image dans le bundle
caleope install jellyfin --domain media.test.local

# Vérifier que Docker n'a pas essayé de télécharger l'image
# (docker pull ne doit PAS être appelé si l'image est déjà chargée)
journalctl -u caleoped --since "1 min ago" | grep -i "pull\|download"
# → aucune ligne ne doit mentionner un pull réseau
```

---

## Scénario B — Test de mise à jour offline

```bash
# Depuis la machine source, créer un nouveau bundle
# (après une mise à jour de Caleope)
caleope upgrade
caleope offline-pack /tmp/submarine-v2/

# Transférer sur la machine cible

# Sur la machine cible :
caleope offline-update /tmp/submarine-v2/caleope-bundle-YYYY-MM-DD

# Vérifier
caleope version   # → nouvelle version
caleope ping      # → daemon actif
sudo systemctl restart caleoped caleope-ui
```

---

## Vérifications post-installation

```bash
# Services systemd actifs
systemctl is-active caleoped caleope-ui docker traefik portainer
# → active (x5)

# Port UI accessible
ss -tlnp | grep 8766
# → LISTEN 0 ... :8766

# Pas de requêtes réseau sortantes
# (avec tcpdump ou ss si réseau coupé)
ss -tn | grep -v "127.0.0.1\|::1"
# → uniquement connexions locales
```

---

## Cas d'erreur connus

| Erreur | Cause | Solution |
|--------|-------|----------|
| `Docker n'est pas installé` | Docker absent sur la cible | Installer Docker avant de couper le réseau |
| `Fichier manquant dans le bundle : binaries/caleoped` | Bundle incomplet | Recréer le bundle sur une machine avec Caleope installé |
| `Échec du chargement de <image>` | Fichier .tar corrompu | Re-créer le bundle, vérifier l'espace disque |
| `store.tar.gz : tar: Error` | Archive corrompue | Re-créer le bundle (`caleope offline-pack`) |
| App refuse de démarrer | Image non chargée | `docker load -i bundle/images/<app>.tar` manuellement |

---

## Structure du bundle attendue (référence)

```
caleope-bundle-2026-06-22/
├── binaries/
│   ├── caleoped          ← daemon (obligatoire)
│   ├── caleope           ← CLI (obligatoire)
│   └── caleope-ui        ← interface web (optionnel)
├── store.tar.gz          ← catalogue apps (obligatoire)
├── images/
│   ├── traefik_v3.0.tar
│   ├── portainer_ce.tar
│   └── *.tar             ← une image par app installée
├── caleope-completion.bash  ← autocomplétion (optionnel)
└── pack-info.json        ← métadonnées (recommandé)
```

---

## Checklist de validation

```
[ ] Bundle créé sans erreur sur la machine source
[ ] pack-info.json présent et lisible
[ ] Binaires présents et exécutables
[ ] store.tar.gz non vide (> 1 Mo)
[ ] images/*.tar présents pour les apps core (traefik, portainer)
[ ] Installation sur VM cible sans accès internet
[ ] Message "apt-get update ignoré" affiché
[ ] Message "images chargées depuis le bundle" affiché
[ ] caleope ping répond après installation
[ ] caleope store liste les apps
[ ] Interface web accessible sur :8766
[ ] Au moins une app installable sans réseau
```
