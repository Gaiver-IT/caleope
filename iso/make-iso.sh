#!/usr/bin/env bash
#
# make-iso.sh — Assistant simple pour fabriquer une ISO Caleope.
#
# Un seul point d'entrée, guidé : il te pose 3-4 questions puis appelle
# build.sh (ISO online) ou offline-builder + build.sh (ISO offline). Tu n'as
# rien à retenir des variables d'environnement.
#
#   cd iso && ./make-iso.sh
#
# Prérequis (hôte Linux) : xorriso wget cpio gzip  (+ go si ISO offline).
# Le script propose de les installer si absents.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"

# ── Couleurs ────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  C='\033[0;36m'; G='\033[0;32m'; Y='\033[1;33m'; R='\033[0;31m'; D='\033[0;90m'; B='\033[1m'; Z='\033[0m'
else C=''; G=''; Y=''; R=''; D=''; B=''; Z=''; fi
say()  { echo -e "$@"; }
ask()  { local p="$1" def="${2:-}" ans; read -rp "$(echo -e "${C}▶${Z} ${p}${def:+ ${D}[$def]${Z}} : ")" ans; echo "${ans:-$def}"; }
yesno(){ local p="$1" def="${2:-o}" ans; ans=$(ask "$p ${D}(o/n)${Z}" "$def"); [[ "${ans,,}" == o* ]]; }

banner() {
  say "${C}${B}"
  say "  ╔══════════════════════════════════════════╗"
  say "  ║       Caleope — Fabrique d'ISO           ║"
  say "  ╚══════════════════════════════════════════╝${Z}"
  say ""
}

# ── Prérequis ────────────────────────────────────────────────────────────────
check_deps() {
  local need_offline="$1" missing=()
  for c in xorriso wget cpio gzip; do command -v "$c" >/dev/null 2>&1 || missing+=("$c"); done
  if [[ "$need_offline" == "yes" ]]; then command -v go >/dev/null 2>&1 || missing+=(golang); fi
  if [[ ${#missing[@]} -gt 0 ]]; then
    say "${Y}Outils manquants : ${missing[*]}${Z}"
    if command -v apt-get >/dev/null 2>&1 && yesno "Les installer via apt maintenant ?" o; then
      sudo apt-get update -qq && sudo apt-get install -y "${missing[@]}"
    else
      say "${R}Installe-les puis relance.${Z}"; exit 1
    fi
  fi
}

banner

# ── 1. Type d'ISO ────────────────────────────────────────────────────────────
say "${B}Quel type d'ISO ?${Z}"
say "  ${D}1) Online  — légère, les apps se téléchargent à l'install (95 % des cas)${Z}"
say "  ${D}2) Offline — apps embarquées, pour un serveur sans internet (air-gap)${Z}"
TYPE=$(ask "Choix 1/2" "1")

VERSION=$(ask "Version Caleope (tag release)" "v0.6.6")
CHANNEL=$(ask "Canal du store (stable/alpha)" "stable")

if [[ "$TYPE" == "2" ]]; then
  # ── ISO OFFLINE ────────────────────────────────────────────────────────────
  check_deps yes
  say ""
  say "${B}Sélection des apps à embarquer${Z} ${D}(vide = toutes)${Z}"
  APPS=$(ask "Apps (séparées par des virgules)" "")

  say "${C}Construction du bundle offline (pull des images sans Docker)…${Z}"
  ( cd offline-builder && go build -o /tmp/caleope-offline-builder . )
  BUNDLE="$HERE/build/offline-bundle"
  /tmp/caleope-offline-builder -version "$VERSION" -channel "$CHANNEL" \
    ${APPS:+-apps "$APPS"} -out "$BUNDLE"

  say "${C}Assemblage de l'ISO offline…${Z}"
  OFFLINE_IMAGES_DIR="$BUNDLE/images" CALEOPE_VERSION="$VERSION" CHANNEL="$CHANNEL" ./build.sh
else
  # ── ISO ONLINE ─────────────────────────────────────────────────────────────
  check_deps no
  say ""
  REG=""; REG_USER=""; REG_PASS=""
  if yesno "Baker un registre miroir (apps pull depuis ton registre plutôt que Docker Hub) ?" n; then
    REG=$(ask "Hôte du registre" "caleope-registry.gaiver-it.fr")
    REG_USER=$(ask "Utilisateur registre" "caleope")
    read -rsp "$(echo -e "${C}▶${Z} Mot de passe registre : ")" REG_PASS; echo ""
  fi
  say "${C}Assemblage de l'ISO online…${Z}"
  CALEOPE_VERSION="$VERSION" CHANNEL="$CHANNEL" \
    CALEOPE_REGISTRY="$REG" CALEOPE_REGISTRY_USER="$REG_USER" CALEOPE_REGISTRY_PASS="$REG_PASS" \
    ./build.sh
fi

say ""
say "${G}${B}✅ Terminé.${Z} L'ISO est dans ${Y}$HERE/build/${Z}"
say "   ${D}Grave-la (dd/balena/Ventoy) ou boote-la en VM. Installation auto, puis wizard au 1er démarrage.${Z}"
