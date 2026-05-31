---
title: AzuraCast
description: Serveur de radio en ligne — streaming audio, gestion de stations
published: true
date: 2026-05-31
---

# AzuraCast

Serveur de radio en ligne tout-en-un. Héberge tes propres stations radio avec streaming Icecast, upload de musique via SFTP et interface admin web. Toute la configuration initiale (compte admin + station) se fait automatiquement.

## Installation

```bash
# Avec nom de domaine (recommandé en production)
caleope install azuracast --domain azuracast.monserveur.fr

# Sans nom de domaine (accès local)
caleope install azuracast
```

L'installeur pose deux questions, puis **le compte admin et la première station sont créés automatiquement**.

## Wizard d'installation

### 🌐 Mode d'accès

```
  Utiliser un nom de domaine ? [O/n] :
```

| Réponse | Ce qui se passe |
|---------|-----------------|
| **O** (défaut) | AzuraCast accessible via Traefik sur `https://azuracast.ton-domaine.com` |
| **n** | Accès direct `http://IP:PORT` — demande l'IP du serveur |

### 📻 Nom de ta station radio

```
  Nom de ta station (ex: Radio Caleope) :
```

Laisse vide pour garder le nom par défaut *"Ma Radio"*. Un slug court est généré automatiquement (`maradio`, `radiocaleope`…).

## Ports

Les trois ports sont alloués **dynamiquement** par Caleope (pas de conflit garanti avec les autres apps). Les valeurs exactes s'affichent dans `caleope list` et dans le résumé post-installation.

| Port | Usage | Notes |
|------|-------|-------|
| **web** | Interface admin (HTTP → Traefik) | Accessible via domaine ou `http://IP:port` |
| **sftp** | Upload de musique | Configurer dans FileZilla avec ce port |
| **icecast** | Flux streaming radio | À ouvrir dans le pare-feu — les auditeurs s'y connectent directement |

> **Pourquoi icecast utilise `host:host` ?** Icecast annonce le numéro de port dans les URLs de flux (`http://IP:PORT/maradio.mp3`). Si le port hôte diffère du port container, les auditeurs reçoivent une URL invalide. Le bootstrap configure AzuraCast avec le port dynamique alloué pour que tout s'aligne.

## Ce qui est configuré automatiquement

Un container **bootstrap** s'exécute une fois après le démarrage d'AzuraCast (peut prendre 3-5 minutes le temps que la base de données s'initialise) :

| Action | Résultat |
|--------|----------|
| Wizard de setup | Compte admin créé via l'API |
| Authentification API | Token obtenu automatiquement |
| Station par défaut | Créée avec le nom et le port Icecast dynamique |

Les identifiants sont affichés dans le terminal et sauvegardés dans `app-config/azuracast/post-install.txt`.

## Ce qu'il reste à faire

### 1. Ouvrir le port Icecast dans le pare-feu

Le port Icecast alloué dynamiquement doit être accessible par tes auditeurs :

```bash
# Voir le port alloué
caleope list

# Ouvrir le port (remplace XXXX par la valeur affichée)
ufw allow XXXX/tcp
```

### 2. Uploader ta musique

Dans l'interface admin AzuraCast : **Station → SFTP Users** → créer un utilisateur SFTP.

Puis connecte-toi avec FileZilla (ou tout client SFTP) :
- **Hôte** : IP du serveur
- **Port** : le port SFTP alloué dynamiquement (voir `caleope list`)
- **Login / Mot de passe** : créés dans l'étape précédente

### 3. Configurer les playlists

**Station → Playlists** → créer une playlist → assigner des dossiers de musique.

AzuraCast gère la rotation automatiquement via Liquidsoap.

## Ajouter des stations supplémentaires

Par défaut, un seul port Icecast est alloué (1 station). Pour plusieurs stations, il faut ajouter des ports supplémentaires **manuellement** dans `apps-installed/azuracast/compose.yml` et ouvrir les ports dans le pare-feu :

```yaml
# Ajouter dans la section ports du service azuracast :
- "8505:8505"   # Station 1 — flux backup
- "8510:8510"   # Station 2
- "8515:8515"   # Station 2 — backup
```

> Le format est `PORT:PORT` (identique des deux côtés — contrainte Icecast).
> Après modification, redémarre la stack : `docker compose -f apps-installed/azuracast/compose.yml up -d`
> Puis reconfigure les stations dans l'admin AzuraCast avec les nouveaux ports.

## Ressources recommandées

| Ressource | Minimum | Recommandé |
|-----------|---------|------------|
| RAM | 1 Go | 2 Go |
| CPU | 1 vCPU | 2 vCPU |
| Stockage | 20 Go | Variable selon bibliothèque |

> AzuraCast fait tourner nginx, PHP, MariaDB, Redis et Liquidsoap dans le **même container**.
> Le premier démarrage prend **3 à 5 minutes** — c'est normal.

## Commandes utiles

```bash
caleope list                   # voir les ports alloués
caleope logs azuracast         # logs du container principal
caleope restart azuracast      # redémarrer

# CLI interne AzuraCast (depuis le container)
docker exec -it azuracast azuracast_cli help
docker exec -it azuracast azuracast_cli backup:run
docker exec -it azuracast azuracast_cli db:migrate
```
