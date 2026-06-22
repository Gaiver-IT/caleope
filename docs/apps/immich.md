---
title: Immich
description: Sauvegarde et galerie photo/vidéo auto-hébergée
published: true
date: 2026-06-19
---

# Immich

Alternative self-hosted à Google Photos. Sauvegarde automatique depuis mobile, reconnaissance faciale, albums, timeline — performant et moderne.

## Installation

```bash
caleope install immich --domain photos.monserveur.fr
```

## Accès

- **Interface web** : `https://photos.monserveur.fr`
- **Login admin** : `admin`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/immich/secrets.env`

## Application mobile

L'app Immich (iOS/Android) se connecte à ton instance pour la sauvegarde automatique :

1. Télécharger **Immich** depuis l'App Store / Play Store
2. Serveur : `https://photos.monserveur.fr`
3. Se connecter avec le compte admin (ou créer un compte utilisateur)
4. Activer la sauvegarde automatique dans les paramètres

## Machine Learning (optionnel)

Immich inclut un service de machine learning pour :
- Reconnaissance faciale
- Recherche par description textuelle (CLIP)

Actif par défaut — consomme ~1 Go de RAM supplémentaire. Désactivable dans l'interface admin si le serveur est limité en ressources.

## Commandes utiles

```bash
caleope logs immich
caleope backup immich        # Sauvegarde DB + bibliothèque (volumineuse si beaucoup de photos)
caleope restart immich
```

## Structure des données

```
app-data/immich/
├── photos/      ← bibliothèque médias (peut devenir très volumineuse)
├── thumbs/      ← miniatures générées
├── encoded-video/ ← vidéos transcodées
└── db/          ← base PostgreSQL + pgvecto.rs
```

> Pour stocker la bibliothèque sur un NAS : `caleope install immich --storage mon-nas`
