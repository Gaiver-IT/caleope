#!/usr/bin/env bash
#
# smoke-install.sh — Test bout-en-bout d'installation d'une app Caleope
#
# Installe une app, vérifie qu'elle démarre, puis la désinstalle.
# Conçu pour tourner À LA MAIN sur le serveur (ou via SSH), sans IA.
#
# UTILISATION (sur le serveur) :
#     sudo bash smoke-install.sh [app-id]
#   app-id par défaut : vaultwarden (léger, 1 conteneur, aucun param requis)
#
# DEPUIS TON MAC :
#     cat scripts/smoke-install.sh | \
#       sshpass -p 'MOT_DE_PASSE' ssh user-caleope@172.16.51.15 'sudo bash -s -- vaultwarden'
#
# PRÉREQUIS : licence activée (caleope license activate <CALP-…>),
#             l'app ne doit pas être déjà installée.
#
# Code de sortie : 0 si le cycle install→vérif→remove réussit, 1 sinon.

APP="${1:-vaultwarden}"
BASE="${CALEOPE_BASE:-/opt/gaiver-it/caleope}"
TIMEOUT="${SMOKE_TIMEOUT:-180}"   # secondes max pour voir le conteneur démarrer

# Détection robuste des conteneurs de l'app : par label de projet compose
# (= l'id de l'app), pas par nom — marche même si les conteneurs ont un nom
# différent de l'app (ex: prometheus-grafana → prometheus, grafana).
PROJ_FILTER="label=com.docker.compose.project=$APP"
app_containers() { docker ps --filter "$PROJ_FILTER" --format '{{.Names}}'; }

if [ -t 1 ]; then
  R=$'\e[31m'; G=$'\e[32m'; Y=$'\e[33m'; B=$'\e[1m'; D=$'\e[2m'; Z=$'\e[0m'
else
  R=""; G=""; Y=""; B=""; D=""; Z=""
fi
ok()   { printf '  %s✓%s %s\n' "$G" "$Z" "$1"; }
ko()   { printf '  %s✗%s %s\n' "$R" "$Z" "$1"; }
step() { printf '\n%s▶ %s%s\n' "$B" "$1" "$Z"; }
info() { printf '    %s%s%s\n' "$D" "$1" "$Z"; }

printf '%s╔══════════════════════════════════════════════╗%s\n' "$B" "$Z"
printf '%s║   Smoke-test install — app: %-16s ║%s\n' "$B" "$APP" "$Z"
printf '%s╚══════════════════════════════════════════════╝%s\n' "$B" "$Z"

# Nettoyage de sécurité : si on a installé l'app, on la retire en sortant,
# SAUF si elle était déjà présente avant (on n'y touche pas dans ce cas).
INSTALLED_BY_US=0
cleanup() {
  if [ "$INSTALLED_BY_US" = "1" ]; then
    step "Nettoyage : désinstallation de '$APP'"
    # --yes : indispensable en non-interactif (sinon 'remove' annule en silence
    # mais renvoie un code succès trompeur).
    caleope remove "$APP" --yes >/dev/null 2>&1
    sleep 4
    # Vérifier RÉELLEMENT que c'est parti (ne pas se fier au code retour).
    if [ -n "$(app_containers 2>/dev/null)" ]; then
      ko "Désinstallation incomplète — conteneur(s) de '$APP' encore présent(s)."
      info "À nettoyer à la main : caleope remove $APP --yes"
    else
      ok "App '$APP' désinstallée (conteneurs supprimés)"
    fi
  fi
}
trap cleanup EXIT

# ── 0. Prérequis ────────────────────────────────────────────────────
step "0. Prérequis"

if ! command -v caleope >/dev/null 2>&1; then
  ko "Binaire 'caleope' introuvable"; exit 1
fi

if caleope license status 2>&1 | grep -qi 'non activée'; then
  ko "Licence non activée — l'installation est verrouillée."
  info "Active d'abord : caleope license activate <CALP-XXXX-XXXX-XXXX>"
  exit 1
fi
ok "Licence active"

# L'app ne doit pas déjà être installée (on ne veut pas l'écraser)
if caleope list --json 2>/dev/null | grep -q "\"$APP\""; then
  ko "'$APP' est déjà installée — abandon (ce test installe une app NEUVE)."
  info "Retire-la d'abord si tu veux tester : caleope remove $APP"
  exit 1
fi
ok "'$APP' n'est pas déjà installée"

# ── 1. Installation ─────────────────────────────────────────────────
step "1. Installation de '$APP'"
if caleope install "$APP"; then
  INSTALLED_BY_US=1
  ok "Commande d'installation terminée sans erreur"
else
  ko "La commande 'caleope install $APP' a échoué"
  exit 1
fi

# ── 2. Démarrage du/des conteneur(s) ────────────────────────────────
step "2. Attente du démarrage (max ${TIMEOUT}s)"
elapsed=0
up=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
  if [ -n "$(app_containers 2>/dev/null)" ]; then
    up=1; break
  fi
  sleep 3; elapsed=$((elapsed+3))
  printf '\r    %s… %ss%s' "$D" "$elapsed" "$Z"
done
printf '\r'
if [ "$up" = "1" ]; then
  ok "Conteneur(s) de '$APP' démarré(s) (après ${elapsed}s)"
  docker ps --filter "$PROJ_FILTER" --format '    {{.Names}}\t{{.Status}}' 2>/dev/null
else
  ko "Aucun conteneur '$APP' après ${TIMEOUT}s"
  info "Logs : caleope logs $APP"
  exit 1
fi

# ── 3. Suivi runtime ────────────────────────────────────────────────
step "3. Suivi dans le runtime"
RJSON="$BASE/runtime/apps/$APP.json"
if [ -r "$RJSON" ]; then
  status=$(grep -oE '"status"[[:space:]]*:[[:space:]]*"[^"]+"' "$RJSON" | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
  if [ "$status" = "running" ]; then
    ok "runtime/apps/$APP.json : status=running"
  else
    ko "runtime/apps/$APP.json : status=$status (attendu running)"
  fi
else
  ko "runtime/apps/$APP.json absent"
fi

# ── 4. Conteneur en bonne santé (pas en restart-loop) ───────────────
step "4. Stabilité du conteneur"
sleep 5
unstable=$(docker ps --filter "$PROJ_FILTER" --format '{{.Names}} {{.Status}}' 2>/dev/null | grep -iE 'restarting|unhealthy' | head -3)
if [ -n "$unstable" ]; then
  ko "Conteneur(s) instable(s) :"
  printf '    %s\n' "$unstable"
  info "Logs : caleope logs $APP"
else
  ok "Conteneur(s) stable(s) : $(app_containers | tr '\n' ' ')"
fi

# ── Bilan (le trap EXIT s'occupe de la désinstallation) ─────────────
step "Résultat"
printf '%s✓ Smoke-test réussi : %s démarre correctement.%s\n' "$G" "$APP" "$Z"
printf '%sDésinstallation en cours pour restaurer létat initial…%s\n' "$D" "$Z"
exit 0
