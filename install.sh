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

parse_args() {
    for arg in "$@"; do
        case $arg in
            --debug)   LOG_MODE="debug"   ;;
            --classic) LOG_MODE="classic" ;;
        esac
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
PORT_PORTAINER=8010
PORT_COCKPIT=8020

# Réseaux Docker
DOCKER_NET_PUBLIC="caleope-public"
DOCKER_NET_INTERNAL="caleope-internal"

# Config interactive (remplie par ask_config)
CALEOPE_DOMAIN=""
CALEOPE_EMAIL=""
CALEOPE_PROXY_MODE=""   # "npm" = NPM en amont, "traefik" = Traefik gère les certs
CALEOPE_CHANNEL=""      # "stable" = releases officielles, "alpha" = pré-releases
CALEOPE_SMTP_HOST=""
CALEOPE_SMTP_PORT=""
CALEOPE_SMTP_USER=""
CALEOPE_SMTP_PASS=""
CALEOPE_SMTP_FROM=""
CALEOPE_SECRETS_PASSWORD=""

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
# CONFIGURATION INTERACTIVE
# =============================================================================

ask_config() {
    log_section "Configuration de Caleope"

    # Mode non-interactif : si les variables essentielles sont déjà définies en
    # variables d'environnement, on les utilise directement sans prompts.
    # Usage : CALEOPE_DOMAIN=... CALEOPE_PROXY_MODE=traefik CALEOPE_CHANNEL=alpha bash install.sh
    if [[ -n "${CALEOPE_DOMAIN:-}" && -n "${CALEOPE_PROXY_MODE:-}" && -n "${CALEOPE_CHANNEL:-}" ]]; then
        log_step "Mode non-interactif détecté (variables d'environnement)"
        echo -e "  Domaine    : ${YELLOW}${CALEOPE_DOMAIN}${NC}"
        echo -e "  Proxy mode : ${YELLOW}${CALEOPE_PROXY_MODE}${NC}"
        echo -e "  Canal      : ${YELLOW}${CALEOPE_CHANNEL}${NC}"
        [[ -n "${CALEOPE_EMAIL:-}" ]] && echo -e "  Email      : ${YELLOW}${CALEOPE_EMAIL}${NC}"
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
    while [[ "${CALEOPE_PROXY_MODE}" != "npm" && "${CALEOPE_PROXY_MODE}" != "traefik" ]]; do
        read -rp "  → Choix [1/2] : " proxy_choice </dev/tty
        case "${proxy_choice}" in
            1) CALEOPE_PROXY_MODE="npm" ;;
            2) CALEOPE_PROXY_MODE="traefik" ;;
            *) echo -e "  ${RED}Choix invalide, entre 1 ou 2${NC}" ;;
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
    echo -e "${BLUE}  Serveur SMTP (optionnel)${NC}"
    echo -e "  ${GRAY}Permet aux apps (Vaultwarden, Nextcloud, Gitea...) d'envoyer des emails.${NC}"
    echo -e "  ${GRAY}Laisser vide pour configurer plus tard via caleope configure.${NC}"
    read -rp "  → Hôte SMTP (ex: smtp.gmail.com, vide pour ignorer) : " CALEOPE_SMTP_HOST </dev/tty
    if [[ -n "${CALEOPE_SMTP_HOST}" ]]; then
        read -rp "  → Port SMTP (ex: 587) : " CALEOPE_SMTP_PORT </dev/tty
        CALEOPE_SMTP_PORT="${CALEOPE_SMTP_PORT:-587}"
        read -rp "  → Utilisateur SMTP : " CALEOPE_SMTP_USER </dev/tty
        read -rsp "  → Mot de passe SMTP : " CALEOPE_SMTP_PASS </dev/tty
        echo ""
        read -rp "  → Adresse expéditeur (ex: noreply@mondomaine.com) : " CALEOPE_SMTP_FROM </dev/tty
    fi

    # ── Mot de passe pour le chiffrement des secrets ──
    echo ""
    echo -e "${BLUE}  Chiffrement des secrets (recommandé)${NC}"
    echo -e "  ${GRAY}Un mot de passe protège vos credentials d'apps.${NC}"
    echo -e "  ${GRAY}Conservez-le précieusement — il ne peut pas être récupéré si perdu.${NC}"
    echo -e "  ${GRAY}Laisser vide pour désactiver le chiffrement.${NC}"
    read -rsp "  → Mot de passe secrets (vide pour ignorer) : " CALEOPE_SECRETS_PASSWORD </dev/tty
    echo ""
    if [[ -n "${CALEOPE_SECRETS_PASSWORD}" ]]; then
        read -rsp "  → Confirmer le mot de passe : " CALEOPE_SECRETS_PASSWORD_CONFIRM </dev/tty
        echo ""
        if [[ "${CALEOPE_SECRETS_PASSWORD}" != "${CALEOPE_SECRETS_PASSWORD_CONFIRM}" ]]; then
            echo -e "  ${RED}Les mots de passe ne correspondent pas. Chiffrement désactivé.${NC}"
            CALEOPE_SECRETS_PASSWORD=""
        fi
    fi

    # ── Résumé ──
    echo ""
    echo -e "${CYAN}  ┌─────────────────────────────────────────┐${NC}"
    echo -e "${CYAN}  │           Récapitulatif                 │${NC}"
    echo -e "${CYAN}  ├─────────────────────────────────────────┤${NC}"
    echo -e "${CYAN}  │${NC}  Domaine    : ${YELLOW}${CALEOPE_DOMAIN}${NC}"
    echo -e "${CYAN}  │${NC}  Proxy mode : ${YELLOW}${CALEOPE_PROXY_MODE}${NC}"
    echo -e "${CYAN}  │${NC}  Canal      : ${YELLOW}${CALEOPE_CHANNEL}${NC}"
    [[ -n "${CALEOPE_EMAIL}" ]] &&     echo -e "${CYAN}  │${NC}  Email      : ${YELLOW}${CALEOPE_EMAIL}${NC}"
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
    id "${CALEOPE_USER}" &>/dev/null || log_error "L'utilisateur '${CALEOPE_USER}' n'existe pas. Crée-le avant de lancer ce script : useradd -m -s /bin/bash ${CALEOPE_USER}"
    log_debug "Utilisateur ${CALEOPE_USER} OK"
}

# =============================================================================
# PRÉREQUIS SYSTÈME
# =============================================================================

install_prerequisites() {
    log_section "Prérequis système"
    log_step "Mise à jour des paquets..."
    run_cmd apt-get update

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
        return 0
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

    download_binaries_from_release || log_error "Impossible de télécharger les binaires Caleope depuis GitHub. Vérifie ta connexion ou https://github.com/${GITHUB_REPO}/releases"
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
    local daemon_url cli_url
    daemon_url=$(echo "${release_info}" | jq -r '.assets[] | select(.name == "caleoped-linux-amd64") | .browser_download_url' 2>/dev/null)
    cli_url=$(echo "${release_info}" | jq -r '.assets[] | select(.name == "caleope-linux-amd64") | .browser_download_url' 2>/dev/null)

    if [[ -z "${daemon_url}" || -z "${cli_url}" ]]; then
        log_debug "Binaires non trouvés dans la release"
        return 1
    fi

    local version
    version=$(echo "${release_info}" | jq -r '.tag_name')
    log_step "Téléchargement des binaires version ${version}..."

    wget -q "${daemon_url}" -O /usr/local/bin/caleoped
    wget -q "${cli_url}"    -O /usr/local/bin/caleope

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

    local completion_url="${GITHUB_RAW}/module/scripts/caleope-completion.bash"
    local completion_dest="/etc/bash_completion.d/caleope"

    log_step "Installation du script d'autocomplétion..."

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
        "${CALEOPE_ROOT}/core/portainer"
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
        "${CALEOPE_ROOT}/data/portainer"
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

    # traefik.yml — deux modes selon config
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
  websecure:
    address: ":${PORT_TRAEFIK_HTTPS}"

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
# PORTAINER
# =============================================================================

deploy_portainer() {
    log_section "Portainer CE"

    if docker ps --format '{{.Names}}' | grep -q "^portainer$"; then
        log_warning "Portainer déjà en cours d'exécution"
        return 0
    fi

    cat > "${CALEOPE_ROOT}/core/portainer/compose.yml" << EOF
services:
  portainer:
    image: portainer/portainer-ce:latest
    container_name: portainer
    restart: unless-stopped
    ports:
      - "${PORT_PORTAINER}:9443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ${CALEOPE_ROOT}/data/portainer:/data
    networks:
      - ${DOCKER_NET_INTERNAL}

networks:
  ${DOCKER_NET_INTERNAL}:
    external: true
EOF

    chown "${CALEOPE_USER}:${CALEOPE_USER}" "${CALEOPE_ROOT}/core/portainer/compose.yml"

    log_step "Démarrage de Portainer..."
    docker compose -f "${CALEOPE_ROOT}/core/portainer/compose.yml" up -d

    local retries=0
    until docker ps --format '{{.Names}}' | grep -q "^portainer$" || [[ $retries -ge 15 ]]; do
        sleep 1
        (( retries++ )) || true
    done

    if docker ps --format '{{.Names}}' | grep -q "^portainer$"; then
        local IP
        IP=$(hostname -I | awk '{print $1}')
        log_success "Portainer actif"

        echo ""
        echo -e "${RED}╔══════════════════════════════════════════════════════╗${NC}"
        echo -e "${RED}║  ⚠️  ACTION REQUISE DANS LES 5 PROCHAINES MINUTES  ║${NC}"
        echo -e "${RED}║                                                      ║${NC}"
        echo -e "${RED}║  Connecte-toi sur Portainer et crée ton compte admin ║${NC}"
        echo -e "${RED}║  avant que le timer de sécurité expire               ║${NC}"
        echo -e "${RED}║                                                      ║${NC}"
        echo -e "${RED}║  → https://${IP}:${PORT_PORTAINER}                       ║${NC}"
        echo -e "${RED}╚══════════════════════════════════════════════════════╝${NC}"
        echo ""
        echo -e "${YELLOW}Appuie sur [Entrée] une fois ton compte Portainer créé...${NC}"
        read -r </dev/tty
    else
        log_warning "Portainer ne semble pas démarré — vérifier : docker logs portainer"
    fi
}

# =============================================================================
# COCKPIT
# =============================================================================

install_cockpit() {
    log_section "Cockpit"

    if ss -tlnp | grep -q ":${PORT_COCKPIT}"; then
        log_warning "Cockpit déjà actif sur le port ${PORT_COCKPIT}"
        return 0
    fi

    log_step "Installation de Cockpit..."
    run_cmd apt-get install -y cockpit

    log_step "Configuration du port ${PORT_COCKPIT}..."
    systemctl stop cockpit.service 2>/dev/null || true
    systemctl stop cockpit.socket  2>/dev/null || true

    mkdir -p /etc/cockpit
    cat > /etc/cockpit/cockpit.conf << EOF
[WebService]
ListenStream=${PORT_COCKPIT}
EOF

    mkdir -p /etc/systemd/system/cockpit.socket.d/
    cat > /etc/systemd/system/cockpit.socket.d/listen.conf << EOF
[Socket]
ListenStream=
ListenStream=${PORT_COCKPIT}
EOF

    log_step "Activation de Cockpit..."
    systemctl daemon-reload
    systemctl enable cockpit.socket
    systemctl restart cockpit.socket
    systemctl restart cockpit.service 2>/dev/null || true

    sleep 3

    if ss -tlnp | grep -q ":${PORT_COCKPIT}"; then
        log_success "Cockpit actif sur le port ${PORT_COCKPIT}"
    else
        log_warning "Cockpit démarré — port ${PORT_COCKPIT} non encore détecté"
    fi
}

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
EOF

    chmod 644 "${CALEOPE_ROOT}/caleope.conf"
    chown "${CALEOPE_USER}:${CALEOPE_USER}" "${CALEOPE_ROOT}/caleope.conf"

    log_success "Config sauvegardée dans ${CALEOPE_ROOT}/caleope.conf"
    log_debug "  CALEOPE_DOMAIN=${CALEOPE_DOMAIN}"
    log_debug "  CALEOPE_PROXY_MODE=${CALEOPE_PROXY_MODE}"
}

# =============================================================================
# CHIFFREMENT DES SECRETS
# =============================================================================

init_secrets_encryption() {
    if [[ -z "${CALEOPE_SECRETS_PASSWORD}" ]]; then
        log_warning "Chiffrement des secrets désactivé (aucun mot de passe fourni)"
        return 0
    fi

    log_section "Initialisation du chiffrement des secrets"

    local master_dir="${CALEOPE_ROOT}/core/daemon"
    mkdir -p "${master_dir}"
    chmod 700 "${master_dir}"

    # Déléguer au daemon Go pour générer master.enc
    # Le daemon doit être démarré pour utiliser son API socket.
    # À l'install, on écrit un fichier temporaire que le daemon lira au 1er démarrage.
    local init_file="${master_dir}/secrets-init-password"
    echo -n "${CALEOPE_SECRETS_PASSWORD}" > "${init_file}"
    chmod 600 "${init_file}"

    log_success "Mot de passe secrets enregistré pour initialisation au 1er démarrage daemon"
    log_warning "⚠️  IMPORTANT : Conservez ce mot de passe en lieu sûr !"
    log_warning "   Sans lui, 'caleope secrets show' sera impossible si master.enc est supprimé."

    # Effacer le mot de passe de la mémoire bash
    CALEOPE_SECRETS_PASSWORD=""
    CALEOPE_SECRETS_PASSWORD_CONFIRM=""
}

# =============================================================================
# SYNC INITIALE DU STORE
# =============================================================================

sync_store() {
    log_section "Synchronisation du store Caleope"

    local store_dir="${CALEOPE_ROOT}/core/cache/official"
    local store_url="https://github.com/${GITHUB_REPO}-store"

    # Supprimer le dossier s'il existe déjà mais est vide (créé par mkdir -p)
    # git clone refuse de cloner dans un dossier non vide
    if [[ -d "${store_dir}" ]] && [[ -z "$(ls -A "${store_dir}")" ]]; then
        rm -rf "${store_dir}"
        log_debug "Dossier cache vide supprimé pour permettre le clone"
    fi

    git config --global --add safe.directory "${store_dir}"

    if [[ -d "${store_dir}/.git" ]]; then
        log_step "Mise à jour du store..."
        git -C "${store_dir}" fetch origin
        git -C "${store_dir}" reset --hard origin/main
    else
        log_step "Téléchargement du store depuis GitHub..."
        git clone --depth=1 "${store_url}" "${store_dir}"
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
| Traefik dashboard  | http://${IP}:${PORT_TRAEFIK_DASHBOARD}      |
| Portainer          | https://${IP}:${PORT_PORTAINER}             |
| Cockpit            | https://${IP}:${PORT_COCKPIT}               |

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
    echo -e "${CYAN}║${NC}  🔀 Traefik     → ${YELLOW}http://${IP}:${PORT_TRAEFIK_DASHBOARD}${NC}"
    echo -e "${CYAN}║${NC}  🐳 Portainer   → ${YELLOW}https://${IP}:${PORT_PORTAINER}${NC}"
    echo -e "${CYAN}║${NC}  🖥️  Cockpit     → ${YELLOW}https://${IP}:${PORT_COCKPIT}${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║                   Caleope Daemon                     ║${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC}  Statut : ${daemon_status}"
    echo -e "${CYAN}║${NC}  Socket : ${SOCKET_PATH}"
    echo -e "${CYAN}║${NC}  Logs   : ${YELLOW}journalctl -u caleoped -f${NC}"
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
# MAIN
# =============================================================================

main() {
    parse_args "$@"

    clear
    echo -e "${CYAN}"
    echo "╔══════════════════════════════════════════╗"
    echo "║      Caleope — Installation              ║"
    echo "║           by Gaiver-IT                   ║"
    echo "║              v0.1.0                      ║"
    echo "╚══════════════════════════════════════════╝"
    echo -e "${NC}"

    if [[ "${LOG_MODE}" == "debug" ]]; then
        echo -e "${GRAY}Mode : DEBUG${NC}\n"
    else
        echo -e "${GRAY}Mode : CLASSIC  |  --debug pour plus de détails${NC}\n"
    fi

    # Vérifications préalables
    check_root
    check_debian
    check_debian_codename
    check_user

    # Configuration interactive (domaine, mode proxy, email)
    ask_config

    # Installation dans l'ordre
    configure_sudo             # En premier — nécessaire pour la suite
    install_prerequisites
    install_docker
    setup_security             # UFW + fail2ban + unattended-upgrades
    create_docker_networks
    create_structure
    setup_caleope_group
    install_caleope_binaries   # release GitHub → fallback compile
    install_bash_completion    # tab completion pour caleope
    init_caleope_runtime
    save_config                # Sauvegarder domaine + mode proxy dans caleope.conf
    init_secrets_encryption    # Chiffrement secrets si MDP fourni
    sync_store                 # git clone du store officiel
    install_caleoped_service
    deploy_traefik
    deploy_portainer
    install_cockpit
    generate_links_file
    print_summary
}

main "$@"
exit 0
