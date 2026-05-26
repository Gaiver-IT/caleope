#!/bin/bash
# release.sh — Script de release Caleope
# Usage : ./release.sh

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
GRAY='\033[0;90m'
NC='\033[0m'

GITHUB_REPO_SLUG="Gaiver-IT/caleope"

# =============================================================================
# ROADMAP
# =============================================================================

print_roadmap() {
    echo -e "${CYAN}  Roadmap Caleope :${NC}"
    echo -e "  ${GREEN}✅ 0.1.x${NC} — MVP CLI + daemon"
    echo -e "  ${GREEN}✅ 0.2.x${NC} — Store + Traefik + install.sh"
    echo -e "  ${GRAY}   0.3.x${NC} — API REST + interface web"
    echo -e "  ${GRAY}   0.4.x${NC} — Backup Restic + supervision"
    echo -e "  ${GRAY}   0.5.x${NC} — Authentik + sécurité"
    echo -e "  ${GRAY}   1.0.0${NC} — Production ready"
    echo ""
}

# =============================================================================
# HELPERS
# =============================================================================

# Extraire les composants d'une version semver
semver_major() { echo "${1#v}" | cut -d. -f1; }
semver_minor() { echo "${1#v}" | cut -d. -f2; }
semver_patch() { echo "${1#v}" | cut -d. -f3; }

# Valider le format vX.Y.Z
validate_semver() {
    [[ "${1}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

# Synchroniser avec le remote avant de releaser
# Règle de conflit : la version locale (plus récente) gagne toujours
sync_with_remote() {
    echo ""
    echo -e "${CYAN}[0/4]${NC} Synchronisation avec GitHub..."

    local root branch
    root=$(git rev-parse --show-toplevel)
    branch=$(git branch --show-current)

    # ── Annuler tout merge/rebase en cours ──
    if [[ -f "${root}/.git/MERGE_HEAD" ]]; then
        echo -e "      ${YELLOW}⚠️  Merge en cours détecté — abandon${NC}"
        git merge --abort
    fi
    if [[ -d "${root}/.git/rebase-merge" ]] || [[ -d "${root}/.git/rebase-apply" ]]; then
        echo -e "      ${YELLOW}⚠️  Rebase en cours détecté — abandon${NC}"
        git rebase --abort
    fi

    # ── Commiter les modifications locales ──
    # (on ne stash pas : le stash échoue si des fichiers sont en conflit)
    if [[ -n "$(git -C "${root}" status --porcelain)" ]]; then
        echo -e "      Commit des modifications locales en cours..."
        git -C "${root}" add -A
        git -C "${root}" commit -m "chore: modifications pré-release (${VERSION})"
        echo -e "      ${GREEN}✔ Modifications commitées${NC}"
    fi

    # ── Récupérer les refs distants ──
    git fetch origin --tags --prune --prune-tags 2>/dev/null || true

    local local_sha remote_sha
    local_sha=$(git rev-parse HEAD)
    remote_sha=$(git rev-parse "origin/${branch}" 2>/dev/null || echo "")

    if [[ -z "${remote_sha}" ]] || [[ "${local_sha}" == "${remote_sha}" ]]; then
        echo -e "      ${GREEN}✔ Déjà à jour${NC}"
        return 0
    fi

    # ── Rebase sur le remote, notre version gagne les conflits ──
    # Note: pendant un rebase, "-X theirs" = garder nos commits locaux rejoués
    echo -e "      Récupération des commits distants..."
    if ! GIT_EDITOR=true git pull --rebase -X theirs origin "${branch}" 2>&1; then
        echo -e "      ${YELLOW}⚠️  Rebase complexe — bascule sur merge (notre version gagne)${NC}"
        git rebase --abort 2>/dev/null || true
        git merge -X ours "origin/${branch}" --no-edit
    fi

    local synced
    synced=$(git log --oneline "${local_sha}..HEAD" 2>/dev/null | wc -l | tr -d ' ')
    echo -e "      ${GREEN}✔ Synchronisé (+${synced} commit(s) distants)${NC}"
}

# =============================================================================
# MAIN
# =============================================================================

echo -e "${CYAN}"
echo "╔══════════════════════════════════════╗"
echo "║     Caleope — Release Builder        ║"
echo "╚══════════════════════════════════════╝"
echo -e "${NC}"

# ── Vérifications préalables ──
command -v go   &>/dev/null || { echo -e "${RED}❌ Go non installé${NC}"; exit 1; }
command -v git  &>/dev/null || { echo -e "${RED}❌ Git non installé${NC}"; exit 1; }
command -v make &>/dev/null || { echo -e "${RED}❌ Make non installé${NC}"; exit 1; }
[[ -f "go.mod" ]] || { echo -e "${RED}❌ Lance depuis la racine du repo caleope${NC}"; exit 1; }

# Récupérer les tags distants avant de calculer la version actuelle
git fetch origin --tags --prune --prune-tags 2>/dev/null || true

# ── Version actuelle — dernier tag par ordre semver (indépendant du HEAD) ──
CURRENT=$(git tag --sort=-v:refname 2>/dev/null | grep '^v' | head -1 || echo "v0.0.0")
[[ -z "${CURRENT}" ]] && CURRENT="v0.0.0"
MAJOR=$(semver_major "${CURRENT}")
MINOR=$(semver_minor "${CURRENT}")
PATCH=$(semver_patch "${CURRENT}")

echo -e "  Version actuelle : ${YELLOW}${CURRENT}${NC}"
echo ""
print_roadmap

# ── Vérifier fichiers non commités ──
if [[ -n "$(git status --porcelain)" ]]; then
    echo -e "${YELLOW}⚠️  Fichiers non commités :${NC}"
    git status --short
    echo ""
    read -rp "  Continuer quand même ? [o/N] " confirm
    [[ "$(echo "${confirm}" | tr "[:upper:]" "[:lower:]")" == "o" ]] || { echo "Annulé."; exit 0; }
    echo ""
fi

# ── Type de release ──
echo -e "${CYAN}  Quel type de release ?${NC}"
# Pré-calculer les versions suivantes pour l'affichage
NEXT_BUGFIX_PATCH=$((PATCH + 1))
NEXT_BUGFIX="v${MAJOR}.${MINOR}.${NEXT_BUGFIX_PATCH}"
while git tag | grep -q "^${NEXT_BUGFIX}$"; do
    NEXT_BUGFIX_PATCH=$((NEXT_BUGFIX_PATCH + 1))
    NEXT_BUGFIX="v${MAJOR}.${MINOR}.${NEXT_BUGFIX_PATCH}"
done

NEXT_MINOR_NUM=$((MINOR + 1))
NEXT_MINOR_V="v${MAJOR}.${NEXT_MINOR_NUM}.0"
while git tag | grep -q "^${NEXT_MINOR_V}$"; do
    NEXT_MINOR_NUM=$((NEXT_MINOR_NUM + 1))
    NEXT_MINOR_V="v${MAJOR}.${NEXT_MINOR_NUM}.0"
done

NEXT_MAJOR_NUM=$((MAJOR + 1))
NEXT_MAJOR_V="v${NEXT_MAJOR_NUM}.0.0"
while git tag | grep -q "^${NEXT_MAJOR_V}$"; do
    NEXT_MAJOR_NUM=$((NEXT_MAJOR_NUM + 1))
    NEXT_MAJOR_V="v${NEXT_MAJOR_NUM}.0.0"
done

echo -e "  ${YELLOW}1) Bugfix${NC}     — correction de bug            → ${NEXT_BUGFIX}"
echo -e "  ${YELLOW}2) Mineure${NC}    — nouvelles fonctionnalités     → ${NEXT_MINOR_V}"
echo -e "  ${YELLOW}3) Majeure${NC}    — version stable complète (1.0) → ${NEXT_MAJOR_V}"
echo -e "  ${YELLOW}4) Manuelle${NC}   — je saisis moi-même"
echo ""

RELEASE_TYPE=""
while [[ -z "${RELEASE_TYPE}" ]]; do
    read -rp "  Choix [1/2/3/4] : " choice
    case "${choice}" in
        1)
            RELEASE_TYPE="bugfix"
            NEXT_PATCH=$((PATCH + 1))
            VERSION="v${MAJOR}.${MINOR}.${NEXT_PATCH}"
            # Auto-incrémenter si le tag existe déjà
            while git tag | grep -q "^${VERSION}$"; do
                NEXT_PATCH=$((NEXT_PATCH + 1))
                VERSION="v${MAJOR}.${MINOR}.${NEXT_PATCH}"
            done
            ;;
        2)
            RELEASE_TYPE="mineure"
            NEXT_MINOR=$((MINOR + 1))
            VERSION="v${MAJOR}.${NEXT_MINOR}.0"
            while git tag | grep -q "^${VERSION}$"; do
                NEXT_MINOR=$((NEXT_MINOR + 1))
                VERSION="v${MAJOR}.${NEXT_MINOR}.0"
            done
            ;;
        3)
            RELEASE_TYPE="majeure"
            NEXT_MAJOR=$((MAJOR + 1))
            VERSION="v${NEXT_MAJOR}.0.0"
            while git tag | grep -q "^${VERSION}$"; do
                NEXT_MAJOR=$((NEXT_MAJOR + 1))
                VERSION="v${NEXT_MAJOR}.0.0"
            done
            ;;
        4) RELEASE_TYPE="manuelle" ;;
        *) echo -e "  ${RED}Choix invalide${NC}" ;;
    esac
done

# Saisie manuelle
if [[ "${RELEASE_TYPE}" == "manuelle" ]]; then
    while true; do
        read -rp "  Version (ex: v0.2.3) : " VERSION
        if validate_semver "${VERSION}"; then
            break
        else
            echo -e "  ${RED}❌ Format invalide — utilise vX.Y.Z${NC}"
        fi
    done
fi

# ── Avertissement montée de version mineure/majeure ──
if [[ "${RELEASE_TYPE}" == "mineure" ]]; then
    echo ""
    echo -e "${YELLOW}  ⚠️  Montée de version mineure (0.${MINOR} → 0.$((MINOR + 1)))${NC}"
    echo -e "  Cela signifie que la phase ${CYAN}0.${MINOR}.x${NC} est terminée."
    echo -e "  Es-tu sûr que toutes les fonctionnalités prévues sont en place ?"
    echo ""
    read -rp "  Confirmer la montée de version mineure ? [o/N] " confirm
    [[ "$(echo "${confirm}" | tr "[:upper:]" "[:lower:]")" == "o" ]] || { echo "Annulé."; exit 0; }
fi

if [[ "${RELEASE_TYPE}" == "majeure" ]]; then
    echo ""
    echo -e "${RED}  ⚠️  MONTÉE DE VERSION MAJEURE → ${VERSION}${NC}"
    echo -e "  C'est la version ${YELLOW}production ready${NC}."
    echo -e "  Toutes les fonctionnalités de la roadmap doivent être présentes."
    echo ""
    read -rp "  Tape 'JE CONFIRME' pour continuer : " confirm
    [[ "${confirm}" == "JE CONFIRME" ]] || { echo "Annulé."; exit 0; }
fi

# ── Canal de publication ──
echo ""
echo -e "${CYAN}  Canal de publication :${NC}"
echo -e "  ${YELLOW}1) Stable${NC}   — release officielle (main, mise à jour auto des users stable)"
echo -e "  ${YELLOW}2) Alpha${NC}    — pré-release (instable, mise à jour auto des users alpha)"
echo ""
RELEASE_CHANNEL=""
while [[ -z "${RELEASE_CHANNEL}" ]]; do
    read -rp "  Canal [1/2] : " ch
    case "${ch}" in
        1) RELEASE_CHANNEL="stable" ;;
        2) RELEASE_CHANNEL="alpha"  ;;
        *) echo -e "  ${RED}Choix invalide${NC}" ;;
    esac
done

IS_PRERELEASE="false"
[[ "${RELEASE_CHANNEL}" == "alpha" ]] && IS_PRERELEASE="true"

# ── Récapitulatif ──
echo ""
echo -e "${CYAN}  ┌─────────────────────────────────────────┐${NC}"
echo -e "${CYAN}  │           Récapitulatif                 │${NC}"
echo -e "${CYAN}  ├─────────────────────────────────────────┤${NC}"
echo -e "${CYAN}  │${NC}  Type    : ${YELLOW}${RELEASE_TYPE}${NC}"
echo -e "${CYAN}  │${NC}  Canal   : ${YELLOW}${RELEASE_CHANNEL}${NC}"
echo -e "${CYAN}  │${NC}  Version : ${YELLOW}${CURRENT} → ${VERSION}${NC}"
echo -e "${CYAN}  │${NC}  Repo    : $(git remote get-url origin)"
echo -e "${CYAN}  │${NC}  Branche : $(git branch --show-current)"
echo -e "${CYAN}  └─────────────────────────────────────────┘${NC}"
echo ""
read -rp "  Lancer la release ? [O/n] " confirm
[[ "$(echo "${confirm}" | tr "[:upper:]" "[:lower:]")" != "n" ]] || { echo "Annulé."; exit 0; }

sync_with_remote
echo ""

# ── Créer le tag git ──
echo -e "${CYAN}[1/4]${NC} Création du tag ${VERSION}..."
if git tag | grep -q "^${VERSION}$"; then
    if [[ "${RELEASE_TYPE}" == "manuelle" ]]; then
        git tag -d "${VERSION}"
        echo -e "      ${YELLOW}⚠️  Ancien tag local supprimé et recréé${NC}"
    else
        echo -e "${RED}❌ Le tag ${VERSION} existe déjà localement${NC}"
        exit 1
    fi
fi
git tag "${VERSION}"
echo -e "      ${GREEN}✔ Tag créé${NC}"

# ── Compiler ──
echo -e "${CYAN}[2/4]${NC} Compilation..."
make release VERSION="${VERSION}"
echo -e "      ${GREEN}✔ Binaires compilés${NC}"

# ── Vérifier la version dans le binaire ──
# Sur macOS avec un binaire linux/amd64, on ne peut pas l'exécuter directement.
# On vérifie via la chaîne embarquée dans le binaire (strings).
if [[ "$(uname -s)" == "Darwin" ]]; then
    BUILT_VERSION=$(strings ./build/release/caleope-linux-amd64 2>/dev/null \
        | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || echo "?")
else
    BUILT_VERSION=$(./build/release/caleope-linux-amd64 version 2>/dev/null | awk '{print $2}' || echo "?")
fi
if [[ "${BUILT_VERSION}" != "${VERSION}" ]]; then
    echo -e "${RED}❌ Version dans le binaire (${BUILT_VERSION}) ≠ tag (${VERSION})${NC}"
    echo -e "   Suppression du tag local..."
    git tag -d "${VERSION}"
    exit 1
fi
echo -e "      ${GREEN}✔ Version vérifiée dans le binaire : ${BUILT_VERSION}${NC}"

# ── Pousser ──
echo -e "${CYAN}[3/4]${NC} Push vers GitHub..."
if ! git push origin "$(git branch --show-current)"; then
    echo -e "${RED}❌ Push du code échoué — suppression du tag local ${VERSION}${NC}"
    git tag -d "${VERSION}"
    echo -e "   Résoudre les conflits (git pull / git stash) puis relancer en mode manuel avec ${VERSION}"
    exit 1
fi
if ! git push origin "${VERSION}"; then
    echo -e "${RED}❌ Push du tag échoué — suppression du tag local ${VERSION}${NC}"
    git tag -d "${VERSION}"
    exit 1
fi
echo -e "      ${GREEN}✔ Code et tag poussés${NC}"

# ── Créer la release GitHub ──
echo -e "${CYAN}[4/4]${NC} Création de la release GitHub..."

# Auto-récupération du token depuis le keychain macOS (GitHub Desktop le stocke là)
if [[ -z "${GITHUB_TOKEN:-}" ]] && command -v security &>/dev/null; then
    GITHUB_TOKEN=$(security find-internet-password -s github.com -w 2>/dev/null || echo "")
fi

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
    echo -e "${YELLOW}⚠️  Token GitHub non trouvé — upload manuel requis${NC}"
    echo -e "   → https://github.com/${GITHUB_REPO_SLUG}/releases/new?tag=${VERSION}"
    exit 0
fi

RELEASE_RESPONSE=$(curl -fsSL \
    -X POST \
    -H "Authorization: token ${GITHUB_TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${GITHUB_REPO_SLUG}/releases" \
    -d "{\"tag_name\": \"${VERSION}\", \"name\": \"Caleope ${VERSION}\", \"body\": \"Release ${VERSION} (${RELEASE_TYPE}) — canal ${RELEASE_CHANNEL}\", \"draft\": false, \"prerelease\": ${IS_PRERELEASE}}")

UPLOAD_URL=$(echo "${RELEASE_RESPONSE}" | grep -o '"upload_url": *"[^"]*"' | grep -o 'https://[^{]*')

if [[ -z "${UPLOAD_URL}" ]]; then
    echo -e "${RED}❌ Impossible de créer la release${NC}"
    echo "${RELEASE_RESPONSE}" | grep -o '"message": *"[^"]*"'
    exit 1
fi

for BINARY in "caleoped-linux-amd64" "caleope-linux-amd64"; do
    echo -e "  → Upload ${BINARY}..."
    curl -fsSL         -X POST         -H "Authorization: token ${GITHUB_TOKEN}"         -H "Accept: application/vnd.github+json"         -H "Content-Type: application/octet-stream"         "${UPLOAD_URL}?name=${BINARY}"         --data-binary "@build/release/${BINARY}" > /dev/null
    echo -e "  ${GREEN}✔ ${BINARY} uploadé${NC}"
done

RELEASE_URL=$(echo "${RELEASE_RESPONSE}" | grep -o '"html_url": *"[^"]*releases/tag[^"]*"' | grep -o 'https://[^"]*')

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              Release publiée avec succès !           ║${NC}"
echo -e "${GREEN}╠══════════════════════════════════════════════════════╣${NC}"
echo -e "${GREEN}║${NC}  Version : ${YELLOW}${VERSION}${NC} (${RELEASE_TYPE})"
echo -e "${GREEN}║${NC}  Canal   : ${YELLOW}${RELEASE_CHANNEL}${NC}"
echo -e "${GREEN}║${NC}  URL     : ${YELLOW}${RELEASE_URL}${NC}"
echo -e "${GREEN}║${NC}"
echo -e "${GREEN}║${NC}  Pour mettre à jour le serveur :"
echo -e "${GREEN}║${NC}  ${YELLOW}caleope upgrade${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════╝${NC}"
echo ""
