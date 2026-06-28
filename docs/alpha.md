---
title: Canal Alpha
description: Installer et utiliser le canal alpha de Caleope (Brownberry)
published: true
date: 2026-06-28
---

# Canal Alpha (Brownberry)

Le canal **alpha** donne accès aux dernières fonctionnalités avant leur passage en stable. Version actuelle : **v0.5.x (Brownberry)**.

> Le canal alpha peut contenir des bugs. Recommandé pour les tests ou les environnements non-critiques.

---

## Installer Caleope en mode alpha

### Installation interactive

```bash
curl -fsSL https://raw.githubusercontent.com/Gaiver-IT/caleope/alpha/install.sh | bash
# → choisir "2) Alpha" quand demandé
```

### Installation non-interactive

```bash
CALEOPE_DOMAIN=home.local \
CALEOPE_PROXY_MODE=standalone \
CALEOPE_CHANNEL=alpha \
bash <(curl -fsSL https://raw.githubusercontent.com/Gaiver-IT/caleope/alpha/install.sh)
```

**Modes proxy disponibles :**

| Mode | Usage |
|------|-------|
| `standalone` | LAN/hors-ligne, HTTP uniquement, pas de Let's Encrypt |
| `npm` | Derrière NPM/Caddy existant |
| `traefik` | HTTPS automatique avec Let's Encrypt |

---

## Mettre à jour le store en alpha

```bash
# Synchroniser le catalogue d'apps (branche alpha)
sudo git -C /opt/gaiver-it/caleope/core/cache/official fetch origin alpha
sudo git -C /opt/gaiver-it/caleope/core/cache/official reset --hard origin/alpha

# Puis mettre à jour le catalogue côté daemon
caleope update
```

> `sudo` requis car le cache est owned par root.

---

## Fonctionnalités Brownberry (v0.5.x)

### Ports dynamiques

Les ports des applications sont alloués dynamiquement par Caleope à l'installation (aucune configuration manuelle requise). Le port alloué est visible dans `caleope list`.

```bash
caleope list
# → jellyfin   ✅ actif   8001   official
```

### Passthrough GPU

```bash
# Installer une app avec accélération GPU (NVIDIA ou Intel auto-détecté)
caleope install jellyfin --gpu

# Vérifier l'overlay généré
cat /opt/gaiver-it/caleope/apps-installed/jellyfin/compose.override.yml
```

### Backup Restic (déduplication)

```bash
# Prérequis : installer restic
caleope install restic

# Backup vers dépôt local
caleope backup jellyfin --restic --repo /mnt/backup/caleope --password <pass>

# Backup vers SFTP
caleope backup jellyfin --restic \
  --repo sftp:user@backup-host:/backups/caleope \
  --password <pass>

# Voir les snapshots (dépôt owned root → sudo)
sudo sh -c 'RESTIC_PASSWORD=<pass> restic -r /mnt/backup/caleope snapshots'
```

### Mode standalone (LAN/hors-ligne)

Installe Caleope sans Let's Encrypt — idéal pour un réseau local sans DNS public :

```bash
CALEOPE_PROXY_MODE=standalone bash install.sh
```

Traefik écoute en HTTP pur sur le port 80, pas de certificats requis.

### Rate limiting API

L'API REST (`:8765`) est limitée à **60 requêtes/minute par IP**. Au-delà :

```
HTTP 429 Too Many Requests
Retry-After: 60
```

### Secure headers HTTP

Traefik applique automatiquement des headers de sécurité sur toutes les réponses :
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Strict-Transport-Security` (HTTPS uniquement)
- Suppression de `Server:` et `X-Powered-By:`

### Suppression d'application (`caleope remove`)

```bash
# Avec confirmation
caleope remove jellyfin

# Sans confirmation (scripts/CI)
caleope remove jellyfin -y
caleope remove jellyfin --yes
```

---

## Mettre à jour Caleope vers une nouvelle version alpha

```bash
# Vérifier la version actuelle
caleope version

# Mettre à jour (télécharge la dernière pre-release)
caleope upgrade
```

---

## Tester l'alpha depuis zéro

Séquence de test recommandée sur un Debian 12 fresh :

```bash
# 1. Install
CALEOPE_DOMAIN=home.local CALEOPE_PROXY_MODE=standalone CALEOPE_CHANNEL=alpha \
  bash <(curl -fsSL https://raw.githubusercontent.com/Gaiver-IT/caleope/alpha/install.sh)

# 2. Vérifier
caleope ping
caleope version      # → v0.5.x

# 3. Sync store
sudo git -C /opt/gaiver-it/caleope/core/cache/official fetch origin alpha
sudo git -C /opt/gaiver-it/caleope/core/cache/official reset --hard origin/alpha
caleope update

# 4. Installer des apps
caleope install authentik
caleope install jellyfin
caleope install restic

# 5. Test GPU (si disponible)
caleope install jellyfin --gpu
cat /opt/gaiver-it/caleope/apps-installed/jellyfin/compose.override.yml

# 6. Test Restic
caleope backup jellyfin --restic --repo /tmp/test-backup --password testpass123
sudo sh -c 'RESTIC_PASSWORD=testpass123 restic -r /tmp/test-backup snapshots'

# 7. Test rate limiting
TOKEN=$(caleope token | grep Token | awk '{print $NF}')
for i in $(seq 1 62); do
  code=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TOKEN" http://localhost:8765/api/v1/apps)
  echo "req $i → $code"
done

# 8. Test suppression sans confirmation
caleope install freshrss
caleope remove freshrss --yes
```
