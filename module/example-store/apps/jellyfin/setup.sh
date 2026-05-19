#!/bin/bash
# setup.sh — Script de préparation pour Jellyfin
#
# Ce script est exécuté PAR Caleope avant de lancer les containers.
# Il prépare l'environnement (dossiers, permissions, etc.)
#
# RÈGLES STRICTES (voir doc sécurité) :
#   ✅ Peut créer des dossiers
#   ✅ Peut écrire des fichiers de config
#   ❌ Ne peut PAS installer des paquets (apt, pip...)
#   ❌ Ne peut PAS modifier le firewall
#   ❌ Ne devrait PAS faire de requêtes réseau
#
# Variables disponibles (injectées par Caleope) :
#   $CALEOPE_APP_ID      = "jellyfin"
#   $CALEOPE_APP_DIR     = dossier de l'app installée
#   $CALEOPE_BASE_DIR    = répertoire base Caleope
#   $CALEOPE_DOMAIN      = domaine configuré

set -euo pipefail  # Arrêt en cas d'erreur, variables non définies interdites

echo "→ Préparation de Jellyfin..."

# Créer les dossiers avec les bonnes permissions
# Jellyfin tourne en UID 1000 dans le container
mkdir -p "${CALEOPE_BASE_DIR}/app-data/jellyfin/"{config,cache,media}
chmod 755 "${CALEOPE_BASE_DIR}/app-data/jellyfin/config"
chmod 755 "${CALEOPE_BASE_DIR}/app-data/jellyfin/cache"
chmod 755 "${CALEOPE_BASE_DIR}/app-data/jellyfin/media"

echo "  ✓ Dossiers créés"

# Vérifier que le domaine est configuré
if [ -z "${CALEOPE_DOMAIN:-}" ]; then
    echo "  ⚠️  Pas de domaine configuré — accès par port uniquement"
fi

echo "→ Jellyfin prêt à démarrer"
