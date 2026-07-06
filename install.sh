#!/bin/bash
# =============================================================================
# Caleope — Script d'installation
# Organisation : Gaiver-IT
# Repo         : https://github.com/gaiver-it/caleope
# Usage        : apt install -y curl && curl -fsSL https://raw.githubusercontent.com/gaiver-it/caleope/main/install.sh | bash
# =============================================================================

set -euo pipefail

# S'assurer que les commandes système sont disponibles quand lancé via "curl | bash"
# (groupadd, usermod, useradd, etc. sont dans /usr/sbin qui peut être absent du PATH)
export PATH="/usr/sbin:/sbin:/usr/bin:/bin:/usr/local/bin:${PATH}"

# =============================================================================
# ARGUMENTS
# =============================================================================

LOG_MODE="classic"
OFFLINE_MODE=false
OFFLINE_BUNDLE_PATH=""
# Mode ISO : install non-interactive depuis le payload embarqué sur l'ISO
# (voir iso/build.sh + iso/preseed.cfg). La config utilisateur (domaine, proxy,
# mots de passe) est reportée au wizard de 1er démarrage (--iso-finalize).
ISO_MODE=false
ISO_FINALIZE=false
# Répertoire du script lui-même (= payload ISO quand lancé en mode --iso)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo ".")"

parse_args() {
    local i=1
    while [[ $i -le $# ]]; do
        local arg="${!i}"
        case $arg in
            --debug)   LOG_MODE="debug"   ;;
            --classic) LOG_MODE="classic" ;;
            --offline)
                OFFLINE_MODE=true
                i=$((i + 1))
                [[ $i -le $# ]] && OFFLINE_BUNDLE_PATH="${!i}" || {
                    echo "Erreur : --offline requiert un chemin vers le bundle" >&2
                    exit 1
                }
                ;;
            # ── Mode ISO ────────────────────────────────────────────────────
            # --iso : install de base non-interactive (preseed late_command).
            #   Le payload (binaires + store [+ images]) est à côté de ce script.
            #   Réutilise la machinerie offline ; la config est reportée au 1er boot.
            --iso)
                ISO_MODE=true
                OFFLINE_MODE=true
                # payload = dossier du script, sauf si --offline l'a déjà défini
                [[ -z "${OFFLINE_BUNDLE_PATH}" ]] && OFFLINE_BUNDLE_PATH="${SCRIPT_DIR}"
                ;;
            # --iso-finalize : wizard interactif de 1er démarrage. Complète la
            #   config différée (domaine/proxy/mdp), déploie Traefik + apps, puis
            #   se désactive. Lancé par caleope-firstboot.service sur tty1.
            --iso-finalize)
                ISO_FINALIZE=true
                ;;
        esac
        i=$((i + 1))
    done
}

# =============================================================================
# VARIABLES
# =============================================================================

CALEOPE_USER="user-caleope"
CALEOPE_ROOT="/opt/gaiver-it/caleope"
CALEOPE_GROUP="caleope"

GITHUB_REPO="gaiver-it/caleope"
GITHUB_RAW="https://raw.githubusercontent.com/${GITHUB_REPO}/main"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"

SOCKET_PATH="/run/caleoped.sock"

# Ports
PORT_TRAEFIK_HTTP=80
PORT_TRAEFIK_HTTPS=443
PORT_TRAEFIK_DASHBOARD=8080
PORT_COCKPIT=8020  # conservé pour rétro-compat. éventuelle
PORT_UI=8766

# Réseaux Docker
DOCKER_NET_PUBLIC="caleope-public"
DOCKER_NET_INTERNAL="caleope-internal"

# Config interactive (remplie par ask_config, ou pré-définie via variables d'environnement)
CALEOPE_DOMAIN="${CALEOPE_DOMAIN:-}"
CALEOPE_EMAIL="${CALEOPE_EMAIL:-}"
CALEOPE_PROXY_MODE="${CALEOPE_PROXY_MODE:-}"   # "npm" = NPM en amont, "traefik" = Traefik gère les certs
CALEOPE_CHANNEL="${CALEOPE_CHANNEL:-}"          # "stable" = releases officielles, "alpha" = pré-releases
# SMTP global — transmis automatiquement aux apps compatibles (optionnel)
CALEOPE_SMTP_HOST="${CALEOPE_SMTP_HOST:-}"
CALEOPE_SMTP_PORT="${CALEOPE_SMTP_PORT:-587}"
CALEOPE_SMTP_USER="${CALEOPE_SMTP_USER:-}"
CALEOPE_SMTP_PASS="${CALEOPE_SMTP_PASS:-}"
CALEOPE_SMTP_FROM="${CALEOPE_SMTP_FROM:-}"
# Mot de passe chiffrement secrets (utilisé une seule fois au démarrage du daemon)
CALEOPE_SECRETS_PASSWORD="${CALEOPE_SECRETS_PASSWORD:-}"
# Mot de passe interface web
CALEOPE_UI_PASSWORD="${CALEOPE_UI_PASSWORD:-}"
# Registre d'images miroir (optionnel) — les apps pull leurs images depuis ici
# au lieu de Docker Hub. Injectable par l'ISO online (build.sh) via env.
CALEOPE_REGISTRY="${CALEOPE_REGISTRY:-}"
CALEOPE_REGISTRY_USER="${CALEOPE_REGISTRY_USER:-}"
CALEOPE_REGISTRY_PASS="${CALEOPE_REGISTRY_PASS:-}"

# Couleurs
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
GRAY='\033[0;90m'
NC='\033[0m'

# =============================================================================
# LOGGING
# =============================================================================

log_debug()   { [[ "${LOG_MODE}" == "debug" ]] && echo -e "${GRAY}[DEBUG]${NC}   $1" || true; }
log_step()    { echo -e "${BLUE}[▶]${NC} $1"; }
log_success() { echo -e "${GREEN}[✔]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[⚠]${NC} $1"; }
log_error()   { echo -e "${RED}[✘]${NC} $1"; exit 1; }

log_section() {
    if [[ "${LOG_MODE}" == "debug" ]]; then
        echo -e "\n${CYAN}========== $1 ==========${NC}\n"
    else
        echo -e "\n${CYAN}◆ $1${NC}"
    fi
}

run_cmd() {
    if [[ "${LOG_MODE}" == "debug" ]]; then
        "$@"
    else
        "$@" &>/dev/null
    fi
}

# =============================================================================
# MODE SUBMARINE (OFFLINE)
# =============================================================================

check_offline_bundle() {
    [[ "${OFFLINE_MODE}" != "true" ]] && return 0

    log_section "Mode submarine — vérification du bundle"

    [[ -z "${OFFLINE_BUNDLE_PATH}" ]] && log_error "--offline requiert un chemin vers le bundle"
    [[ ! -d "${OFFLINE_BUNDLE_PATH}" ]] && log_error "Bundle introuvable : ${OFFLINE_BUNDLE_PATH}"

    local required=("binaries/caleoped" "binaries/caleope" "store.tar.gz")
    for f in "${required[@]}"; do
        [[ -f "${OFFLINE_BUNDLE_PATH}/${f}" ]] || log_error "Fichier manquant dans le bundle : ${f}"
    done

    # Lire et afficher les métadonnées du bundle
    if [[ -f "${OFFLINE_BUNDLE_PATH}/pack-info.json" ]]; then
        local pack_version pack_date
        pack_version=$(jq -r '.caleope_version // "?"' "${OFFLINE_BUNDLE_PATH}/pack-info.json" 2>/dev/null || echo "?")
        pack_date=$(jq -r '.packed_at // "?"' "${OFFLINE_BUNDLE_PATH}/pack-info.json" 2>/dev/null || echo "?")
        log_success "Bundle valide — version ${pack_version} (empaqueté le ${pack_date})"
    else
        log_warning "pack-info.json absent — bundle non versionné"
    fi
}

load_docker_images_from_bundle() {
    [[ "${OFFLINE_MODE}" != "true" ]] && return 0

    local images_dir="${OFFLINE_BUNDLE_PATH}/images"
    [[ ! -d "${images_dir}" ]] && { log_warning "Aucun répertoire images/ dans le bundle — images ignorées"; return 0; }

    local tars
    tars=$(find "${images_dir}" -maxdepth 1 -name "*.tar" 2>/dev/null)
    [[ -z "${tars}" ]] && { log_warning "Aucune image Docker dans le bundle"; return 0; }

    log_section "Chargement des images Docker (mode submarine)"
    local count=0
    while IFS= read -r tar_file; do
        local name
        name=$(basename "${tar_file}" .tar)
        log_step "Chargement de ${name}..."
        if docker load -i "${tar_file}" &>/dev/null; then
            log_success "${name} chargée"
            count=$((count + 1))
        else
            log_warning "Échec du chargement de ${name}"
        fi
    done <<< "${tars}"
    log_success "${count} image(s) Docker chargée(s) depuis le bundle"
}

# =============================================================================
# CONFIGURATION INTERACTIVE
# =============================================================================

ask_config() {
    log_section "Configuration de Caleope"

    # Mode ISO (install de base) : aucune question. On pose des valeurs
    # provisoires ; le wizard de 1er démarrage (--iso-finalize) les remplacera.
    if [[ "${ISO_MODE}" == "true" ]]; then
        CALEOPE_DOMAIN="${CALEOPE_DOMAIN:-caleope.local}"
        CALEOPE_PROXY_MODE="${CALEOPE_PROXY_MODE:-standalone}"
        # Canal : depuis pack-info.json du payload si présent, sinon stable
        if [[ -z "${CALEOPE_CHANNEL:-}" ]] && [[ -f "${OFFLINE_BUNDLE_PATH}/pack-info.json" ]]; then
            CALEOPE_CHANNEL=$(jq -r '.channel // "stable"' "${OFFLINE_BUNDLE_PATH}/pack-info.json" 2>/dev/null)
        fi
        CALEOPE_CHANNEL="${CALEOPE_CHANNEL:-stable}"
        # Registre miroir baké dans l'ISO online (pack-info.json)
        if [[ -z "${CALEOPE_REGISTRY:-}" ]] && [[ -f "${OFFLINE_BUNDLE_PATH}/pack-info.json" ]]; then
            CALEOPE_REGISTRY=$(jq -r '.registry // ""' "${OFFLINE_BUNDLE_PATH}/pack-info.json" 2>/dev/null)
            CALEOPE_REGISTRY_USER=$(jq -r '.registry_user // ""' "${OFFLINE_BUNDLE_PATH}/pack-info.json" 2>/dev/null)
            CALEOPE_REGISTRY_PASS=$(jq -r '.registry_pass // ""' "${OFFLINE_BUNDLE_PATH}/pack-info.json" 2>/dev/null)
        fi
        log_step "Mode ISO — configuration reportée au 1er démarrage (valeurs provisoires)"
        echo -e "  Canal : ${YELLOW}${CALEOPE_CHANNEL}${NC}  ${GRAY}(domaine/proxy/mdp au 1er boot)${NC}"
        return
    fi

    # Mode non-interactif : si les variables essentielles sont déjà définies en
    # variables d'environnement, on les utilise directement sans prompts.
    # Usage : CALEOPE_DOMAIN=... CALEOPE_PROXY_MODE=traefik CALEOPE_CHANNEL=alpha bash install.sh
    if [[ -n "${CALEOPE_DOMAIN:-}" && -n "${CALEOPE_PROXY_MODE:-}" && -n "${CALEOPE_CHANNEL:-}" ]]; then
        log_step "Mode non-interactif détecté (variables d'environnement)"
        echo -e "  Domaine    : ${YELLOW}${CALEOPE_DOMAIN}${NC}"
        echo -e "  Proxy mode : ${YELLOW}${CALEOPE_PROXY_MODE}${NC}"
        echo -e "  Canal      : ${YELLOW}${CALEOPE_CHANNEL}${NC}"
        if [[ -n "${CALEOPE_EMAIL:-}" ]]; then echo -e "  Email      : ${YELLOW}${CALEOPE_EMAIL}${NC}"; fi
        return
    fi

    echo ""
    echo -e "${CYAN}  Quelques questions pour configurer ton installation.${NC}"
    echo ""

    # ── Domaine de base ──
    while [[ -z "${CALEOPE_DOMAIN}" ]]; do
        echo -e "${BLUE}  Domaine de base pour ce serveur Caleope${NC}"
        echo -e "  ${GRAY}Ex: caleope.mondomaine.com${NC}"
        echo -e "  ${GRAY}Les apps seront accessibles sur jellyfin.<domaine>, nextcloud.<domaine>...${NC}"
        read -rp "  → Domaine : " CALEOPE_DOMAIN </dev/tty
    done

    # ── Mode reverse proxy ──
    echo ""
    echo -e "${BLUE}  Mode reverse proxy${NC}"
    echo -e "  ${GRAY}1) NPM/Caddy/autre en amont  — Traefik reçoit du HTTP, pas de gestion des certs${NC}"
    echo -e "  ${GRAY}2) Traefik natif             — Traefik gère HTTPS et Let's Encrypt directement${NC}"
    echo -e "  ${GRAY}3) Standalone                — HTTP seul, sans certificat (LAN/offline/air-gap)${NC}"
    while [[ "${CALEOPE_PROXY_MODE}" != "npm" && "${CALEOPE_PROXY_MODE}" != "traefik" && "${CALEOPE_PROXY_MODE}" != "standalone" ]]; do
        read -rp "  → Choix [1/2/3] : " proxy_choice </dev/tty
        case "${proxy_choice}" in
            1) CALEOPE_PROXY_MODE="npm" ;;
            2) CALEOPE_PROXY_MODE="traefik" ;;
            3) CALEOPE_PROXY_MODE="standalone" ;;
            *) echo -e "  ${RED}Choix invalide, entre 1, 2 ou 3${NC}" ;;
        esac
    done

    # ── Canal de mise à jour ──
    echo ""
    echo -e "${BLUE}  Canal de mises à jour${NC}"
    echo -e "  ${GRAY}1) Stable ${GREEN}(recommandé)${GRAY} — versions validées et testées${NC}"
    echo -e "  ${GRAY}2) Alpha                — dernières fonctionnalités, peut être instable${NC}"
    while [[ "${CALEOPE_CHANNEL}" != "stable" && "${CALEOPE_CHANNEL}" != "alpha" ]]; do
        read -rp "  → Choix [1/2] : " channel_choice </dev/tty
        case "${channel_choice}" in
            1) CALEOPE_CHANNEL="stable" ;;
            2) CALEOPE_CHANNEL="alpha"  ;;
            *) echo -e "  ${RED}Choix invalide${NC}" ;;
        esac
    done

    # ── Email Let's Encrypt (seulement si mode traefik) ──
    if [[ "${CALEOPE_PROXY_MODE}" == "traefik" ]]; then
        echo ""
        echo -e "${BLUE}  Email pour Let's Encrypt${NC}"
        echo -e "  ${GRAY}Utilisé pour les notifications de renouvellement de certificats${NC}"
        while [[ -z "${CALEOPE_EMAIL}" ]]; do
            read -rp "  → Email : " CALEOPE_EMAIL </dev/tty
        done
    fi

    # ── SMTP global (optionnel) ──
    echo ""
    echo -e "${BLUE}  Relai SMTP (optionnel)${NC}"
    echo -e "  ${GRAY}Permet aux apps d'envoyer des emails (notifications, réinitialisation mdp...).${NC}"
    echo -e "  ${GRAY}Compatible Orange, Gmail (app password), Proton…${NC}"
    read -rp "  → Configurer le SMTP maintenant ? [o/N] : " smtp_choice </dev/tty
    if [[ "${smtp_choice,,}" == "o" ]]; then
        read -rp "    Serveur SMTP (ex: smtp.gmail.com) : " CALEOPE_SMTP_HOST </dev/tty
        read -rp "    Port (ex: 587) : " CALEOPE_SMTP_PORT </dev/tty
        CALEOPE_SMTP_PORT="${CALEOPE_SMTP_PORT:-587}"
        read -rp "    Utilisateur SMTP (email) : " CALEOPE_SMTP_USER </dev/tty
        read -rsp "    Mot de passe SMTP : " CALEOPE_SMTP_PASS </dev/tty; echo ""
        read -rp "    Adresse expéditeur (ex: noreply@mondomaine.com) : " CALEOPE_SMTP_FROM </dev/tty
        CALEOPE_SMTP_FROM="${CALEOPE_SMTP_FROM:-noreply@${CALEOPE_DOMAIN}}"
    fi

    # ── Mot de passe chiffrement des secrets ──
    echo ""
    echo -e "${BLUE}  Chiffrement des secrets${NC}"
    echo -e "  ${GRAY}Choisir un mot de passe pour protéger les secrets des apps (mots de passe BDD,${NC}"
    echo -e "  ${GRAY}tokens API...). Requis pour 'caleope secrets show <app>'.${NC}"
    echo -e "  ${YELLOW}  ⚠ Notez ce mot de passe — il ne peut pas être récupéré si perdu.${NC}"
    local secrets_pass secrets_pass2
    while true; do
        read -rsp "    Mot de passe : " secrets_pass </dev/tty; echo ""
        if [[ -z "${secrets_pass}" ]]; then
            echo -e "  ${RED}Le mot de passe ne peut pas être vide${NC}"
            continue
        fi
        read -rsp "    Confirmer : " secrets_pass2 </dev/tty; echo ""
        if [[ "${secrets_pass}" == "${secrets_pass2}" ]]; then
            CALEOPE_SECRETS_PASSWORD="${secrets_pass}"
            break
        fi
        echo -e "  ${RED}Les mots de passe ne correspondent pas${NC}"
    done
    unset secrets_pass secrets_pass2

    # ── Mot de passe interface web ──
    if [[ -z "${CALEOPE_UI_PASSWORD}" ]]; then
        echo ""
        echo -e "${BLUE}  Mot de passe de l'interface web Caleope${NC}"
        echo -e "  ${GRAY}Protège l'accès à l'UI sur le port ${PORT_UI}.${NC}"
        local ui_pass ui_pass2
        while true; do
            read -rsp "    Mot de passe UI : " ui_pass </dev/tty; echo ""
            if [[ -z "${ui_pass}" ]]; then
                echo -e "  ${RED}Le mot de passe ne peut pas être vide${NC}"
                continue
            fi
            read -rsp "    Confirmer : " ui_pass2 </dev/tty; echo ""
            if [[ "${ui_pass}" == "${ui_pass2}" ]]; then
                CALEOPE_UI_PASSWORD="${ui_pass}"
                break
            fi
            echo -e "  ${RED}Les mots de passe ne correspondent pas${NC}"
        done
        unset ui_pass ui_pass2
    fi

    # ── Résumé ──
    echo ""
    echo -e "${CYAN}  ┌─────────────────────────────────────────┐${NC}"
    echo -e "${CYAN}  │           Récapitulatif                 │${NC}"
    echo -e "${CYAN}  ├─────────────────────────────────────────┤${NC}"
    echo -e "${CYAN}  │${NC}  Domaine    : ${YELLOW}${CALEOPE_DOMAIN}${NC}"
    echo -e "${CYAN}  │${NC}  Proxy mode : ${YELLOW}${CALEOPE_PROXY_MODE}${NC}"
    echo -e "${CYAN}  │${NC}  Canal      : ${YELLOW}${CALEOPE_CHANNEL}${NC}"
    if [[ -n "${CALEOPE_EMAIL}" ]]; then echo -e "${CYAN}  │${NC}  Email      : ${YELLOW}${CALEOPE_EMAIL}${NC}"; fi
    echo -e "${CYAN}  └─────────────────────────────────────────┘${NC}"
    echo ""
    read -rp "  Confirmer ? [O/n] : " confirm </dev/tty
    if [[ "${confirm,,}" == "n" ]]; then
        CALEOPE_DOMAIN=""
        CALEOPE_PROXY_MODE=""
        CALEOPE_EMAIL=""
        ask_config
    fi
}

# =============================================================================
# VÉRIFICATIONS
# =============================================================================

check_root() {
    log_debug "Vérification des droits root..."
    [[ $EUID -eq 0 ]] || log_error "Ce script doit être exécuté en root"
    log_debug "Droits root OK"
}

check_debian() {
    log_debug "Vérification du système..."
    [[ -f /etc/debian_version ]] || log_error "Ce script est prévu pour Debian uniquement"
    DEBIAN_VERSION=$(cat /etc/debian_version)
    log_debug "Debian détecté : ${DEBIAN_VERSION}"
}

check_debian_codename() {
    log_debug "Vérification du codename Debian..."
    CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
    SUPPORTED=("bookworm" "bullseye" "trixie")

    if [[ ! " ${SUPPORTED[*]} " =~ " ${CODENAME} " ]]; then
        log_warning "Codename '${CODENAME}' non supporté officiellement, fallback sur 'bookworm'"
        DOCKER_CODENAME="bookworm"
    else
        # Trixie n'a pas encore de repo Docker dédié → fallback bookworm
        [[ "${CODENAME}" == "trixie" ]] && DOCKER_CODENAME="bookworm" || DOCKER_CODENAME="${CODENAME}"
    fi
    log_debug "Codename Docker : ${DOCKER_CODENAME}"
}

check_user() {
    log_debug "Vérification de l'utilisateur ${CALEOPE_USER}..."
    if ! id "${CALEOPE_USER}" &>/dev/null; then
        log_step "Utilisateur '${CALEOPE_USER}' absent — création automatique (appliance/ISO)."
        /usr/sbin/useradd -m -s /bin/bash "${CALEOPE_USER}" \
            || log_error "Impossible de créer l'utilisateur '${CALEOPE_USER}'."
        log_success "Utilisateur '${CALEOPE_USER}' créé."
    fi
    log_debug "Utilisateur ${CALEOPE_USER} OK"
}

# =============================================================================
# PRÉREQUIS SYSTÈME
# =============================================================================

install_prerequisites() {
    log_section "Prérequis système"

    if [[ "${OFFLINE_MODE}" != "true" ]]; then
        log_step "Mise à jour des paquets..."
        run_cmd apt-get update
    else
        log_warning "Mode submarine : apt-get update ignoré (pas d'accès réseau)"
    fi

    log_step "Installation des outils de base..."
    run_cmd apt-get install -y \
        curl wget git ca-certificates gnupg lsb-release \
        sudo apt-transport-https \
        tar gzip jq

    log_step "Installation des outils réseau (montages SMB/SFTP)..."
    run_cmd apt-get install -y \
        cifs-utils \
        sshfs \
        fuse3

    log_step "Installation des outils sécurité..."
    run_cmd apt-get install -y \
        ufw \
        fail2ban \
        unattended-upgrades

    log_success "Prérequis installés"
}

# =============================================================================
# DOCKER
# =============================================================================

install_docker() {
    log_section "Docker Engine"

    if command -v docker &>/dev/null; then
        log_warning "Docker déjà installé : $(docker --version)"
        usermod -aG docker "${CALEOPE_USER}" 2>/dev/null || true
        return 0
    fi

    # Submarine PUR (--offline air-gap) : Docker doit être préinstallé. Mais en
    # mode --iso, on a le réseau au 1er boot → on installe Docker normalement
    # (les IMAGES, elles, sont chargées depuis le bundle → pas de pull fragile).
    if [[ "${OFFLINE_MODE}" == "true" && "${ISO_MODE}" != "true" ]]; then
        log_error "Mode submarine : Docker n'est pas installé. Installe Docker avant de lancer l'installation offline (apt-get install docker-ce depuis un dépôt local ou via dpkg)."
    fi

    log_step "Suppression des anciennes versions..."
    run_cmd apt-get remove -y docker.io docker-doc docker-compose \
        podman-docker containerd runc 2>/dev/null || true

    log_step "Ajout du dépôt officiel Docker..."
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/debian/gpg" \
        -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc

    echo \
        "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
        https://download.docker.com/linux/debian ${DOCKER_CODENAME} stable" \
        > /etc/apt/sources.list.d/docker.list

    log_step "Installation de Docker Engine..."
    run_cmd apt-get update
    run_cmd apt-get install -y \
        docker-ce docker-ce-cli containerd.io \
        docker-buildx-plugin docker-compose-plugin

    run_cmd systemctl enable docker
    run_cmd systemctl start docker

    usermod -aG docker "${CALEOPE_USER}"

    log_success "Docker installé : $(docker --version)"
}

# =============================================================================
# SÉCURITÉ — UFW + fail2ban + unattended-upgrades
# =============================================================================

setup_security() {
    log_section "Configuration de la sécurité"

    # ── UFW ──
    log_step "Configuration du pare-feu UFW..."
    ufw --force reset >/dev/null 2>&1 || true
    ufw default deny incoming
    ufw default allow outgoing
    ufw allow 22/tcp comment "SSH"
    ufw allow 80/tcp comment "HTTP Traefik"
    ufw allow 443/tcp comment "HTTPS Traefik"
    ufw --force enable
    log_success "UFW actif (SSH + HTTP/HTTPS autorisés)"

    # ── fail2ban ──
    log_step "Activation fail2ban..."
    systemctl enable fail2ban >/dev/null 2>&1 || true
    systemctl start fail2ban 2>/dev/null || true
    log_success "fail2ban actif"

    # ── unattended-upgrades ──
    log_step "Activation des mises à jour automatiques de sécurité..."
    cat > /etc/apt/apt.conf.d/20auto-upgrades << 'EOF_APT'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF_APT
    systemctl enable unattended-upgrades >/dev/null 2>&1 || true
    systemctl start unattended-upgrades 2>/dev/null || true
    log_success "Mises à jour de sécurité automatiques activées"

    # ── Créer le dossier de logs Caleope ──
    mkdir -p /var/log/caleope
    chmod 750 /var/log/caleope
    chown root:caleope /var/log/caleope 2>/dev/null || true
}

# =============================================================================
# RÉSEAUX DOCKER
# =============================================================================

create_docker_networks() {
    log_section "Réseaux Docker"

    for net in "${DOCKER_NET_PUBLIC}" "${DOCKER_NET_INTERNAL}"; do
        if docker network inspect "${net}" &>/dev/null; then
            log_warning "Réseau '${net}' existe déjà"
        else
            log_step "Création du réseau '${net}'..."
            docker network create --driver bridge "${net}"
            log_success "Réseau '${net}' créé"
        fi
    done
}

# =============================================================================
# BINAIRES CALEOPE
# =============================================================================

install_caleope_binaries() {
    log_section "Binaires Caleope (caleoped + caleope)"

    if [[ "${OFFLINE_MODE}" == "true" ]]; then
        install_binaries_from_bundle
    else
        download_binaries_from_release || log_error "Impossible de télécharger les binaires Caleope depuis GitHub. Vérifie ta connexion ou https://github.com/${GITHUB_REPO}/releases"
    fi
}

install_binaries_from_bundle() {
    local bin_dir="${OFFLINE_BUNDLE_PATH}/binaries"
    log_step "Copie des binaires depuis le bundle..."

    cp "${bin_dir}/caleoped" /usr/local/bin/caleoped
    cp "${bin_dir}/caleope"  /usr/local/bin/caleope
    chmod 755 /usr/local/bin/caleoped /usr/local/bin/caleope

    if [[ -f "${bin_dir}/caleope-ui" ]]; then
        cp "${bin_dir}/caleope-ui" /usr/local/bin/caleope-ui
        chmod 755 /usr/local/bin/caleope-ui
        log_debug "Binaire caleope-ui copié"
    else
        log_warning "caleope-ui absent du bundle — interface web non disponible"
    fi

    ln -sf /usr/local/bin/caleope /usr/local/bin/caleope-store

    local pack_ver="?"
    [[ -f "${OFFLINE_BUNDLE_PATH}/pack-info.json" ]] && \
        pack_ver=$(jq -r '.caleope_version // "?"' "${OFFLINE_BUNDLE_PATH}/pack-info.json" 2>/dev/null)
    log_success "Binaires installés depuis le bundle (version ${pack_ver})"
}

download_binaries_from_release() {
    log_step "Recherche de la dernière release GitHub (canal: ${CALEOPE_CHANNEL:-stable})..."

    # stable → /releases/latest (ignore les pré-releases)
    # alpha  → /releases?per_page=1 (inclut les pré-releases, plus récent en premier)
    local release_info
    if [[ "${CALEOPE_CHANNEL:-stable}" == "alpha" ]]; then
        local raw
        raw=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=1" 2>/dev/null) || {
            log_debug "API GitHub inaccessible"
            return 1
        }
        # L'endpoint renvoie un tableau — on extrait le premier élément
        release_info=$(echo "${raw}" | jq '.[0]' 2>/dev/null)
    else
        release_info=$(curl -fsSL "${GITHUB_API}" 2>/dev/null) || {
            log_debug "API GitHub inaccessible ou repo sans releases"
            return 1
        }
    fi

    # Extraire les URLs des binaires (jq parse le JSON)
    local daemon_url cli_url ui_url
    daemon_url=$(echo "${release_info}" | jq -r '.assets[] | select(.name == "caleoped-linux-amd64") | .browser_download_url' 2>/dev/null)
    cli_url=$(echo "${release_info}" | jq -r '.assets[] | select(.name == "caleope-linux-amd64") | .browser_download_url' 2>/dev/null)
    ui_url=$(echo "${release_info}" | jq -r '.assets[] | select(.name == "caleope-ui-linux-amd64") | .browser_download_url' 2>/dev/null)

    if [[ -z "${daemon_url}" || -z "${cli_url}" ]]; then
        log_debug "Binaires non trouvés dans la release"
        return 1
    fi

    local version
    version=$(echo "${release_info}" | jq -r '.tag_name')
    log_step "Téléchargement des binaires version ${version}..."

    wget -q "${daemon_url}" -O /usr/local/bin/caleoped
    wget -q "${cli_url}"    -O /usr/local/bin/caleope

    if [[ -n "${ui_url}" ]]; then
        wget -q "${ui_url}" -O /usr/local/bin/caleope-ui
        chmod 755 /usr/local/bin/caleope-ui
        log_debug "Binaire caleope-ui téléchargé"
    else
        log_warning "Binaire caleope-ui absent de la release — interface web non disponible"
    fi

    chmod 755 /usr/local/bin/caleoped /usr/local/bin/caleope
    ln -sf /usr/local/bin/caleope /usr/local/bin/caleope-store

    log_success "Binaires installés depuis la release ${version}"
    return 0
}

# =============================================================================
# AUTOCOMPLÉTION BASH
# =============================================================================

install_bash_completion() {
    log_section "Autocomplétion bash"

    local completion_dest="/etc/bash_completion.d/caleope"
    log_step "Installation du script d'autocomplétion..."

    if [[ "${OFFLINE_MODE}" == "true" ]]; then
        if [[ -f "${OFFLINE_BUNDLE_PATH}/caleope-completion.bash" ]]; then
            cp "${OFFLINE_BUNDLE_PATH}/caleope-completion.bash" "${completion_dest}"
            chmod 644 "${completion_dest}"
            log_success "Autocomplétion installée depuis le bundle"
        else
            log_warning "Script d'autocomplétion absent du bundle — ignoré"
        fi
        return 0
    fi

    local completion_url="${GITHUB_RAW}/module/scripts/caleope-completion.bash"
    if wget -q "${completion_url}" -O "${completion_dest}" 2>/dev/null; then
        chmod 644 "${completion_dest}"
        log_success "Autocomplétion installée → ${completion_dest}"
        log_debug "Active dans les nouvelles sessions (ou : source ${completion_dest})"
    else
        log_warning "Impossible de télécharger le script d'autocomplétion — ignoré"
    fi
}

# =============================================================================
# STRUCTURE CALEOPE
# =============================================================================

create_structure() {
    log_section "Structure des répertoires Caleope"
    log_step "Création de l'arborescence..."

    local dirs=(
        "${CALEOPE_ROOT}/core/traefik"
        "${CALEOPE_ROOT}/apps-store"
        "${CALEOPE_ROOT}/apps-installed"
        "${CALEOPE_ROOT}/app-config"
        "${CALEOPE_ROOT}/app-data"
        "${CALEOPE_ROOT}/runtime/apps"
        "${CALEOPE_ROOT}/runtime/events"
        "${CALEOPE_ROOT}/backups"
        "${CALEOPE_ROOT}/logs"
        # Données des services core
        "${CALEOPE_ROOT}/data/traefik/certs"
    )

    for dir in "${dirs[@]}"; do
        mkdir -p "${dir}"
        log_debug "Créé : ${dir}"
    done

    chown -R "${CALEOPE_USER}:${CALEOPE_USER}" "${CALEOPE_ROOT}"
    chmod -R 755 "${CALEOPE_ROOT}"

    log_success "Arborescence créée dans ${CALEOPE_ROOT}"
}

# =============================================================================
# GROUPE CALEOPE (accès au socket)
# =============================================================================

setup_caleope_group() {
    log_section "Groupe système Caleope"

    groupadd -f "${CALEOPE_GROUP}"
    log_debug "Groupe '${CALEOPE_GROUP}' créé (ou existait déjà)"

    usermod -aG "${CALEOPE_GROUP}" "${CALEOPE_USER}"
    log_success "Utilisateur '${CALEOPE_USER}' ajouté au groupe '${CALEOPE_GROUP}'"
}

# =============================================================================
# SERVICE SYSTEMD CALEOPED
# =============================================================================

install_caleoped_service() {
    log_section "Service caleoped"

    log_step "Installation du fichier service..."
    cat > /etc/systemd/system/caleoped.service << EOF
[Unit]
Description=Caleope Application Daemon
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
Group=${CALEOPE_GROUP}
ExecStartPre=/bin/rm -f ${SOCKET_PATH}
ExecStart=/usr/local/bin/caleoped --base-dir ${CALEOPE_ROOT} --socket ${SOCKET_PATH}
Restart=on-failure
RestartSec=5
TimeoutStopSec=30
Environment=HOME=/root
StandardOutput=journal
StandardError=journal
SyslogIdentifier=caleoped
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable caleoped
    systemctl start caleoped || true

    # Attendre que le socket soit créé (max 10s)
    local retries=0
    until [[ -S "${SOCKET_PATH}" ]] || [[ $retries -ge 10 ]]; do
        sleep 1
        (( retries++ )) || true
    done

    if [[ -S "${SOCKET_PATH}" ]]; then
        log_success "Daemon caleoped actif"
    else
        log_warning "Daemon démarré mais socket non détecté — vérifier : journalctl -u caleoped"
    fi
}

# =============================================================================
# SERVICE SYSTEMD CALEOPE-UI
# =============================================================================

install_caleope_ui_service() {
    log_section "Service caleope-ui (interface web)"

    if ! command -v caleope-ui &>/dev/null; then
        log_warning "Binaire caleope-ui absent — interface web non installée"
        return 0
    fi

    log_step "Installation du fichier service caleope-ui..."
    cat > /etc/systemd/system/caleope-ui.service << EOF
[Unit]
Description=Caleope UI Server
After=network.target caleoped.service
Requires=caleoped.service

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/caleope-ui \\
    --base-dir ${CALEOPE_ROOT} \\
    --daemon   http://127.0.0.1:8765 \\
    --port     ${PORT_UI}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable caleope-ui
    systemctl start caleope-ui || true

    sleep 2

    if systemctl is-active --quiet caleope-ui; then
        local IP
        IP=$(hostname -I | awk '{print $1}')
        log_success "Interface web active → http://${IP}:${PORT_UI}"
    else
        log_warning "caleope-ui démarré — vérifier : journalctl -u caleope-ui"
    fi
}

# =============================================================================
# TRAEFIK
# =============================================================================

deploy_traefik() {
    log_section "Traefik (reverse proxy)"

    if docker ps --format '{{.Names}}' | grep -q "^traefik$"; then
        log_warning "Traefik déjà en cours d'exécution"
        return 0
    fi

    local IP
    IP=$(hostname -I | awk '{print $1}')

    log_step "Génération de la configuration Traefik (mode: ${CALEOPE_PROXY_MODE})..."

    mkdir -p "${CALEOPE_ROOT}/data/traefik/dynamic"
    touch "${CALEOPE_ROOT}/data/traefik/certs/acme.json"
    chmod 600 "${CALEOPE_ROOT}/data/traefik/certs/acme.json"

    # traefik.yml — trois modes selon config
    if [[ "${CALEOPE_PROXY_MODE}" == "npm" ]]; then
        # Mode NPM : Traefik reçoit HTTP depuis NPM, pas de gestion des certs
        cat > "${CALEOPE_ROOT}/data/traefik/traefik.yml" << EOF
global:
  checkNewVersion: false
  sendAnonymousUsage: false

api:
  dashboard: true
  insecure: true

entryPoints:
  web:
    address: ":${PORT_TRAEFIK_HTTP}"
    http:
      middlewares:
        - secure-headers@file
  websecure:
    address: ":${PORT_TRAEFIK_HTTPS}"
    http:
      middlewares:
        - secure-headers@file

providers:
  docker:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
    network: ${DOCKER_NET_PUBLIC}
  file:
    directory: /etc/traefik/dynamic
    watch: true
EOF
    elif [[ "${CALEOPE_PROXY_MODE}" == "standalone" ]]; then
        # Mode standalone : HTTP seul, sans Let's Encrypt — pour LAN/offline/air-gap
        cat > "${CALEOPE_ROOT}/data/traefik/traefik.yml" << EOF
global:
  checkNewVersion: false
  sendAnonymousUsage: false

api:
  dashboard: true
  insecure: true

entryPoints:
  web:
    address: ":${PORT_TRAEFIK_HTTP}"
    http:
      middlewares:
        - secure-headers@file

providers:
  docker:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
    network: ${DOCKER_NET_PUBLIC}
  file:
    directory: /etc/traefik/dynamic
    watch: true
EOF
    else
        # Mode Traefik natif : gestion Let's Encrypt directe
        cat > "${CALEOPE_ROOT}/data/traefik/traefik.yml" << EOF
global:
  checkNewVersion: false
  sendAnonymousUsage: false

api:
  dashboard: true
  insecure: true

entryPoints:
  web:
    address: ":${PORT_TRAEFIK_HTTP}"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
          permanent: true
  websecure:
    address: ":${PORT_TRAEFIK_HTTPS}"
    http:
      middlewares:
        - secure-headers@file

providers:
  docker:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
    network: ${DOCKER_NET_PUBLIC}
  file:
    directory: /etc/traefik/dynamic
    watch: true

certificatesResolvers:
  letsencrypt:
    acme:
      email: ${CALEOPE_EMAIL}
      storage: /certs/acme.json
      httpChallenge:
        entryPoint: web
EOF
    fi

    # secure-headers.yml — middleware de sécurité HTTP commun à tous les modes
    cat > "${CALEOPE_ROOT}/data/traefik/dynamic/secure-headers.yml" << 'EOF'
http:
  middlewares:
    secure-headers:
      headers:
        browserXssFilter: true
        contentTypeNosniff: true
        frameDeny: true
        stsSeconds: 31536000
        stsIncludeSubdomains: true
        stsPreload: true
        customResponseHeaders:
          X-Powered-By: ""
          Server: ""
EOF

    # compose.yml Traefik (commun aux deux modes)
    cat > "${CALEOPE_ROOT}/core/traefik/compose.yml" << EOF
services:
  traefik:
    image: traefik:v2.11
    container_name: traefik
    restart: unless-stopped
    environment:
      - DOCKER_API_VERSION=1.45
    ports:
      - "${PORT_TRAEFIK_HTTP}:${PORT_TRAEFIK_HTTP}"
      - "${PORT_TRAEFIK_HTTPS}:${PORT_TRAEFIK_HTTPS}"
      - "${PORT_TRAEFIK_DASHBOARD}:${PORT_TRAEFIK_DASHBOARD}"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ${CALEOPE_ROOT}/data/traefik/traefik.yml:/etc/traefik/traefik.yml:ro
      - ${CALEOPE_ROOT}/data/traefik/dynamic:/etc/traefik/dynamic:ro
      - ${CALEOPE_ROOT}/data/traefik/certs:/certs
    networks:
      - ${DOCKER_NET_PUBLIC}
      - ${DOCKER_NET_INTERNAL}
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.dashboard.rule=Host(\`traefik.${CALEOPE_DOMAIN}\`)"
      - "traefik.http.routers.dashboard.entrypoints=web"
      - "traefik.http.routers.dashboard.service=api@internal"

networks:
  ${DOCKER_NET_PUBLIC}:
    external: true
  ${DOCKER_NET_INTERNAL}:
    external: true
EOF

    chown -R "${CALEOPE_USER}:${CALEOPE_USER}" \
        "${CALEOPE_ROOT}/core/traefik" \
        "${CALEOPE_ROOT}/data/traefik"

    log_step "Démarrage de Traefik..."
    docker compose -f "${CALEOPE_ROOT}/core/traefik/compose.yml" up -d

    local retries=0
    until docker ps --format '{{.Names}}' | grep -q "^traefik$" || [[ $retries -ge 15 ]]; do
        sleep 1
        (( retries++ )) || true
    done

    if docker ps --format '{{.Names}}' | grep -q "^traefik$"; then
        log_success "Traefik actif"
        log_debug "Dashboard : http://${IP}:${PORT_TRAEFIK_DASHBOARD}"
    else
        log_warning "Traefik ne semble pas démarré — vérifier : docker logs traefik"
    fi
}

# =============================================================================
# APPS PAR DÉFAUT (CrowdSec + Authentik)
# =============================================================================

install_default_apps() {
    log_section "Applications de sécurité par défaut"

    if [[ "${OFFLINE_MODE}" == "true" ]]; then
        log_warning "Mode submarine : installation des apps par défaut ignorée (images déjà chargées — installer via 'caleope install' après le démarrage)"
        return 0
    fi

    # Attendre que le daemon soit opérationnel (max 30s)
    local max=15 waited=0
    log_step "Attente du daemon caleoped..."
    until caleope list &>/dev/null 2>&1; do
        sleep 2; waited=$((waited + 2))
        if [[ ${waited} -ge ${max} ]]; then
            log_warning "Daemon non joignable — skip installation par défaut"
            return 0
        fi
    done

    # CrowdSec — IDS/IPS réseau (pas de params requis, disponible sur canal alpha)
    if caleope store 2>/dev/null | grep -q "crowdsec"; then
        log_step "Installation de CrowdSec (IDS/IPS)..."
        if caleope install crowdsec 2>/dev/null; then
            log_success "CrowdSec installé"
        else
            log_warning "CrowdSec non installé (sera disponible via 'caleope install crowdsec')"
        fi
    else
        log_warning "CrowdSec non disponible dans ce canal — à installer via 'caleope install crowdsec' (canal alpha)"
    fi

    # Authentik — Identity Provider SSO (secrets générés automatiquement)
    log_step "Installation d'Authentik (SSO/Identity Provider)..."
    if caleope install authentik 2>/dev/null; then
        log_success "Authentik installé"
    else
        log_warning "Authentik non installé (sera disponible via 'caleope install authentik')"
    fi
}

# =============================================================================
# COCKPIT
# =============================================================================

# Cockpit remplacé par le terminal intégré dans Caleope UI (section SERVEUR)

# =============================================================================
# SUDO
# =============================================================================

configure_sudo() {
    log_section "Configuration sudo"
    run_cmd apt-get install -y sudo
    /usr/sbin/usermod -aG sudo "${CALEOPE_USER}"
    log_success "'${CALEOPE_USER}' ajouté au groupe sudo"
}

# =============================================================================
# INITIALISATION RUNTIME CALEOPE
# =============================================================================

init_caleope_runtime() {
    log_section "Initialisation du runtime Caleope"

    # Initialiser repos.json avec le repo officiel
    cat > "${CALEOPE_ROOT}/runtime/repos.json" << EOF
[
  {
    "name": "official",
    "url": "https://github.com/${GITHUB_REPO}-store",
    "trust": "official",
    "local_dir": "${CALEOPE_ROOT}/core/cache/official",
    "last_sync": "0001-01-01T00:00:00Z"
  }
]
EOF

    # Initialiser ports.json vide
    echo "{}" > "${CALEOPE_ROOT}/runtime/ports.json"

    chown -R "${CALEOPE_USER}:${CALEOPE_USER}" "${CALEOPE_ROOT}/runtime"

    log_success "Runtime initialisé"
}

# =============================================================================
# SAUVEGARDE DE LA CONFIG
# =============================================================================

save_config() {
    log_section "Sauvegarde de la configuration"

    # caleope.conf — fichier de config persistante
    # Utilisé par caleope pour construire les domaines automatiquement
    cat > "${CALEOPE_ROOT}/caleope.conf" << EOF
# Configuration Caleope — généré à l'installation
# Modifiable à tout moment, rechargé par le daemon

CALEOPE_DOMAIN=${CALEOPE_DOMAIN}
CALEOPE_PROXY_MODE=${CALEOPE_PROXY_MODE}
CALEOPE_EMAIL=${CALEOPE_EMAIL}
CALEOPE_CHANNEL=${CALEOPE_CHANNEL}
CALEOPE_VERSION=0.1.0

# SMTP global — transmis automatiquement aux apps compatibles
CALEOPE_SMTP_HOST=${CALEOPE_SMTP_HOST}
CALEOPE_SMTP_PORT=${CALEOPE_SMTP_PORT}
CALEOPE_SMTP_USER=${CALEOPE_SMTP_USER}
CALEOPE_SMTP_PASS=${CALEOPE_SMTP_PASS}
CALEOPE_SMTP_FROM=${CALEOPE_SMTP_FROM}

# Registre d'images miroir (optionnel) — vide = Docker Hub par défaut
CALEOPE_REGISTRY=${CALEOPE_REGISTRY}
CALEOPE_REGISTRY_USER=${CALEOPE_REGISTRY_USER}
CALEOPE_REGISTRY_PASS=${CALEOPE_REGISTRY_PASS}
EOF

    chmod 640 "${CALEOPE_ROOT}/caleope.conf"
    chown "${CALEOPE_USER}:${CALEOPE_GROUP}" "${CALEOPE_ROOT}/caleope.conf"

    # Mot de passe interface web
    if [[ -n "${CALEOPE_UI_PASSWORD}" ]]; then
        mkdir -p "${CALEOPE_ROOT}/core/daemon"
        echo -n "${CALEOPE_UI_PASSWORD}" > "${CALEOPE_ROOT}/core/daemon/ui-password"
        chmod 600 "${CALEOPE_ROOT}/core/daemon/ui-password"
        CALEOPE_UI_PASSWORD=""
        log_success "Mot de passe UI enregistré"
    fi

    # Si un registre miroir est configuré avec des identifiants, tenter un
    # docker login (best-effort) pour que les pulls d'images fonctionnent.
    if [[ -n "${CALEOPE_REGISTRY}" && -n "${CALEOPE_REGISTRY_USER}" && -n "${CALEOPE_REGISTRY_PASS}" ]] \
        && command -v docker >/dev/null 2>&1; then
        log_step "Connexion au registre miroir ${CALEOPE_REGISTRY}..."
        if echo "${CALEOPE_REGISTRY_PASS}" | docker login "${CALEOPE_REGISTRY}" \
            -u "${CALEOPE_REGISTRY_USER}" --password-stdin >/dev/null 2>&1; then
            log_success "Connecté au registre miroir"
        else
            log_warning "Connexion au registre miroir échouée (pulls Docker Hub par défaut)"
        fi
    fi

    log_success "Config sauvegardée dans ${CALEOPE_ROOT}/caleope.conf"
    log_debug "  CALEOPE_DOMAIN=${CALEOPE_DOMAIN}"
    log_debug "  CALEOPE_PROXY_MODE=${CALEOPE_PROXY_MODE}"
}

# =============================================================================
# INIT CHIFFREMENT SECRETS
# =============================================================================

init_secrets_encryption() {
    log_section "Initialisation du chiffrement des secrets"

    if [[ -z "${CALEOPE_SECRETS_PASSWORD}" ]]; then
        log_warning "Pas de mot de passe fourni — chiffrement ignoré"
        return 0
    fi

    local init_file="${CALEOPE_ROOT}/core/daemon/secrets-init-password"
    mkdir -p "$(dirname "${init_file}")"
    echo -n "${CALEOPE_SECRETS_PASSWORD}" > "${init_file}"
    chmod 600 "${init_file}"
    chown "${CALEOPE_USER}:${CALEOPE_USER}" "${init_file}" 2>/dev/null || true
    # Effacer le mot de passe de la mémoire
    CALEOPE_SECRETS_PASSWORD=""

    log_success "Mot de passe transmis au daemon (sera supprimé au premier démarrage)"
    log_warning "Conservez ce mot de passe en lieu sûr — il ne peut pas être récupéré !"
}

# =============================================================================
# SYNC INITIALE DU STORE
# =============================================================================

sync_store() {
    log_section "Synchronisation du store Caleope"

    local store_dir="${CALEOPE_ROOT}/core/cache/official"

    if [[ "${OFFLINE_MODE}" == "true" ]]; then
        local store_archive="${OFFLINE_BUNDLE_PATH}/store.tar.gz"
        log_step "Extraction du store depuis le bundle..."
        mkdir -p "${store_dir}"
        tar -xzf "${store_archive}" -C "${store_dir}" --strip-components=1
        chown -R "${CALEOPE_USER}:${CALEOPE_USER}" "${store_dir}"
        log_success "Store extrait — $(find "${store_dir}/apps" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l) application(s) disponible(s)"
        return 0
    fi

    local store_url="https://github.com/${GITHUB_REPO}-store"

    # Supprimer le dossier s'il existe déjà mais est vide (créé par mkdir -p)
    # git clone refuse de cloner dans un dossier non vide
    if [[ -d "${store_dir}" ]] && [[ -z "$(ls -A "${store_dir}")" ]]; then
        rm -rf "${store_dir}"
        log_debug "Dossier cache vide supprimé pour permettre le clone"
    fi

    git config --global --add safe.directory "${store_dir}"

    # La branche du store suit le canal : alpha → origin/alpha, stable → origin/main
    local store_branch="main"
    [[ "${CALEOPE_CHANNEL:-stable}" == "alpha" ]] && store_branch="alpha"

    if [[ -d "${store_dir}/.git" ]]; then
        log_step "Mise à jour du store (branche ${store_branch})..."
        git -C "${store_dir}" fetch origin "${store_branch}"
        git -C "${store_dir}" reset --hard FETCH_HEAD
    else
        log_step "Téléchargement du store depuis GitHub (branche ${store_branch})..."
        git clone --depth=1 --branch "${store_branch}" "${store_url}" "${store_dir}"
    fi

    chown -R "${CALEOPE_USER}:${CALEOPE_USER}" "${store_dir}"
    log_success "Store synchronisé — $(find "${store_dir}/apps" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l) application(s) disponible(s)"
}

# =============================================================================
# FICHIER RÉSUMÉ
# =============================================================================

generate_links_file() {
    local IP
    IP=$(hostname -I | awk '{print $1}')

    cat > "${CALEOPE_ROOT}/LIENS.md" << EOF
# Caleope — Accès aux services

## Services

| Service            | URL                                         |
|--------------------|---------------------------------------------|
| Caleope UI         | http://${IP}:${PORT_UI}                     |
| Traefik dashboard  | http://${IP}:${PORT_TRAEFIK_DASHBOARD}      |
| Terminal           | Intégré dans Caleope UI → section SERVEUR   |

---

## Caleope CLI

\`\`\`bash
caleope ping
caleope list
caleope search <terme>
caleope install <app>
\`\`\`

---

## Arborescence

- Racine      : ${CALEOPE_ROOT}
- Apps store  : ${CALEOPE_ROOT}/apps-store
- Apps datas  : ${CALEOPE_ROOT}/app-data
- Runtime     : ${CALEOPE_ROOT}/runtime

---

## Notes

- Déconnecte/reconnecte ta session pour Docker sans sudo
- Le daemon Caleope : systemctl status caleoped
- Logs daemon : journalctl -u caleoped -f
EOF

    chown "${CALEOPE_USER}:${CALEOPE_USER}" "${CALEOPE_ROOT}/LIENS.md"
}

# =============================================================================
# RÉSUMÉ FINAL
# =============================================================================

print_summary() {
    local IP
    IP=$(hostname -I | awk '{print $1}')

    # Test rapide du daemon
    local daemon_status="❌ inactif"
    systemctl is-active --quiet caleoped && daemon_status="✅ actif"

    echo ""
    echo -e "${GREEN}"
    echo "  ██████╗ █████╗ ██╗     ███████╗ ██████╗ ██████╗ ███████╗"
    echo " ██╔════╝██╔══██╗██║     ██╔════╝██╔═══██╗██╔══██╗██╔════╝"
    echo " ██║     ███████║██║     █████╗  ██║   ██║██████╔╝█████╗  "
    echo " ██║     ██╔══██║██║     ██╔══╝  ██║   ██║██╔═══╝ ██╔══╝  "
    echo " ╚██████╗██║  ██║███████╗███████╗╚██████╔╝██║     ███████╗"
    echo "  ╚═════╝╚═╝  ╚═╝╚══════╝╚══════╝ ╚═════╝ ╚═╝     ╚══════╝"
    echo -e "${NC}"
    echo -e "${CYAN}Installation terminée !${NC}"
    echo ""
    echo -e "${CYAN}╔══════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║                   Services actifs                    ║${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    # Test statut UI
    local ui_status="❌ inactif"
    systemctl is-active --quiet caleope-ui 2>/dev/null && ui_status="✅ actif"

    echo -e "${CYAN}║${NC}  🌐 Caleope UI  → ${YELLOW}http://${IP}:${PORT_UI}${NC}"
    echo -e "${CYAN}║${NC}  🔀 Traefik     → ${YELLOW}http://${IP}:${PORT_TRAEFIK_DASHBOARD}${NC}"
    echo -e "${CYAN}║${NC}  🖥️  Terminal    → ${YELLOW}Caleope UI → section SERVEUR${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║                   Caleope Daemon                     ║${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC}  Daemon : ${daemon_status}  |  UI : ${ui_status}"
    echo -e "${CYAN}║${NC}  Socket : ${SOCKET_PATH}"
    echo -e "${CYAN}║${NC}  Logs   : ${YELLOW}journalctl -u caleoped -f${NC}"
    echo -e "${CYAN}║${NC}  UI     : ${YELLOW}journalctl -u caleope-ui -f${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║                   Prochaines étapes                  ║${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC}  ${YELLOW}⚠️  Reconnecte-toi en tant que ${CALEOPE_USER}${NC}"
    echo -e "${CYAN}║${NC}     ${YELLOW}pour utiliser Docker et caleope${NC}"
    echo -e "${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}  ${GREEN}caleope ping${NC}           # tester le daemon"
    echo -e "${CYAN}║${NC}  ${GREEN}caleope search media${NC}   # chercher une app"
    echo -e "${CYAN}║${NC}  ${GREEN}caleope install jellyfin${NC}"
    echo -e "${CYAN}╚══════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  📄 Résumé complet : ${YELLOW}${CALEOPE_ROOT}/LIENS.md${NC}"
    echo ""
}

# =============================================================================
# WIZARD DE PREMIER DÉMARRAGE (ISO)
# =============================================================================

FIRSTBOOT_MARKER="${CALEOPE_ROOT}/.firstboot-needed"
FIRSTBOOT_INSTALLER="${CALEOPE_ROOT}/bin/install.sh"
FIRSTBOOT_UNIT="/etc/systemd/system/caleope-firstboot.service"

# Installe le service systemd qui lance le wizard interactif au 1er démarrage.
# Appelé uniquement en mode --iso (install de base).
setup_firstboot() {
    log_section "Wizard de premier démarrage"

    # Copier ce script à un emplacement stable (le payload ISO peut disparaître)
    mkdir -p "${CALEOPE_ROOT}/bin"
    if cp "${BASH_SOURCE[0]}" "${FIRSTBOOT_INSTALLER}" 2>/dev/null; then
        chmod 755 "${FIRSTBOOT_INSTALLER}"
    else
        log_warning "Impossible de copier install.sh — le wizard utilisera le payload d'origine"
        FIRSTBOOT_INSTALLER="${SCRIPT_DIR}/install.sh"
    fi

    # Marqueur : le wizard ne s'exécute que s'il est présent (idempotence)
    touch "${FIRSTBOOT_MARKER}"

    # Unité systemd : s'exécute sur tty1, prend la main sur getty le temps du wizard
    cat > "${FIRSTBOOT_UNIT}" << EOF
[Unit]
Description=Caleope — Assistant de premier démarrage
After=network-online.target docker.service caleoped.service
Wants=network-online.target
Conflicts=getty@tty1.service
Before=getty.target
ConditionPathExists=${FIRSTBOOT_MARKER}

[Service]
Type=oneshot
ExecStart=/bin/bash ${FIRSTBOOT_INSTALLER} --iso-finalize
StandardInput=tty-force
StandardOutput=tty
StandardError=tty
TTYPath=/dev/tty1
TTYReset=yes
TTYVHangup=yes
RemainAfterExit=no

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload 2>/dev/null || true
    systemctl enable caleope-firstboot.service 2>/dev/null || true
    log_success "Wizard de 1er démarrage activé (s'exécutera au prochain boot sur la console)"
}

# Flux interactif exécuté au 1er démarrage (via caleope-firstboot.service).
# Complète la config différée, déploie Traefik + apps, puis se désactive.
run_iso_finalize() {
    check_root

    # Si le marqueur a disparu (déjà configuré), on ne rejoue pas le wizard
    if [[ ! -f "${FIRSTBOOT_MARKER}" ]]; then
        echo -e "${GRAY}Configuration déjà effectuée — rien à faire.${NC}"
        systemctl disable caleope-firstboot.service 2>/dev/null || true
        return
    fi

    clear 2>/dev/null || true
    echo -e "${CYAN}"
    echo "╔══════════════════════════════════════════════════════╗"
    echo "║   Bienvenue sur Caleope — Configuration initiale     ║"
    echo "╚══════════════════════════════════════════════════════╝"
    echo -e "${NC}"
    echo -e "  ${GRAY}Quelques questions pour finaliser ton serveur.${NC}\n"

    # Préserver le canal choisi à la construction de l'ISO
    if [[ -f "${CALEOPE_ROOT}/caleope.conf" ]]; then
        CALEOPE_CHANNEL=$(grep -E '^CALEOPE_CHANNEL=' "${CALEOPE_ROOT}/caleope.conf" 2>/dev/null | cut -d= -f2)
    fi

    # Poser les vraies questions (domaine, proxy, email, SMTP, mots de passe)
    ask_config

    # Écrire la config réelle + transmettre les secrets au daemon
    save_config
    init_secrets_encryption

    # Recharger le daemon avec le vrai domaine
    log_step "Redémarrage du daemon avec la configuration finale..."
    systemctl restart caleoped 2>/dev/null || true
    sleep 3

    # Déployer le reverse proxy et les apps par défaut
    deploy_traefik
    install_default_apps
    generate_links_file

    # Terminé : retirer le marqueur et désactiver le service
    rm -f "${FIRSTBOOT_MARKER}"
    systemctl disable caleope-firstboot.service 2>/dev/null || true
    # Rendre la main à getty (session de login normale)
    systemctl start getty@tty1.service 2>/dev/null || true

    print_summary
    echo -e "  ${GREEN}Configuration terminée — ce wizard ne se relancera plus.${NC}\n"
}

# Résumé affiché après l'install de base ISO (avant le 1er démarrage réel).
print_iso_summary() {
    echo ""
    echo -e "${GREEN}◆ Image Caleope installée (base).${NC}"
    echo -e "  ${GRAY}Au prochain démarrage, un assistant configurera le domaine,${NC}"
    echo -e "  ${GRAY}le reverse proxy et les mots de passe, puis déploiera les apps.${NC}"
    echo ""
}

# =============================================================================
# MAIN
# =============================================================================

main() {
    parse_args "$@"

    clear 2>/dev/null || true
    echo -e "${CYAN}"
    echo "╔══════════════════════════════════════════╗"
    echo "║      Caleope — Installation              ║"
    echo "║           by Gaiver-IT                   ║"
    echo "║              v0.1.0                      ║"
    echo "╚══════════════════════════════════════════╝"
    echo -e "${NC}"

    # ── Mode finalize (wizard 1er démarrage) : flux dédié, puis on sort ──
    if [[ "${ISO_FINALIZE}" == "true" ]]; then
        run_iso_finalize
        return
    fi

    if [[ "${ISO_MODE}" == "true" ]]; then
        echo -e "${YELLOW}Mode : ISO (install de base — config au 1er démarrage)${NC}\n"
    elif [[ "${OFFLINE_MODE}" == "true" ]]; then
        echo -e "${YELLOW}Mode : SUBMARINE (offline) — bundle : ${OFFLINE_BUNDLE_PATH}${NC}\n"
    elif [[ "${LOG_MODE}" == "debug" ]]; then
        echo -e "${GRAY}Mode : DEBUG${NC}\n"
    else
        echo -e "${GRAY}Mode : CLASSIC  |  --debug pour plus de détails${NC}\n"
    fi

    # Vérifications préalables
    check_root
    check_debian
    check_debian_codename
    check_user
    check_offline_bundle       # Valide le bundle si --offline / --iso

    # Configuration interactive (domaine, mode proxy, email) — placeholders en ISO
    ask_config

    # Installation dans l'ordre
    configure_sudo             # En premier — nécessaire pour la suite
    install_prerequisites
    install_docker
    setup_security             # UFW + fail2ban + unattended-upgrades
    create_docker_networks
    load_docker_images_from_bundle  # Mode submarine/ISO offline : charge les images .tar
    create_structure
    setup_caleope_group
    install_caleope_binaries   # release GitHub → bundle (mode submarine/ISO)
    install_bash_completion    # tab completion pour caleope
    init_caleope_runtime
    save_config                # Sauvegarder domaine + mode proxy + SMTP dans caleope.conf
    init_secrets_encryption    # Transmettre mot de passe chiffrement au daemon
    sync_store                 # git clone officiel → extract bundle (mode submarine/ISO)
    install_caleoped_service
    install_caleope_ui_service

    # En mode ISO, on s'arrête ici : le domaine/proxy réels et le déploiement
    # (Traefik + apps par défaut) sont faits au 1er démarrage par le wizard.
    if [[ "${ISO_MODE}" == "true" ]]; then
        setup_firstboot
        generate_links_file
        print_iso_summary
        return
    fi

    deploy_traefik
    install_default_apps
    generate_links_file
    print_summary
}

main "$@"
exit 0
