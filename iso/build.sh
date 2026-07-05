#!/usr/bin/env bash
#
# iso/build.sh — Construit l'ISO d'installation Caleope (Debian 13 Trixie).
#
# Prend une netinst Debian 13, y injecte le preseed (install auto) + le payload
# Caleope (binaires + install.sh + cache store), et repack une ISO bootable
# BIOS+UEFI. Résultat : une clé/ISO qui installe Caleope sans interaction.
#
# PRÉREQUIS (hôte Linux) : xorriso, wget, gzip, cpio, fdisk (paquet: xorriso).
#   sudo apt install -y xorriso wget cpio gzip
#
# USAGE :
#   ./build.sh                       # ISO online (apps pull depuis le registre miroir)
#   CALEOPE_VERSION=v0.6.3 ./build.sh
#
# NOTE : pour l'ISO OFFLINE (images Docker bundlées), voir l'outil builder
#        séparé (iso/offline-builder/) — pas ce script.

set -euo pipefail

# ── Paramètres ──────────────────────────────────────────────────────────────
DEBIAN_VER="${DEBIAN_VER:-13.0.0}"
ARCH="amd64"
CALEOPE_VERSION="${CALEOPE_VERSION:-v0.6.6}"
REPO="Gaiver-IT/caleope"
STORE_REPO="gaiver-it/caleope-store"
# Canal : stable → branche store main, alpha → branche store alpha
CHANNEL="${CHANNEL:-stable}"
# Registre miroir baké dans l'ISO (les apps pull depuis là plutôt que Docker Hub)
CALEOPE_REGISTRY="${CALEOPE_REGISTRY:-}"
CALEOPE_REGISTRY_USER="${CALEOPE_REGISTRY_USER:-}"
CALEOPE_REGISTRY_PASS="${CALEOPE_REGISTRY_PASS:-}"
# ISO offline : dossier d'images Docker (.tar) à embarquer (produit par
# offline-builder). Vide = ISO online (apps pull depuis le registre/Docker Hub).
OFFLINE_IMAGES_DIR="${OFFLINE_IMAGES_DIR:-}"

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="${WORK:-$HERE/build}"
CACHE="${CACHE:-$HERE/cache}"
EXTRACT="$WORK/iso-extract"
OUT="${OUT:-$WORK/caleope-installer-${CALEOPE_VERSION}.iso}"

# ── Netinst Debian : auto-détection de la version courante ───────────────────
# Les point-releases changent (13.0.0 → 13.5.0…) et Debian ne garde que `current`.
# On lit le dossier et on prend le nom réel du fichier netinst.
NETINST_BASE="https://cdimage.debian.org/debian-cd/current/${ARCH}/iso-cd"
if [[ -z "${DEBIAN_NETINST_FILE:-}" ]]; then
  DEBIAN_NETINST_FILE="$(wget -qO- "${NETINST_BASE}/" | grep -oE "debian-[0-9.]+-${ARCH}-netinst\.iso" | head -1)"
fi
[[ -n "${DEBIAN_NETINST_FILE:-}" ]] || { echo "❌ netinst Debian courante introuvable sur cdimage"; exit 1; }
NETINST="$CACHE/${DEBIAN_NETINST_FILE}"
NETINST_URL="${NETINST_BASE}/${DEBIAN_NETINST_FILE}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "❌ manquant: $1 (apt install $2)"; exit 1; }; }
need xorriso xorriso; need wget wget; need cpio cpio; need gzip gzip

mkdir -p "$WORK" "$CACHE"

# ── 1. Récupérer la netinst Debian ──────────────────────────────────────────
if [[ ! -f "$NETINST" ]]; then
  echo "⬇️  Téléchargement Debian ${DEBIAN_VER} netinst..."
  wget -O "$NETINST" "$NETINST_URL"
fi

# ── 2. Extraire l'ISO (xorriso -osirrox, pas besoin de root/mount) ──────────
echo "📂 Extraction de l'ISO..."
rm -rf "$EXTRACT"; mkdir -p "$EXTRACT"
xorriso -osirrox on -indev "$NETINST" -extract / "$EXTRACT" 2>/dev/null
chmod -R u+w "$EXTRACT"

# ── 3. Injecter le preseed dans l'initrd (d-i le charge automatiquement) ────
echo "💉 Injection du preseed dans l'initrd..."
for INITRD in "$EXTRACT/install.amd/initrd.gz" "$EXTRACT/install.amd/gtk/initrd.gz"; do
  [[ -f "$INITRD" ]] || continue
  TMP="$(mktemp -d)"
  ( cd "$TMP" && gzip -d < "$INITRD" > initrd.cpio \
    && cp "$HERE/preseed.cfg" preseed.cfg \
    && echo preseed.cfg | cpio -H newc -o -A -F initrd.cpio 2>/dev/null \
    && gzip -9 < initrd.cpio > "$INITRD" )
  rm -rf "$TMP"
done

# ── 4. Assembler le payload Caleope (format bundle offline) ─────────────────
# Layout attendu par install.sh --iso / --offline :
#   caleope/binaries/{caleoped,caleope,caleope-ui}
#   caleope/store.tar.gz            (le store, wrappé dans un dossier)
#   caleope/install.sh              (l'installeur, lancé par le preseed)
#   caleope/caleope-completion.bash (autocomplétion)
#   caleope/pack-info.json          (métadonnées : version, canal, registre)
#   caleope/images/*.tar            (optionnel — ISO offline uniquement)
echo "📦 Assemblage du payload Caleope (${CALEOPE_VERSION}, canal ${CHANNEL})..."
PAYLOAD="$EXTRACT/caleope"; rm -rf "$PAYLOAD"; mkdir -p "$PAYLOAD/binaries"
BASE="https://github.com/${REPO}/releases/download/${CALEOPE_VERSION}"
for b in caleoped caleope caleope-ui; do
  echo "   ⬇️  $b"
  wget -qO "$PAYLOAD/binaries/$b" "$BASE/${b}-linux-amd64"
done
chmod +x "$PAYLOAD/binaries/"*

# install.sh : local (checkout de la branche, contient --iso/--iso-finalize) en
# priorité, sinon depuis le tag/main. Indispensable tant que la branche ISO n'est
# pas mergée : les releases n'ont pas encore le mode --iso.
if [[ -f "$HERE/../install.sh" ]]; then
  echo "   📎 install.sh local (branche)"
  cp "$HERE/../install.sh" "$PAYLOAD/install.sh"
else
  wget -qO "$PAYLOAD/install.sh" "https://raw.githubusercontent.com/${REPO}/${CALEOPE_VERSION}/install.sh" \
    || wget -qO "$PAYLOAD/install.sh" "https://raw.githubusercontent.com/${REPO}/main/install.sh"
fi
chmod +x "$PAYLOAD/install.sh"
wget -qO "$PAYLOAD/caleope-completion.bash" \
  "https://raw.githubusercontent.com/${REPO}/${CALEOPE_VERSION}/module/scripts/caleope-completion.bash" 2>/dev/null || true

# Store : clone de la branche du canal, wrappé dans un dossier puis tar.gz
# (install.sh extrait avec --strip-components=1 → dossier wrapper retiré).
STORE_BRANCH="main"; [[ "$CHANNEL" == "alpha" ]] && STORE_BRANCH="alpha"
STORE_TMP="$WORK/store-clone"; rm -rf "$STORE_TMP"
if git clone --depth 1 --branch "$STORE_BRANCH" "https://github.com/${STORE_REPO}.git" \
     "$STORE_TMP/caleope-store" 2>/dev/null; then
  ( cd "$STORE_TMP" && tar czf "$PAYLOAD/store.tar.gz" caleope-store )
  rm -rf "$STORE_TMP"
  echo "   ✅ store ($STORE_BRANCH) empaqueté"
else
  echo "   ❌ clone du store échoué — build interrompu"; exit 1
fi

# Images Docker (ISO offline uniquement)
if [[ -n "$OFFLINE_IMAGES_DIR" && -d "$OFFLINE_IMAGES_DIR" ]]; then
  echo "   📦 Intégration des images offline depuis $OFFLINE_IMAGES_DIR"
  mkdir -p "$PAYLOAD/images"; cp "$OFFLINE_IMAGES_DIR"/*.tar "$PAYLOAD/images/" 2>/dev/null || true
fi

# Service systemd d'install au 1er boot (activé par le preseed late_command)
cp "$HERE/caleope-install.service" "$PAYLOAD/caleope-install.service"

# pack-info.json — métadonnées du bundle
cat > "$PAYLOAD/pack-info.json" << JSON
{
  "caleope_version": "${CALEOPE_VERSION}",
  "channel": "${CHANNEL}",
  "packed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "mode": "$([[ -d "$PAYLOAD/images" ]] && echo offline || echo online)",
  "registry": "${CALEOPE_REGISTRY}",
  "registry_user": "${CALEOPE_REGISTRY_USER}",
  "registry_pass": "${CALEOPE_REGISTRY_PASS}"
}
JSON

# ── 5. Patcher les bootloaders pour l'install auto (BIOS + UEFI) ────────────
echo "⚙️  Patch des bootloaders (auto-install)..."
BOOT_ARGS="auto=true priority=critical"
# isolinux (BIOS)
if [[ -f "$EXTRACT/isolinux/txt.cfg" ]]; then
  sed -i "s#\(append.*vmlinuz.*\)#\1 ${BOOT_ARGS}#" "$EXTRACT/isolinux/txt.cfg" || true
fi
# grub (UEFI) — booter direct sur l'install auto
if [[ -f "$EXTRACT/boot/grub/grub.cfg" ]]; then
  sed -i "s#\(linux.*vmlinuz.*\)#\1 ${BOOT_ARGS}#" "$EXTRACT/boot/grub/grub.cfg" || true
  sed -i 's/set default=.*/set default=0/; s/set timeout=.*/set timeout=3/' "$EXTRACT/boot/grub/grub.cfg" || true
fi

# ── 6. Recalculer md5 + repack ISO hybride (BIOS+UEFI) ──────────────────────
echo "🔧 Repack de l'ISO..."
( cd "$EXTRACT" && find . -type f ! -name md5sum.txt -exec md5sum {} \; > md5sum.txt 2>/dev/null || true )

# Récupère les infos de boot El Torito depuis la netinst d'origine
xorriso -indev "$NETINST" -report_el_torito as_mkisofs > "$WORK/eltorito.txt" 2>/dev/null || true

xorriso -as mkisofs \
  -r -V "CALEOPE_${CALEOPE_VERSION}" \
  -o "$OUT" \
  -J -joliet-long \
  -isohybrid-mbr /usr/lib/ISOLINUX/isohdpfx.bin \
  -c isolinux/boot.cat \
  -b isolinux/isolinux.bin -no-emul-boot -boot-load-size 4 -boot-info-table \
  -eltorito-alt-boot -e boot/grub/efi.img -no-emul-boot -isohybrid-gpt-basdat \
  "$EXTRACT" 2>/dev/null

echo ""
echo "✅ ISO construite : $OUT"
ls -lh "$OUT"
echo "   Grave-la (dd/balena) ou boote-la en VM. Elle installe Caleope sans interaction."
