#!/usr/bin/env bash
#
# verify-platform.sh — Vérification de santé d'une plateforme Caleope
#
# Lance une batterie de contrôles et affiche un rapport clair (✓ / ⚠ / ✗).
# Conçu pour être lancé À LA MAIN, sans IA, directement sur le serveur.
#
# UTILISATION (sur le serveur) :
#     sudo bash verify-platform.sh
#
# DEPUIS TON MAC (sans copier le fichier) :
#     cat scripts/verify-platform.sh | \
#       sshpass -p 'MOT_DE_PASSE' ssh user-caleope@172.16.51.15 'sudo bash -s'
#
# Code de sortie : 0 si tout est OK (warnings tolérés), 1 si au moins un échec.
#
# Variable optionnelle :
#     CALEOPE_BASE=/chemin   (défaut: /opt/gaiver-it/caleope)

BASE="${CALEOPE_BASE:-/opt/gaiver-it/caleope}"

# ── Couleurs (désactivées si pas un terminal) ───────────────────────
if [ -t 1 ]; then
  R=$'\e[31m'; G=$'\e[32m'; Y=$'\e[33m'; B=$'\e[1m'; D=$'\e[2m'; Z=$'\e[0m'
else
  R=""; G=""; Y=""; B=""; D=""; Z=""
fi

PASS=0; WARN=0; FAIL=0

section() { printf '\n%s── %s ──%s\n' "$B" "$1" "$Z"; }
ok()   { PASS=$((PASS+1)); printf '  %s✓%s %s\n' "$G" "$Z" "$1"; }
warn() { WARN=$((WARN+1)); printf '  %s⚠%s %s\n' "$Y" "$Z" "$1"; }
fail() { FAIL=$((FAIL+1)); printf '  %s✗%s %s\n' "$R" "$Z" "$1"; }
info() { printf '    %s%s%s\n' "$D" "$1" "$Z"; }

# sudo helper : on relance la commande telle quelle (le script est censé
# tourner en root ; sinon on prévient).
need_root() {
  if [ "$(id -u)" -ne 0 ]; then
    printf '%s⚠ Ce script doit tourner en root pour tout vérifier.%s\n' "$Y" "$Z"
    printf '  Relance-le avec : %ssudo bash %s%s\n\n' "$B" "$0" "$Z"
  fi
}

printf '%s╔══════════════════════════════════════════════╗%s\n' "$B" "$Z"
printf '%s║   Vérification plateforme Caleope            ║%s\n' "$B" "$Z"
printf '%s╚══════════════════════════════════════════════╝%s\n' "$B" "$Z"
printf '%sServeur : %s   —   %s%s\n' "$D" "$(hostname)" "$(date '+%Y-%m-%d %H:%M')" "$Z"
need_root

# Charger la config (domaine, mode proxy)
DOMAIN=""; PROXY_MODE=""; VERSION=""
if [ -r "$BASE/caleope.conf" ]; then
  DOMAIN=$(grep -E '^CALEOPE_DOMAIN=' "$BASE/caleope.conf" | cut -d= -f2-)
  PROXY_MODE=$(grep -E '^CALEOPE_PROXY_MODE=' "$BASE/caleope.conf" | cut -d= -f2-)
fi

# ════════════════════════════════════════════════════════════════════
section "1. Système"
# ════════════════════════════════════════════════════════════════════

# Espace disque sur /
DISK_USE=$(df -P / | awk 'NR==2{gsub("%","",$5); print $5}')
if [ -n "$DISK_USE" ]; then
  if   [ "$DISK_USE" -ge 90 ]; then fail "Disque / : ${DISK_USE}% utilisé (critique)"
  elif [ "$DISK_USE" -ge 80 ]; then warn "Disque / : ${DISK_USE}% utilisé (à surveiller)"
  else ok "Disque / : ${DISK_USE}% utilisé"; fi
else
  warn "Espace disque : impossible à lire"
fi

# Mémoire
if command -v free >/dev/null 2>&1; then
  MEM=$(free -m | awk '/^Mem:/{printf "%d/%d Mo libres", $7, $2}')
  MEM_AVAIL=$(free -m | awk '/^Mem:/{print $7}')
  if [ "${MEM_AVAIL:-1}" -lt 200 ]; then warn "Mémoire : $MEM (faible)"; else ok "Mémoire : $MEM"; fi
fi

# Charge système
info "Charge : $(uptime | sed 's/.*load average/load average/')"

# ════════════════════════════════════════════════════════════════════
section "2. Services Caleope"
# ════════════════════════════════════════════════════════════════════

for svc in caleoped caleope-ui; do
  state=$(systemctl is-active "$svc" 2>/dev/null)
  if [ "$state" = "active" ]; then ok "Service $svc : actif"
  else fail "Service $svc : $state (attendu: active)"; fi
done

# Socket du daemon
if [ -S /run/caleoped.sock ]; then ok "Socket /run/caleoped.sock présent"
else fail "Socket /run/caleoped.sock absent (daemon injoignable)"; fi

# ════════════════════════════════════════════════════════════════════
section "3. CLI & version"
# ════════════════════════════════════════════════════════════════════

if command -v caleope >/dev/null 2>&1; then
  if LIST=$(caleope list 2>&1); then
    ok "Commande 'caleope list' répond"
  else
    fail "Commande 'caleope list' en erreur"
    info "$(echo "$LIST" | head -2)"
  fi
else
  fail "Binaire 'caleope' introuvable dans le PATH"
fi
info "Domaine : ${DOMAIN:-?}   |   Proxy : ${PROXY_MODE:-?}"

# ════════════════════════════════════════════════════════════════════
section "4. Docker & proxy"
# ════════════════════════════════════════════════════════════════════

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  NB=$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')
  ok "Docker actif — $NB conteneur(s) en cours"

  # Conteneur proxy attendu selon le mode
  if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx traefik; then
    ok "Conteneur 'traefik' en cours"
  else
    fail "Conteneur 'traefik' absent (routage cassé)"
  fi
  if [ "$PROXY_MODE" = "npm" ]; then
    if docker ps --format '{{.Names}}' 2>/dev/null | grep -qiE 'nginx-proxy-manager|npm'; then
      ok "Conteneur NPM en cours (mode proxy = npm)"
    else
      info "Mode proxy = npm mais aucun conteneur NPM local (NPM peut être externe)"
    fi
  fi
else
  fail "Docker injoignable (daemon arrêté ou droits insuffisants)"
fi

# Réseaux Docker Caleope
for net in caleope-public caleope-internal; do
  if docker network ls --format '{{.Name}}' 2>/dev/null | grep -qx "$net"; then
    ok "Réseau Docker '$net' présent"
  else
    warn "Réseau Docker '$net' absent (normal si aucune app installée)"
  fi
done

# ════════════════════════════════════════════════════════════════════
section "5. Réseau & firewall"
# ════════════════════════════════════════════════════════════════════

if command -v ufw >/dev/null 2>&1; then
  if ufw status 2>/dev/null | grep -q '^Status: active'; then
    ok "UFW actif"
    for port in 22 80 443; do
      if ufw status 2>/dev/null | grep -qE "^${port}(/tcp)?[[:space:]]+ALLOW"; then
        ok "Port $port autorisé"
      else
        warn "Port $port non listé dans UFW"
      fi
    done
  else
    warn "UFW inactif"
  fi
fi

# Ports réellement en écoute
if command -v ss >/dev/null 2>&1; then
  for port in 80 443; do
    if ss -tlnH 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${port}\$"; then
      ok "Port $port en écoute"
    else
      fail "Port $port PAS en écoute (proxy ne répond pas)"
    fi
  done
fi

# ════════════════════════════════════════════════════════════════════
section "6. Apps installées"
# ════════════════════════════════════════════════════════════════════

shopt -s nullglob
APP_JSONS=("$BASE"/runtime/apps/*.json)
shopt -u nullglob

if [ "${#APP_JSONS[@]}" -eq 0 ]; then
  info "Aucune app installée (runtime/apps vide)."
else
  for f in "${APP_JSONS[@]}"; do
    id=$(grep -oE '"id"[[:space:]]*:[[:space:]]*"[^"]+"' "$f" | head -1 | sed -E 's/.*"id"[^"]*"([^"]+)".*/\1/')
    status=$(grep -oE '"status"[[:space:]]*:[[:space:]]*"[^"]+"' "$f" | head -1 | sed -E 's/.*"status"[^"]*"([^"]+)".*/\1/')
    [ -z "$id" ] && id=$(basename "$f" .json)

    # Y a-t-il un conteneur dont le nom contient l'id de l'app, en cours ?
    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "$id"; then
      ok "App '$id' : conteneur(s) en cours (déclaré: ${status:-?})"
    else
      if [ "$status" = "running" ]; then
        fail "App '$id' : déclarée 'running' mais aucun conteneur up"
      else
        warn "App '$id' : aucun conteneur up (déclaré: ${status:-?})"
      fi
    fi
  done
fi

# ════════════════════════════════════════════════════════════════════
section "7. Accès HTTP"
# ════════════════════════════════════════════════════════════════════

if command -v curl >/dev/null 2>&1; then
  code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 8 http://localhost 2>/dev/null)
  if [ "$code" = "000" ] || [ -z "$code" ]; then
    fail "http://localhost ne répond pas (proxy down)"
  else
    ok "Proxy local répond (HTTP $code)"
  fi

  if [ -n "$DOMAIN" ]; then
    code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 -k "https://$DOMAIN" 2>/dev/null)
    if [ "$code" = "000" ] || [ -z "$code" ]; then
      warn "https://$DOMAIN injoignable depuis le serveur (DNS/proxy externe ?)"
    else
      ok "https://$DOMAIN répond (HTTP $code)"
    fi
  fi
fi

# ════════════════════════════════════════════════════════════════════
section "8. Journaux"
# ════════════════════════════════════════════════════════════════════

# Journal d'audit
AUDIT=""
for cand in "/var/log/caleope/audit.log" "$BASE/logs/audit.log"; do
  [ -r "$cand" ] && AUDIT="$cand" && break
done
if [ -n "$AUDIT" ]; then ok "Journal d'audit présent ($AUDIT)"
else warn "Journal d'audit introuvable"; fi

# Erreurs récentes du daemon
if command -v journalctl >/dev/null 2>&1; then
  ERRS=$(journalctl -u caleoped --since "1 hour ago" --no-pager 2>/dev/null \
         | grep -icE 'level=error|panic|fatal')
  if [ "${ERRS:-0}" -gt 0 ]; then
    warn "$ERRS ligne(s) d'erreur dans caleoped (dernière heure)"
    info "Voir : journalctl -u caleoped --since '1 hour ago' | grep -i error"
  else
    ok "Aucune erreur récente dans caleoped (dernière heure)"
  fi
fi

# ════════════════════════════════════════════════════════════════════
# Résumé
# ════════════════════════════════════════════════════════════════════
printf '\n%s──────────────────────────────────────%s\n' "$B" "$Z"
printf '%sRésumé :%s  %s%d OK%s   %s%d avertissement(s)%s   %s%d échec(s)%s\n' \
  "$B" "$Z" "$G" "$PASS" "$Z" "$Y" "$WARN" "$Z" "$R" "$FAIL" "$Z"

if [ "$FAIL" -gt 0 ]; then
  printf '%s➜ Des problèmes critiques nécessitent ton attention.%s\n' "$R" "$Z"
  exit 1
elif [ "$WARN" -gt 0 ]; then
  printf '%s➜ Plateforme fonctionnelle, quelques points à surveiller.%s\n' "$Y" "$Z"
  exit 0
else
  printf '%s➜ Plateforme en pleine santé.%s\n' "$G" "$Z"
  exit 0
fi
