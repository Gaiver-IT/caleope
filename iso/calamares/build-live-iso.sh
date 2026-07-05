#!/usr/bin/env bash
#
# build-live-iso.sh — construit l'ISO Caleope GRAPHIQUE (live + Calamares).
#
# Produit une image live Debian (squashfs) qui démarre en RAM sur un X minimal
# et lance l'installeur Calamares brandé Caleope. Alternative graphique à
# iso/build.sh (netinst/preseed).
#
# PRÉREQUIS (hôte Debian, root) :
#   sudo apt install -y live-build xorriso debootstrap squashfs-tools wget
#
# USAGE :
#   sudo CALEOPE_VERSION=v0.6.11 ./build-live-iso.sh
#   → build/caleope-live-<version>.iso
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="Gaiver-IT/caleope"
CALEOPE_VERSION="${CALEOPE_VERSION:-v0.6.11}"
CHANNEL="${CHANNEL:-stable}"
DIST="${DIST:-trixie}"
WORK="${WORK:-$HERE/build/live}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "❌ manquant: $1 (apt install $2)"; exit 1; }; }
need lb live-build; need xorriso xorriso; need debootstrap debootstrap; need mksquashfs squashfs-tools

[[ $EUID -eq 0 ]] || { echo "❌ live-build doit tourner en root (sudo)"; exit 1; }

# Démonte d'éventuels montages résiduels sous le chroot (build interrompu / tests
# headless) — sinon « rm -rf » échoue sur /dev, /proc… et set -e avorte le build.
if [ -d "$WORK/chroot" ]; then
  for m in $(mount | awk -v w="$WORK/chroot" 'index($3, w)==1 {print $3}' | sort -r); do
    umount -R -l "$m" 2>/dev/null || true
  done
fi
rm -rf "$WORK"; mkdir -p "$WORK"; cd "$WORK"

# ── 1. Config live-build (live, sans debian-installer) ──────────────────────
echo "⚙️  Configuration live-build ($DIST)..."
lb config \
  --distribution "$DIST" \
  --archive-areas "main contrib non-free-firmware" \
  --debian-installer none \
  --binary-images iso-hybrid \
  --bootappend-live "boot=live components locales=fr_FR.UTF-8 keyboard-layouts=fr" \
  --iso-application "Caleope Installer" \
  --iso-volume "CALEOPE-LIVE"

# ── 2. Paquets : X minimal + WM léger + Calamares + réseau + prérequis Caleope ──
mkdir -p config/package-lists
cat > config/package-lists/caleope.list.chroot <<'PKLIST'
xserver-xorg
xinit
openbox
feh
x11-xserver-utils
xterm
libgl1-mesa-dri
calamares
calamares-settings-debian
network-manager
sudo
curl
ca-certificates
gnupg
git
PKLIST

# ── 3. Payload Caleope + config Calamares → dans le système live ────────────
echo "📦 Assemblage du payload Caleope..."
INCL="$WORK/config/includes.chroot"   # absolu : robuste aux cd des sous-shells
mkdir -p "$INCL/caleope/binaries" "$INCL/etc/calamares" "$INCL/etc/xdg/openbox"
BASE="https://github.com/${REPO}/releases/download/${CALEOPE_VERSION}"
for b in caleoped caleope caleope-ui; do
  wget -qO "$INCL/caleope/binaries/$b" "$BASE/${b}-linux-amd64"
done
chmod +x "$INCL/caleope/binaries/"*
# install.sh (racine du dépôt) + service (iso/) : local (branche, contient --iso)
cp "$HERE/../../install.sh"              "$INCL/caleope/install.sh"
cp "$HERE/../caleope-install.service"    "$INCL/caleope/caleope-install.service"
chmod +x "$INCL/caleope/install.sh"
# store
git clone --depth 1 --branch "$([[ $CHANNEL == alpha ]] && echo alpha || echo main)" \
  "https://github.com/gaiver-it/caleope-store.git" "$WORK/store-clone/caleope-store" 2>/dev/null \
  && ( cd "$WORK/store-clone" && tar czf "$INCL/caleope/store.tar.gz" caleope-store )
cat > "$INCL/caleope/pack-info.json" <<JSON
{ "caleope_version": "${CALEOPE_VERSION}", "channel": "${CHANNEL}", "mode": "online-live" }
JSON

# Config Calamares (branding + séquence + modules)
cp -a "$HERE/branding"                "$INCL/etc/calamares/branding"
cp    "$HERE/config/settings.conf"    "$INCL/etc/calamares/settings.conf"
mkdir -p "$INCL/etc/calamares/modules"
cp -a "$HERE/config/modules/."        "$INCL/etc/calamares/modules/"

# ── 3b. Branding du BOOT : splash des menus d'amorçage + fond d'écran Caleope ──
# NB : PAS de Plymouth ici. Sur une image live sans display-manager (getty autologin
# → startx), la bascule Plymouth→X laisse un écran noir. On se contente du splash des
# menus (BIOS/UEFI) + d'un fond d'écran Caleope posé par openbox → jamais d'écran noir.
echo "🎨 Branding du boot (splash menus + fond d'écran)..."
for BL in isolinux grub-pc; do
  mkdir -p "config/bootloaders/$BL"
done
cp "$HERE/bootsplash/splash-640.png"  "config/bootloaders/isolinux/splash.png"
cp "$HERE/bootsplash/splash-1024.png" "config/bootloaders/grub-pc/splash.png"
# Fond d'écran de la session live (posé par openbox avant Calamares → jamais noir)
mkdir -p "$INCL/usr/share/caleope"
cp "$HERE/bootsplash/splash-1024.png" "$INCL/usr/share/caleope/wallpaper.png"
# Garantit que la session live (utilisateur du groupe sudo) peut élever Calamares
# sans mot de passe (l'autostart fait `sudo -E calamares`). ISO éphémère → OK.
mkdir -p "$INCL/etc/sudoers.d"
printf '%%sudo ALL=(ALL) NOPASSWD: ALL\n' > "$INCL/etc/sudoers.d/caleope-live"
chmod 440 "$INCL/etc/sudoers.d/caleope-live"

# ── 4. Autostart : lancer Calamares plein écran au démarrage de la session ──
cat > "$INCL/etc/xdg/openbox/autostart" <<'AUTO'
# Session live Caleope : fond d'écran + installeur.
# Robuste : session root OU user (live-config), et VM sans OpenGL.
xset -dpms 2>/dev/null; xset s off 2>/dev/null
xsetroot -solid "#0c0c0e" 2>/dev/null
feh --bg-fill /usr/share/caleope/wallpaper.png 2>/dev/null &
xhost +local: 2>/dev/null   # autorise root (sudo) à joindre le serveur X de la session

# RENDU LOGICIEL : une VM (Proxmox/QEMU) n'a pas d'OpenGL exploitable ; le
# slideshow QtQuick de Calamares exige GL → sinon crash au démarrage.
export QT_QUICK_BACKEND=software
export QT_XCB_GL_INTEGRATION=none
export LIBGL_ALWAYS_SOFTWARE=1
export QT_QPA_PLATFORM=xcb

# Calamares DOIT être root (il installe le système). La session live est souvent
# « user » (live-config) → dans ce cas /var/log n'est pas inscriptible et un
# lancement direct échoue silencieusement. On journalise dans /tmp (toujours
# inscriptible) et on élève via sudo -E (NOPASSWD en live). Si Calamares se
# ferme en <8 s, un xterm affiche le diagnostic complet.
LOG=/tmp/caleope-calamares.log
( sleep 2
  { echo "id: $(id)"; echo "calamares: $(command -v calamares)"; echo "DISPLAY=$DISPLAY"; echo "----"; } > "$LOG" 2>&1
  t0=$(date +%s)
  if [ "$(id -u)" = 0 ]; then
    calamares >> "$LOG" 2>&1
  else
    sudo -E calamares >> "$LOG" 2>&1
  fi
  t1=$(date +%s)
  if [ $((t1 - t0)) -lt 8 ]; then
    { echo; echo "=== ~/.cache/calamares/session.log ==="; tail -60 "$HOME/.cache/calamares/session.log" 2>/dev/null
      echo; echo "=== /root/.cache/calamares/session.log ==="; tail -60 /root/.cache/calamares/session.log 2>/dev/null; } >> "$LOG" 2>&1
    xterm -fa Monospace -fs 10 -geometry 140x50 +sb -e sh -c "cat $LOG; echo; echo '>>> Capture cette fenetre puis ferme-la. <<<'; sleep 100000"
  fi
) &
AUTO
# Démarrage auto de X (getty autologin → startx) : hook
mkdir -p "$INCL/root"
cat > "$INCL/root/.bash_profile" <<'PROFILE'
[ -z "$DISPLAY" ] && [ "$(tty)" = "/dev/tty1" ] && exec startx /usr/bin/openbox-session
PROFILE

# ── 5. Autologin root sur tty1 (pour lancer X → Calamares) ──────────────────
mkdir -p "$INCL/etc/systemd/system/getty@tty1.service.d"
cat > "$INCL/etc/systemd/system/getty@tty1.service.d/autologin.conf" <<'GETTY'
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin root --noclear %I $TERM
GETTY

# ── 6. Build ────────────────────────────────────────────────────────────────
echo "🔧 Build de l'ISO live (long : debootstrap + squashfs)..."
lb build

OUT="$HERE/build/caleope-live-${CALEOPE_VERSION}.iso"
mkdir -p "$HERE/build"
mv "$WORK"/live-image-*.hybrid.iso "$OUT" 2>/dev/null || mv "$WORK"/*.iso "$OUT" 2>/dev/null || true
echo ""
echo "✅ ISO live construite : $OUT"
ls -lh "$OUT" 2>/dev/null || echo "⚠️  ISO non trouvée — voir les logs live-build ci-dessus."
