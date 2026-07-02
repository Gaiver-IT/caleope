// Caleope UI — SPA vanilla JS
// Pas de framework, pas de build step. Go embed + vanilla = zéro deps.

'use strict';

// ── État global ───────────────────────────────────────────────────────────────
const S = {
  section: 'apps',
  tab: 'installed',
  apps: [],
  catalog: [],
  stats: {},
  sysinfo: {},
  locations: [],
  logApp: null,
  logStream: null,
  installTarget: null,
  installParams: [],
  backupApp: null,
  tasks: [],       // file de tâches style Proxmox
  taskSeq: 0,      // compteur d'ID de tâche
  appSearch: '',   // filtre recherche section apps
  appView: (() => { try { return localStorage.getItem('caleope-appview') || 'grid'; } catch(e) { return 'grid'; } })(),
  _statsAutoRefresh: false,
  _statsTimer: null,
};

// ── API client ────────────────────────────────────────────────────────────────
const api = {
  async req(method, path, body) {
    const opts = { method, headers: { 'Content-Type': 'application/json' } };
    if (body) opts.body = JSON.stringify(body);
    const r = await fetch(path, opts);
    if (r.status === 401) { showLogin(); return null; }
    const ct = r.headers.get('content-type') || '';
    if (ct.includes('application/json')) return r.json();
    return r.text();
  },
  get:    (p)       => api.req('GET',    p),
  post:   (p, b)    => api.req('POST',   p, b),
  patch:  (p, b)    => api.req('PATCH',  p, b),
  delete: (p)       => api.req('DELETE', p),
  del:    (p)       => api.req('DELETE', p),
};

// ── Auth ──────────────────────────────────────────────────────────────────────
async function checkAuth() {
  const r = await fetch('/auth/check');
  return r.ok;
}

async function login(password) {
  const r = await api.post('/auth/login', { password });
  if (!r) return { ok: false };
  return { ok: r.status === 'ok', totp_required: !!r.totp_required };
}

async function logout() {
  await fetch('/auth/logout', { method: 'POST' });
  showLogin();
}

// ── Notifications ─────────────────────────────────────────────────────────────
const _notifHistory = [];

function notify(msg, type = 'info') {
  const stack = document.getElementById('notif-stack');
  const n = document.createElement('div');
  n.className = `notif notif-${type}`;
  const icon = { ok: 'ti-check', err: 'ti-alert-circle', info: 'ti-info-circle' }[type];
  n.innerHTML = `<i class="ti ${icon}" aria-hidden="true"></i>${msg}`;
  stack.appendChild(n);
  setTimeout(() => n.remove(), 4000);
  // Track history
  _notifHistory.unshift({ msg, type, time: new Date().toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) });
  if (_notifHistory.length > 50) _notifHistory.pop();
  _updateNotifBadge();
}

let _notifUnread = 0;
function _updateNotifBadge() {
  _notifUnread++;
  const badge = document.getElementById('notif-history-badge');
  if (badge) {
    badge.textContent = _notifUnread > 9 ? '9+' : String(_notifUnread);
    badge.style.display = '';
  }
}

function openNotifHistory() {
  _notifUnread = 0;
  const badge = document.getElementById('notif-history-badge');
  if (badge) badge.style.display = 'none';
  const overlay = document.getElementById('notif-history-overlay');
  if (!overlay) return;
  const body = document.getElementById('notif-history-body');
  if (body) {
    if (!_notifHistory.length) {
      body.innerHTML = '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune notification.</div>';
    } else {
      body.innerHTML = _notifHistory.map(n => {
        const ico = { ok: 'ti-check', err: 'ti-alert-circle', info: 'ti-info-circle' }[n.type] || 'ti-info-circle';
        const col = { ok: 'var(--ok)', err: 'var(--err)', info: 'var(--accent)' }[n.type] || 'var(--accent)';
        return `<div style="display:flex;align-items:center;gap:8px;padding:5px 0;border-bottom:1px solid var(--border)">
          <i class="ti ${ico}" style="color:${col};font-size:11px;flex-shrink:0"></i>
          <span style="flex:1;font-size:9px;color:var(--text1)">${escapeHtml(n.msg)}</span>
          <span style="font-size:8px;color:var(--text3);flex-shrink:0">${escapeHtml(n.time)}</span>
        </div>`;
      }).join('');
    }
  }
  overlay.style.display = 'flex';
}

function closeNotifHistory() {
  const overlay = document.getElementById('notif-history-overlay');
  if (overlay) overlay.style.display = 'none';
}

// ── Segmented bar ─────────────────────────────────────────────────────────────
function segBar(pct, total = 20, cls = 'on') {
  const filled = Math.round(pct / 100 * total);
  return Array.from({ length: total }, (_, i) => {
    let c = 'seg';
    if (i < filled) c += (i === filled - 1) ? ' on-b' : ` ${cls}`;
    return `<div class="${c}"></div>`;
  }).join('');
}

// ── Clock + dashboard countdown ───────────────────────────────────────────────
function startClock() {
  const el = document.getElementById('clock');
  if (!el) return;
  const tick = () => {
    el.textContent = new Date().toLocaleTimeString('fr-FR', { hour12: false });
    // Countdown pour le dashboard
    if (S._dashRefreshedAt && S.section === 'dashboard') {
      const interval = parseInt(localStorage.getItem('caleope-refresh-interval') || '30');
      const cntEl = document.getElementById('dash-next-refresh');
      if (cntEl) {
        if (!interval) { cntEl.textContent = 'auto OFF'; cntEl.style.color = 'var(--text3)'; }
        else {
          const elapsed = Math.floor((Date.now() - S._dashRefreshedAt) / 1000);
          const remaining = Math.max(0, interval - (elapsed % interval));
          cntEl.textContent = remaining <= 5 ? `dans ${remaining}s…` : `prochain dans ${remaining}s`;
          cntEl.style.color = remaining <= 5 ? 'var(--warn)' : 'var(--accent)';
        }
      }
    }
  };
  tick();
  setInterval(tick, 1000);
}

// ── Params codés en dur pour les apps dont l'API ne retourne pas de params ────
// Structure: { id, label, type, default, required, description, options[], depends_on: {param, values[]} }
// Types: bool | text | secret | select | location
// depends_on masque le champ si la condition n'est pas remplie
const HARDCODED_PARAMS = {

  // ── arr-stack (Sonarr + Radarr + Prowlarr + qBittorrent + Gluetun) ───────────
  'arr-stack': [
    { id: 'vpn_enabled',          label: 'Kill-switch VPN (Gluetun)',  type: 'bool',     default: 'true',         required: false,
      description: 'Isole tout le trafic torrent derrière un VPN — fortement recommandé' },
    { id: 'vpn_protocol',         label: 'Protocole VPN',              type: 'select',   default: 'wireguard',    required: false,
      description: 'WireGuard (plus rapide) ou OpenVPN',
      options: ['wireguard','openvpn'],
      depends_on: { param: 'vpn_enabled', values: ['true'] } },
    // WireGuard fields
    { id: 'wireguard_private_key',label: 'Clé privée WireGuard',       type: 'secret',   default: '',             required: false,
      description: 'Clé privée WireGuard — dans le tableau de bord de votre fournisseur VPN',
      depends_on: { param: 'vpn_protocol', values: ['wireguard'] } },
    { id: 'wireguard_addresses',  label: 'Adresse tunnel WireGuard',   type: 'text',     default: '10.64.0.1/32', required: false,
      description: 'IP dans le tunnel (ex: 10.64.0.1/32 Mullvad, 10.2.0.2/32 ProtonVPN)',
      depends_on: { param: 'vpn_protocol', values: ['wireguard'] } },
    { id: 'vpn_server_countries', label: 'Pays du serveur VPN',        type: 'text',     default: 'Netherlands',  required: false,
      description: 'Pays de sortie (ex: Netherlands, Switzerland, France)',
      depends_on: { param: 'vpn_protocol', values: ['wireguard'] } },
    // OpenVPN fields
    { id: 'openvpn_user',         label: 'Identifiant OpenVPN',        type: 'text',     default: '',             required: false,
      description: 'Identifiant service OpenVPN (pas forcément votre email fournisseur)',
      depends_on: { param: 'vpn_protocol', values: ['openvpn'] } },
    { id: 'openvpn_password',     label: 'Mot de passe OpenVPN',       type: 'secret',   default: '',             required: false,
      description: 'Mot de passe service OpenVPN',
      depends_on: { param: 'vpn_protocol', values: ['openvpn'] } },
    { id: 'openvpn_server',       label: 'Serveur OpenVPN',            type: 'text',     default: '',             required: false,
      description: 'Ex: nl.protonvpn.net ou un serveur .ovpn de votre fournisseur',
      depends_on: { param: 'vpn_protocol', values: ['openvpn'] } },
    { id: 'media_path',           label: 'Stockage médias',            type: 'location', default: '/opt/gaiver-it/caleope/data/media', required: false,
      description: 'Emplacement des films, séries et téléchargements' },
  ],

  // ── authentik (Identity Provider / SSO) ──────────────────────────────────────
  'authentik': [
    { id: '_ram_info', label: '', type: 'info',
      description: '⚠️ Authentik nécessite ~600 Mo de RAM minimum. Si votre serveur a moins de 2 Go de RAM, préférez une solution plus légère comme Authelia ou gérez l\'accès au niveau réseau.' },
    { id: 'admin_email',    label: 'Email administrateur',   type: 'text',   default: '',       required: true,
      description: 'Email du premier compte admin Authentik' },
    { id: 'admin_password', label: 'Mot de passe admin',     type: 'secret', default: '',       required: true,
      description: 'Mot de passe du compte admin (min 8 caractères)' },
    { id: 'secret_key',     label: 'Clé secrète (optionnel)',type: 'secret', default: '',       required: false,
      description: 'Laisser vide pour générer automatiquement — signant des tokens JWT' },
    { id: 'smtp_enabled',   label: 'Activer l\'envoi d\'emails', type: 'bool', default: 'false', required: false,
      description: 'Pour l\'envoi de mails de vérification, réinitialisation de mot de passe, etc.' },
    { id: 'smtp_host',      label: 'Serveur SMTP',           type: 'text',   default: '',       required: false,
      description: 'Serveur SMTP (ex: smtp.gmail.com)',
      depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',      label: 'Port SMTP',              type: 'text',   default: '587',    required: false,
      description: '587 (STARTTLS) ou 465 (SSL)',
      depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',      label: 'Identifiant SMTP',       type: 'text',   default: '',       required: false,
      description: 'Identifiant pour l\'authentification SMTP',
      depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password',  label: 'Mot de passe SMTP',      type: 'secret', default: '',       required: false,
      description: 'Mot de passe ou token d\'application SMTP',
      depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_from',      label: 'Adresse expéditeur',     type: 'text',   default: '',       required: false,
      description: 'Ex: noreply@votre-domaine.com',
      depends_on: { param: 'smtp_enabled', values: ['true'] } },
  ],

  // ── azuracast (Radio Streaming) ───────────────────────────────────────────────
  'azuracast': [
    { id: 'admin_email',    label: 'Email administrateur',   type: 'text',   default: '',   required: true,
      description: 'Email du compte admin AzuraCast' },
    { id: 'admin_password', label: 'Mot de passe admin',     type: 'secret', default: '',   required: true,
      description: 'Mot de passe du compte admin' },
  ],

  // ── crowdsec (IDS/IPS) ────────────────────────────────────────────────────────
  'crowdsec': [
    { id: 'enroll_key', label: 'Clé d\'enrôlement (optionnel)', type: 'text', default: '', required: false,
      description: 'Clé depuis app.crowdsec.net pour connecter à la console centralisée' },
  ],

  // ── ghost (Blog CMS) ──────────────────────────────────────────────────────────
  'ghost': [
    { id: 'site_title',     label: 'Nom du site',            type: 'text',   default: 'Mon Blog', required: true,
      description: 'Titre public affiché sur le blog' },
    { id: 'admin_email',    label: 'Email administrateur',   type: 'text',   default: '',         required: true,
      description: 'Email du compte admin Ghost' },
    { id: 'admin_password', label: 'Mot de passe admin',     type: 'secret', default: '',         required: true,
      description: 'Mot de passe admin (min 10 caractères pour Ghost)' },
    { id: 'smtp_enabled',   label: 'Activer l\'envoi d\'emails', type: 'bool', default: 'false',  required: false,
      description: 'Nécessaire pour newsletters, notifications aux auteurs' },
    { id: 'smtp_host',      label: 'Serveur SMTP', type: 'text',   default: '',    required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',      label: 'Port SMTP',    type: 'text',   default: '587', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',      label: 'Identifiant SMTP',  type: 'text',   default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password',  label: 'Mot de passe SMTP', type: 'secret', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
  ],

  // ── gitea (Git self-hosted) ───────────────────────────────────────────────────
  'gitea': [
    { id: 'admin_username', label: 'Identifiant admin',      type: 'text',   default: 'gitadmin', required: true,
      description: 'Premier compte administrateur Gitea' },
    { id: 'admin_email',    label: 'Email admin',            type: 'text',   default: '',         required: true,
      description: 'Email du compte admin' },
    { id: 'admin_password', label: 'Mot de passe admin',     type: 'secret', default: '',         required: true,
      description: 'Mot de passe admin (min 8 caractères)' },
    { id: 'site_name',      label: 'Nom de l\'instance',     type: 'text',   default: 'Gitea',    required: false,
      description: 'Nom affiché dans l\'interface' },
    { id: 'smtp_enabled',   label: 'Activer l\'envoi d\'emails', type: 'bool', default: 'false',  required: false,
      description: 'Pour confirmation d\'email, notifications, etc.' },
    { id: 'smtp_host',      label: 'Serveur SMTP', type: 'text',   default: '',    required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',      label: 'Port SMTP',    type: 'text',   default: '587', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',      label: 'Identifiant SMTP',  type: 'text',   default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password',  label: 'Mot de passe SMTP', type: 'secret', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_from',      label: 'Adresse expéditeur', type: 'text',  default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] },
      description: 'Ex: gitea@votre-domaine.com' },
  ],

  // ── glpi (IT Asset Management) ────────────────────────────────────────────────
  'glpi': [],  // Setup via wizard web — aucun param requis à l'installation

  // ── immich (Bibliothèque photos) ──────────────────────────────────────────────
  'immich': [
    { id: 'upload_path', label: 'Stockage photos',  type: 'location', default: '/opt/gaiver-it/caleope/data/immich', required: false,
      description: 'Emplacement de stockage des photos importées depuis les appareils' },
    { id: 'db_password', label: 'Mot de passe base de données', type: 'secret', default: '', required: false,
      description: 'Laisser vide pour générer automatiquement' },
  ],

  // ── jellyfin (Media server) ───────────────────────────────────────────────────
  'jellyfin': [
    { id: 'media_path',       label: 'Stockage médias',          type: 'location', default: '/opt/gaiver-it/caleope/data/media', required: false,
      description: 'Emplacement des films, séries et musique' },
    { id: 'hw_transcoding',   label: 'Transcodage GPU (NVIDIA)', type: 'bool',     default: 'true', required: false,
      description: 'Active l\'accélération matérielle NVIDIA pour le transcodage vidéo' },
  ],

  // ── nextcloud (Cloud storage) ─────────────────────────────────────────────────
  'nextcloud': [
    { id: 'admin_user',     label: 'Identifiant admin',      type: 'text',     default: 'admin', required: true,
      description: 'Premier compte administrateur Nextcloud' },
    { id: 'admin_password', label: 'Mot de passe admin',     type: 'secret',   default: '',      required: true,
      description: 'Mot de passe du compte admin' },
    { id: 'data_path',      label: 'Stockage données',       type: 'location', default: '/opt/gaiver-it/caleope/data/nextcloud', required: false,
      description: 'Emplacement de stockage des fichiers utilisateurs' },
    { id: 'smtp_enabled',   label: 'Activer l\'envoi d\'emails', type: 'bool', default: 'false', required: false,
      description: 'Pour notifications, invitations de partage, etc.' },
    { id: 'smtp_host',      label: 'Serveur SMTP', type: 'text',   default: '',    required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',      label: 'Port SMTP',    type: 'text',   default: '587', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',      label: 'Identifiant SMTP',  type: 'text',   default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password',  label: 'Mot de passe SMTP', type: 'secret', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
  ],

  // ── prometheus-grafana (Monitoring) ──────────────────────────────────────────
  'prometheus-grafana': [
    { id: 'grafana_admin_user',     label: 'Identifiant admin Grafana', type: 'text',   default: 'admin', required: false,
      description: 'Identifiant pour l\'accès à Grafana' },
    { id: 'grafana_admin_password', label: 'Mot de passe Grafana',      type: 'secret', default: '',      required: false,
      description: 'Laisser vide pour le défaut "admin" (à changer au premier login)' },
  ],

  // ── pterodactyl-panel (Game server panel) ─────────────────────────────────────
  'pterodactyl-panel': [
    { id: 'admin_email',    label: 'Email administrateur',   type: 'text',   default: '',              required: true,
      description: 'Email du premier compte admin Pterodactyl' },
    { id: 'admin_username', label: 'Identifiant admin',      type: 'text',   default: 'admin',         required: true,
      description: 'Identifiant de connexion à Pterodactyl' },
    { id: 'admin_password', label: 'Mot de passe admin',     type: 'secret', default: '',              required: true,
      description: 'Mot de passe admin (min 8 caractères)' },
    { id: 'app_name',       label: 'Nom du panneau',         type: 'text',   default: 'Pterodactyl',   required: false,
      description: 'Nom affiché dans l\'interface' },
    { id: 'smtp_enabled',   label: 'Activer l\'envoi d\'emails', type: 'bool', default: 'false', required: false,
      description: 'Pour notifications serveurs, mots de passe oubliés, etc.' },
    { id: 'smtp_host',      label: 'Serveur SMTP', type: 'text',   default: '',    required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',      label: 'Port SMTP',    type: 'text',   default: '587', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',      label: 'Identifiant SMTP',  type: 'text',   default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password',  label: 'Mot de passe SMTP', type: 'secret', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_from',      label: 'Adresse expéditeur', type: 'text', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
  ],

  // ── pterodactyl-wings (Game server node) ──────────────────────────────────────
  'pterodactyl-wings': [
    { id: 'panel_url',    label: 'URL du panneau Pterodactyl', type: 'text',   default: '', required: true,
      description: 'Ex: https://panel.votre-domaine.com — doit être accessible depuis ce serveur' },
    { id: 'wings_token',  label: 'Token du nœud Wings',        type: 'secret', default: '', required: true,
      description: 'Généré dans Admin > Nodes > [votre nœud] > Configuration dans Pterodactyl Panel' },
    { id: 'node_name',    label: 'Nom du nœud',                type: 'text',   default: 'node-01', required: true,
      description: 'Nom identifiant ce nœud dans Pterodactyl' },
  ],

  // ── restic (Backup) ───────────────────────────────────────────────────────────
  'restic': [
    { id: 'repo_backend',    label: 'Type de dépôt',          type: 'select', default: 'sftp', required: true,
      description: 'Où stocker les sauvegardes',
      options: ['sftp','s3','b2','local'] },
    { id: 'repo_password',   label: 'Mot de passe dépôt',     type: 'secret', default: '',     required: true,
      description: 'Chiffre le dépôt Restic — ne pas perdre ce mot de passe !' },
    // SFTP fields
    { id: 'sftp_host',  label: 'Hôte SFTP', type: 'text', default: '', required: false, depends_on: { param: 'repo_backend', values: ['sftp'] },
      description: 'Adresse du serveur SSH/SFTP' },
    { id: 'sftp_user',  label: 'Utilisateur SFTP', type: 'text', default: '', required: false, depends_on: { param: 'repo_backend', values: ['sftp'] } },
    { id: 'sftp_path',  label: 'Chemin distant SFTP', type: 'text', default: '/backups/caleope', required: false, depends_on: { param: 'repo_backend', values: ['sftp'] },
      description: 'Répertoire sur le serveur SFTP' },
    // S3 fields
    { id: 's3_endpoint',   label: 'Endpoint S3',        type: 'text',   default: 's3.amazonaws.com', required: false, depends_on: { param: 'repo_backend', values: ['s3'] },
      description: 'Ex: s3.amazonaws.com, minio.votre-domaine.com' },
    { id: 's3_bucket',     label: 'Bucket S3',          type: 'text',   default: '',    required: false, depends_on: { param: 'repo_backend', values: ['s3'] } },
    { id: 's3_region',     label: 'Région S3',          type: 'text',   default: 'us-east-1', required: false, depends_on: { param: 'repo_backend', values: ['s3'] } },
    { id: 's3_access_key', label: 'Access Key S3',      type: 'text',   default: '',    required: false, depends_on: { param: 'repo_backend', values: ['s3'] } },
    { id: 's3_secret_key', label: 'Secret Key S3',      type: 'secret', default: '',    required: false, depends_on: { param: 'repo_backend', values: ['s3'] } },
    // Backblaze B2 fields
    { id: 'b2_account_id',  label: 'B2 Account ID',    type: 'text',   default: '', required: false, depends_on: { param: 'repo_backend', values: ['b2'] } },
    { id: 'b2_account_key', label: 'B2 Application Key', type: 'secret', default: '', required: false, depends_on: { param: 'repo_backend', values: ['b2'] } },
    { id: 'b2_bucket',      label: 'B2 Bucket',         type: 'text',   default: '', required: false, depends_on: { param: 'repo_backend', values: ['b2'] } },
    // Local fields
    { id: 'local_repo_path', label: 'Emplacement dépôt local', type: 'location', default: '/opt/gaiver-it/caleope/data/restic', required: false,
      depends_on: { param: 'repo_backend', values: ['local'] } },
    // Schedule
    { id: 'schedule', label: 'Planification cron', type: 'text', default: '0 2 * * *', required: false,
      description: 'Cron de déclenchement automatique (ex: 0 2 * * * = chaque nuit à 2h)' },
  ],

  // ── tailscale (Mesh VPN) ──────────────────────────────────────────────────────
  'tailscale': [
    { id: 'auth_key',          label: 'Clé d\'authentification',    type: 'secret', default: '',     required: true,
      description: 'Clé tskey-auth-... depuis admin.tailscale.com > Settings > Keys' },
    { id: 'hostname',          label: 'Nom d\'hôte (optionnel)',    type: 'text',   default: '',     required: false,
      description: 'Nom du nœud dans votre tailnet — par défaut le hostname système' },
    { id: 'exit_node_enabled', label: 'Agir comme nœud de sortie', type: 'bool',   default: 'false', required: false,
      description: 'Permet à d\'autres appareils du tailnet d\'utiliser ce serveur comme sortie internet' },
    { id: 'accept_routes',     label: 'Accepter les routes subnet', type: 'bool',   default: 'true',  required: false,
      description: 'Accepte les routes annoncées par d\'autres nœuds du tailnet' },
  ],

  // ── vaultwarden (Gestionnaire de mots de passe) ───────────────────────────────
  'vaultwarden': [
    { id: 'admin_token',  label: 'Token admin interface /admin', type: 'secret', default: '', required: false,
      description: 'Laisser vide pour désactiver l\'interface /admin — recommandé en prod de mettre un token fort' },
    { id: 'data_path',    label: 'Stockage des coffres',         type: 'location', default: '/opt/gaiver-it/caleope/data/vaultwarden', required: false,
      description: 'Emplacement des bases de données chiffrées' },
    { id: 'smtp_enabled', label: 'Activer l\'envoi d\'emails',   type: 'bool', default: 'false', required: false,
      description: 'Pour vérification email, invitations, alertes de connexion' },
    { id: 'smtp_host',     label: 'Serveur SMTP', type: 'text',   default: '',    required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',     label: 'Port SMTP',    type: 'text',   default: '587', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',     label: 'Identifiant SMTP',  type: 'text',   default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password', label: 'Mot de passe SMTP', type: 'secret', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_from',     label: 'Adresse expéditeur', type: 'text',  default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
  ],

  // ── wg-easy (Serveur VPN WireGuard) ──────────────────────────────────────────
  'wg-easy': [
    { id: 'wg_host',     label: 'IP ou domaine public',       type: 'text',   default: '',       required: true,
      description: 'IP publique ou DNS du serveur — les clients VPN se connectent à cette adresse' },
    { id: 'wg_password', label: 'Mot de passe interface web', type: 'secret', default: '',       required: true,
      description: 'Mot de passe pour accéder à l\'interface web de gestion' },
    { id: 'wg_port',     label: 'Port UDP WireGuard',         type: 'text',   default: '51820',  required: false,
      description: 'Port UDP d\'écoute — doit être ouvert sur le pare-feu' },
    { id: 'wg_cidr',     label: 'Sous-réseau VPN',            type: 'text',   default: '10.8.0.0/24', required: false,
      description: 'Plage d\'adresses IP attribuées aux clients VPN' },
    { id: 'wg_dns',      label: 'DNS pour les clients',       type: 'text',   default: '1.1.1.1', required: false,
      description: 'Serveur DNS utilisé par les appareils connectés au VPN' },
  ],

  // ── wikijs (Wiki) ─────────────────────────────────────────────────────────────
  'wikijs': [
    { id: 'admin_email',    label: 'Email administrateur',   type: 'text',   default: '',     required: true,
      description: 'Email du premier compte admin Wiki.js' },
    { id: 'admin_password', label: 'Mot de passe admin',     type: 'secret', default: '',     required: true,
      description: 'Mot de passe du compte admin' },
    { id: 'site_title',     label: 'Titre du wiki',          type: 'text',   default: 'Wiki', required: false,
      description: 'Nom affiché dans l\'interface et les onglets' },
    { id: 'smtp_enabled',   label: 'Activer l\'envoi d\'emails', type: 'bool', default: 'false', required: false,
      description: 'Pour notifications, invitations, récupération de compte' },
    { id: 'smtp_host',     label: 'Serveur SMTP', type: 'text',   default: '',    required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',     label: 'Port SMTP',    type: 'text',   default: '587', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',     label: 'Identifiant SMTP',  type: 'text',   default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password', label: 'Mot de passe SMTP', type: 'secret', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_from',     label: 'Adresse expéditeur', type: 'text',  default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
  ],

  // ── wordpress (CMS) ───────────────────────────────────────────────────────────
  'wordpress': [
    { id: 'site_title',     label: 'Titre du site',           type: 'text',   default: 'Mon Site', required: true,
      description: 'Titre affiché dans l\'interface et les résultats de recherche' },
    { id: 'admin_user',     label: 'Identifiant admin',       type: 'text',   default: 'admin',    required: true,
      description: 'Identifiant du compte administrateur WordPress' },
    { id: 'admin_email',    label: 'Email admin',             type: 'text',   default: '',         required: true,
      description: 'Email du compte admin WordPress' },
    { id: 'admin_password', label: 'Mot de passe admin',      type: 'secret', default: '',         required: true,
      description: 'Mot de passe du compte admin' },
    { id: 'smtp_enabled',   label: 'Activer l\'envoi d\'emails', type: 'bool', default: 'false',   required: false,
      description: 'Pour formulaires de contact, notifications, etc.' },
    { id: 'smtp_host',     label: 'Serveur SMTP', type: 'text',   default: '',    required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_port',     label: 'Port SMTP',    type: 'text',   default: '587', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_user',     label: 'Identifiant SMTP',  type: 'text',   default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
    { id: 'smtp_password', label: 'Mot de passe SMTP', type: 'secret', default: '', required: false, depends_on: { param: 'smtp_enabled', values: ['true'] } },
  ],

  // ── pterodactyl (Panel + Wings) ────────────────────────────────────────────
  'pterodactyl': [
    { id: 'admin_email', label: 'Email administrateur', type: 'text',   default: '',      required: true,
      description: 'Email du compte admin du panel' },
    { id: 'admin_user',  label: 'Identifiant admin',    type: 'text',   default: 'admin', required: true,
      description: 'Identifiant de connexion au panel' },
    { id: 'admin_first', label: 'Prénom',               type: 'text',   default: 'Admin', required: false },
    { id: 'admin_last',  label: 'Nom',                  type: 'text',   default: 'Caleope', required: false },
  ],

  // ── mealie (recettes) ────────────────────────────────────────────────────────
  'mealie': [
    { id: 'MEALIE_PORT_WEB', label: 'Port web', type: 'port', default: '9000', required: false,
      description: 'Port d\'accès à l\'interface Mealie' },
  ],

  // ── ntfy (notifications push) ────────────────────────────────────────────────
  'ntfy': [
    { id: 'NTFY_PORT_WEB', label: 'Port web', type: 'port', default: '8070', required: false,
      description: 'Port d\'accès à l\'interface ntfy' },
  ],

  // ── n8n (automation) ─────────────────────────────────────────────────────────
  'n8n': [
    { id: 'N8N_PORT_WEB', label: 'Port web', type: 'port', default: '5678', required: false,
      description: 'Port d\'accès à l\'interface n8n' },
  ],

  // ── filebrowser ───────────────────────────────────────────────────────────────
  'filebrowser': [
    { id: 'FILEBROWSER_PORT_WEB', label: 'Port web', type: 'port', default: '8085', required: false,
      description: 'Port d\'accès à l\'interface File Browser' },
  ],

  'changedetection': [
    { id: 'CHANGEDETECTION_PORT_WEB', label: 'Port web', type: 'port', default: '5055', required: false,
      description: 'Port d\'accès à l\'interface Changedetection.io' },
  ],

  'gotify': [
    { id: 'GOTIFY_PORT_WEB', label: 'Port web', type: 'port', default: '8090', required: false,
      description: 'Port d\'accès à l\'interface Gotify' },
    { id: 'GOTIFY_DEFAULTUSER_PASS', label: 'Mot de passe admin', type: 'secret', default: '', required: false,
      description: 'Mot de passe du compte admin par défaut' },
  ],

  'homarr': [
    { id: 'HOMARR_PORT_WEB', label: 'Port web', type: 'port', default: '7575', required: false,
      description: 'Port d\'accès à l\'interface Homarr' },
  ],

  'watchtower': [
    { id: 'WATCHTOWER_SCHEDULE', label: 'Planification (cron)', type: 'text', default: '0 0 4 * * *', required: false,
      description: 'Cron expression pour la vérification (ex: 0 0 4 * * * = chaque jour à 4h)' },
    { id: 'WATCHTOWER_CLEANUP', label: 'Nettoyage images', type: 'bool', default: 'true', required: false,
      description: 'Supprimer les anciennes images après mise à jour' },
  ],

  'grocy': [
    { id: 'GROCY_PORT_WEB', label: 'Port web', type: 'port', default: '9283', required: false,
      description: 'Port d\'accès à l\'interface Grocy' },
  ],

  'jellyseerr': [
    { id: 'JELLYSEERR_PORT_WEB', label: 'Port web', type: 'port', default: '5099', required: false,
      description: 'Port d\'accès à l\'interface Jellyseerr' },
  ],

  'monica': [
    { id: 'MONICA_PORT_WEB', label: 'Port web', type: 'port', default: '8082', required: false,
      description: 'Port d\'accès à l\'interface Monica' },
    { id: 'MYSQL_PASSWORD', label: 'Mot de passe DB', type: 'secret', default: '', required: false,
      description: 'Mot de passe MySQL pour le compte monica' },
  ],

  'home-assistant': [
    { id: 'HA_PORT_WEB', label: 'Port web', type: 'port', default: '8123', required: false,
      description: 'Port d\'accès à l\'interface Home Assistant' },
  ],

  'calibre-web': [
    { id: 'CALIBREWEB_PORT_WEB', label: 'Port web', type: 'port', default: '8083', required: false,
      description: 'Port d\'accès à l\'interface Calibre-Web' },
  ],

  'navidrome': [
    { id: 'NAVIDROME_PORT_WEB', label: 'Port web', type: 'port', default: '4533', required: false,
      description: 'Port d\'accès à l\'interface Navidrome' },
  ],

  'photoprism': [
    { id: 'PHOTOPRISM_PORT_WEB', label: 'Port web', type: 'port', default: '2342', required: false,
      description: 'Port d\'accès à l\'interface PhotoPrism' },
    { id: 'PHOTOPRISM_ADMIN_PASSWORD', label: 'Mot de passe admin', type: 'secret', default: '', required: false,
      description: 'Mot de passe du compte admin' },
  ],

  'kavita': [
    { id: 'KAVITA_PORT_WEB', label: 'Port web', type: 'port', default: '5001', required: false,
      description: 'Port d\'accès à l\'interface Kavita' },
  ],
  'komga': [
    { id: 'KOMGA_PORT', label: 'Port web', type: 'port', default: '8085', required: false,
      description: 'Port d\'accès à l\'interface Komga' },
    { id: 'KOMGA_ADMIN_EMAIL', label: 'Email admin', type: 'email', default: '', required: true,
      description: 'Email du compte administrateur' },
    { id: 'KOMGA_ADMIN_PASSWORD', label: 'Mot de passe admin', type: 'secret', default: '', required: true,
      description: 'Mot de passe du compte administrateur (8 car. min)' },
  ],
  'code-server': [
    { id: 'CODE_SERVER_PORT', label: 'Port web', type: 'port', default: '8443', required: false,
      description: 'Port d\'accès à Code Server' },
    { id: 'CODE_SERVER_PASSWORD', label: 'Mot de passe', type: 'secret', default: '', required: false,
      description: 'Mot de passe d\'accès (auto-généré si vide)' },
  ],
  'scrutiny': [
    { id: 'SCRUTINY_PORT', label: 'Port web', type: 'port', default: '8086', required: false,
      description: 'Port d\'accès à l\'interface Scrutiny' },
  ],
  'memos': [
    { id: 'MEMOS_PORT_WEB', label: 'Port web', type: 'port', default: '5230', required: false,
      description: 'Port d\'accès à l\'interface Memos' },
    { id: 'MEMOS_ADMIN_USER', label: 'Nom d\'utilisateur admin', type: 'text', default: 'admin', required: false,
      description: 'Nom d\'utilisateur du compte administrateur' },
    { id: 'MEMOS_ADMIN_PASS', label: 'Mot de passe admin', type: 'secret', default: '', required: false,
      description: 'Mot de passe du compte admin (auto-généré si vide)' },
  ],
  'linkding': [
    { id: 'LINKDING_PORT_WEB', label: 'Port web', type: 'port', default: '9090', required: false,
      description: 'Port d\'accès à l\'interface Linkding' },
    { id: 'LINKDING_ADMIN_USER', label: 'Nom d\'utilisateur admin', type: 'text', default: 'admin', required: false,
      description: 'Nom d\'utilisateur du compte administrateur' },
    { id: 'LINKDING_ADMIN_PASS', label: 'Mot de passe admin', type: 'secret', default: '', required: false,
      description: 'Mot de passe du compte admin (auto-généré si vide)' },
  ],
};

// ── Icons apps (défaut) ───────────────────────────────────────────────────────
const APP_ICONS = {
  // Médias
  jellyfin: '🎬', 'arr-stack': '📡', azuracast: '🎵', immich: '📸', jellyseerr: '🎟️',
  // Cloud & fichiers
  nextcloud: '☁️', filebrowser: '📁', syncthing: '🔄',
  // Sécurité & réseau
  vaultwarden: '🔒', authentik: '🔑', crowdsec: '🛡️', 'wg-easy': '🌐',
  tailscale: '🔐', pihole: '🚫', adguard: '🛡️',
  // Dev & ops
  gitea: '🐙', pterodactyl: '🦕', watchtower: '🔭',
  'prometheus-grafana': '📊', 'uptime-kuma': '📈',
  // Web & contenu
  ghost: '👻', wordpress: '📝', 'wiki-js': '📚',
  // Productivity
  glpi: '🎫', n8n: '⚙️', changedetection: '👁️',
  // Lifestyle & smart home
  mealie: '🥗', grocy: '🛒', monica: '👤', 'home-assistant': '🏡',
  // Outils
  memos: '📓', linkding: '🔖', 'paperless-ngx': '📄', 'stirling-pdf': '📑',
  freshrss: '📰', ntfy: '🔔', gotify: '🔔', homarr: '🏠',
  // Médias spécialisés
  navidrome: '🎵', photoprism: '🖼️', kavita: '📚', komga: '📔', jellyseerr: '🎟️',
  'calibre-web': '📖', plex: '🎥', azuracast: '🎙️',
  // Dev & monitoring
  'code-server': '💻', scrutiny: '🔬',
};
const icon = id => APP_ICONS[id] || '📦';

// ── Sidebar search ────────────────────────────────────────────────────────────
function filterSidebar(q) {
  const query = q.trim().toLowerCase();
  document.querySelectorAll('.sb-nav .nav-btn[data-section]').forEach(btn => {
    const label = btn.textContent.toLowerCase();
    const section = btn.dataset.section || '';
    const match = !query || label.includes(query) || section.includes(query);
    btn.style.display = match ? '' : 'none';
  });
  document.querySelectorAll('.sb-section').forEach(sec => {
    if (!query) { sec.style.display = ''; return; }
    const visibleBtns = sec.querySelectorAll('.nav-btn[data-section]:not([style*="none"])');
    sec.style.display = visibleBtns.length ? '' : 'none';
  });
}

// ── Recent sections ───────────────────────────────────────────────────────────
function getRecentSections() {
  try { return JSON.parse(localStorage.getItem('caleope-recents') || '[]'); } catch(e) { return []; }
}
function pushRecentSection(id) {
  if (id === 'dashboard') return;
  let recents = getRecentSections().filter(r => r !== id);
  recents.unshift(id);
  recents = recents.slice(0, 5);
  try { localStorage.setItem('caleope-recents', JSON.stringify(recents)); } catch(e) {}
  buildRecentSection();
}
function buildRecentSection() {
  const recents = getRecentSections();
  const list = document.getElementById('sb-recent-list');
  const sec = document.getElementById('sb-section-recent');
  if (!list || !sec) return;
  if (!recents.length) { sec.style.display = 'none'; return; }
  sec.style.display = '';
  list.innerHTML = recents.map(id => {
    const sec2 = SECTIONS[id];
    if (!sec2) return '';
    const app = Object.keys(APP_PANELS).find(aid => APP_PANELS[aid].panels?.some(p => p.id === id));
    const panelDef = app ? APP_PANELS[app]?.panels?.find(p => p.id === id) : null;
    const label = sec2.label || panelDef?.label || id.toUpperCase();
    const appIcon = app ? icon(app) : '';
    return `<button class="nav-btn" data-section="${id}" onclick="goSection('${id}')">
      <span style="font-size:11px;margin-right:2px">${appIcon}</span>${escapeHtml(label)}
    </button>`;
  }).join('');
}

// ── Badge statut ──────────────────────────────────────────────────────────────
function statusBadge(status) {
  const map = {
    running: `<span class="badge badge-run"><span style="width:5px;height:5px;background:var(--vio-b);display:inline-block;animation:pulse 2s infinite"></span>&nbsp;ACTIF</span>`,
    stopped: `<span class="badge badge-stop">ARRÊTÉE</span>`,
    installing: `<span class="badge badge-warn">INSTALL...</span>`,
    error: `<span class="badge badge-err">ERREUR</span>`,
  };
  return map[status] || `<span class="badge badge-stop">${status.toUpperCase()}</span>`;
}

// ── SECTION: APPS ─────────────────────────────────────────────────────────────
async function loadApps() {
  const [apps, store, stats, ping] = await Promise.all([
    api.get('/api/v1/apps'),
    api.get('/api/v1/store'),
    api.get('/api/v1/stats'),
    api.get('/api/v1/ping'),
  ]);
  const _appsData = apps?.data;
  S.apps    = Array.isArray(_appsData) ? _appsData : (_appsData?.apps || []);
  S.catalog = store?.data   || [];
  S.stats   = { ...(stats?.data || {}), version: ping?.data?.version };
  updateTbSysbar();
  buildDynamicNav();
  buildPinnedSection();
  buildRecentSection();
  renderApps();
}

function renderApps() {
  const c = document.getElementById('content-apps');
  if (!c) return;

  const running = S.apps.filter(a => a.status === 'running').length;
  const diskUsed  = S.stats.disk_used_gb  || 0;
  const diskTotal = S.stats.disk_total_gb || 200;
  const diskPct   = Math.round(diskUsed / diskTotal * 100);

  c.innerHTML = `
    <div class="metrics">
      <div class="mc">
        <div class="mc-label">INSTALLÉES</div>
        <div class="mc-val">${String(S.apps.length).padStart(2,'0')}</div>
        <div class="mc-sub">SUR ${S.catalog.length} DISPO</div>
      </div>
      <div class="mc mc-vio">
        <div class="mc-label">EN COURS</div>
        <div class="mc-val">${String(running).padStart(2,'0')}</div>
        <div class="mc-sub">${S.apps.length - running} ARRÊTÉE${S.apps.length - running > 1 ? 'S' : ''}</div>
      </div>
      <div class="mc">
        <div class="mc-label">STOCKAGE APPS</div>
        <div class="mc-val">${diskUsed > 0 ? diskUsed.toFixed(1) + 'G' : '—'}</div>
        <div class="mc-sub">
          <div class="seg-wrap" style="margin:4px 0 0">
            <div class="seg-bar">${segBar(diskPct)}</div>
          </div>
        </div>
      </div>
      <div class="mc">
        <div class="mc-label">DAEMON</div>
        <div class="mc-val mc-ok" style="font-size:14px;letter-spacing:0;padding-top:4px">EN LIGNE</div>
        <div class="mc-sub">v${S.stats.version || '—'}</div>
      </div>
    </div>

    <div class="tabs" style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">
      <button class="tab-btn ${S.tab === 'installed' ? 'active' : ''}" onclick="switchTab('installed')">
        INSTALLÉES <span class="tab-count">${S.apps.length}</span>
      </button>
      <button class="tab-btn ${S.tab === 'catalog' ? 'active' : ''}" onclick="switchTab('catalog')">
        CATALOGUE <span class="tab-count">${S.catalog.length}</span>
      </button>
      ${S.tab === 'installed' ? `
      <span style="width:1px;height:14px;background:var(--border);margin:0 2px"></span>
      <button class="tab-btn ${(S.statusFilter||'all')==='all'?'active':''}" style="font-size:8px;padding:3px 7px" onclick="filterAppStatus('all')">TOUTES</button>
      <button class="tab-btn ${(S.statusFilter||'all')==='running'?'active':''}" style="font-size:8px;padding:3px 7px;color:var(--vio-b)" onclick="filterAppStatus('running')">ACTIVES <span class="tab-count" style="background:var(--vio-b);color:var(--bg)">${running}</span></button>
      <button class="tab-btn ${(S.statusFilter||'all')==='stopped'?'active':''}" style="font-size:8px;padding:3px 7px" onclick="filterAppStatus('stopped')">ARRÊTÉES <span class="tab-count">${S.apps.length - running}</span></button>
      ` : ''}
      <input id="app-search" type="search" placeholder="Rechercher..." autocomplete="off"
        value="${escapeHtml(S.appSearch || '')}"
        oninput="filterApps(this.value)"
        style="margin-left:auto;font-size:9px;padding:4px 8px;background:var(--card);border:1px solid var(--border);
               border-radius:4px;color:var(--text1);width:140px;outline:none">
      <div style="display:flex;border:1px solid var(--border);border-radius:4px;overflow:hidden">
        <button title="Vue grille" onclick="setAppView('grid')"
          style="padding:4px 7px;background:${(S.appView||'grid')==='grid'?'var(--bg3)':'transparent'};border:none;cursor:pointer;color:var(--text2)">
          <i class="ti ti-layout-grid" style="font-size:11px"></i>
        </button>
        <button title="Vue liste" onclick="setAppView('list')"
          style="padding:4px 7px;background:${(S.appView||'grid')==='list'?'var(--bg3)':'transparent'};border:none;cursor:pointer;color:var(--text2)">
          <i class="ti ti-list" style="font-size:11px"></i>
        </button>
      </div>
    </div>

    <div id="tab-installed" class="${S.tab !== 'installed' ? 'hidden' : ''}">
      ${S.apps.length > 0 && S.tab === 'installed' ? `
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:10px;flex-wrap:wrap">
        <span style="font-size:8px;color:var(--text3);letter-spacing:.5px">ACTIONS EN LOT :</span>
        <button class="btn-sm" onclick="bulkAction('start')" title="Démarrer toutes les apps arrêtées"
          style="font-size:8px;padding:3px 8px">
          <i class="ti ti-player-play"></i>DÉMARRER ARRÊTÉES
        </button>
        <button class="btn-sm" onclick="bulkAction('restart')" title="Redémarrer toutes les apps actives"
          style="font-size:8px;padding:3px 8px">
          <i class="ti ti-refresh"></i>REDÉMARRER ACTIVES
        </button>
        <button class="btn-sm danger" onclick="bulkAction('stop')" title="Arrêter toutes les apps actives"
          style="font-size:8px;padding:3px 8px">
          <i class="ti ti-player-pause"></i>TOUT ARRÊTER
        </button>
        <span id="bulk-status" style="font-size:8px;color:var(--text3);margin-left:4px"></span>
      </div>` : ''}
      ${S.apps.length === 0
        ? `<div class="empty-state">
            <div class="empty-icon"><i class="ti ti-apps" aria-hidden="true"></i></div>
            <div class="empty-title">AUCUNE APP INSTALLÉE</div>
            <div class="empty-sub">Ouvrez le catalogue pour installer votre première app.</div>
           </div>`
        : (S.appView === 'list'
          ? `<div class="apps-list" id="installed-grid">${S.apps.map(appListRow).join('')}</div>`
          : `<div class="apps-grid" id="installed-grid">${S.apps.map(appCard).join('')}</div>`)
      }
    </div>

    <div id="tab-catalog" class="${S.tab !== 'catalog' ? 'hidden' : ''}">
      ${S.catalog.length === 0
        ? `<div class="empty-state">
            <div class="empty-icon"><i class="ti ti-store" aria-hidden="true"></i></div>
            <div class="empty-title">CATALOGUE VIDE</div>
            <div class="empty-sub">Vérifiez la connexion au store.</div>
           </div>`
        : (() => {
            const cats = [...new Set(S.catalog.map(a => a.category || 'other'))].sort();
            const catBtns = cats.map(cat => `
              <button class="tab-btn cat-filter-btn" data-cat="${escapeHtml(cat)}"
                onclick="filterCatalogCat(this,'${escapeHtml(cat)}')"
                style="font-size:8px;padding:3px 8px">
                ${cat.toUpperCase()}
              </button>`).join('');
            return `
              <div style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px;align-items:center">
                <button class="tab-btn cat-filter-btn active" data-cat="all"
                  onclick="filterCatalogCat(this,'all')"
                  style="font-size:8px;padding:3px 8px">TOUT</button>
                ${catBtns}
              </div>
              <div class="cat-grid" id="catalog-grid">${S.catalog.map(catalogCard).join('')}</div>`;
          })()
      }
    </div>
  `;
}

function _hasUpgrade(app) {
  const catalogEntry = (S.catalog || []).find(c => c.id === app.id);
  if (!catalogEntry || !catalogEntry.version || !app.version) return false;
  return catalogEntry.version !== app.version;
}

function appCard(app) {
  const isRunning = app.status === 'running';
  const domain = app.domain ? `https://${app.domain}` : null;
  const iconEl = domain
    ? `<a class="app-icon" href="${domain}" target="_blank" rel="noopener" title="Ouvrir ${escapeHtml(app.name || app.id)}">${icon(app.id)}</a>`
    : `<div class="app-icon">${icon(app.id)}</div>`;
  // Container resource stats (matched by app.id, containers can have prefix '/')
  const containers = S.containers || [];
  const ct = containers.find(c => {
    const name = (c.name || '').replace(/^\//, '');
    return name === app.id || name.startsWith(app.id + '-') || name.startsWith(app.id + '_');
  });
  const ctStats = ct && isRunning
    ? `<div style="display:flex;gap:6px;margin-top:4px">
        <span style="font-size:8px;color:var(--text3);background:var(--bg);padding:1px 4px;border-radius:3px" title="CPU">
          <i class="ti ti-cpu" style="font-size:8px"></i> ${escapeHtml(ct.cpu || '—')}</span>
        <span style="font-size:8px;color:var(--text3);background:var(--bg);padding:1px 4px;border-radius:3px" title="RAM">
          <i class="ti ti-device-desktop" style="font-size:8px"></i> ${escapeHtml((ct.mem || '').split(' / ')[0] || '—')}</span>
      </div>` : '';
  const hasUpgrade = _hasUpgrade(app);
  return `
    <div class="app-card ${isRunning ? 'running' : ''}">
      <div class="card-corner"></div>
      <div class="app-top">
        ${iconEl}
        <div class="app-meta">
          <div class="app-name">${app.name || app.id}</div>
          <div class="app-ver">${app.version || '—'}${hasUpgrade ? `&nbsp;<span title="Mise à jour disponible dans le store" style="font-size:7px;font-weight:800;color:var(--warn);background:rgba(255,200,0,.12);padding:1px 4px;border-radius:3px;letter-spacing:.5px">UPDATE</span>` : ''}</div>
        </div>
        ${statusBadge(app.status)}
      </div>
      ${ctStats}
      <div class="app-desc">${app.description || ''}</div>
      <div class="app-footer">
        <div class="app-actions">
          <button class="action-btn" onclick="openLogs('${app.id}')" title="Logs">
            <i class="ti ti-terminal-2"></i>
            <span class="btn-label">LOGS</span>
          </button>
          ${isRunning
            ? `<button class="action-btn" onclick="appAction('${app.id}','restart')" title="Redémarrer">
                <i class="ti ti-refresh"></i>
                <span class="btn-label">REDÉMARRER</span>
               </button>
               <button class="action-btn danger" onclick="appAction('${app.id}','stop')" title="Arrêter">
                <i class="ti ti-player-pause"></i>
                <span class="btn-label">ARRÊTER</span>
               </button>`
            : `<button class="action-btn success" onclick="appAction('${app.id}','start')" title="Démarrer">
                <i class="ti ti-player-play"></i>
                <span class="btn-label">DÉMARRER</span>
               </button>`
          }
          <button class="action-btn" onclick="triggerAppBackup('${app.id}')" title="Sauvegarder">
            <i class="ti ti-device-floppy"></i>
            <span class="btn-label">BACKUP</span>
          </button>
          ${HARDCODED_PARAMS[app.id] ? `
          <button class="action-btn" onclick="openReconfigureModal('${app.id}')" title="Reconfigurer">
            <i class="ti ti-settings"></i>
            <span class="btn-label">CONFIG</span>
          </button>` : ''}
          ${isRunning ? `
          <button class="action-btn" onclick="openInspect('${app.id}')" title="Inspecter le conteneur">
            <i class="ti ti-info-circle"></i>
            <span class="btn-label">INSPECT</span>
          </button>` : ''}
          <button class="action-btn" onclick="showPostInstallNotes('${app.id}','${escapeHtml(app.name||app.id)}')" title="Notes d'installation">
            <i class="ti ti-notes"></i>
            <span class="btn-label">NOTES</span>
          </button>
          ${APP_PANELS[app.id] && isRunning ? `
          <button class="action-btn" onclick="goSection('${APP_PANELS[app.id].panels[0]?.id || 'panel-'+app.id}')" title="Ouvrir le panel intégré">
            <i class="ti ti-layout-sidebar-right"></i>
            <span class="btn-label">PANEL</span>
          </button>` : ''}
          ${(() => {
            const pinned = getPins().includes(app.id);
            return `<button class="action-btn${pinned ? ' active' : ''}" onclick="togglePinApp('${app.id}')" title="${pinned ? 'Retirer des favoris' : 'Épingler dans la sidebar'}">
              <i class="ti ti-star${pinned ? '-filled' : ''}"></i>
              <span class="btn-label">${pinned ? 'ÉPINGLÉ' : 'ÉPINGLER'}</span>
            </button>`;
          })()}
          <button class="action-btn danger" onclick="removeApp('${app.id}')" title="Supprimer (action irréversible)">
            <i class="ti ti-trash"></i>
            <span class="btn-label">SUPPRIMER</span>
          </button>
        </div>
        ${domain ? `<a class="app-link" href="${domain}" target="_blank" rel="noopener"><i class="ti ti-external-link" style="font-size:10px"></i>OUVRIR</a>` : ''}
      </div>
    </div>
  `;
}

function setAppView(mode) {
  S.appView = mode;
  try { localStorage.setItem('caleope-appview', mode); } catch(e) {}
  renderApps();
}

function appListRow(app) {
  const isRunning = app.status === 'running';
  const domain = app.domain ? `https://${app.domain}` : null;
  const containers = S.containers || [];
  const ct = containers.find(c => {
    const name = (c.name || '').replace(/^\//, '');
    return name === app.id || name.startsWith(app.id + '-') || name.startsWith(app.id + '_');
  });
  const pinned = getPins().includes(app.id);
  const hasUpgradeList = _hasUpgrade(app);
  return `<div class="app-list-row" data-status="${app.status}" data-id="${app.id}">
    <span style="font-size:16px;width:24px;text-align:center;flex-shrink:0">${icon(app.id)}</span>
    <div style="flex:1;min-width:0">
      <div style="display:flex;align-items:center;gap:6px">
        <span style="font-size:10px;font-weight:700;color:var(--text1)">${escapeHtml(app.name || app.id)}</span>
        <span style="font-size:8px;color:var(--text3)">${escapeHtml(app.version || '')}</span>
        ${statusBadge(app.status)}
        ${hasUpgradeList ? `<span style="font-size:7px;font-weight:800;color:var(--warn);background:rgba(255,200,0,.12);padding:1px 4px;border-radius:3px;letter-spacing:.5px">UPDATE</span>` : ''}
      </div>
      ${ct && isRunning ? `<div style="font-size:8px;color:var(--text3);margin-top:2px">
        <i class="ti ti-cpu" style="font-size:8px"></i> ${escapeHtml(ct.cpu || '—')} &nbsp;
        <i class="ti ti-device-desktop" style="font-size:8px"></i> ${escapeHtml((ct.mem || '').split(' / ')[0] || '—')}
      </div>` : ''}
    </div>
    <div style="display:flex;align-items:center;gap:4px;flex-shrink:0">
      ${APP_PANELS[app.id] && isRunning ? `<button class="btn-sm" onclick="goSection('${APP_PANELS[app.id].panels[0]?.id || 'panel-'+app.id}')" title="Panel"><i class="ti ti-layout-sidebar-right" style="font-size:10px"></i></button>` : ''}
      ${domain ? `<a class="btn-sm" href="${domain}" target="_blank" rel="noopener" title="Ouvrir" style="text-decoration:none"><i class="ti ti-external-link" style="font-size:10px"></i></a>` : ''}
      <button class="btn-sm" onclick="openLogs('${app.id}')" title="Logs"><i class="ti ti-terminal-2" style="font-size:10px"></i></button>
      ${HARDCODED_PARAMS[app.id]?.length ? `<button class="btn-sm" onclick="openReconfigureModal('${app.id}')" title="Reconfigurer"><i class="ti ti-settings" style="font-size:10px"></i></button>` : ''}
      ${isRunning
        ? `<button class="btn-sm" onclick="openInspect('${app.id}')" title="Inspecter"><i class="ti ti-info-circle" style="font-size:10px"></i></button>
           <button class="btn-sm" onclick="appAction('${app.id}','restart')" title="Redémarrer"><i class="ti ti-refresh" style="font-size:10px"></i></button>
           <button class="btn-sm danger" onclick="appAction('${app.id}','stop')" title="Arrêter"><i class="ti ti-player-pause" style="font-size:10px"></i></button>`
        : `<button class="btn-sm success" onclick="appAction('${app.id}','start')" title="Démarrer"><i class="ti ti-player-play" style="font-size:10px"></i></button>`}
      <button class="btn-sm${pinned?' active':''}" onclick="togglePinApp('${app.id}')" title="${pinned?'Retirer':'Épingler'}">
        <i class="ti ti-star${pinned?'-filled':''}" style="font-size:10px"></i>
      </button>
    </div>
  </div>`;
}

function catalogCard(app) {
  const installed = S.apps.some(a => a.id === app.id);
  return `
    <div class="cat-card" data-cat="${escapeHtml(app.category || 'other')}">
      <div class="cat-top">
        <div class="app-icon" style="width:32px;height:32px;font-size:15px">${icon(app.id)}</div>
        <div>
          <div class="cat-name">${app.name || app.id}</div>
          <div class="cat-tag">${app.category?.toUpperCase() || 'APP'}</div>
        </div>
      </div>
      <div class="cat-desc">${app.description || ''}</div>
      ${installed
        ? `<button class="install-btn" style="color:var(--text3);cursor:default" disabled><i class="ti ti-check" style="font-size:10px"></i>INSTALLÉE</button>`
        : `<button class="install-btn" onclick="openInstallModal('${app.id}')"><i class="ti ti-plus" style="font-size:10px"></i>INSTALLER</button>`
      }
    </div>
  `;
}

function switchTab(tab) {
  S.tab = tab;
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  event.currentTarget.classList.add('active');
  document.getElementById('tab-installed')?.classList.toggle('hidden', tab !== 'installed');
  document.getElementById('tab-catalog')?.classList.toggle('hidden',   tab !== 'catalog');
  filterApps(S.appSearch);
}

function filterCatalogCat(btn, cat) {
  document.querySelectorAll('.cat-filter-btn').forEach(b => b.classList.toggle('active', b === btn));
  const grid = document.getElementById('catalog-grid');
  if (!grid) return;
  grid.querySelectorAll('.cat-card').forEach(card => {
    const cardCat = card.dataset.cat || '';
    card.style.display = (cat === 'all' || cardCat === cat) ? '' : 'none';
  });
  // Re-apply text search if any
  if (S.appSearch) filterApps(S.appSearch);
}

function filterApps(q) {
  S.appSearch = (q || '').trim().toLowerCase();
  const installedGrid = document.getElementById('installed-grid');
  const catalogGrid   = document.getElementById('catalog-grid');
  if (installedGrid) {
    installedGrid.querySelectorAll('.app-card, .app-list-row').forEach(card => {
      const text = card.textContent.toLowerCase();
      const sf = S.statusFilter || 'all';
      const statusMatch = sf === 'all' || (sf === 'running' ? card.dataset.status === 'running' : card.dataset.status !== 'running');
      card.style.display = (!S.appSearch || text.includes(S.appSearch)) && statusMatch ? '' : 'none';
    });
  }
  if (catalogGrid) {
    catalogGrid.querySelectorAll('.cat-card').forEach(card => {
      const text = card.textContent.toLowerCase();
      card.style.display = (!S.appSearch || text.includes(S.appSearch)) ? '' : 'none';
    });
  }
}

function applyStatusFilter(card, sf) {
  if (sf === 'all') return true;
  const isRunning = card.classList.contains('running');
  return sf === 'running' ? isRunning : !isRunning;
}

function filterAppStatus(status) {
  S.statusFilter = status;
  renderApps();
}

// ── Actions en lot ────────────────────────────────────────────────────────────
async function bulkAction(action) {
  const targets = action === 'start'
    ? S.apps.filter(a => a.status !== 'running')
    : S.apps.filter(a => a.status === 'running');
  if (targets.length === 0) { notify('Aucune app à ' + action, 'info'); return; }
  const status = document.getElementById('bulk-status');
  let done = 0;
  if (status) status.textContent = `0/${targets.length}...`;
  for (const app of targets) {
    await api.post(`/api/v1/apps/${app.id}/${action}`);
    done++;
    if (status) status.textContent = `${done}/${targets.length}...`;
  }
  if (status) status.textContent = `✓ ${done} apps`;
  notify(`Bulk ${action} — ${done} apps`, 'ok');
  setTimeout(() => { if (status) status.textContent = ''; loadApps(); }, 2000);
}

// ── Actions app ───────────────────────────────────────────────────────────────
async function appAction(id, action) {
  notify(`${action.toUpperCase()} ${id}...`, 'info');
  const r = await api.post(`/api/v1/apps/${id}/${action}`);
  if (r && (r.success !== false)) {
    notify(`${id} — ${action} OK`, 'ok');
    setTimeout(loadApps, 1500);
  } else {
    notify(r?.error || `Erreur ${action}`, 'err');
  }
}

async function startAllStopped() {
  const stopped = S.apps.filter(a => a.status !== 'running' && a.status !== 'installing');
  if (!stopped.length) { notify('Aucune app arrêtée', 'info'); return; }
  notify(`Démarrage de ${stopped.length} app(s)...`, 'info');
  await Promise.all(stopped.map(a => api.post(`/api/v1/apps/${a.id}/start`)));
  setTimeout(() => { loadApps(); loadDashboard(); }, 2000);
}

async function restartApp(id) {
  notify(`Redémarrage de ${id}...`, 'info');
  const r = await api.post(`/api/v1/apps/${id}/restart`);
  if (r && r.success !== false) {
    notify(`${id} — redémarré`, 'ok');
    setTimeout(() => { loadApps(); goSection(S.section); }, 2000);
  } else {
    notify(r?.error || 'Erreur restart', 'err');
  }
}

async function startApp(id) {
  await appAction(id, 'start');
}

async function stopApp(id) {
  if (!confirm(`Arrêter ${id} ?`)) return;
  await appAction(id, 'stop');
}

async function removeApp(id) {
  if (!confirm(`Supprimer ${id} ? Les données seront conservées (app-data/).`)) return;
  notify(`Suppression de ${id}...`, 'info');
  const r = await api.del(`/api/v1/apps/${id}?keep_data=true`);
  if (r && r.success !== false) {
    notify(`${id} supprimé`, 'ok');
    setTimeout(loadApps, 1000);
  } else {
    notify(r?.error || 'Erreur suppression', 'err');
  }
}

async function triggerAppBackup(id) {
  notify(`Sauvegarde de ${id}...`, 'info');
  const r = await api.post(`/api/v1/apps/${id}/backup`);
  if (r && r.success !== false) {
    notify(`${id} — sauvegarde créée`, 'ok');
  } else {
    notify(r?.error || 'Erreur backup', 'err');
  }
}

async function openReconfigureModal(appId) {
  S.installTarget = appId;
  const app = S.apps.find(a => a.id === appId);
  const params = HARDCODED_PARAMS[appId] || [];

  // Précharger les emplacements
  if (params.some(p => p.type === 'location') && S.locations.length === 0) {
    const locData = await api.get('/api/v1/locations');
    S.locations = Array.isArray(locData?.data) ? locData.data : [];
  }

  S.installParams = params;
  document.getElementById('modal-app-name').textContent = appId.toUpperCase() + ' — RECONFIGURER';
  const paramsEl = document.getElementById('modal-params');
  paramsEl.innerHTML = params.length === 0
    ? `<div style="font-size:10px;color:var(--text3)">Aucun paramètre reconfigurable pour cette app.</div>`
    : params.map(p => renderParamField(p)).join('');

  updateParamVisibility();
  document.getElementById('install-modal').classList.add('open');
  document.getElementById('install-modal').dataset.mode = 'reconfigure';
}

// ── Install modal ─────────────────────────────────────────────────────────────
async function openInstallModal(appId) {
  S.installTarget = appId;
  const info = S.catalog.find(a => a.id === appId) || {};

  // Charger les params depuis le store (endpoint individuel)
  try {
    const paramsData = await api.get(`/api/v1/store/${appId}`);
    S.installParams = Array.isArray(paramsData?.data) ? paramsData.data : [];
  } catch (_) {
    S.installParams = info.params || info.parameters || info.install_params
      || HARDCODED_PARAMS[appId] || [];
  }

  // Précharger les emplacements pour le type 'location'
  if (S.installParams.some(p => p.type === 'location') && S.locations.length === 0) {
    const locData = await api.get('/api/v1/locations');
    S.locations = Array.isArray(locData?.data) ? locData.data : [];
  }

  document.getElementById('modal-app-name').textContent = appId.toUpperCase();
  const paramsEl = document.getElementById('modal-params');
  paramsEl.innerHTML = S.installParams.length === 0
    ? `<div style="font-size:10px;color:var(--text3)">Aucun paramètre configurable — installation avec les valeurs par défaut.</div>`
    : S.installParams.map(p => renderParamField(p)).join('');

  updateParamVisibility();
  document.getElementById('install-modal').classList.add('open');
}

// Rendu d'un champ de paramètre dans le modal install/reconfigure.
function renderParamField(p) {
  let inner;
  if (p.type === 'info') {
    return `<div id="param-wrap-${p.id}" class="field full">
      <div style="padding:8px 12px;border-radius:6px;background:rgba(217,119,6,.08);border:1px solid var(--warn);font-size:9px;color:var(--text2);line-height:1.5">${p.description || ''}</div>
    </div>`;
  } else if (p.type === 'bool') {
    inner = `
      <div style="display:flex;gap:6px">
        <label style="display:flex;align-items:center;gap:6px;font-size:10px;color:var(--text2);cursor:pointer">
          <input type="radio" name="param-${p.id}" id="param-${p.id}-on" value="true" ${p.default !== 'false' ? 'checked' : ''} style="accent-color:var(--vio)" onclick="updateParamVisibility()">ACTIVÉ
        </label>
        <label style="display:flex;align-items:center;gap:6px;font-size:10px;color:var(--text2);cursor:pointer">
          <input type="radio" name="param-${p.id}" id="param-${p.id}-off" value="false" ${p.default === 'false' ? 'checked' : ''} style="accent-color:var(--vio)" onclick="updateParamVisibility()">DÉSACTIVÉ
        </label>
      </div>`;
  } else if (p.type === 'location') {
    const systemOpt = `<option value="${p.default}">SYSTÈME — ${p.default}</option>`;
    const locOpts = S.locations.map(l =>
      `<option value="${l.mount_point || l.path}">${escapeHtml(l.name)} — ${escapeHtml(l.mount_point || l.path)}</option>`
    ).join('');
    inner = `<select class="field-input" id="param-${p.id}" onchange="updateParamVisibility()">
        ${systemOpt}${locOpts}
      </select>
      <div style="font-size:9px;color:var(--text3);margin-top:2px">${p.description || ''} — <span style="color:var(--blue)">Emplacements dans EMPLACEMENTS</span></div>`;
  } else if (p.type === 'select' && p.options) {
    inner = `<select class="field-input" id="param-${p.id}" onchange="updateParamVisibility()">
        ${p.options.map(o => `<option value="${o}" ${o === p.default ? 'selected' : ''}>${o}</option>`).join('')}
      </select>`;
  } else {
    inner = `<input class="field-input" id="param-${p.id}" type="${p.type === 'secret' ? 'password' : 'text'}"
      placeholder="${p.description || ''}" value="${p.default || ''}" />`;
  }
  const desc = (p.type !== 'location' && p.description) ? `<div style="font-size:9px;color:var(--text3);margin-top:2px">${p.description}</div>` : '';
  return `<div id="param-wrap-${p.id}" class="field full">
    <div class="field-label">${escapeHtml(p.label).toUpperCase()}${p.required ? ' *' : ''}</div>
    ${inner}${desc}
  </div>`;
}

// ── Visibilité conditionnelle des params (depends_on) ─────────────────────────
function updateParamVisibility() {
  S.installParams.forEach(p => {
    if (!p.depends_on) return;
    const conds = Array.isArray(p.depends_on) ? p.depends_on : [p.depends_on];
    const visible = conds.every(cond => {
      const dep = S.installParams.find(d => d.id === cond.param);
      if (!dep) return false;
      let val;
      if (dep.type === 'bool') {
        const el = document.getElementById(`param-${dep.id}-on`);
        val = el ? (el.checked ? 'true' : 'false') : (dep.default || 'false');
      } else {
        const el = document.getElementById(`param-${dep.id}`);
        val = el ? el.value : (dep.default || '');
      }
      return cond.values.includes(val);
    });
    const wrap = document.getElementById(`param-wrap-${p.id}`);
    if (wrap) wrap.style.display = visible ? '' : 'none';
  });
}

async function confirmInstall() {
  const params = {};
  S.installParams.forEach(p => {
    const wrap = document.getElementById(`param-wrap-${p.id}`);
    if (wrap && wrap.style.display === 'none') return;
    if (p.type === 'bool') {
      const on = document.getElementById(`param-${p.id}-on`);
      if (on) params[p.id] = on.checked ? 'true' : 'false';
    } else {
      const el = document.getElementById(`param-${p.id}`);
      if (el) params[p.id] = el.value;
    }
  });

  const modal = document.getElementById('install-modal');
  const mode = modal.dataset.mode || 'install';
  modal.classList.remove('open');
  delete modal.dataset.mode;

  const appLabel = S.catalog.find(a => a.id === S.installTarget)?.name || S.installTarget;

  if (mode === 'reconfigure') {
    const taskId = taskAdd('install', S.installTarget, `Reconfiguration — ${appLabel}`);
    const r = await api.post(`/api/v1/apps/${S.installTarget}/reconfigure`, { params });
    if (r && r.success !== false) {
      taskDone(taskId, 'Reconfiguration terminée', true);
      notify(`${S.installTarget} — reconfiguration terminée`, 'ok');
      setTimeout(loadApps, 1000);
    } else {
      taskDone(taskId, r?.error || 'Erreur inconnue', false);
      notify(r?.error || 'Erreur reconfiguration', 'err');
    }
    return;
  }

  const taskId = taskAdd('install', S.installTarget, `Installation — ${appLabel}`);
  const r = await api.post(`/api/v1/apps/${S.installTarget}/install`, { params, async: true });
  if (r && r.success !== false) {
    taskDone(taskId, 'Installation terminée', true);
    notify(`${S.installTarget} — installation terminée`, 'ok');
    S.tab = 'installed';
    goSection('apps');
    setTimeout(loadApps, 1000);
    // Afficher les notes post-install si disponibles
    showPostInstallNotes(S.installTarget, appLabel);
  } else {
    taskDone(taskId, r?.error || 'Erreur inconnue', false);
    notify(r?.error || 'Erreur installation', 'err');
  }
}

async function showPostInstallNotes(appId, appLabel) {
  try {
    const r = await fetch(`/sys/app-notes/${encodeURIComponent(appId)}`);
    if (!r.ok) return;
    const d = await r.json();
    if (!d.found || !d.notes?.trim()) return;
    const modal = document.getElementById('post-install-modal');
    if (!modal) return;
    document.getElementById('post-install-title').textContent = appLabel + ' — Notes d\'installation';
    document.getElementById('post-install-body').textContent = d.notes;
    modal.classList.add('open');
  } catch(e) {}
}

// ── SECTION: LOGS ─────────────────────────────────────────────────────────────
function openLogs(appId) {
  S.logApp = appId;
  goSection('logs');
}

async function loadLogs() {
  if (!S.apps.length) await loadApps();

  const select = document.getElementById('log-select');
  if (select && S.apps.length) {
    select.innerHTML = S.apps.map(a =>
      `<option value="${a.id}" ${a.id === S.logApp ? 'selected' : ''}>${a.name || a.id}</option>`
    ).join('');
  }

  const appId = S.logApp || S.apps[0]?.id;
  if (!appId) return;

  stopLogStream();
  clearLogs();

  // Badge LIVE
  const badge = document.getElementById('log-live-badge');
  if (badge) badge.style.display = 'none';

  // SSE streaming temps réel
  const sse = new EventSource(`/api/v1/apps/${appId}/logs/stream?tail=200`);
  S.logStream = sse;

  sse.onopen = () => {
    if (badge) badge.style.display = '';
  };
  sse.onmessage = (e) => {
    if (e.data) appendLog(e.data);
  };
  sse.addEventListener('close', () => {
    stopLogStream();
    if (badge) badge.style.display = 'none';
    appendLog('[— Fin du stream —]');
    appendLogCursor();
  });
  sse.onerror = () => {
    // SSE non supporté ou coupé — fallback one-shot
    stopLogStream();
    if (badge) badge.style.display = 'none';
    api.get(`/api/v1/apps/${appId}/logs?tail=500`).then(data => {
      const raw = data?.data?.logs ?? data?.data?.lines ?? data?.lines ?? data?.data ?? data;
      const lines = Array.isArray(raw) ? raw : (typeof raw === 'string' ? raw.split('\n') : null);
      if (lines?.length) lines.filter(Boolean).forEach(l => appendLog(l));
      else { appendLog(`[Aucun log disponible pour ${appId}]`); }
      appendLogCursor();
    });
  };
}

function clearLogs() {
  const body = document.getElementById('log-body');
  if (body) body.innerHTML = '';
}

function filterLogLevel(level) {
  S._logLevelFilter = level;
  _applyLogFilters();
}

function filterLogText(q) {
  S._logTextFilter = q.trim().toLowerCase();
  _applyLogFilters();
}

function _applyLogFilters() {
  const body = document.getElementById('log-body');
  if (!body) return;
  const level = S._logLevelFilter || '';
  const q = S._logTextFilter || '';
  body.querySelectorAll('.log-line').forEach(line => {
    const text = line.textContent.toLowerCase();
    const levelOk = !level ||
      (level === 'error' && (text.includes('error') || text.includes('err') || line.querySelector('.log-err'))) ||
      (level === 'warn'  && (text.includes('warn')  || line.querySelector('.log-warn'))) ||
      (level === 'info'  && (text.includes('info')  || line.querySelector('.log-step') || line.querySelector('.log-ok')));
    const textOk = !q || text.includes(q);
    line.style.display = (levelOk && textOk) ? '' : 'none';
  });
}

function toggleLogWrap() {
  const body = document.getElementById('log-body');
  const btn = document.getElementById('log-wrap-btn');
  if (!body) return;
  const isWrap = body.style.whiteSpace === 'pre-wrap';
  body.style.whiteSpace = isWrap ? 'pre' : 'pre-wrap';
  body.style.overflowX = isWrap ? 'auto' : 'hidden';
  if (btn) btn.style.background = isWrap ? '' : 'var(--bg3)';
}

function appendLog(line) {
  const body = document.getElementById('log-body');
  if (!body) return;

  // Retirer le curseur s'il existe
  body.querySelector('.log-cursor')?.parentElement?.remove();

  const div = document.createElement('div');
  div.className = 'log-line';

  // Détection simple de sévérité
  let tagClass = 'log-info', tag = '  INFO ';
  if (/\[install\]|INSTALL/i.test(line))   { tagClass = 'log-step'; tag = '[INSTALL]'; }
  if (/\[✓\]|success|SUCCESS/i.test(line)) { tagClass = 'log-ok';   tag = '  [✓]  '; }
  if (/error|ERROR|fatal|FATAL/i.test(line)){ tagClass = 'log-err';  tag = ' [ERR] '; }

  const ts = new Date().toLocaleTimeString('fr-FR', { hour12: false });
  div.innerHTML = `
    <span class="log-ts">${ts}</span>
    <span class="log-tag ${tagClass}">${tag}</span>
    <span class="log-txt">${escapeHtml(line)}</span>
  `;
  body.appendChild(div);
  body.scrollTop = body.scrollHeight;
}

function appendLogCursor() {
  const body = document.getElementById('log-body');
  if (!body) return;
  const div = document.createElement('div');
  div.className = 'log-line';
  div.innerHTML = '<span class="log-cursor"></span>';
  body.appendChild(div);
  body.scrollTop = body.scrollHeight;
}

function stopLogStream() {
  if (S.logStream) { S.logStream.close(); S.logStream = null; }
}

function changeLogApp() {
  const select = document.getElementById('log-select');
  if (select) { S.logApp = select.value; loadLogs(); }
}

function exportLogs() {
  const body = document.getElementById('log-body');
  if (!body) return;
  const lines = Array.from(body.querySelectorAll('.log-line')).map(el => el.textContent.trim()).join('\n');
  if (!lines) { notify('Aucun log à exporter', 'info'); return; }
  const blob = new Blob([lines], { type: 'text/plain' });
  const url  = URL.createObjectURL(blob);
  const a    = document.createElement('a');
  a.href     = url;
  a.download = `caleope-logs-${S.logApp || 'app'}-${new Date().toISOString().slice(0,19).replace(/[T:]/g,'-')}.txt`;
  a.click();
  URL.revokeObjectURL(url);
}

// ── SECTION: BACKUPS ──────────────────────────────────────────────────────────
async function loadBackups() {
  const c = document.getElementById('content-backups');
  if (!c) return;

  if (!S.apps.length) await loadApps();
  if (!S.apps.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-archive"></i></div><div class="empty-title">AUCUNE APP INSTALLÉE</div><div class="empty-sub">Installez au moins une application.</div></div>`;
    return;
  }

  S.backupApp = S.backupApp || S.apps[0]?.id;

  // Charger les tâches planifiées pour afficher la prochaine sauvegarde
  let nextBackupHtml = '';
  try {
    const tasksData = await api.get('/api/v1/tasks');
    const backupTasks = (tasksData?.data || []).filter(t => t.type === 'backup' && t.enabled);
    if (backupTasks.length) {
      const nextRuns = backupTasks.map(t => ({ t, next: computeNextRun(t) })).filter(x => x.next).sort((a, b) => a.next - b.next);
      if (nextRuns.length) {
        const { t, next } = nextRuns[0];
        const label = TASK_SCOPE_LABELS[t.scope] || 'Complète';
        nextBackupHtml = `
          <div style="display:flex;align-items:center;gap:8px;padding:8px 12px;background:var(--card);border:1px solid var(--border);border-left:3px solid var(--accent);border-radius:6px;margin-bottom:12px">
            <i class="ti ti-clock-play" style="color:var(--accent);font-size:14px;flex-shrink:0"></i>
            <div style="flex:1">
              <div style="font-size:9px;font-weight:700">PROCHAINE SAUVEGARDE PLANIFIÉE</div>
              <div style="font-size:8px;color:var(--text3)">${escapeHtml(label)} — ${next.toLocaleString('fr-FR', {weekday:'long',hour:'2-digit',minute:'2-digit'})} <span style="color:var(--accent)">(${formatRelTime(next)})</span></div>
            </div>
            <button class="btn-sm" onclick="goSection('tasks')" style="font-size:8px;flex-shrink:0"><i class="ti ti-settings" style="font-size:9px"></i></button>
          </div>`;
      }
    }
  } catch(e) {}

  const runningApps = S.apps.filter(a => a.status === 'running');
  c.innerHTML = `
    ${nextBackupHtml}
    <div style="display:flex;align-items:center;gap:8px;margin-bottom:14px;flex-wrap:wrap">
      <span style="font-size:9px;color:var(--text3);letter-spacing:.5px">AFFICHER LES SAUVEGARDES DE</span>
      <select class="log-select" id="backup-select" onchange="changeBackupApp()" style="flex:0;min-width:160px">
        ${S.apps.map(a => `<option value="${a.id}" ${a.id === S.backupApp ? 'selected' : ''}>${a.name || a.id}</option>`).join('')}
      </select>
      <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
        <button class="btn-sm" id="backup-all-btn" onclick="backupAll()" title="Sauvegarder toutes les apps actives">
          <i class="ti ti-stack"></i>TOUT SAUVEGARDER <span style="opacity:.6">(${runningApps.length})</span>
        </button>
      </div>
    </div>
    <div id="backup-all-progress" style="display:none;margin-bottom:12px"></div>
    <div id="backup-list"></div>
  `;

  await refreshBackupList();
}

async function refreshBackupList() {
  const appId = document.getElementById('backup-select')?.value || S.backupApp;
  if (!appId) return;
  const list = document.getElementById('backup-list');
  if (!list) return;

  list.innerHTML = `<div class="empty-state" style="padding-top:30px"><div class="empty-icon"><i class="ti ti-loader" style="animation:spin .8s linear infinite"></i></div><div class="empty-title" style="font-size:10px">CHARGEMENT...</div></div>`;

  const data = await api.get(`/api/v1/apps/${appId}/backups`);
  const backups = data?.data || [];

  if (!backups.length) {
    list.innerHTML = `<div class="empty-state" style="padding-top:30px"><div class="empty-icon"><i class="ti ti-archive"></i></div><div class="empty-title">AUCUNE SAUVEGARDE</div><div class="empty-sub">Cliquez sur SAUVEGARDER pour créer la première.</div></div>`;
    return;
  }

  list.innerHTML = `<div class="backup-list">${backups.map(b => backupRow(appId, b)).join('')}</div>`;
}

function backupRow(appId, b) {
  // BackupManifest fields: app, app_name, timestamp (ISO), dir (nom répertoire = id stable)
  const dir = b.dir || b.id || b.name || '';
  const name = b.app_name || b.app || b.name || b.id || 'Sauvegarde';
  const size = b.size_human || (b.size_bytes ? (b.size_bytes / 1024 / 1024).toFixed(1) + ' Mo' : '—');
  const date = b.timestamp ? new Date(b.timestamp).toLocaleString('fr-FR') : (b.created_at || b.date || '—');
  const flags = [b.has_data && 'DATA', b.has_config && 'CFG'].filter(Boolean).join(' · ');
  return `
    <div class="backup-row">
      <div class="backup-info">
        <div class="backup-name"><i class="ti ti-file-zip" style="font-size:10px;margin-right:4px;color:var(--text3)"></i>${escapeHtml(dir || name)}</div>
        <div class="backup-meta">${escapeHtml(size)} · ${escapeHtml(date)}${flags ? ' · ' + flags : ''}</div>
      </div>
      <div class="backup-actions">
        <button class="btn-sm" onclick="restoreBackup('${escapeHtml(appId)}', '${escapeHtml(dir)}')">
          <i class="ti ti-restore"></i>RESTAURER
        </button>
        <button class="btn-sm danger" onclick="deleteBackup('${escapeHtml(appId)}', '${escapeHtml(dir)}')">
          <i class="ti ti-trash"></i>
        </button>
      </div>
    </div>
  `;
}

function triggerBackup() {
  openBackupModal();
}

function openBackupModal() {
  if (!S.apps.length) { notify('Aucune application installée', 'err'); return; }
  const sel = document.getElementById('backup-modal-app');
  if (sel) {
    sel.innerHTML = S.apps.map(a =>
      `<option value="${a.id}">${icon(a.id)} ${a.name || a.id}${a.status === 'running' ? ' ●' : ''}</option>`
    ).join('');
    // Présélectionner l'app couramment vue dans la section backups
    if (S.backupApp) sel.value = S.backupApp;
  }
  document.getElementById('backup-modal').classList.add('open');
}

async function backupAll() {
  const btn = document.getElementById('backup-all-btn');
  const progress = document.getElementById('backup-all-progress');
  if (!btn || !progress) return;

  const targets = S.apps.filter(a => a.status === 'running');
  if (!targets.length) { notify('Aucune app active à sauvegarder', 'warn'); return; }

  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span>EN COURS...';
  progress.style.display = 'block';

  let done = 0, errors = 0;
  const results = [];

  progress.innerHTML = `<div style="font-size:10px;color:var(--text3);margin-bottom:6px">SAUVEGARDE DE ${targets.length} APP${targets.length>1?'S':''}…</div>
    <div id="backup-all-rows" style="display:flex;flex-direction:column;gap:4px"></div>`;

  const rowsEl = document.getElementById('backup-all-rows');

  for (const app of targets) {
    const rowId = `bar-${app.id}`;
    if (rowsEl) rowsEl.insertAdjacentHTML('beforeend', `
      <div id="${rowId}" style="display:flex;align-items:center;gap:8px;font-size:10px">
        <span class="spinner" style="width:10px;height:10px;border-width:1.5px"></span>
        <span style="color:var(--text2)">${escapeHtml(app.name || app.id)}</span>
      </div>`);

    const res = await api.post(`/api/v1/apps/${app.id}/backups`, {});
    const row = document.getElementById(rowId);
    if (res && !res.error) {
      done++;
      if (row) row.innerHTML = `<i class="ti ti-check" style="color:var(--ok);font-size:10px"></i><span style="color:var(--text2)">${escapeHtml(app.name || app.id)}</span>`;
    } else {
      errors++;
      if (row) row.innerHTML = `<i class="ti ti-x" style="color:var(--err);font-size:10px"></i><span style="color:var(--text3)">${escapeHtml(app.name || app.id)}</span>`;
    }
  }

  btn.disabled = false;
  btn.innerHTML = `<i class="ti ti-stack"></i>TOUT SAUVEGARDER <span style="opacity:.6">(${targets.length})</span>`;

  const summary = errors
    ? `${done}/${targets.length} sauvegardes réussies (${errors} erreur${errors>1?'s':''})`
    : `${done} sauvegarde${done>1?'s':''} créée${done>1?'s':''}`;
  notify(summary, errors ? 'warn' : 'ok');

  if (done > 0) setTimeout(refreshBackupList, 600);
}

async function confirmBackupModal() {
  const appId = document.getElementById('backup-modal-app')?.value;
  if (!appId) return;
  document.getElementById('backup-modal').classList.remove('open');
  const appName = S.apps.find(a => a.id === appId)?.name || appId;
  const taskId = taskAdd('backup', appId, `Sauvegarde — ${appName}`);
  const r = await api.post(`/api/v1/apps/${appId}/backups`);
  if (r && r.success !== false) {
    taskDone(taskId, r?.data?.message || r?.data?.backup_dir || 'Terminé', true);
    notify(`${appId} — sauvegarde terminée`, 'ok');
    S.backupApp = appId;
    if (S.section === 'backups') setTimeout(refreshBackupList, 1000);
  } else if (r?.error?.includes('route non trouvée') || r?.error?.includes('404')) {
    taskDone(taskId, 'Endpoint non disponible sur ce daemon', false);
    notify(`Daemon v${S.stats.version || '?'} — sauvegardes manuelles pas encore disponibles`, 'err');
  } else {
    taskDone(taskId, r?.error || 'Erreur inconnue', false);
    notify(r?.error || 'Erreur lors de la sauvegarde', 'err');
  }
}

async function restoreBackup(appId, backupId) {
  if (!confirm(`Restaurer "${backupId}" pour ${appId} ?\nL'application sera arrêtée pendant la restauration.`)) return;
  const appName = S.apps.find(a => a.id === appId)?.name || appId;
  const taskId = taskAdd('restore', appId, `Restauration — ${appName}`);
  const r = await api.post(`/api/v1/apps/${appId}/backups/${encodeURIComponent(backupId)}/restore`);
  if (r && r.success !== false) {
    taskDone(taskId, 'Restauration terminée', true);
    notify(`${appId} — restauration terminée`, 'ok');
    if (S.section === 'backups') setTimeout(refreshBackupList, 1000);
  } else {
    taskDone(taskId, r?.error || 'Erreur inconnue', false);
    notify(r?.error || 'Erreur lors de la restauration', 'err');
  }
}

async function deleteBackup(appId, dir) {
  if (!confirm(`Supprimer le backup "${dir}" de ${appId} ?\nCette action est irréversible.`)) return;
  const r = await api.del(`/api/v1/apps/${appId}/backups/${encodeURIComponent(dir)}`);
  if (r && r.success !== false) {
    notify(`Backup "${dir}" supprimé`, 'ok');
    refreshBackupList();
  } else {
    notify(r?.error || 'Erreur lors de la suppression', 'err');
  }
}

function changeBackupApp() {
  S.backupApp = document.getElementById('backup-select')?.value;
  refreshBackupList();
}

// ── File de tâches (style Proxmox) ────────────────────────────────────────────

let _taskTimer = null;
function taskAdd(type, appId, label) {
  const id = ++S.taskSeq;
  S.tasks.unshift({ id, type, appId, label, status: 'running', detail: '', startedAt: Date.now() });
  renderTaskQueue();
  // Rafraîchit le timer chaque seconde tant qu'une tâche tourne
  if (!_taskTimer) {
    _taskTimer = setInterval(() => {
      if (S.tasks.some(t => t.status === 'running')) {
        renderTaskQueue();
      } else {
        clearInterval(_taskTimer);
        _taskTimer = null;
      }
    }, 1000);
  }
  return id;
}

function taskDone(taskId, detail = '', ok = true) {
  const t = S.tasks.find(t => t.id === taskId);
  if (!t) return;
  t.status = ok ? 'done' : 'error';
  t.detail = detail;
  t.endedAt = Date.now();
  renderTaskQueue();
  // Nettoyer les tâches terminées après 12s
  setTimeout(() => {
    S.tasks = S.tasks.filter(t => t.id !== taskId);
    renderTaskQueue();
  }, 12000);
}

function renderTaskQueue() {
  let el = document.getElementById('task-queue');
  if (!el) {
    el = document.createElement('div');
    el.id = 'task-queue';
    el.style.cssText = 'position:fixed;bottom:0;left:200px;right:0;background:var(--bg2);border-top:1px solid var(--border2);z-index:200;max-height:180px;overflow-y:auto';
    document.getElementById('app')?.appendChild(el);
  }
  if (!S.tasks.length) { el.style.display = 'none'; return; }
  el.style.display = 'block';
  el.innerHTML = S.tasks.map(t => {
    const elapsed = t.endedAt ? ((t.endedAt - t.startedAt) / 1000).toFixed(1) + 's' : elapsedSec(t.startedAt);
    const isRun = t.status === 'running';
    const isErr = t.status === 'error';
    const iconCls = isRun ? 'ti-loader-2 spinning' : isErr ? 'ti-alert-circle' : 'ti-check';
    const barHtml = isRun
      ? `<div style="display:flex;gap:1px;margin-top:4px">${Array.from({length:20},(_,i)=>`<div class="seg task-seg-run" style="animation-delay:${i*60}ms"></div>`).join('')}</div>`
      : `<div style="display:flex;gap:1px;margin-top:4px">${Array.from({length:20},()=>`<div class="seg ${isErr?'seg-err':'on'}"></div>`).join('')}</div>`;
    return `<div class="task-row" onclick="toggleTaskDetail(${t.id})" style="padding:8px 16px;cursor:pointer;border-bottom:1px solid var(--border1);display:flex;align-items:flex-start;gap:10px">
      <i class="ti ${iconCls}" style="font-size:12px;margin-top:2px;flex-shrink:0;color:${isErr?'var(--err)':isRun?'var(--text2)':'var(--ok)'}"></i>
      <div style="flex:1;min-width:0">
        <div style="display:flex;align-items:center;gap:8px">
          <span style="font-size:10px;font-weight:700;letter-spacing:.5px">${escapeHtml(t.label)}</span>
          <span style="font-size:9px;color:var(--text3)">${escapeHtml(elapsed)}</span>
          ${t.status !== 'running' ? `<span style="font-size:9px;color:${isErr?'var(--err)':'var(--ok)'}">${isErr?'ERREUR':'OK'}</span>` : ''}
        </div>
        ${barHtml}
        <div id="task-detail-${t.id}" style="display:none;font-size:9px;color:var(--text3);margin-top:4px;font-family:monospace">${escapeHtml(t.detail||'')}</div>
      </div>
    </div>`;
  }).join('');
}

function toggleTaskDetail(taskId) {
  const el = document.getElementById(`task-detail-${taskId}`);
  if (el) el.style.display = el.style.display === 'none' ? 'block' : 'none';
}

function elapsedSec(startMs) {
  return ((Date.now() - startMs) / 1000).toFixed(1) + 's';
}

// ── SECTION: SECRETS ──────────────────────────────────────────────────────────
async function loadSecrets() {
  const c = document.getElementById('content-secrets');
  if (!c) return;

  // Récupérer la liste des apps avec secrets (metadata, sans valeurs)
  const listData = await api.get('/api/v1/secrets');
  const apps = listData?.data || [];

  if (!apps.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-lock"></i></div>
      <div class="empty-title">AUCUN SECRET</div>
      <div class="empty-sub">Aucune application installée avec des secrets configurés.</div></div>`;
    return;
  }

  const unlocked = sessionStorage.getItem('secrets_unlocked') === '1';
  // Si secrets_unlocked=1 mais pas de valeurs en session → reset (reload de page)
  const secretVarsRaw = sessionStorage.getItem('secrets_vars');
  const secretVars = (unlocked && secretVarsRaw) ? JSON.parse(secretVarsRaw) : null;
  const effectivelyUnlocked = unlocked && secretVars !== null;

  const appListHtml = apps.map((a, idx) => {
    const appVars = secretVars?.find(s => s.app_id === a.app_id)?.vars || null;
    const bodyId = `secret-body-${a.app_id}`;
    const varsHtml = appVars
      ? Object.entries(appVars).map(([k, v]) => `
          <div class="setting-row" style="font-family:monospace;font-size:9px;align-items:center">
            <span style="color:var(--text2)">${escapeHtml(k)}</span>
            <div style="display:flex;align-items:center;gap:4px">
              <span class="setting-val" style="font-size:9px;word-break:break-all">${escapeHtml(v)}</span>
              <button class="btn-sm" title="Copier" style="padding:2px 5px;flex-shrink:0"
                onclick="navigator.clipboard.writeText(${JSON.stringify(v)}).then(()=>notify('Copié','ok'))">
                <i class="ti ti-copy" style="font-size:9px"></i>
              </button>
            </div>
          </div>`).join('')
      : `<div style="font-size:9px;color:var(--text3);padding:4px 0">Saisir le mot de passe maître pour afficher les valeurs.</div>`;
    return `
      <div class="settings-card" style="padding:0">
        <button onclick="toggleSecretApp('${escapeHtml(a.app_id)}')"
          style="width:100%;background:none;border:none;cursor:pointer;padding:10px 12px;display:flex;align-items:center;gap:6px;color:inherit;text-align:left">
          ${icon(a.app_id)} <span style="font-size:10px;font-weight:700;letter-spacing:1px">${escapeHtml(a.app_name || a.app_id)}</span>
          <span style="margin-left:auto;font-size:9px;color:var(--text3)">${a.key_count} VARIABLE${a.key_count > 1 ? 'S' : ''}</span>
          ${a.encrypted ? (effectivelyUnlocked
            ? '<span class="badge badge-ok"  style="font-size:8px;margin-left:4px"><i class="ti ti-lock-open"  style="font-size:9px"></i>&nbsp;EN CLAIR</span>'
            : '<span class="badge badge-warn" style="font-size:8px;margin-left:4px"><i class="ti ti-lock"       style="font-size:9px"></i>&nbsp;CHIFFRÉ</span>') : ''}
          <i class="ti ti-chevron-right" id="secret-chevron-${escapeHtml(a.app_id)}" style="font-size:10px;margin-left:4px;transition:transform .15s;color:var(--text3)"></i>
        </button>
        <div id="${bodyId}" style="display:none;padding:0 12px 10px">${varsHtml}</div>
      </div>`;
  }).join('');

  const lockBar = effectivelyUnlocked ? `
    <div class="secret-unlocked-bar">
      <i class="ti ti-shield-check" style="font-size:14px;flex-shrink:0"></i>
      DÉVERROUILLÉ — VALEURS EN CLAIR
      <button class="btn-sm danger" onclick="lockSecrets()" style="margin-left:auto">
        <i class="ti ti-lock"></i>VERROUILLER
      </button>
    </div>` : `
    <div class="secret-alert">
      <i class="ti ti-shield-lock" style="font-size:14px;flex-shrink:0" aria-hidden="true"></i>
      SAISIR LE MOT DE PASSE MAÎTRE POUR CONSULTER LES VALEURS
    </div>
    <div class="settings-card">
      <div class="settings-title">DÉVERROUILLAGE</div>
      <div style="display:flex;gap:8px;align-items:center">
        <input class="field-input" id="secrets-pw" type="password"
          placeholder="Mot de passe maître" style="flex:1"
          onkeydown="if(event.key==='Enter')unlockSecrets()" />
        <button class="btn btn-vio" onclick="unlockSecrets()" style="flex:0;min-width:140px">
          <i class="ti ti-lock-open"></i>DÉVERROUILLER
        </button>
      </div>
    </div>`;

  c.innerHTML = lockBar + appListHtml;
  if (!effectivelyUnlocked) setTimeout(() => document.getElementById('secrets-pw')?.focus(), 50);
}

async function unlockSecrets() {
  if (S.section !== 'secrets') { goSection('secrets'); return; }
  const pw = document.getElementById('secrets-pw')?.value;
  if (!pw) { notify('Saisissez un mot de passe maître', 'err'); return; }

  const btn = document.querySelector('#content-secrets .btn-vio');
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="spinner"></span>&nbsp;DÉCHIFFREMENT...'; }

  const r = await api.post('/api/v1/secrets', { password: pw });

  if (btn) { btn.disabled = false; btn.innerHTML = '<i class="ti ti-lock-open"></i>DÉVERROUILLER'; }

  if (r && r.success !== false) {
    sessionStorage.setItem('secrets_unlocked', '1');
    sessionStorage.setItem('secrets_vars', JSON.stringify(r.data || []));
    notify('Secrets déverrouillés', 'ok');
    loadSecrets();
  } else {
    notify(r?.error || 'Mot de passe incorrect', 'err');
  }
}

function lockSecrets() {
  sessionStorage.removeItem('secrets_unlocked');
  sessionStorage.removeItem('secrets_vars');
  notify('Secrets verrouillés', 'info');
  loadSecrets();
}

function toggleSecretApp(appId) {
  const body = document.getElementById(`secret-body-${appId}`);
  const chevron = document.getElementById(`secret-chevron-${appId}`);
  if (!body) return;
  const open = body.style.display !== 'none';
  body.style.display = open ? 'none' : 'block';
  if (chevron) chevron.style.transform = open ? '' : 'rotate(90deg)';
}

// ── SECTION: LOCATIONS ────────────────────────────────────────────────────────
async function loadLocations() {
  const c = document.getElementById('content-locations');
  if (!c) return;

  const data = await api.get('/api/v1/locations');
  const locations = Array.isArray(data?.data) ? data.data : [];

  if (!locations.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-network"></i></div><div class="empty-title">AUCUN EMPLACEMENT</div><div class="empty-sub">Ajoutez un partage NFS ou SMB comme stockage réseau.</div></div>`;
    return;
  }

  c.innerHTML = `<div class="loc-list">${locations.map(locRow).join('')}</div>`;
}

function locRow(loc) {
  return `
    <div class="loc-row">
      <span class="loc-type-badge">${(loc.type || 'NFS').toUpperCase()}</span>
      <div class="loc-info">
        <div class="loc-name">${escapeHtml(loc.name || loc.id || '—')}</div>
        <div class="loc-meta">${escapeHtml(loc.host || '')}:${escapeHtml(loc.path || '')} → ${escapeHtml(loc.mount_point || '—')}</div>
      </div>
      <div class="backup-actions">
        <button class="btn-sm danger" onclick="removeLocation('${escapeHtml(loc.id || loc.name)}')">
          <i class="ti ti-trash"></i>SUPPRIMER
        </button>
      </div>
    </div>
  `;
}

function openAddLocationModal() {
  ['loc-name','loc-host','loc-path','loc-mount','loc-user','loc-pass'].forEach(id => {
    const el = document.getElementById(id);
    if (el) el.value = '';
  });
  document.getElementById('location-modal').classList.add('open');
  updateLocTypeFields();
}

function updateLocTypeFields() {
  const type = document.getElementById('loc-type')?.value;
  const creds = document.getElementById('loc-smb-creds');
  if (creds) creds.style.display = (type === 'smb') ? 'grid' : 'none';
  // Proposer un mount point par défaut basé sur le nom
  const name = document.getElementById('loc-name')?.value;
  const mount = document.getElementById('loc-mount');
  if (mount && !mount.value && name) mount.value = `/mnt/${name}`;
}

function autoMountPoint() {
  const name = document.getElementById('loc-name')?.value?.trim();
  const mount = document.getElementById('loc-mount');
  if (mount && name && !mount._userEdited) mount.value = `/mnt/${name}`;
}

async function saveLocation() {
  const name  = document.getElementById('loc-name')?.value.trim();
  const type  = document.getElementById('loc-type')?.value;
  const host  = document.getElementById('loc-host')?.value.trim();
  const path  = document.getElementById('loc-path')?.value.trim();
  const mount = document.getElementById('loc-mount')?.value.trim() || `/mnt/${name}`;
  const user  = document.getElementById('loc-user')?.value.trim();
  const pass  = document.getElementById('loc-pass')?.value;

  if (!name || !host || !path) { notify('Nom, hôte et chemin sont requis', 'err'); return; }
  if (type === 'smb' && !user) { notify('Identifiant SMB requis', 'err'); return; }

  // Test de connectivité avant d'enregistrer
  const addBtn = document.querySelector('#location-modal .btn-vio');
  if (addBtn) { addBtn.disabled = true; addBtn.innerHTML = '<span class="spinner"></span>&nbsp;TEST...'; }
  notify(`Test de connectivité vers ${host}...`, 'info');

  const testR = await fetch('/ui/location/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type, host }),
  }).then(r => r.json()).catch(() => ({ reachable: false, error: 'Timeout' }));

  if (addBtn) { addBtn.disabled = false; addBtn.innerHTML = '<i class="ti ti-plus"></i>AJOUTER'; }

  if (!testR.reachable) {
    notify(`${host} injoignable (port ${type === 'smb' ? 445 : 2049}) — ${testR.error || 'hôte inaccessible'}`, 'err');
    return;
  }
  notify(`${host} accessible (${testR.latency_ms}ms)`, 'ok');

  const payload = { name, type, host, path, mount_point: mount };
  if (type === 'smb') { payload.username = user; payload.password = pass; }

  const r = await api.post('/api/v1/locations', payload);
  if (r && r.success !== false) {
    document.getElementById('location-modal').classList.remove('open');
    notify(`Emplacement "${name}" ajouté (${mount})`, 'ok');
    loadLocations();
  } else {
    notify(r?.error || 'Erreur lors de l\'ajout de l\'emplacement', 'err');
  }
}

async function removeLocation(id) {
  if (!confirm(`Supprimer l'emplacement "${id}" ?`)) return;
  const r = await api.del(`/api/v1/locations/${id}`);
  if (r && r.success !== false) {
    notify(`Emplacement supprimé`, 'ok');
    loadLocations();
  } else {
    notify(r?.error || 'Erreur lors de la suppression', 'err');
  }
}

// ── SECTION: STATS (dashboard) ────────────────────────────────────────────────
async function loadStats() {
  const [statsResp, sysResp, ctResp] = await Promise.all([
    api.get('/api/v1/stats?disk=true'),
    api.get('/api/v1/system'),
    api.get('/api/v1/containers'),
  ]);
  S.stats      = statsResp?.data || {};
  S.sysinfo    = sysResp?.data   || {};
  S.containers = ctResp?.data?.containers || [];
  updateTbSysbar();
  renderStats();
}

function renderStats() {
  const c = document.getElementById('content-stats');
  if (!c) return;

  const sys = S.sysinfo || {};

  // RAM : priorité aux données de /api/v1/system (bytes), fallback /api/v1/stats (MB)
  const memTotal  = sys.mem_total  || (S.stats.mem_total_mb  ? S.stats.mem_total_mb  * 1024 * 1024 : 0);
  const memUsed   = sys.mem_used   || (S.stats.mem_used_mb   ? S.stats.mem_used_mb   * 1024 * 1024 : 0);
  const ramUsedGb  = memTotal  ? (memUsed  / 1073741824).toFixed(1) : '—';
  const ramTotalGb = memTotal  ? (memTotal / 1073741824).toFixed(1) : '—';
  const ram = memTotal ? Math.round(memUsed / memTotal * 100) : 0;

  // Disque : priorité /api/v1/system (bytes), fallback /api/v1/stats (GB)
  const diskTotal = sys.disk_total || (S.stats.disk_total_gb ? S.stats.disk_total_gb * 1073741824 : 0);
  const diskUsed  = sys.disk_used  || (S.stats.disk_used_gb  ? S.stats.disk_used_gb  * 1073741824 : 0);
  const diskUsedGb  = diskTotal ? (diskUsed  / 1073741824).toFixed(1) : '—';
  const diskTotalGb = diskTotal ? (diskTotal / 1073741824).toFixed(1) : '—';
  const disk = diskTotal ? Math.round(diskUsed / diskTotal * 100) : 0;

  // Stats conteneurs avec barres CPU/RAM
  const containers = S.containers || [];
  const ctSorted = [...containers].sort((a, b) => {
    const memA = parseFloat((a.mem || '').split(' / ')[0]) || 0;
    const memB = parseFloat((b.mem || '').split(' / ')[0]) || 0;
    return memB - memA;
  });
  const ctRows = containers.length > 0 ? ctSorted.map(ct => {
    const cpuStr = ct.cpu || '0%';
    const cpuPct = parseFloat(cpuStr) || 0;
    const cpuBar = cpuPct > 0 ? `<div style="width:60px;height:3px;background:var(--bg3);border-radius:2px;overflow:hidden;margin-top:2px">
      <div style="width:${Math.min(cpuPct,100)}%;height:100%;background:${cpuPct>80?'var(--red-b)':cpuPct>50?'var(--warn)':'var(--vio-b)'}"></div></div>` : '';
    const memParts = (ct.mem || '').split(' / ');
    const memUsedStr = memParts[0] || '—';
    const memTotalStr = memParts[1] || '';
    return `
    <div class="loc-row" style="align-items:flex-start">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(ct.name || '?')}</div>
        ${cpuBar}
      </div>
      <div style="display:flex;gap:12px;font-size:9px;text-align:right">
        <div>
          <div style="color:var(--vio-b);font-weight:700">${escapeHtml(cpuStr)}</div>
          <div style="color:var(--text3);font-size:8px">CPU</div>
        </div>
        <div>
          <div style="color:var(--text2);font-weight:700">${escapeHtml(memUsedStr)}</div>
          ${memTotalStr ? `<div style="color:var(--text3);font-size:8px">${escapeHtml(memTotalStr)}</div>` : ''}
        </div>
      </div>
    </div>`;
  }).join('') : '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune donnée.</div>';

  // Score de santé simplifié
  let healthScore = 100;
  const healthIssues = [];
  if (ram > 90)  { healthScore -= 30; healthIssues.push('RAM critique'); }
  else if (ram > 80) { healthScore -= 10; healthIssues.push('RAM élevée'); }
  if (disk > 90) { healthScore -= 30; healthIssues.push('Disque critique'); }
  else if (disk > 80) { healthScore -= 10; healthIssues.push('Disque élevé'); }
  const failedApps = S.apps.filter(a => a.status === 'error' || a.status === 'failed');
  if (failedApps.length) { healthScore -= failedApps.length * 15; healthIssues.push(`${failedApps.length} app(s) en erreur`); }
  healthScore = Math.max(0, Math.min(100, healthScore));
  const healthColor = healthScore >= 80 ? 'var(--green-b)' : healthScore >= 50 ? 'var(--warn)' : 'var(--red-b)';
  const healthLabel = healthScore >= 80 ? 'BON' : healthScore >= 50 ? 'DÉGRADÉ' : 'CRITIQUE';

  c.innerHTML = `
    <div class="settings-card" style="padding:12px 14px;margin-bottom:8px">
      <div style="display:flex;align-items:center;gap:12px">
        <div style="position:relative;width:50px;height:50px;flex-shrink:0">
          <svg viewBox="0 0 36 36" style="width:50px;height:50px;transform:rotate(-90deg)">
            <circle cx="18" cy="18" r="15.9" fill="none" stroke="var(--bg3)" stroke-width="3"/>
            <circle cx="18" cy="18" r="15.9" fill="none" stroke="${healthColor}" stroke-width="3"
              stroke-dasharray="${healthScore} ${100-healthScore}" stroke-linecap="round"/>
          </svg>
          <div style="position:absolute;inset:0;display:flex;align-items:center;justify-content:center;font-size:10px;font-weight:900;color:${healthColor}">${healthScore}</div>
        </div>
        <div style="flex:1">
          <div style="font-size:8px;color:var(--text3);letter-spacing:1px">SANTÉ SYSTÈME</div>
          <div style="font-size:14px;font-weight:900;color:${healthColor};letter-spacing:1.5px">${healthLabel}</div>
          ${healthIssues.length ? `<div style="font-size:8px;color:var(--text3);margin-top:2px">${healthIssues.join(' · ')}</div>` : '<div style="font-size:8px;color:var(--text3);margin-top:2px">Aucun problème détecté</div>'}
        </div>
        <div style="text-align:right;font-size:9px;color:var(--text3)">
          <div>${S.apps.filter(a=>a.status==='running').length} actives</div>
          <div style="color:${S.apps.filter(a=>a.status==='error').length?'var(--red-b)':'var(--text3)'}">${S.apps.filter(a=>a.status==='error').length} erreurs</div>
          <div>${containers.length} conteneurs</div>
        </div>
      </div>
    </div>
    <div class="settings-card">
      <div class="settings-title">HÔTE</div>
      <div class="setting-row"><span>HOSTNAME</span><span class="setting-val">${escapeHtml(sys.hostname || '—')}</span></div>
      <div class="setting-row"><span>UPTIME</span><span class="setting-val">${escapeHtml(sys.uptime || '—')}</span></div>
      <div class="setting-row"><span>OS</span><span class="setting-val">${escapeHtml(sys.os || '—')}</span></div>
      <div class="setting-row"><span>KERNEL</span><span class="setting-val">${escapeHtml(sys.kernel || '—')}</span></div>
      <div class="setting-row"><span>CPU</span><span class="setting-val">${sys.cpu_count ? sys.cpu_count + ' cœur(s)' : '—'}</span></div>
      ${sys.load_avg_1 !== undefined ? `
      <div class="setting-row"><span>CHARGE (1m/5m/15m)</span><span class="setting-val" style="font-family:monospace">${sys.load_avg_1?.toFixed(2)} · ${sys.load_avg_5?.toFixed(2)} · ${sys.load_avg_15?.toFixed(2)}</span></div>` : ''}
    </div>
    <div class="settings-card">
      <div class="settings-title">RESSOURCES</div>
      <div class="seg-wrap">
        <div class="seg-meta"><span>RAM</span><span>${ramUsedGb}G / ${ramTotalGb}G</span></div>
        <div class="seg-bar">${segBar(ram)}</div>
      </div>
      <div class="seg-wrap">
        <div class="seg-meta"><span>DISQUE</span><span>${diskUsedGb}G / ${diskTotalGb}G</span></div>
        <div class="seg-bar">${segBar(disk, 20, 'on-ok')}</div>
      </div>
    </div>
    ${containers.length > 0 ? `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">CONTENEURS (${containers.length})</div>
      <div style="padding:0 12px 12px">${ctRows}</div>
    </div>` : ''}
    <div class="settings-card">
      <div class="settings-title">DAEMON</div>
      <div class="setting-row"><span>SOCKET</span><span class="setting-val">/run/caleoped.sock</span></div>
      <div class="setting-row"><span>APPS ACTIVES</span><span class="setting-val text-vio">${S.apps.filter(a => a.status === 'running').length}</span></div>
    </div>
    <div id="stats-processes-widget" style="margin-top:8px"></div>
    <div id="stats-docker-widget" style="margin-top:8px"></div>
    <div style="display:flex;align-items:center;justify-content:space-between;margin-top:12px">
      <div style="font-size:9px;color:var(--text3)">
        <i class="ti ti-refresh" style="font-size:9px"></i>
        Actualisé à ${new Date().toLocaleTimeString('fr-FR',{hour:'2-digit',minute:'2-digit',second:'2-digit'})}
      </div>
      <div style="display:flex;gap:6px;align-items:center">
        <label style="display:flex;align-items:center;gap:5px;font-size:9px;color:var(--text3);cursor:pointer">
          <input type="checkbox" id="stats-autorefresh" onchange="toggleStatsAutoRefresh(this.checked)"
            ${S._statsAutoRefresh ? 'checked' : ''} style="width:12px;height:12px">
          AUTO (5s)
        </label>
        <button class="btn-sm" onclick="loadStats()"><i class="ti ti-refresh" style="font-size:10px"></i></button>
      </div>
    </div>
  `;
  if (S._statsAutoRefresh) {
    clearInterval(S._statsTimer);
    S._statsTimer = setInterval(() => { if (S.section === 'stats') loadStats(); }, 5000);
  }
  loadStatsProcessWidget();
  loadStatsDockerWidget();
}

async function loadStatsDockerWidget() {
  const w = document.getElementById('stats-docker-widget');
  if (!w) return;
  let data = null;
  try {
    const r = await fetch('/sys/docker-stats');
    if (r.ok) data = await r.json();
  } catch(e) {}

  if (!data?.stats?.length) { w.innerHTML = ''; return; }

  const cpuNum = s => parseFloat(s?.replace('%','') || '0');
  const memNum = s => parseFloat(s?.replace('%','') || '0');
  const sorted = [...data.stats].sort((a,b) => cpuNum(b.cpu) - cpuNum(a.cpu));
  w.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div style="padding:10px 12px 6px">
        <div class="settings-title" style="margin:0">CONTENEURS — RESSOURCES</div>
      </div>
      <table style="width:100%;border-collapse:collapse;font-size:8px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="padding:3px 8px 3px 12px;text-align:left">NOM</th>
          <th style="padding:3px 6px;text-align:right">CPU</th>
          <th style="padding:3px 6px;text-align:right">RAM</th>
          <th style="padding:3px 6px;text-align:right">NET I/O</th>
          <th style="padding:3px 12px 3px 6px;text-align:right">UPTIME</th>
        </tr></thead>
        <tbody>${sorted.map(s => {
          const cpu = cpuNum(s.cpu);
          const mem = memNum(s.mem_perc);
          const uptimeStr = (() => {
            if (!s.started_at) return '—';
            const diff = Date.now() - new Date(s.started_at).getTime();
            if (diff < 0) return '—';
            const h = Math.floor(diff / 3600000);
            const m = Math.floor((diff % 3600000) / 60000);
            if (h > 48) return `${Math.floor(h/24)}j`;
            if (h > 0) return `${h}h${m}m`;
            return `${m}m`;
          })();
          return `<tr style="border-bottom:1px solid var(--border)">
            <td style="padding:3px 8px 3px 12px;font-weight:600;color:var(--accent);cursor:pointer"
              title="Voir les logs" onclick="openLogs('${escapeHtml(s.name.replace(/^\//, ''))}')">
              ${escapeHtml(s.name.replace(/^\//, ''))}</td>
            <td style="padding:3px 6px;text-align:right;font-family:monospace;color:${cpu>50?'var(--red-b)':cpu>20?'var(--warn)':'var(--ok)'}">${escapeHtml(s.cpu)}</td>
            <td style="padding:3px 6px;text-align:right;font-family:monospace;color:${mem>80?'var(--red-b)':mem>50?'var(--warn)':'var(--text1)'}">${escapeHtml(s.mem)}</td>
            <td style="padding:3px 6px;text-align:right;font-family:monospace;color:var(--text3)">${escapeHtml(s.net_io)}</td>
            <td style="padding:3px 12px 3px 6px;text-align:right;font-family:monospace;color:var(--text3)">${uptimeStr}</td>
          </tr>`;
        }).join('')}
        </tbody>
      </table>
    </div>`;
}

async function loadStatsProcessWidget() {
  const w = document.getElementById('stats-processes-widget');
  if (!w) return;
  w.innerHTML = `<div class="settings-card" style="padding:10px 12px">
    <div class="settings-title" style="margin-bottom:6px">TOP PROCESSUS</div>
    <div style="font-size:9px;color:var(--text3)"><span class="spinner" style="width:8px;height:8px;border-width:1px"></span> Chargement…</div>
  </div>`;

  const sortBy = w.dataset.sort || 'cpu';
  let data = null;
  try {
    const r = await fetch('/sys/processes?sort=' + sortBy);
    if (r.ok) data = await r.json();
  } catch(e) {}

  if (!data?.processes?.length) { w.innerHTML = ''; return; }

  const procs = data.processes;
  w.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div style="display:flex;align-items:center;padding:10px 12px 6px;gap:8px">
        <div class="settings-title" style="flex:1;margin:0">TOP PROCESSUS</div>
        <button class="btn-sm${sortBy==='cpu'?' active':''}" onclick="statsSetProcSort('cpu')" style="font-size:8px">CPU</button>
        <button class="btn-sm${sortBy==='mem'?' active':''}" onclick="statsSetProcSort('mem')" style="font-size:8px">RAM</button>
      </div>
      <table style="width:100%;border-collapse:collapse;font-size:8px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="padding:3px 8px 3px 12px;text-align:left">PID</th>
          <th style="padding:3px 6px;text-align:left">UTILISATEUR</th>
          <th style="padding:3px 6px;text-align:right">CPU%</th>
          <th style="padding:3px 6px;text-align:right">RAM%</th>
          <th style="padding:3px 12px 3px 6px;text-align:left">COMMANDE</th>
        </tr></thead>
        <tbody>${procs.map(p => `
          <tr style="border-bottom:1px solid var(--border)">
            <td style="padding:3px 8px 3px 12px;font-family:monospace;color:var(--text3)">${escapeHtml(p.pid)}</td>
            <td style="padding:3px 6px;color:var(--text2)">${escapeHtml(p.user)}</td>
            <td style="padding:3px 6px;text-align:right;color:${parseFloat(p.cpu)>30?'var(--red-b)':parseFloat(p.cpu)>10?'var(--warn)':'var(--text1)'};font-family:monospace">${escapeHtml(p.cpu)}%</td>
            <td style="padding:3px 6px;text-align:right;color:${parseFloat(p.mem)>20?'var(--red-b)':parseFloat(p.mem)>10?'var(--warn)':'var(--text1)'};font-family:monospace">${escapeHtml(p.mem)}%</td>
            <td style="padding:3px 12px 3px 6px;color:var(--text2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:200px" title="${escapeHtml(p.cmd)}">${escapeHtml(p.cmd)}</td>
          </tr>`).join('')}
        </tbody>
      </table>
    </div>`;
}

function statsSetProcSort(sort) {
  const w = document.getElementById('stats-processes-widget');
  if (w) { w.dataset.sort = sort; loadStatsProcessWidget(); }
}

let _statsRefreshTimer = null;
function toggleStatsAutoRefresh(on) {
  S._statsAutoRefresh = on;
  clearInterval(S._statsTimer);
  if (on) {
    S._statsTimer = setInterval(() => { if (S.section === 'stats') loadStats(); }, 5000);
  }
}

// ── SECTION: SETTINGS ─────────────────────────────────────────────────────────
async function loadSettings() {
  const resp = await api.get('/api/v1/ping');
  const data = resp?.data || resp || {};
  const c = document.getElementById('content-settings');
  if (!c) return;
  c.innerHTML = `
    <div class="settings-card">
      <div class="settings-title">SERVEUR</div>
      <div class="setting-row"><span>DOMAINE</span><span class="setting-val">${data?.domain || '—'}</span></div>
      <div class="setting-row"><span>PROXY</span><span class="setting-val">${data?.proxy_mode || '—'}</span></div>
      <div class="setting-row"><span>CANAL</span><span class="badge ${data?.channel === 'alpha' ? 'badge-warn' : 'badge-run'}"><span style="width:5px;height:5px;background:${data?.channel === 'alpha' ? 'var(--warn)' : 'var(--vio-b)'};display:inline-block"></span>&nbsp;${(data?.channel || 'stable').toUpperCase()}</span></div>
      <div class="setting-row"><span>BASE DIR</span><span class="setting-val">/opt/gaiver-it/caleope</span></div>
    </div>
    <div class="settings-card" id="upgrade-card">
      <div class="settings-title">MISE À JOUR</div>
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <div style="font-size:10px;color:var(--text3)">VERSION : <span class="setting-val">${data?.version || '—'}</span> &nbsp; CANAL : <span class="badge ${data?.channel === 'alpha' ? 'badge-warn' : 'badge-run'}"><span style="width:5px;height:5px;background:${data?.channel === 'alpha' ? 'var(--warn)' : 'var(--vio-b)'};display:inline-block"></span>&nbsp;${(data?.channel || 'stable').toUpperCase()}</span></div>
      </div>
      <div style="display:flex;gap:6px;flex-wrap:wrap">
        <button class="btn" onclick="checkUpgrade()"><i class="ti ti-refresh"></i>VÉRIFIER</button>
        <button class="btn" id="btn-upgrade" onclick="runUpgrade()" style="display:none"><i class="ti ti-arrow-up"></i>METTRE À JOUR</button>
        <button class="btn" onclick="runUpdate()"><i class="ti ti-refresh-dot"></i>SYNC STORE</button>
      </div>
      <div id="upgrade-log" style="display:none;margin-top:10px;background:var(--bg1);border:1px solid var(--border1);padding:8px 10px;font-size:10px;font-family:monospace;color:var(--text2);max-height:160px;overflow-y:auto;line-height:1.7"></div>
    </div>
    <div class="settings-card">
      <div class="settings-title">LOGO DE L'INTERFACE</div>
      <div style="display:flex;align-items:center;gap:16px">
        <div style="width:56px;height:56px;border:1px solid var(--border2);background:var(--bg3);flex-shrink:0;overflow:hidden;display:flex;align-items:center;justify-content:center">
          <img src="/ui/logo?${Date.now()}" class="logo-img" style="width:100%;height:100%;object-fit:cover" onerror="this.style.opacity=0" alt="">
        </div>
        <div style="flex:1">
          <div style="font-size:10px;color:var(--text2);margin-bottom:8px">PNG, SVG, JPG ou WebP — max 5 Mo</div>
          <div style="display:flex;gap:6px;flex-wrap:wrap">
            <label class="btn" style="cursor:pointer;flex:0">
              <i class="ti ti-upload"></i>IMPORTER UN LOGO
              <input type="file" accept="image/png,image/jpeg,image/svg+xml,image/webp" style="display:none" onchange="uploadLogo(this)">
            </label>
            <button class="btn-sm danger" onclick="resetLogo()"><i class="ti ti-trash"></i>RÉINITIALISER</button>
          </div>
        </div>
      </div>
    </div>
    <div class="settings-card" id="license-settings-card">
      <div class="settings-title">LICENCE</div>
      <div id="license-settings-content"><div style="font-size:9px;color:var(--text3)"><span class="spinner"></span> Chargement…</div></div>
    </div>
    <div class="settings-card">
      <div class="settings-title">MOT DE PASSE</div>
      <div style="display:flex;flex-direction:column;gap:8px">
        <input type="password" id="pw-current" placeholder="Mot de passe actuel" class="param-input" style="max-width:300px">
        <input type="password" id="pw-new" placeholder="Nouveau mot de passe (min 8 car.)" class="param-input" style="max-width:300px">
        <input type="password" id="pw-confirm" placeholder="Confirmer le nouveau mot de passe" class="param-input" style="max-width:300px">
        <div>
          <button class="btn" onclick="changePassword()"><i class="ti ti-lock"></i>CHANGER</button>
        </div>
      </div>
    </div>
    <div class="settings-card" id="totp-settings-card">
      <div class="settings-title">DOUBLE AUTHENTIFICATION (2FA TOTP)</div>
      <div id="totp-settings-content"><div style="font-size:9px;color:var(--text3)"><span class="spinner"></span> Chargement…</div></div>
    </div>
    <div class="settings-card">
      <div class="settings-title">APPARENCE</div>
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:14px">
        <div>
          <div style="font-size:10px;font-weight:600;color:var(--text2)">MODE SOMBRE / CLAIR</div>
          <div style="font-size:9px;color:var(--text3);margin-top:2px">Basculer entre le thème sombre et le thème clair</div>
        </div>
        <button id="mode-toggle-btn" class="btn" onclick="toggleMode()" style="flex-shrink:0">
          ${(localStorage.getItem('caleope-mode')||'dark') === 'light' ? '<i class="ti ti-moon"></i> MODE SOMBRE' : '<i class="ti ti-sun"></i> MODE CLAIR'}
        </button>
      </div>
    </div>
    <div class="settings-card">
      <div class="settings-title">THÈME DE COULEUR</div>
      <div style="font-size:9px;color:var(--text3);margin-bottom:10px">Couleur d'accentuation de l'interface</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center">
        ${[
          {name:'Violet', vio:'#7C3AED', vioB:'#A78BFA', vioDim:'#4C1D95', vioG:'#7C3AED14', accent:'#A78BFA'},
          {name:'Bleu',   vio:'#1D6FDB', vioB:'#60A5FA', vioDim:'#1E3A6E', vioG:'#1D6FDB14', accent:'#60A5FA'},
          {name:'Cyan',   vio:'#0E7490', vioB:'#22D3EE', vioDim:'#164E63', vioG:'#0E749014', accent:'#22D3EE'},
          {name:'Vert',   vio:'#059669', vioB:'#34D399', vioDim:'#064E3B', vioG:'#05966914', accent:'#34D399'},
          {name:'Rose',   vio:'#BE185D', vioB:'#F472B6', vioDim:'#831843', vioG:'#BE185D14', accent:'#F472B6'},
          {name:'Orange', vio:'#C2410C', vioB:'#FB923C', vioDim:'#7C2D12', vioG:'#C2410C14', accent:'#FB923C'},
        ].map(t => `
          <button class="btn-sm" onclick="applyTheme(${JSON.stringify(t).replace(/"/g,"'")})"
            style="display:flex;align-items:center;gap:5px;font-size:9px">
            <span style="display:inline-block;width:12px;height:12px;border-radius:50%;background:${t.vioB}"></span>
            ${t.name}
          </button>`).join('')}
        <button class="btn-sm" onclick="resetTheme()" style="font-size:9px;color:var(--text3)">DÉFAUT</button>
      </div>
    </div>
    <div class="settings-card" id="settings-certs-card">
      <div class="settings-title">CERTIFICATS SSL <span style="font-size:8px;color:var(--text3);font-weight:400;letter-spacing:0">Chargement…</span></div>
    </div>
    <div class="settings-card">
      <div class="settings-title">EXPORT SYSTÈME</div>
      <div style="font-size:10px;color:var(--text3);margin-bottom:10px">Télécharger un snapshot JSON de l'état courant (apps, tâches, événements)</div>
      <button id="snapshot-btn" class="btn" onclick="exportSystemSnapshot()"><i class="ti ti-download"></i> SNAPSHOT</button>
    </div>
    <div class="settings-card">
      <div class="settings-title">ACTUALISATION AUTOMATIQUE</div>
      <div style="font-size:9px;color:var(--text3);margin-bottom:10px">Intervalle de rafraîchissement du tableau de bord et des stats</div>
      <div style="display:flex;gap:4px;flex-wrap:wrap">
        ${[15,30,60,120,0].map(v => {
          const cur = parseInt(localStorage.getItem('caleope-refresh-interval') || '30');
          const label = v === 0 ? 'OFF' : v < 60 ? `${v}s` : `${v/60}m`;
          return `<button class="btn-sm${cur===v?' active':''}" onclick="setRefreshInterval(${v})" style="font-size:9px">${label}</button>`;
        }).join('')}
      </div>
    </div>
    <div class="settings-card" id="maintenance-card">
      <div class="settings-title">MAINTENANCE</div>
      <div id="maintenance-content">
        <div style="display:flex;align-items:center;gap:8px;color:var(--text3);font-size:9px">
          <span class="spinner"></span> Analyse en cours...
        </div>
      </div>
    </div>
    <div class="settings-card">
      <div class="settings-title">SSO / OIDC</div>
      <div style="font-size:9px;color:var(--text3);margin-bottom:10px">
        Configurez un provider OpenID Connect (Authentik, Keycloak, etc.) pour ajouter un bouton de connexion SSO sur l'écran de login.
        <br>Pour Authentik, l'issuer est au format <code style="font-size:8px;background:var(--bg3);padding:1px 4px;border-radius:3px">https://authentik.domain.tld/application/o/slug/</code>
      </div>
      <div id="oidc-settings-form" style="display:flex;flex-direction:column;gap:8px">
        <div style="display:flex;align-items:center;gap:6px;margin-bottom:4px">
          <span class="spinner" id="oidc-loading" style="display:none"></span>
          <span id="oidc-status-badge"></span>
        </div>
        <input type="text" id="oidc-name" placeholder="Nom du bouton (ex: Connexion Authentik)" class="param-input">
        <input type="text" id="oidc-issuer" placeholder="OIDC Issuer URL" class="param-input">
        <input type="text" id="oidc-client-id" placeholder="Client ID" class="param-input">
        <input type="password" id="oidc-client-secret" placeholder="Client Secret" class="param-input">
        <input type="text" id="oidc-redirect-uri" placeholder="Redirect URI (optionnel — dérivé automatiquement)" class="param-input">
        <div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:4px">
          <button class="btn" onclick="saveOidcSettings()"><i class="ti ti-device-floppy"></i> SAUVEGARDER</button>
          <button class="btn-sm danger" onclick="clearOidcSettings()"><i class="ti ti-trash"></i> DÉSACTIVER SSO</button>
        </div>
      </div>
    </div>
    <div class="settings-card">
      <div class="settings-title">DÉPÔTS DE MISE À JOUR</div>
      <div style="font-size:9px;color:var(--text3);margin-bottom:10px">
        Dépôts git des définitions d'apps du store. Le dépôt officiel ne peut pas être retiré.
      </div>
      <div id="repos-list" style="display:flex;flex-direction:column;gap:6px;margin-bottom:10px">
        <div style="font-size:9px;color:var(--text3)"><span class="spinner"></span> Chargement...</div>
      </div>
      <div style="display:flex;gap:6px;flex-wrap:wrap">
        <input type="text" id="repo-name" placeholder="Nom" class="param-input" style="flex:0 0 110px">
        <input type="text" id="repo-url" placeholder="URL git (https://github.com/...)" class="param-input" style="flex:1;min-width:160px">
        <input type="text" id="repo-branch" placeholder="Branche (main)" class="param-input" style="flex:0 0 110px">
        <button class="btn" onclick="addRepo()"><i class="ti ti-plus"></i> AJOUTER</button>
      </div>
      <div id="repos-msg" style="font-size:9px;color:var(--text3);min-height:12px;margin-top:6px"></div>
    </div>
    <div class="settings-card">
      <div class="settings-title">REGISTRE D'IMAGES</div>
      <div style="font-size:9px;color:var(--text3);margin-bottom:10px">
        Registre miroir depuis lequel les apps récupèrent leurs images Docker (au lieu de Docker Hub). Laisser vide = images d'origine.
      </div>
      <div id="registry-settings-form" style="display:flex;flex-direction:column;gap:8px">
        <div style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:2px">
          <button class="btn-sm" onclick="setRegistryPreset('caleope-registry.gaiver-it.fr')">Caleope</button>
          <button class="btn-sm" onclick="setRegistryPreset('ghcr.io')">GHCR</button>
          <button class="btn-sm" onclick="setRegistryPreset('lscr.io')">LSCR</button>
          <button class="btn-sm" onclick="setRegistryPreset('registry-1.docker.io')">Docker Hub</button>
          <button class="btn-sm danger" onclick="setRegistryPreset('')">Aucun</button>
        </div>
        <input type="text" id="registry-url" placeholder="URL du registre (ex: caleope-registry.gaiver-it.fr)" class="param-input">
        <input type="text" id="registry-user" placeholder="Utilisateur (optionnel)" class="param-input" autocomplete="off">
        <input type="password" id="registry-pass" placeholder="Mot de passe (vide = garder l'actuel)" class="param-input" autocomplete="new-password">
        <div id="registry-msg" style="font-size:9px;color:var(--text3);min-height:12px"></div>
        <div style="display:flex;gap:8px;margin-top:2px">
          <button class="btn" onclick="saveRegistrySettings()"><i class="ti ti-device-floppy"></i> ENREGISTRER</button>
        </div>
      </div>
    </div>
    <div class="settings-card">
      <div class="settings-title">SESSION</div>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div style="font-size:10px;color:var(--text3)">CONNECTÉ À L'INTERFACE WEB</div>
        <button class="btn btn-sm danger" onclick="logout()"><i class="ti ti-logout"></i>SE DÉCONNECTER</button>
      </div>
    </div>
  `;
  loadSettingsCerts();
  loadSettingsLicense();
  loadMaintenancePanel();
  loadOidcSettings();
  loadTOTPSettings();
  loadRegistrySettings();
  loadRepos();
}

async function loadRepos() {
  const el = document.getElementById('repos-list');
  if (!el) return;
  try {
    const r = await fetch('/api/v1/repos');
    const raw = r.ok ? await r.json() : null;
    const repos = (raw && raw.data) ? raw.data : (Array.isArray(raw) ? raw : []);
    if (!repos.length) { el.innerHTML = '<div style="font-size:9px;color:var(--text3)">Aucun dépôt.</div>'; return; }
    el.innerHTML = repos.map(rp => {
      const official = rp.trust === 'official';
      const rm = official
        ? '<span style="font-size:8px;color:var(--text3);flex-shrink:0">verrouillé</span>'
        : `<button class="btn-sm danger" style="flex-shrink:0" onclick="removeRepo('${escapeHtml(rp.name)}')"><i class="ti ti-trash"></i></button>`;
      return `<div style="display:flex;align-items:center;gap:8px;padding:8px 10px;background:var(--bg3);border:1px solid var(--border);border-radius:6px">
        <div style="flex:1;min-width:0">
          <div style="font-size:11px;font-weight:700">${escapeHtml(rp.name)}
            <span style="font-size:8px;color:var(--vio-b);letter-spacing:.5px">${(rp.trust||'').toUpperCase()}</span>
            <span style="font-size:8px;color:var(--text3)">@${escapeHtml(rp.branch||'main')}</span></div>
          <div style="font-size:9px;color:var(--text3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(rp.url)}</div>
        </div>${rm}</div>`;
    }).join('');
  } catch(e) { el.innerHTML = '<div style="font-size:9px;color:var(--red-b)">Erreur de chargement</div>'; }
}
async function addRepo() {
  const msg = document.getElementById('repos-msg');
  const name = (document.getElementById('repo-name').value || '').trim();
  const url = (document.getElementById('repo-url').value || '').trim();
  const branch = (document.getElementById('repo-branch').value || '').trim();
  if (!name || !url) { if (msg) { msg.style.color = 'var(--red-b)'; msg.textContent = 'Nom et URL requis'; } return; }
  if (msg) { msg.style.color = 'var(--text3)'; msg.textContent = 'Ajout...'; }
  try {
    const r = await fetch('/api/v1/repos', { method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, url, branch, trust: 'community' }) });
    if (r.ok) {
      if (msg) { msg.style.color = 'var(--ok)'; msg.textContent = '✓ Dépôt ajouté'; }
      ['repo-name','repo-url','repo-branch'].forEach(id => { const e = document.getElementById(id); if (e) e.value = ''; });
      loadRepos();
    } else { const e = await r.json().catch(() => ({})); if (msg) { msg.style.color = 'var(--red-b)'; msg.textContent = 'Erreur : ' + (e.error || r.status); } }
  } catch(e) { if (msg) { msg.style.color = 'var(--red-b)'; msg.textContent = 'Erreur réseau'; } }
}
async function removeRepo(name) {
  const msg = document.getElementById('repos-msg');
  try {
    const r = await fetch('/api/v1/repos?name=' + encodeURIComponent(name), { method: 'DELETE' });
    if (r.ok) { if (msg) { msg.style.color = 'var(--ok)'; msg.textContent = '✓ Dépôt retiré'; } loadRepos(); }
    else { const e = await r.json().catch(() => ({})); if (msg) { msg.style.color = 'var(--red-b)'; msg.textContent = 'Erreur : ' + (e.error || r.status); } }
  } catch(e) { if (msg) { msg.style.color = 'var(--red-b)'; msg.textContent = 'Erreur réseau'; } }
}

async function loadRegistrySettings() {
  try {
    const r = await fetch('/api/v1/registry');
    if (!r.ok) return;
    const raw = await r.json();
    const d = (raw && raw.data) ? raw.data : raw;
    const url = document.getElementById('registry-url');
    const user = document.getElementById('registry-user');
    const pass = document.getElementById('registry-pass');
    if (url) url.value = d.registry || '';
    if (user) user.value = d.user || '';
    if (pass) pass.placeholder = d.has_pass ? "Mot de passe enregistré (vide = garder)" : "Mot de passe (optionnel)";
  } catch(e) {}
}
function setRegistryPreset(url) {
  const el = document.getElementById('registry-url');
  if (el) el.value = url;
}
async function saveRegistrySettings() {
  const msg = document.getElementById('registry-msg');
  const body = {
    registry: (document.getElementById('registry-url').value || '').trim(),
    user: (document.getElementById('registry-user').value || '').trim(),
    pass: document.getElementById('registry-pass').value || '',
  };
  if (msg) { msg.style.color = 'var(--text3)'; msg.textContent = 'Enregistrement...'; }
  try {
    const r = await fetch('/api/v1/registry', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (r.ok) {
      if (msg) { msg.style.color = 'var(--ok)'; msg.textContent = body.registry
        ? "✓ Enregistré — les prochaines installs pull depuis ce registre."
        : "✓ Enregistré — images d'origine."; }
      const p = document.getElementById('registry-pass'); if (p) p.value = '';
      loadRegistrySettings();
    } else {
      const e = await r.json().catch(() => ({}));
      if (msg) { msg.style.color = 'var(--red-b)'; msg.textContent = 'Erreur : ' + (e.error || r.status); }
    }
  } catch(e) {
    if (msg) { msg.style.color = 'var(--red-b)'; msg.textContent = 'Erreur réseau'; }
  }
}

async function loadMaintenancePanel() {
  const el = document.getElementById('maintenance-content');
  if (!el) return;

  // Lecture du mode submarine depuis la config daemon
  let submarineMode = false;
  try {
    const r = await api.get('/api/v1/ping');
    submarineMode = !!(r?.data?.submarine_mode || r?.submarine_mode);
  } catch(e) {}

  // Chargement du preview
  let tasks = [];
  try {
    const r = await fetch('/sys/maintenance');
    if (r.ok) { const d = await r.json(); tasks = d.tasks || []; }
  } catch(e) {}

  const totalReclaimable = tasks.reduce((s, t) => s + (t.reclaimable_bytes || 0), 0);

  const renderTasks = (running = false) => {
    return tasks.map(t => {
      const isSubWarning = t.submarine && submarineMode;
      const checkId = `maint-chk-${t.id}`;
      return `
        <label style="display:flex;align-items:flex-start;gap:10px;padding:8px 0;border-bottom:1px solid var(--border);cursor:pointer" for="${checkId}">
          <input type="checkbox" id="${checkId}" data-task="${t.id}" ${isSubWarning ? '' : 'checked'}
            style="margin-top:2px;accent-color:var(--blue);flex-shrink:0" ${running ? 'disabled' : ''}>
          <div style="flex:1;min-width:0">
            <div style="display:flex;align-items:center;gap:6px;margin-bottom:2px">
              <span style="font-size:9px;font-weight:700;color:var(--text)">${t.label}</span>
              ${t.estimate && t.estimate !== '0 B' && t.estimate !== '—' ? `<span style="font-size:8px;color:var(--ok);background:rgba(22,163,74,.1);padding:1px 5px;border-radius:3px">~${t.estimate}</span>` : ''}
              ${isSubWarning ? `<span style="font-size:8px;color:var(--warn);background:rgba(217,119,6,.1);padding:1px 5px;border-radius:3px"><i class="ti ti-submarine" style="font-size:8px"></i> MODE HORS-LIGNE</span>` : ''}
              ${t.submarine && !submarineMode ? `<span style="font-size:8px;color:var(--text3);background:var(--bg3);padding:1px 5px;border-radius:3px"><i class="ti ti-cloud-off" style="font-size:8px"></i> NÉCESSITE INTERNET</span>` : ''}
            </div>
            <div style="font-size:8px;color:var(--text3);line-height:1.5">${t.description}</div>
          </div>
        </label>`;
    }).join('');
  };

  el.innerHTML = `
    <div style="margin-bottom:10px;padding:8px 10px;background:var(--bg3);border-radius:6px;display:flex;align-items:center;justify-content:space-between">
      <div>
        <span style="font-size:9px;color:var(--text2)">ESPACE POTENTIELLEMENT RÉCUPÉRABLE</span>
        <span style="font-size:16px;font-weight:700;color:var(--blue);margin-left:10px">${formatBytes(totalReclaimable)}</span>
      </div>
      ${submarineMode ? `<span style="font-size:8px;color:var(--warn);background:rgba(217,119,6,.1);padding:3px 8px;border-radius:4px;border:1px solid var(--warn)"><i class="ti ti-submarine"></i> MODE SUBMARINE ACTIF</span>` : ''}
    </div>
    <div id="maint-task-list" style="margin-bottom:12px">${renderTasks()}</div>
    <div style="display:flex;gap:8px;align-items:center">
      <button class="btn" id="maint-run-btn" onclick="runMaintenance()">
        <i class="ti ti-trash"></i> NETTOYER LA SÉLECTION
      </button>
      <button class="btn-sm" onclick="loadMaintenancePanel()" style="font-size:8px">
        <i class="ti ti-refresh" style="font-size:9px"></i> Rafraîchir
      </button>
    </div>
    <div id="maint-results" style="margin-top:12px;display:none"></div>
  `;
}

async function runMaintenance() {
  const btn = document.getElementById('maint-run-btn');
  const resultsEl = document.getElementById('maint-results');
  if (!btn || !resultsEl) return;

  const checkboxes = document.querySelectorAll('#maint-task-list input[type=checkbox]:checked');
  const tasks = Array.from(checkboxes).map(c => c.dataset.task);
  if (tasks.length === 0) { notify('Aucune tâche sélectionnée', 'warn'); return; }

  // Confirmation pour images Docker (submarine warning)
  const hasAllImages = tasks.includes('docker_all_images');
  if (hasAllImages) {
    const ok = confirm('⚠ Supprimer TOUTES les images Docker inutilisées ?\n\nEn mode hors-ligne (submarine), ces images ne pourront pas être re-téléchargées et les apps concernées ne pourront plus démarrer.\n\nConfirmer ?');
    if (!ok) return;
  }

  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> NETTOYAGE EN COURS...';
  resultsEl.style.display = 'none';

  try {
    const r = await fetch('/sys/maintenance', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tasks })
    });
    if (!r.ok) throw new Error('HTTP ' + r.status);
    const data = await r.json();
    const results = data.results || [];

    let totalFreed = 0;
    const rows = results.map(res => {
      const icon = res.success ? '✓' : '✗';
      const color = res.success ? 'var(--ok)' : 'var(--err)';
      return `<div style="display:flex;align-items:center;justify-content:space-between;padding:5px 0;border-bottom:1px solid var(--border)">
        <span style="font-size:9px;color:${color}">${icon} ${res.id.replace(/_/g,' ').toUpperCase()}</span>
        <span style="font-size:9px;font-weight:700;color:var(--blue)">${res.freed || '—'} libérés</span>
      </div>`;
    }).join('');

    resultsEl.innerHTML = `
      <div style="background:var(--bg3);border-radius:6px;padding:10px 12px">
        <div style="font-size:9px;font-weight:700;color:var(--text2);margin-bottom:8px">RÉSULTATS</div>
        ${rows}
      </div>`;
    resultsEl.style.display = 'block';
    notify('Nettoyage terminé', 'ok');

    // Rafraîchir les estimations
    setTimeout(loadMaintenancePanel, 1000);
  } catch(e) {
    notify('Erreur : ' + e.message, 'err');
  } finally {
    btn.disabled = false;
    btn.innerHTML = '<i class="ti ti-trash"></i> NETTOYER LA SÉLECTION';
  }
}

function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B','KB','MB','GB','TB'];
  let i = 0;
  let v = bytes;
  while (v >= 1000 && i < units.length - 1) { v /= 1000; i++; }
  return v.toFixed(1) + ' ' + units[i];
}

async function loadSettingsCerts() {
  const card = document.getElementById('settings-certs-card');
  if (!card) return;
  let data = null;
  try {
    const r = await fetch('/sys/certs');
    if (r.ok) data = await r.json();
  } catch(e) {}

  if (!data?.certs?.length) {
    card.innerHTML = `<div class="settings-title">CERTIFICATS SSL</div>
      <div style="font-size:9px;color:var(--text3)">Aucun certificat trouvé dans /opt/gaiver-it/caleope/data/traefik/certs/</div>`;
    return;
  }

  const rows = data.certs.map(c => {
    const color = c.expired ? 'var(--red-b)' : c.days_left < 14 ? 'var(--warn)' : 'var(--ok)';
    const label = c.expired ? 'EXPIRÉ' : c.days_left < 14 ? `${c.days_left}j` : `${c.days_left}j`;
    const badge = `<span style="font-size:8px;color:${color};font-weight:700">${label}</span>`;
    return `<div class="setting-row" style="align-items:flex-start;padding:5px 0">
      <div>
        <div style="font-size:9px;font-weight:700">${escapeHtml(c.file)}</div>
        <div style="font-size:8px;color:var(--text3);margin-top:2px">${escapeHtml(c.subject)} · ${c.self_signed ? 'auto-signé' : escapeHtml(c.issuer)}</div>
        <div style="font-size:8px;color:var(--text3)">${escapeHtml(c.not_before)} → ${escapeHtml(c.not_after)}</div>
      </div>
      <div style="display:flex;flex-direction:column;align-items:flex-end;gap:2px">
        ${badge}
        ${c.self_signed ? '<span style="font-size:7px;color:var(--text3)">AUTO-SIGNÉ</span>' : ''}
      </div>
    </div>`;
  }).join('');

  card.innerHTML = `<div class="settings-title">CERTIFICATS SSL <span style="font-size:8px;color:var(--text3);font-weight:400;letter-spacing:0">${data.certs.length}</span></div>${rows}`;
}

async function loadSettingsLicense() {
  const el = document.getElementById('license-settings-content');
  if (!el) return;
  const r = await fetch('/api/v1/license');
  const raw = r.ok ? await r.json() : null;
  if (!raw) { el.innerHTML = '<div style="font-size:9px;color:var(--red-b)">Impossible de contacter le serveur de licence</div>'; return; }
  // Réponse API enveloppée : {data:{...},success}. On lit d.data.
  const d = (raw && raw.data) ? raw.data : raw;

  if (d.activated) {
    el.innerHTML = `
      <div class="setting-row"><span>ÉDITION</span><span class="badge badge-ok">${(d.edition||'').toUpperCase()}</span></div>
      <div class="setting-row"><span>CLÉ</span><span class="setting-val" style="font-family:monospace">${d.license_key || '—'}</span></div>
      <div style="margin-top:10px;display:flex;flex-direction:column;gap:6px">
        <div style="font-size:9px;color:var(--text3)">Changer de clé de licence :</div>
        <div style="display:flex;gap:6px;align-items:center">
          <input id="lic-settings-input" class="param-input" type="text" placeholder="CALP-XXXX-XXXX-XXXX" style="flex:1;font-family:monospace;text-transform:uppercase">
          <button class="btn" onclick="activateLicenseSettings()"><i class="ti ti-refresh"></i>CHANGER</button>
        </div>
        <div id="lic-settings-err" style="display:none;font-size:9px;color:var(--red-b)"></div>
      </div>`;
  } else {
    el.innerHTML = `
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px;padding:8px 10px;background:var(--bg3);border:1px solid var(--border);border-radius:6px">
        <i class="ti ti-alert-circle" style="color:var(--warn);font-size:13px"></i>
        <div style="font-size:9px;color:var(--text2)">Licence non activée — l'installation d'apps est désactivée.</div>
      </div>
      <div style="display:flex;flex-direction:column;gap:6px">
        <div style="display:flex;gap:6px;align-items:center">
          <input id="lic-settings-input" class="param-input" type="text" placeholder="CALP-XXXX-XXXX-XXXX" style="flex:1;font-family:monospace;text-transform:uppercase">
          <button class="btn btn-vio" onclick="activateLicenseSettings()"><i class="ti ti-check"></i>ACTIVER</button>
        </div>
        <div id="lic-settings-err" style="display:none;font-size:9px;color:var(--red-b)"></div>
        <a href="https://caleope-pay.gaiver-it.fr" target="_blank" style="font-size:8px;color:var(--vio-b)">Obtenir une clé (Community gratuit)</a>
      </div>`;
  }
}

async function activateLicenseSettings() {
  const key = document.getElementById('lic-settings-input')?.value.trim().toUpperCase();
  const errEl = document.getElementById('lic-settings-err');
  errEl.style.display = 'none';
  if (!key || !key.startsWith('CALP-')) { errEl.textContent = 'Format invalide (CALP-XXXX-XXXX-XXXX)'; errEl.style.display = 'block'; return; }
  const r = await fetch('/api/v1/license/activate', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({license_key: key})
  });
  const d = await r.json();
  if (!r.ok) { errEl.textContent = d.error || 'Erreur'; errEl.style.display = 'block'; return; }
  notify(`Licence ${(d.edition||'').toUpperCase()} activée ✓`, 'ok');
  loadSettingsLicense();
}

async function exportSystemSnapshot() {
  const btn = document.getElementById('snapshot-btn');
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="spinner"></span> EXPORT...'; }
  try {
    const [sysR, appsR, tasksR, eventsR] = await Promise.all([
      api.get('/api/v1/system').catch(() => null),
      api.get('/api/v1/apps').catch(() => null),
      api.get('/api/v1/tasks').catch(() => null),
      api.get('/api/v1/events?limit=50').catch(() => null),
    ]);
    const snapshot = {
      exported_at: new Date().toISOString(),
      version: '0.5',
      system: sysR?.data || null,
      apps: (Array.isArray(appsR?.data) ? appsR.data : appsR?.data?.apps) || [],
      tasks: tasksR?.data || [],
      events: eventsR?.data || [],
    };
    const blob = new Blob([JSON.stringify(snapshot, null, 2)], { type: 'application/json' });
    const url  = URL.createObjectURL(blob);
    const a    = document.createElement('a');
    a.href = url; a.download = `caleope-snapshot-${new Date().toISOString().slice(0,10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
    notify('Snapshot exporté', 'ok');
  } catch(e) {
    notify('Erreur export', 'err');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="ti ti-download"></i> SNAPSHOT'; }
  }
}

function applyTheme(t) {
  const root = document.documentElement;
  root.style.setProperty('--vio', t.vio);
  root.style.setProperty('--vio-b', t.vioB);
  root.style.setProperty('--vio-dim', t.vioDim);
  root.style.setProperty('--vio-g', t.vioG);
  root.style.setProperty('--accent', t.accent);
  try { localStorage.setItem('caleope-theme', JSON.stringify(t)); } catch(e) {}
  notify(`Thème ${t.name} appliqué`, 'ok');
}

function resetTheme() {
  const root = document.documentElement;
  root.style.removeProperty('--vio');
  root.style.removeProperty('--vio-b');
  root.style.removeProperty('--vio-dim');
  root.style.removeProperty('--vio-g');
  root.style.removeProperty('--accent');
  try { localStorage.removeItem('caleope-theme'); } catch(e) {}
  notify('Thème réinitialisé', 'ok');
}

function loadSavedTheme() {
  try {
    const t = JSON.parse(localStorage.getItem('caleope-theme') || 'null');
    if (t) applyTheme(t);
  } catch(e) {}
  loadSavedMode();
}

function toggleMode() {
  const cur = localStorage.getItem('caleope-mode') || 'dark';
  setMode(cur === 'light' ? 'dark' : 'light');
}

function setMode(mode) {
  document.documentElement.setAttribute('data-theme', mode);
  try { localStorage.setItem('caleope-mode', mode); } catch(e) {}
  const btn = document.getElementById('mode-toggle-btn');
  if (btn) {
    btn.innerHTML = mode === 'light'
      ? '<i class="ti ti-moon"></i> MODE SOMBRE'
      : '<i class="ti ti-sun"></i> MODE CLAIR';
  }
  notify(mode === 'light' ? 'Mode clair activé' : 'Mode sombre activé', 'ok');
}

function loadSavedMode() {
  const mode = localStorage.getItem('caleope-mode') || 'dark';
  document.documentElement.setAttribute('data-theme', mode);
}

function toggleSidebar() {
  const sb = document.getElementById('main-sidebar');
  if (!sb) return;
  const collapsed = sb.classList.toggle('collapsed');
  try { localStorage.setItem('caleope-sidebar-collapsed', collapsed ? '1' : '0'); } catch(e) {}
}

function loadSavedSidebar() {
  const sb = document.getElementById('main-sidebar');
  if (!sb) return;
  if (localStorage.getItem('caleope-sidebar-collapsed') === '1') sb.classList.add('collapsed');
}

async function changePassword() {
  const cur = document.getElementById('pw-current')?.value || '';
  const nw = document.getElementById('pw-new')?.value || '';
  const confirm = document.getElementById('pw-confirm')?.value || '';
  if (!cur || !nw) { notify('Remplissez tous les champs', 'err'); return; }
  if (nw !== confirm) { notify('Les mots de passe ne correspondent pas', 'err'); return; }
  if (nw.length < 8) { notify('Minimum 8 caractères', 'err'); return; }
  const r = await fetch('/auth/password', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current: cur, new: nw }),
  });
  const d = await r.json().catch(() => ({}));
  if (r.ok) {
    notify('Mot de passe changé', 'ok');
    ['pw-current','pw-new','pw-confirm'].forEach(id => { const el = document.getElementById(id); if(el) el.value=''; });
  } else {
    notify(d.error || 'Erreur', 'err');
  }
}

// ── Logo upload ───────────────────────────────────────────────────────────────
async function uploadLogo(input) {
  const file = input.files?.[0];
  if (!file) return;
  if (file.size > 5 * 1024 * 1024) { notify('Fichier trop grand (max 5 Mo)', 'err'); return; }
  const form = new FormData();
  form.append('logo', file);
  notify('Upload en cours...', 'info');
  const r = await fetch('/ui/logo/upload', { method: 'POST', body: form });
  const data = await r.json().catch(() => ({}));
  if (r.ok && data.status === 'ok') {
    notify('Logo mis à jour', 'ok');
    const ts = '?' + Date.now();
    document.querySelectorAll('img.logo-img').forEach(img => {
      img.style.opacity = 1;
      img.src = '/ui/logo' + ts;
    });
  } else {
    notify(data.error || 'Erreur upload logo', 'err');
  }
  input.value = '';
}

async function resetLogo() {
  if (!confirm('Supprimer le logo personnalisé et revenir au SVG par défaut ?')) return;
  const r = await fetch('/ui/logo/reset', { method: 'POST' });
  if (r.ok) {
    notify('Logo réinitialisé', 'ok');
    const ts = '?' + Date.now();
    document.querySelectorAll('img.logo-img').forEach(img => {
      img.style.opacity = 1;
      img.src = '/ui/logo' + ts;
    });
  }
}

async function checkUpgrade() {
  notify('Vérification des mises à jour...', 'info');
  const r = await api.post('/api/v1/upgrade?check=true');
  if (r?.data?.status === 'update_available' || r?.update_available) {
    const latest = r?.data?.latest || r?.latest_version;
    notify(`Mise à jour disponible : ${latest}`, 'ok');
    const btn = document.getElementById('btn-upgrade');
    if (btn) btn.style.display = '';
  } else {
    notify('Caleope est à jour', 'ok');
  }
}

async function runUpgrade() {
  const log = document.getElementById('upgrade-log');
  const btn = document.getElementById('btn-upgrade');
  if (!log) return;
  log.style.display = '';
  log.innerHTML = '';
  if (btn) btn.disabled = true;

  const addLog = (msg) => {
    log.innerHTML += msg + '\n';
    log.scrollTop = log.scrollHeight;
  };

  addLog('▶ Lancement de la mise à jour...');
  notify('Mise à jour en cours...', 'info');

  try {
    const r = await api.post('/api/v1/upgrade');
    const d = r?.data || r || {};
    if (d.status === 'up_to_date') {
      addLog('✓ Déjà à jour : ' + d.version);
      notify('Déjà à jour', 'ok');
      if (btn) btn.disabled = false;
      return;
    }
    addLog('✓ Binaires installés : ' + d.from + ' → ' + d.to);
    addLog('⟳ Redémarrage des services...');
    notify('Redémarrage en cours...', 'info');
  } catch(e) {
    // La connexion peut couper pendant le restart — c'est attendu
    addLog('⟳ Connexion coupée — redémarrage en cours...');
  }

  // Attendre que le daemon revienne, puis recharger
  addLog('⟳ Reconnexion dans quelques secondes...');
  let attempts = 0;
  const poll = setInterval(async () => {
    attempts++;
    try {
      const ping = await fetch('/auth/check', { signal: AbortSignal.timeout(3000) });
      if (ping.status === 200 || ping.status === 401) {
        clearInterval(poll);
        addLog('✓ Service redémarré — rechargement...');
        setTimeout(() => location.reload(), 1000);
      }
    } catch(_) {
      if (attempts < 30) {
        addLog('  · attente (' + attempts + '/30)...');
      }
    }
    if (attempts >= 30) {
      clearInterval(poll);
      addLog('✗ Timeout — vérifier manuellement le service');
      notify('Timeout reconnexion', 'err');
      if (btn) btn.disabled = false;
    }
  }, 2000);
}

async function runUpdate() {
  notify('Synchronisation du store...', 'info');
  const log = document.getElementById('upgrade-log');
  if (log) {
    log.style.display = '';
    log.innerHTML += '▶ Synchronisation du store...\n';
  }
  const r = await api.post('/api/v1/update');
  const msg = r?.data?.message || 'Store synchronisé';
  if (log) log.innerHTML += '✓ ' + msg + '\n';
  notify(msg, 'ok');
}

// ── SECTION: DASHBOARD ────────────────────────────────────────────────────────
// ── Dashboard block customization ─────────────────────────────────────────────
function getDashHiddenBlocks() {
  try { return new Set(JSON.parse(localStorage.getItem('caleope-hidden-blocks') || '[]')); } catch(e) { return new Set(); }
}
function saveDashHiddenBlocks(s) {
  try { localStorage.setItem('caleope-hidden-blocks', JSON.stringify([...s])); } catch(e) {}
}
function _wrapDashBlocks(container) {
  container.querySelectorAll('[id^="dash-"][id$="-widget"]').forEach(widget => {
    const bid = widget.id.slice(5, -7);
    if (widget.closest('.dash-block')) return;
    const wrapper = document.createElement('div');
    wrapper.className = 'dash-block';
    wrapper.dataset.bid = bid;
    const prev = widget.previousElementSibling;
    const parent = widget.parentNode;
    const hideBtn = document.createElement('button');
    hideBtn.className = 'dash-block-hide-btn';
    hideBtn.title = 'Masquer ce bloc';
    hideBtn.innerHTML = '<i class="ti ti-eye-off" style="font-size:10px;pointer-events:none"></i>';
    hideBtn.addEventListener('click', () => toggleDashBlock(bid));
    if (prev && /^\/\//.test(prev.textContent.trim())) {
      parent.insertBefore(wrapper, prev);
      wrapper.appendChild(prev);
    } else {
      parent.insertBefore(wrapper, widget);
    }
    wrapper.appendChild(widget);
    wrapper.appendChild(hideBtn);
  });
}
function applyDashBlocksVisibility() {
  const hidden = getDashHiddenBlocks();
  document.querySelectorAll('.dash-block[data-bid]').forEach(block => {
    block.style.display = hidden.has(block.dataset.bid) ? 'none' : '';
  });
  const count = hidden.size;
  const badge = document.getElementById('dash-hidden-count');
  if (badge) badge.textContent = count > 0 ? ` (${count} masqué${count > 1 ? 's' : ''})` : '';
}
function toggleDashBlock(bid) {
  const hidden = getDashHiddenBlocks();
  if (hidden.has(bid)) hidden.delete(bid);
  else hidden.add(bid);
  saveDashHiddenBlocks(hidden);
  applyDashBlocksVisibility();
}
function toggleDashCustomize() {
  const c = document.getElementById('content-dashboard');
  if (!c) return;
  const isCustom = c.classList.toggle('dash-customize');
  const btn = document.getElementById('dash-customize-btn');
  if (btn) btn.classList.toggle('active', isCustom);
  if (isCustom) notify('Mode personnalisation — cliquez <i class="ti ti-eye-off"></i> pour masquer un bloc', 'info');
}
function resetDashBlocks() {
  saveDashHiddenBlocks(new Set());
  applyDashBlocksVisibility();
  notify('Tous les blocs affichés', 'ok');
}

// ── Dashboard mode simple (Heimdall-style) ────────────────────────────────────
function getDashMode() {
  return localStorage.getItem('caleope-dash-mode') || 'extended';
}
function toggleDashMode() {
  const cur = getDashMode();
  localStorage.setItem('caleope-dash-mode', cur === 'extended' ? 'simple' : 'extended');
  loadDashboard();
}

function _sparkline(values, color, w, h) {
  if (!values || values.length < 2) return '';
  const max = Math.max(...values, 1);
  const pts = values.map((v, i) => {
    const x = Math.round(i / (values.length - 1) * w);
    const y = Math.round(h - (v / max) * (h - 2) - 1);
    return `${x},${y}`;
  }).join(' ');
  return `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" style="display:block;overflow:visible">
    <polyline points="${pts}" fill="none" stroke="${color}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
  </svg>`;
}

async function loadDashSimple() {
  const c = document.getElementById('content-dashboard');
  if (!c) return;

  const ram  = S.stats.mem_total_mb  ? Math.round(S.stats.mem_used_mb  / S.stats.mem_total_mb  * 100) : 0;
  const disk = S.stats.disk_total_gb ? Math.round(S.stats.disk_used_gb / S.stats.disk_total_gb * 100) : 0;
  const cpu  = Math.round(S.stats.cpu_pct || 0);

  // Fetch history for sparklines (best-effort)
  let hist = null;
  try {
    const rh = await fetch('/sys/stats/history');
    if (rh.ok) { const dh = await rh.json(); hist = dh.samples || null; }
  } catch(e) {}

  // Fetch health (best-effort, no await → non-blocking UI)
  let healthMap = {};
  fetch('/sys/healthcheck').then(r => r.ok ? r.json() : null).then(d => {
    if (!d) return;
    (d.containers || []).forEach(c => { healthMap[c.name] = c; });
    // Mettre à jour les badges health après chargement
    document.querySelectorAll('[data-hc]').forEach(el => {
      const h = healthMap[el.dataset.hc];
      if (!h) return;
      const isHealthy = h.health === 'healthy' || (h.health === 'none' && h.status === 'running');
      const isUnhealthy = h.health === 'unhealthy';
      el.innerHTML = isHealthy
        ? `<span style="display:inline-block;width:5px;height:5px;border-radius:50%;background:var(--green-b)" title="Healthy"></span>`
        : isUnhealthy
        ? `<span style="display:inline-block;width:5px;height:5px;border-radius:50%;background:var(--red-b)" title="Unhealthy"></span>`
        : '';
    });
  }).catch(() => {});

  const cpuHist  = hist ? hist.map(s => s.cpu)  : [];
  const ramHist  = hist ? hist.map(s => s.ram)  : [];
  const diskHist = hist ? hist.map(s => s.disk) : [];

  const runApps  = S.apps.filter(a => a.status === 'running');
  const stopApps = S.apps.filter(a => a.status !== 'running');
  const allApps  = [...runApps, ...stopApps];

  const sysCard = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:8px;padding:12px 16px;margin-bottom:16px;display:flex;gap:20px;flex-wrap:wrap;align-items:center">
      ${[
        { label:'CPU',  pct: cpu,  hist: cpuHist,  color:'var(--vio-b)' },
        { label:'RAM',  pct: ram,  hist: ramHist,  color:'var(--blue)' },
        { label:'DISK', pct: disk, hist: diskHist, color: disk > 85 ? 'var(--red-b)' : 'var(--ok)' },
      ].map(m => {
        const col = m.pct > 85 ? 'var(--red-b)' : m.pct > 70 ? 'var(--warn)' : m.color;
        return `<div style="display:flex;align-items:center;gap:10px">
          <div>
            <div style="font-size:8px;color:var(--text3);letter-spacing:1px">${m.label}</div>
            <div style="font-size:18px;font-weight:700;color:${col};line-height:1">${m.pct}%</div>
          </div>
          <div style="opacity:.7">${_sparkline(m.hist, m.color, 60, 24)}</div>
        </div>`;
      }).join('<div style="width:1px;height:30px;background:var(--border)"></div>')}
      <div style="margin-left:auto;font-size:8px;color:var(--text3)">
        <span style="color:var(--green-b);font-weight:700">${runApps.length}</span> apps actives
        · <span style="color:var(--text3)">${stopApps.length}</span> arrêtées
      </div>
    </div>`;

  const tiles = allApps.map(app => {
    const isRun = app.status === 'running';
    const domain = app.domain ? `https://${app.domain}` : null;
    const hasPanel = APP_PANELS[app.id];
    const containerName = app.container_name || app.id;
    const statusColor = isRun ? 'var(--green-b)' : 'var(--red-b)';
    const statusLabel = isRun ? 'EN LIGNE' : (app.status || 'ARRÊTÉ').toUpperCase().slice(0,8);
    return `<div class="dash-tile-simple${isRun ? '' : ' offline'}">
      <div class="dts-icon">${icon(app.id)}</div>
      <div class="dts-name" title="${escapeHtml(app.name || app.id)}">${escapeHtml((app.name || app.id).toUpperCase())}</div>
      <div class="dts-status">
        <span style="display:inline-block;width:5px;height:5px;border-radius:50%;background:${statusColor};flex-shrink:0"></span>
        <span style="font-size:7px;color:${statusColor}">${statusLabel}</span>
        <span data-hc="${escapeHtml(containerName)}" style="margin-left:2px"></span>
      </div>
      <div class="dts-actions">
        ${domain ? `<a href="${domain}" target="_blank" rel="noopener" class="btn-sm" title="Ouvrir ${escapeHtml(app.name||app.id)}" style="font-size:9px;text-decoration:none;flex:1;text-align:center"><i class="ti ti-external-link"></i></a>` : '<span style="flex:1"></span>'}
        ${hasPanel ? `<button class="btn-sm" onclick="goSection('${hasPanel.panels?.[0]?.id||''}')" title="Panel" style="font-size:9px;flex:1;text-align:center"><i class="ti ti-layout-sidebar-right"></i></button>` : ''}
        ${!isRun ? `<button class="btn-sm" style="color:var(--green-b);font-size:9px;flex:0" onclick="appAction('${app.id}','start');setTimeout(loadDashboard,2500)" title="Démarrer"><i class="ti ti-player-play"></i></button>` : ''}
      </div>
    </div>`;
  }).join('');

  c.innerHTML = `
    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
      <div style="font-size:8px;color:var(--text3)">
        <i class="ti ti-refresh" style="font-size:9px"></i>
        ${new Date().toLocaleTimeString('fr-FR',{hour:'2-digit',minute:'2-digit'})}
      </div>
      <div style="display:flex;gap:4px">
        <button class="btn-sm active" id="dash-mode-btn" onclick="toggleDashMode()" title="Mode étendu (widgets détaillés)">
          <i class="ti ti-layout-grid" style="font-size:10px"></i> SIMPLE
        </button>
        <button class="btn-sm" onclick="refreshSection()" title="Actualiser">
          <i class="ti ti-refresh" style="font-size:10px"></i>
        </button>
      </div>
    </div>
    ${sysCard}
    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:0 0 10px">// APPLICATIONS</div>
    <div class="dash-simple-grid">${tiles}</div>
  `;
}

async function loadDashboard() {
  const c = document.getElementById('content-dashboard');
  if (!c) return;

  // Charger les données si pas encore disponibles
  const needsLoad = !S.apps.length && !S.catalog.length;
  if (needsLoad) {
    c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;
    await loadApps();
  }

  // Charger stats + sysinfo si absent
  if (!S.stats.mem_total_mb) await loadStats();

  // Dispatcher selon le mode (simple ou étendu)
  if (getDashMode() === 'simple') {
    await loadDashSimple();
    return;
  }

  const running = S.apps.filter(a => a.status === 'running').length;
  const stopped = S.apps.length - running;
  const ram  = S.stats.mem_total_mb ? Math.round(S.stats.mem_used_mb / S.stats.mem_total_mb * 100) : 0;
  const disk = S.stats.disk_total_gb ? Math.round(S.stats.disk_used_gb / S.stats.disk_total_gb * 100) : 0;

  const shortcuts = [
    { id: 'apps',      icon: 'ti-layout-grid',    label: 'APPLICATIONS', val: `${S.apps.length} installées` },
    { id: 'logs',      icon: 'ti-terminal-2',     label: 'LOGS',         val: 'Temps réel' },
    { id: 'backups',   icon: 'ti-archive',         label: 'SAUVEGARDES',  val: 'Restic SFTP' },
    { id: 'secrets',   icon: 'ti-lock',            label: 'SECRETS',      val: 'AES-256-GCM' },
    { id: 'events',    icon: 'ti-history',         label: 'ÉVÉNEMENTS',   val: 'Historique' },
    { id: 'audit',     icon: 'ti-clipboard-list',  label: 'AUDIT',        val: 'Journal sécurisé' },
    { id: 'stats',     icon: 'ti-chart-bar',       label: 'RESSOURCES',   val: ram ? `RAM ${ram}%` : '—' },
    { id: 'settings',  icon: 'ti-settings',        label: 'PARAMÈTRES',   val: `v${S.stats.version || '—'}` },
  ];

  S._dashRefreshedAt = Date.now();

  // ── Alertes système ──────────────────────────────────────────────────────────
  const alerts = [];
  if (disk > 90)       alerts.push({ lvl: 'err',  msg: `Disque critique : ${disk}% utilisé (${((S.stats.disk_total_gb||0)-(S.stats.disk_used_gb||0)).toFixed(1)} Go libres)`, icon: 'ti-device-floppy' });
  else if (disk > 85)  alerts.push({ lvl: 'warn', msg: `Disque presque plein : ${disk}% utilisé`, icon: 'ti-device-floppy' });
  if (ram > 90)        alerts.push({ lvl: 'err',  msg: `RAM critique : ${ram}% utilisée`, icon: 'ti-cpu' });
  else if (ram > 85)   alerts.push({ lvl: 'warn', msg: `RAM élevée : ${ram}% utilisée`, icon: 'ti-cpu' });
  const failedApps = S.apps.filter(a => a.status === 'error' || a.status === 'failed');
  if (failedApps.length) alerts.push({ lvl: 'err', msg: `${failedApps.length} app(s) en erreur : ${failedApps.map(a=>a.name||a.id).join(', ')}`, icon: 'ti-alert-triangle' });

  const alertsHtml = alerts.length ? `
    <div style="display:flex;flex-direction:column;gap:6px;margin-bottom:16px">
      ${alerts.map(al => `
        <div style="display:flex;align-items:center;gap:8px;padding:8px 12px;border-radius:6px;
            background:${al.lvl==='err'?'rgba(255,80,80,.08)':'rgba(255,180,0,.06)'};
            border:1px solid ${al.lvl==='err'?'var(--red-b)':'var(--warn)'}">
          <i class="ti ${al.icon}" style="color:${al.lvl==='err'?'var(--red-b)':'var(--warn)'};font-size:13px;flex-shrink:0"></i>
          <span style="font-size:9px;font-weight:700;color:${al.lvl==='err'?'var(--red-b)':'var(--warn)'}">${escapeHtml(al.msg)}</span>
        </div>`).join('')}
    </div>` : '';

  c.innerHTML = `
    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
      <div style="font-size:8px;color:var(--text3)">
        <i class="ti ti-refresh" style="font-size:9px"></i>
        Actualisé à <span id="dash-refreshed-time">${new Date().toLocaleTimeString('fr-FR',{hour:'2-digit',minute:'2-digit',second:'2-digit'})}</span>
        · <span id="dash-next-refresh" style="color:var(--accent)">prochain dans 30s</span>
      </div>
      <div style="display:flex;gap:4px;align-items:center">
        <button class="btn-sm" id="dash-mode-btn" onclick="toggleDashMode()" title="Mode simple (vue Heimdall)">
          <i class="ti ti-layout-grid" style="font-size:10px"></i>
        </button>
        <span id="dash-hidden-count" style="font-size:8px;color:var(--text3)"></span>
        <button class="btn-sm" id="dash-customize-btn" onclick="toggleDashCustomize()" title="Personnaliser — masquer/afficher les blocs">
          <i class="ti ti-layout-columns" style="font-size:10px"></i>
        </button>
        <button class="btn-sm" onclick="resetDashBlocks()" title="Réafficher tous les blocs masqués" style="display:none" id="dash-reset-btn">
          <i class="ti ti-eye" style="font-size:10px"></i>
        </button>
        <button class="btn-sm" onclick="refreshSection()" title="Actualiser maintenant">
          <i class="ti ti-refresh" style="font-size:10px"></i>
        </button>
      </div>
    </div>
    <div class="metrics" style="margin-bottom:20px">
      <div class="mc mc-vio" style="cursor:pointer" onclick="goSection('apps')" title="Voir les applications">
        <div class="mc-label">APPS ACTIVES</div>
        <div class="mc-val">${String(running).padStart(2,'0')}</div>
        <div class="mc-sub">${stopped} arrêtée${stopped !== 1 ? 's' : ''}</div>
      </div>
      <div class="mc" style="cursor:pointer" onclick="goSection('apps')" title="Voir le catalogue">
        <div class="mc-label">INSTALLÉES</div>
        <div class="mc-val">${String(S.apps.length).padStart(2,'0')}</div>
        <div class="mc-sub">SUR ${S.catalog.length} DISPO</div>
      </div>
      <div class="mc" style="cursor:pointer" onclick="goSection('stats')" title="Voir les ressources système">
        <div class="mc-label">RAM</div>
        <div class="mc-val ${ram > 85 ? 'mc-err' : ''}">${ram || '—'}${ram ? '%' : ''}</div>
        <div class="mc-sub"><div class="seg-bar" style="margin-top:4px">${segBar(ram, 12, ram > 85 ? 'on-err' : ram > 70 ? 'on-warn' : 'on')}</div></div>
      </div>
      <div class="mc" style="cursor:pointer" onclick="goSection('storage')" title="Voir le stockage">
        <div class="mc-label">DISQUE</div>
        <div class="mc-val ${disk > 85 ? 'mc-err' : ''}">${disk || '—'}${disk ? '%' : ''}</div>
        <div class="mc-sub"><div class="seg-bar" style="margin-top:4px">${segBar(disk, 12, disk > 85 ? 'on-err' : disk > 70 ? 'on-warn' : 'on-ok')}</div></div>
      </div>
    </div>

    ${alertsHtml}

    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin-bottom:10px">// ACCÈS RAPIDE</div>
    <div class="dash-grid">
      ${shortcuts.map(s => `
        <button class="dash-tile" onclick="goSection('${s.id}')">
          <i class="ti ${s.icon} dash-tile-icon"></i>
          <div class="dash-tile-label">${s.label}</div>
          <div class="dash-tile-sub">${s.val}</div>
        </button>
      `).join('')}
    </div>

    ${(() => {
      const downApps = S.apps.filter(a => a.status !== 'running' && a.status !== 'installing');
      if (!downApps.length) return '';
      return `
        <div style="display:flex;align-items:center;gap:8px;margin:20px 0 10px">
          <div style="font-size:9px;color:var(--red-b);letter-spacing:1.5px;font-weight:700;flex:1">
            <i class="ti ti-alert-circle" style="font-size:10px"></i> ALERTES (${downApps.length})
          </div>
          <button class="btn-sm" onclick="startAllStopped()" title="Démarrer toutes les apps arrêtées">
            <i class="ti ti-player-play" style="font-size:10px"></i> TOUT DÉMARRER
          </button>
        </div>
        <div style="display:flex;flex-direction:column;gap:6px;margin-bottom:4px">
          ${downApps.map(a => `
            <div style="display:flex;align-items:center;gap:8px;background:var(--card);border:1px solid var(--border);
                        border-left:3px solid var(--red-b);border-radius:4px;padding:8px 10px">
              <span style="font-size:12px">${icon(a.id)}</span>
              <div style="flex:1">
                <div style="font-size:10px;font-weight:700">${escapeHtml(a.name || a.id)}</div>
                <div style="font-size:8px;color:var(--text3)">${a.status?.toUpperCase() || 'INCONNU'}</div>
              </div>
              <button class="btn-sm" onclick="appAction('${a.id}','start');loadDashboard()" title="Démarrer">
                <i class="ti ti-player-play" style="font-size:10px"></i> DÉMARRER
              </button>
              <button class="btn-sm" onclick="openLogs('${a.id}')" title="Voir les logs">
                <i class="ti ti-terminal-2" style="font-size:10px"></i>
              </button>
            </div>`).join('')}
        </div>`;
    })()}

    ${S.apps.length ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// ÉTAT DES APPLICATIONS</div>
      <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(90px,1fr));gap:5px">
        ${S.apps.map(a => {
          const domain = a.domain ? `https://${a.domain}` : null;
          const isRun = a.status === 'running';
          const dotColor = isRun ? 'var(--green-b)' : (a.status === 'error' ? 'var(--red-b)' : 'var(--text3)');
          return `<div style="background:var(--card);border:1px solid var(--border);border-radius:5px;padding:6px 8px;
                              cursor:pointer;display:flex;flex-direction:column;gap:4px;transition:border-color .15s"
                       onclick="${domain && isRun ? `window.open('${domain}','_blank','noopener')` : `goSection('apps')`}"
                       title="${escapeHtml(a.name||a.id)} — ${(a.status||'inconnu').toUpperCase()}">
            <div style="display:flex;align-items:center;gap:5px">
              <span style="display:inline-block;width:6px;height:6px;border-radius:50%;background:${dotColor};flex-shrink:0;${isRun ? 'box-shadow:0 0 4px '+dotColor : ''}"></span>
              <span style="font-size:9px;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1">${escapeHtml(a.name||a.id)}</span>
            </div>
            <div style="font-size:7px;color:var(--text3);text-transform:uppercase;letter-spacing:.5px">${a.status||'inconnu'}</div>
          </div>`;
        }).join('')}
      </div>
    ` : ''}

    ${S.sysinfo?.hostname ? (() => {
      const sys = S.sysinfo;
      const ram = sys.mem_total ? Math.round(sys.mem_used / sys.mem_total * 100) : 0;
      const ramUsed = sys.mem_total ? (sys.mem_used / 1073741824).toFixed(1) : '—';
      const ramTotal = sys.mem_total ? (sys.mem_total / 1073741824).toFixed(1) : '—';
      const disk = sys.disk_total ? Math.round(sys.disk_used / sys.disk_total * 100) : 0;
      const diskUsed = sys.disk_total ? (sys.disk_used / 1073741824).toFixed(0) : '—';
      const diskTotal = sys.disk_total ? (sys.disk_total / 1073741824).toFixed(0) : '—';
      const ramColor = ram > 90 ? 'var(--red-b)' : ram > 70 ? 'var(--warn)' : 'var(--accent)';
      const diskColor = disk > 90 ? 'var(--red-b)' : disk > 80 ? 'var(--warn)' : 'var(--accent)';
      return `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// HÔTE</div>
      <div class="settings-card" style="padding:10px 12px">
        <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:10px">
          <div><div style="font-size:8px;color:var(--text3)">HOSTNAME</div><div style="font-size:10px;font-weight:700">${escapeHtml(sys.hostname)}</div></div>
          <div><div style="font-size:8px;color:var(--text3)">UPTIME</div><div style="font-size:10px;font-weight:700">${escapeHtml(sys.uptime || '—')}</div></div>
          <div><div style="font-size:8px;color:var(--text3)">CPU</div><div style="font-size:10px;font-weight:700">${sys.cpu_count || '—'} cœur(s)</div></div>
        </div>
        ${sys.mem_total ? `
        <div style="margin-bottom:6px">
          <div style="display:flex;justify-content:space-between;font-size:8px;color:var(--text3);margin-bottom:3px">
            <span>RAM</span><span style="color:${ramColor}">${ramUsed} / ${ramTotal} Go (${ram}%)</span>
          </div>
          <div style="height:4px;background:var(--border);border-radius:2px">
            <div style="height:100%;width:${ram}%;background:${ramColor};border-radius:2px;transition:width .3s"></div>
          </div>
        </div>` : ''}
        ${sys.disk_total ? `
        <div>
          <div style="display:flex;justify-content:space-between;font-size:8px;color:var(--text3);margin-bottom:3px">
            <span>DISQUE</span><span style="color:${diskColor}">${diskUsed} / ${diskTotal} Go (${disk}%)</span>
          </div>
          <div style="height:4px;background:var(--border);border-radius:2px">
            <div style="height:100%;width:${disk}%;background:${diskColor};border-radius:2px;transition:width .3s"></div>
          </div>
        </div>` : ''}
        ${(sys.load_avg_1 !== undefined) ? (() => {
          const l1 = sys.load_avg_1?.toFixed(2) ?? '—';
          const l5 = sys.load_avg_5?.toFixed(2) ?? '—';
          const l15 = sys.load_avg_15?.toFixed(2) ?? '—';
          const cpus = sys.cpu_count || 1;
          const loadColor = sys.load_avg_1 > cpus * 1.5 ? 'var(--red-b)' : sys.load_avg_1 > cpus ? 'var(--warn)' : 'var(--text2)';
          return `<div style="display:flex;justify-content:space-between;align-items:center;margin-top:8px;padding-top:8px;border-top:1px solid var(--border)">
            <span style="font-size:8px;color:var(--text3)">CHARGE</span>
            <span style="font-size:9px;color:${loadColor};font-family:monospace">${l1} · ${l5} · ${l15}</span>
          </div>`;
        })() : ''}
      </div>`;
    })() : ''}

    ${S.apps.some(a => a.id === 'uptime-kuma' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// MONITORING</div>
      <div id="dash-uptime-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${(() => {
      const pins = getPins().map(id => S.apps.find(a => a.id === id)).filter(Boolean);
      if (!pins.length) return '';
      return `
        <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// FAVORIS</div>
        <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:6px">
          ${pins.map(a => {
            const domain = a.domain ? `https://${a.domain}` : null;
            const isRun = a.status === 'running';
            const dotColor = isRun ? 'var(--green-b)' : 'var(--red-b)';
            const hasPanel = APP_PANELS[a.id];
            return `<div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:8px 10px;display:flex;flex-direction:column;gap:6px">
              <div style="display:flex;align-items:center;gap:6px">
                <span style="font-size:15px">${icon(a.id)}</span>
                <div style="flex:1;min-width:0">
                  <div style="font-size:9px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(a.name || a.id)}</div>
                  <div style="display:flex;align-items:center;gap:4px;margin-top:1px">
                    <span style="display:inline-block;width:5px;height:5px;border-radius:50%;background:${dotColor}"></span>
                    <span style="font-size:7px;color:var(--text3)">${isRun ? 'EN LIGNE' : (a.status || 'ARRÊTÉ').toUpperCase()}</span>
                  </div>
                </div>
              </div>
              <div style="display:flex;gap:4px">
                ${domain ? `<a href="${domain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px;flex:1;text-align:center"><i class="ti ti-external-link" style="font-size:9px"></i></a>` : ''}
                ${hasPanel ? `<button class="btn-sm" style="font-size:8px;flex:1" onclick="goSection('${APP_PANELS[a.id].panels[0]?.id}')"><i class="ti ti-layout-sidebar-right" style="font-size:9px"></i></button>` : ''}
                ${!isRun ? `<button class="btn-sm" style="font-size:8px;color:var(--green-b)" onclick="appAction('${a.id}','start');setTimeout(loadDashboard,2000)" title="Démarrer"><i class="ti ti-player-play" style="font-size:9px"></i></button>` : `<button class="btn-sm" style="font-size:8px;color:var(--warn)" onclick="appAction('${a.id}','stop');setTimeout(loadDashboard,2000)" title="Arrêter"><i class="ti ti-player-stop" style="font-size:9px"></i></button>`}
              </div>
            </div>`;
          }).join('')}
        </div>`;
    })()}

    <div id="dash-resources-widget"></div>

    ${S.apps.some(a => a.id === 'jellyfin' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// MÉDIATHÈQUE</div>
      <div id="dash-jellyfin-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'azuracast' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// RADIO / STREAMING</div>
      <div id="dash-azuracast-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'immich' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// PHOTOS</div>
      <div id="dash-immich-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'paperless-ngx' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// DOCUMENTS</div>
      <div id="dash-paperless-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'ntfy' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// NOTIFICATIONS PUSH</div>
      <div id="dash-ntfy-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'pihole' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// DNS / PIHOLE</div>
      <div id="dash-pihole-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'authentik' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// SSO / IDENTITÉ</div>
      <div id="dash-authentik-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'nextcloud' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// CLOUD</div>
      <div id="dash-nextcloud-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'crowdsec' && a.status === 'running') ? `
      <div id="dash-crowdsec-widget"></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'gotify' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// NOTIFICATIONS</div>
      <div id="dash-gotify-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'memos' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// NOTES RÉCENTES</div>
      <div id="dash-memos-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'arr-stack' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// MÉDIAS RÉCENTS</div>
      <div id="dash-arr-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'linkding' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// MARQUE-PAGES</div>
      <div id="dash-linkding-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'vaultwarden' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// GESTIONNAIRE MOTS DE PASSE</div>
      <div id="dash-vaultwarden-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'navidrome' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// MUSIQUE</div>
      <div id="dash-navidrome-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'gitea' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// DÉPÔTS GIT</div>
      <div id="dash-gitea-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'adguard' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// DNS / AD BLOCK</div>
      <div id="dash-adguard-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'freshrss' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// FLUX RSS</div>
      <div id="dash-freshrss-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'grocy' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// STOCKS GROCY</div>
      <div id="dash-grocy-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'changedetection' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// SURVEILLANCE WEB</div>
      <div id="dash-changedetect-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'wg-easy' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// VPN</div>
      <div id="dash-wgeasy-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'wikijs' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// WIKI</div>
      <div id="dash-wikijs-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'komga' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// BD / MANGAS</div>
      <div id="dash-komga-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'scrutiny' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// SANTÉ DISQUES</div>
      <div id="dash-scrutiny-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'jellyseerr' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// DEMANDES MÉDIAS</div>
      <div id="dash-jellyseerr-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'n8n' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// AUTOMATISATION</div>
      <div id="dash-n8n-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'ghost' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// BLOG GHOST</div>
      <div id="dash-ghost-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'wordpress' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// BLOG WORDPRESS</div>
      <div id="dash-wordpress-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'mealie' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// RECETTES</div>
      <div id="dash-mealie-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'syncthing' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// SYNCHRONISATION</div>
      <div id="dash-syncthing-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    ${S.apps.some(a => a.id === 'glpi' && a.status === 'running') ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// TICKETS GLPI</div>
      <div id="dash-glpi-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
    ` : ''}

    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// MISES À JOUR</div>
    <div id="dash-updates-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>

    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// SAUVEGARDES</div>
    <div id="dash-backup-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>

    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// CONTENEURS</div>
    <div id="dash-containers-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>

    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// ÉVÉNEMENTS RÉCENTS</div>
    <div id="dash-events-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>

    <div style="font-size:9px;color:var(--red-b);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// ERREURS SYSTÈME RÉCENTES</div>
    <div id="dash-journal-errors-widget"><div class="dash-loading" style="padding:8px 0"><span class="spinner"></span></div></div>
  `;

  // Personnalisation des blocs — wrap + appliquer les blocs masqués
  _wrapDashBlocks(c);
  applyDashBlocksVisibility();
  const hidden = getDashHiddenBlocks();
  const resetBtn = document.getElementById('dash-reset-btn');
  if (resetBtn) resetBtn.style.display = hidden.size > 0 ? '' : 'none';

  // Charger les widgets async
  if (S.apps.some(a => a.id === 'uptime-kuma' && a.status === 'running')) {
    loadDashUptimeWidget();
  }
  loadDashResourcesWidget();
  if (S.apps.some(a => a.id === 'jellyfin' && a.status === 'running')) {
    loadDashJellyfinWidget();
  }
  if (S.apps.some(a => a.id === 'azuracast' && a.status === 'running')) {
    loadDashAzuracastWidget();
  }
  if (S.apps.some(a => a.id === 'immich' && a.status === 'running')) {
    loadDashImmichWidget();
  }
  if (S.apps.some(a => a.id === 'paperless-ngx' && a.status === 'running')) {
    loadDashPaperlessWidget();
  }
  if (S.apps.some(a => a.id === 'ntfy' && a.status === 'running')) {
    loadDashNtfyWidget();
  }
  if (S.apps.some(a => a.id === 'pihole' && a.status === 'running')) {
    loadDashPiholeWidget();
  }
  if (S.apps.some(a => a.id === 'authentik' && a.status === 'running')) {
    loadDashAuthentikWidget();
  }
  if (S.apps.some(a => a.id === 'nextcloud' && a.status === 'running')) {
    loadDashNextcloudWidget();
  }
  if (S.apps.some(a => a.id === 'crowdsec' && a.status === 'running')) {
    loadDashCrowdSecWidget();
  }
  if (S.apps.some(a => a.id === 'gotify' && a.status === 'running')) {
    loadDashGotifyWidget();
  }
  if (S.apps.some(a => a.id === 'memos' && a.status === 'running')) {
    loadDashMemosWidget();
  }
  if (S.apps.some(a => a.id === 'arr-stack' && a.status === 'running')) {
    loadDashArrWidget();
  }
  if (S.apps.some(a => a.id === 'linkding' && a.status === 'running')) {
    loadDashLinkdingWidget();
  }
  if (S.apps.some(a => a.id === 'vaultwarden' && a.status === 'running')) {
    loadDashVaultwardenWidget();
  }
  if (S.apps.some(a => a.id === 'navidrome' && a.status === 'running')) {
    loadDashNavidromeWidget();
  }
  if (S.apps.some(a => a.id === 'gitea' && a.status === 'running')) {
    loadDashGiteaWidget();
  }
  if (S.apps.some(a => a.id === 'adguard' && a.status === 'running')) {
    loadDashAdGuardWidget();
  }
  if (S.apps.some(a => a.id === 'freshrss' && a.status === 'running')) {
    loadDashFreshRssWidget();
  }
  if (S.apps.some(a => a.id === 'grocy' && a.status === 'running')) {
    loadDashGrocyWidget();
  }
  if (S.apps.some(a => a.id === 'changedetection' && a.status === 'running')) {
    loadDashChangedetectWidget();
  }
  if (S.apps.some(a => a.id === 'wg-easy' && a.status === 'running')) {
    loadDashWgEasyWidget();
  }
  if (S.apps.some(a => a.id === 'wikijs' && a.status === 'running')) {
    loadDashWikiJsWidget();
  }
  if (S.apps.some(a => a.id === 'komga' && a.status === 'running')) {
    loadDashKomgaWidget();
  }
  loadDashUpdatesWidget();
  loadDashBackupWidget();
  if (S.apps.some(a => a.id === 'scrutiny' && a.status === 'running')) {
    loadDashScrutinyWidget();
  }
  if (S.apps.some(a => a.id === 'jellyseerr' && a.status === 'running')) {
    loadDashJellyseerrWidget();
  }
  if (S.apps.some(a => a.id === 'n8n' && a.status === 'running')) {
    loadDashN8nWidget();
  }
  if (S.apps.some(a => a.id === 'ghost' && a.status === 'running')) {
    loadDashGhostWidget();
  }
  if (S.apps.some(a => a.id === 'wordpress' && a.status === 'running')) {
    loadDashWordpressWidget();
  }
  if (S.apps.some(a => a.id === 'mealie' && a.status === 'running')) {
    loadDashMealieWidget();
  }
  if (S.apps.some(a => a.id === 'syncthing' && a.status === 'running')) {
    loadDashSyncthingWidget();
  }
  if (S.apps.some(a => a.id === 'glpi' && a.status === 'running')) {
    loadDashGlpiWidget();
  }
  loadDashContainersWidget();
  loadDashEventsWidget();
  loadDashJournalErrorsWidget();
}

async function loadDashLinkdingWidget() {
  const w = document.getElementById('dash-linkding-widget');
  if (!w) return;
  let bookmarkCount = 0, recentBookmarks = [];
  try {
    const r = await fetch('/ui/proxy/linkding/api/bookmarks/?limit=3');
    if (r.ok) {
      const d = await r.json();
      bookmarkCount = d.count || 0;
      recentBookmarks = d.results || [];
    }
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'linkding')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  const rows = recentBookmarks.slice(0, 3).map(b => `
    <div style="display:flex;align-items:center;gap:6px;padding:4px 6px;background:var(--card);border-radius:4px;border:1px solid var(--border);margin-bottom:3px">
      <i class="ti ti-bookmark" style="font-size:9px;color:var(--vio);flex-shrink:0"></i>
      <div style="flex:1;min-width:0">
        ${b.url ? `<a href="${escapeHtml(b.url)}" target="_blank" rel="noopener" style="font-size:8px;font-weight:600;color:var(--text1);text-decoration:none;display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(b.title || b.url)}</a>`
          : `<div style="font-size:8px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(b.title || '—')}</div>`}
      </div>
    </div>`).join('');

  w.innerHTML = `
    <div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">
      <span style="font-size:13px">🔖</span>
      <span style="font-size:9px;font-weight:700">Linkding</span>
      <span style="font-size:8px;color:var(--text3)">${bookmarkCount} favori(s)</span>
      <div style="margin-left:auto;display:flex;gap:4px">
        ${openBtn}
        <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-linkding-bookmarks')">
          <i class="ti ti-bookmark" style="font-size:9px"></i>
        </button>
      </div>
    </div>
    ${rows || '<div style="font-size:9px;color:var(--text3);text-align:center;padding:6px 0">Aucun marque-page.</div>'}`;
}

async function loadDashVaultwardenWidget() {
  const w = document.getElementById('dash-vaultwarden-widget');
  if (!w) return;
  const appDomain = S.apps.find(a => a.id === 'vaultwarden')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  const adminBtn = appDomain
    ? `<a href="https://${appDomain}/admin" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-settings" style="font-size:9px"></i> ADMIN</a>`
    : '';

  let userCount = null;
  try {
    const r = await fetch('/ui/proxy/vaultwarden/admin/users/overview');
    if (r.ok) {
      const html = await r.text();
      const doc = new DOMParser().parseFromString(html, 'text/html');
      const rows = doc.querySelectorAll('table tbody tr');
      userCount = rows.length;
    }
  } catch(e) {}

  const userStr = userCount === null ? '—' : String(userCount);

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px">
        <span style="font-size:13px">🔐</span>
        <span style="font-size:9px;font-weight:700">Vaultwarden</span>
        <div style="margin-left:auto;display:flex;align-items:center;gap:8px">
          ${userCount !== null ? `<div style="text-align:center"><div style="font-size:18px;font-weight:800;color:var(--accent)">${userStr}</div><div style="font-size:7px;color:var(--text3);letter-spacing:.5px">COMPTES</div></div>` : ''}
          <div style="display:flex;gap:4px">
            ${openBtn}
            ${adminBtn}
          </div>
        </div>
      </div>
    </div>`;
}

async function loadDashWgEasyWidget() {
  const w = document.getElementById('dash-wgeasy-widget');
  if (!w) return;
  let clients = null;
  try {
    const r = await fetch('/ui/proxy/wg-easy/api/wireguard/client');
    if (r.ok) clients = await r.json();
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'wg-easy')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  if (!Array.isArray(clients)) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <i class="ti ti-vpn" style="color:var(--text3);font-size:13px"></i>
      <span style="font-size:9px;color:var(--text3)">WG-Easy indisponible</span>
      <div style="margin-left:auto">${openBtn}</div>
    </div>`;
    return;
  }

  const connected = clients.filter(c => c.endpoint && c.latestHandshakeAt && (Date.now() - new Date(c.latestHandshakeAt).getTime()) < 3 * 60000);
  const enabled = clients.filter(c => c.enabled !== false);

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:10px">
        <i class="ti ti-shield-lock" style="font-size:13px;color:var(--vio)"></i>
        <span style="font-size:9px;font-weight:700">WireGuard VPN</span>
        <div style="margin-left:auto;display:flex;gap:4px">
          ${openBtn}
          <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-wgeasy-peers')">
            <i class="ti ti-vpn" style="font-size:9px"></i>
          </button>
        </div>
      </div>
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:4px;text-align:center">
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:${connected.length > 0 ? 'var(--green-b)' : 'var(--text3)'}">${connected.length}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">CONNECTÉS</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:var(--accent)">${enabled.length}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">ACTIVÉS</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:var(--text2)">${clients.length}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">TOTAL</div>
        </div>
      </div>
    </div>`;
}

async function loadDashWikiJsWidget() {
  const w = document.getElementById('dash-wikijs-widget');
  if (!w) return;
  const appDomain = S.apps.find(a => a.id === 'wikijs')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  let pageCount = null;
  try {
    const r = await fetch('/ui/proxy/wikijs/graphql', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: '{ pages { list(orderBy: UPDATED) { id title updatedAt } } }' }),
    });
    if (r.ok) { const d = await r.json(); pageCount = d?.data?.pages?.list?.length ?? null; }
  } catch(e) {}

  const pageStr = pageCount === null ? '—' : String(pageCount);

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px">
        <span style="font-size:13px">📖</span>
        <span style="font-size:9px;font-weight:700">Wiki.js</span>
        <div style="margin-left:auto;display:flex;align-items:center;gap:8px">
          <span style="font-size:20px;font-weight:800;color:var(--accent)">${escapeHtml(pageStr)}</span>
          <span style="font-size:7px;color:var(--text3);letter-spacing:.5px">PAGES</span>
          <div style="display:flex;gap:4px">
            ${openBtn}
            <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-wikijs-pages')">
              <i class="ti ti-file-text" style="font-size:9px"></i>
            </button>
          </div>
        </div>
      </div>
    </div>`;
}

async function loadDashKomgaWidget() {
  const w = document.getElementById('dash-komga-widget');
  if (!w) return;
  let seriesCount = 0, bookCount = 0;
  try {
    const [rs, rb] = await Promise.all([
      fetch('/ui/proxy/komga/api/v1/series?page=0&size=0'),
      fetch('/ui/proxy/komga/api/v1/books?page=0&size=0'),
    ]);
    if (rs.ok) { const d = await rs.json(); seriesCount = d?.totalElements || 0; }
    if (rb.ok) { const d = await rb.json(); bookCount = d?.totalElements || 0; }
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'komga')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:10px">
        <span style="font-size:13px">📔</span>
        <span style="font-size:9px;font-weight:700">Komga</span>
        <div style="margin-left:auto;display:flex;gap:4px">
          ${openBtn}
          <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-komga-library')">
            <i class="ti ti-books" style="font-size:9px"></i>
          </button>
        </div>
      </div>
      <div style="display:grid;grid-template-columns:repeat(2,1fr);gap:4px;text-align:center">
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:var(--accent)">${seriesCount}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">SÉRIES</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:var(--accent)">${bookCount}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">VOLUMES</div>
        </div>
      </div>
    </div>`;
}

async function loadDashGrocyWidget() {
  const w = document.getElementById('dash-grocy-widget');
  if (!w) return;
  let stock = null, tasks = null;
  try {
    const [rs, rt] = await Promise.all([
      fetch('/ui/proxy/grocy/api/stock'),
      fetch('/ui/proxy/grocy/api/tasks?done=0'),
    ]);
    if (rs.ok) stock = await rs.json();
    if (rt.ok) tasks = await rt.json();
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'grocy')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  if (!stock) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <span style="font-size:13px">🛒</span>
      <span style="font-size:9px;color:var(--text3)">Grocy indisponible</span>
      <div style="margin-left:auto">${openBtn}</div>
    </div>`;
    return;
  }

  const lowStock = Array.isArray(stock) ? stock.filter(p => {
    if (!p.min_stock_amount) return false;
    return parseFloat(p.amount || 0) <= parseFloat(p.min_stock_amount);
  }) : [];
  const dueSoon = Array.isArray(stock) ? stock.filter(p =>
    p.best_before_date && new Date(p.best_before_date) < new Date(Date.now() + 7 * 86400000)
  ) : [];
  const pendingTasks = Array.isArray(tasks) ? tasks.length : 0;

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:10px">
        <span style="font-size:13px">🛒</span>
        <span style="font-size:9px;font-weight:700">Grocy</span>
        <div style="margin-left:auto;display:flex;gap:4px">
          ${openBtn}
          <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-grocy-stock')">
            <i class="ti ti-shopping-cart" style="font-size:9px"></i>
          </button>
        </div>
      </div>
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:4px;text-align:center">
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:${lowStock.length > 0 ? 'var(--warn)' : 'var(--text3)'}">${lowStock.length}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">STOCK BAS</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:${dueSoon.length > 0 ? 'var(--red-b)' : 'var(--text3)'}">${dueSoon.length}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">EXPIRE BIENTÔT</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:${pendingTasks > 0 ? 'var(--blue)' : 'var(--text3)'}">${pendingTasks}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">TÂCHES</div>
        </div>
      </div>
    </div>`;
}

async function loadDashChangedetectWidget() {
  const w = document.getElementById('dash-changedetect-widget');
  if (!w) return;
  let watches = null;
  try {
    const r = await fetch('/ui/proxy/changedetection/api/v1/watch?limit=50', { headers: { Accept: 'application/json' } });
    if (r.ok) { const d = await r.json(); watches = Array.isArray(d) ? d : Object.values(d); }
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'changedetection')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  if (!watches) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <i class="ti ti-eye-off" style="color:var(--text3);font-size:13px"></i>
      <span style="font-size:9px;color:var(--text3)">Changedetection indisponible</span>
      <div style="margin-left:auto">${openBtn}</div>
    </div>`;
    return;
  }

  const changed = watches.filter(w => w.last_changed && w.last_changed > 0).length;
  const recent = watches.filter(w => w.last_changed && w.last_changed > 0)
    .sort((a, b) => b.last_changed - a.last_changed).slice(0, 3);

  const rows = recent.length === 0
    ? `<div style="font-size:9px;color:var(--text3);text-align:center;padding:8px 0">Aucune modification détectée.</div>`
    : recent.map(r => {
        const title = r.title || r.url || '—';
        const dt = r.last_changed ? new Date(r.last_changed * 1000).toLocaleDateString('fr-FR') : '';
        return `<div style="padding:5px 8px;background:var(--card);border-radius:5px;border:1px solid var(--border);border-left:3px solid var(--warn);margin-bottom:4px">
          <div style="font-size:9px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(title)}</div>
          <div style="font-size:7px;color:var(--text3)">${escapeHtml(dt)}</div>
        </div>`;
      }).join('');

  w.innerHTML = `
    <div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">
      <span style="font-size:9px;color:var(--text3)">${watches.length} surveillance(s) · ${changed} modifiée(s)</span>
      <div style="margin-left:auto;display:flex;gap:4px">
        ${openBtn}
        <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-changedetection-watches')">
          <i class="ti ti-eye" style="font-size:9px"></i>
        </button>
      </div>
    </div>
    ${rows}`;
}

async function loadDashAdGuardWidget() {
  const w = document.getElementById('dash-adguard-widget');
  if (!w) return;
  let stats = null, status = null;
  try {
    const [rs, rp] = await Promise.all([
      fetch('/ui/proxy/adguard/control/stats'),
      fetch('/ui/proxy/adguard/control/status'),
    ]);
    if (rs.ok) stats = await rs.json();
    if (rp.ok) status = await rp.json();
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'adguard')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  if (!stats) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <i class="ti ti-shield-off" style="color:var(--text3);font-size:13px"></i>
      <span style="font-size:9px;color:var(--text3)">AdGuard indisponible</span>
      <div style="margin-left:auto">${openBtn}</div>
    </div>`;
    return;
  }

  const total = stats.num_dns_queries || 0;
  const blocked = stats.num_blocked_filtering || 0;
  const pct = total > 0 ? ((blocked / total) * 100).toFixed(1) : '0';
  const protActive = status?.protection_enabled;
  const statusColor = protActive === false ? 'var(--red-b)' : 'var(--green-b)';

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:10px">
        <span style="width:7px;height:7px;border-radius:50%;background:${statusColor};flex-shrink:0"></span>
        <span style="font-size:9px;font-weight:700">AdGuard Home</span>
        <div style="margin-left:auto;display:flex;gap:4px">
          ${openBtn}
          <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-adguard-stats')">
            <i class="ti ti-chart-bar" style="font-size:9px"></i>
          </button>
        </div>
      </div>
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:4px;text-align:center">
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:13px;font-weight:800;color:var(--text1)">${total > 999 ? (total/1000).toFixed(0)+'k' : total}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">REQUÊTES</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:13px;font-weight:800;color:var(--red-b)">${blocked > 999 ? (blocked/1000).toFixed(0)+'k' : blocked}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">BLOQUÉES</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:13px;font-weight:800;color:var(--warn)">${pct}%</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">TAUX BLOQ.</div>
        </div>
      </div>
    </div>`;
}

async function loadDashFreshRssWidget() {
  const w = document.getElementById('dash-freshrss-widget');
  if (!w) return;
  let unreadCount = null;
  try {
    const r = await fetch('/ui/proxy/freshrss/api/greader.php/reader/api/0/unread-count?output=json');
    if (r.ok) {
      const d = await r.json();
      unreadCount = d.max || (d.unreadcounts || []).reduce((s, x) => s + (x.count || 0), 0);
    }
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'freshrss')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  const unreadStr = unreadCount === null ? '—' : String(unreadCount);
  const unreadColor = unreadCount > 50 ? 'var(--warn)' : unreadCount > 0 ? 'var(--blue)' : 'var(--text3)';

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px">
        <span style="font-size:13px">📰</span>
        <span style="font-size:9px;font-weight:700">FreshRSS</span>
        <div style="margin-left:auto;display:flex;align-items:center;gap:8px">
          <span style="font-size:18px;font-weight:800;color:${unreadColor}">${escapeHtml(unreadStr)}</span>
          <span style="font-size:7px;color:var(--text3);letter-spacing:.5px">NON LUS</span>
          <div style="display:flex;gap:4px">
            ${openBtn}
            <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-freshrss-articles')">
              <i class="ti ti-rss" style="font-size:9px"></i>
            </button>
          </div>
        </div>
      </div>
    </div>`;
}

// ── Update checker widget ─────────────────────────────────────────────────────

async function loadDashUpdatesWidget() {
  const w = document.getElementById('dash-updates-widget');
  if (!w) return;
  let containers = [];
  try {
    const r = await fetch('/sys/update-check');
    if (r.ok) { const d = await r.json(); containers = d.containers || []; }
  } catch(e) {}

  const unhealthy = [];
  try {
    const rh = await fetch('/sys/healthcheck');
    if (rh.ok) {
      const dh = await rh.json();
      (dh.containers || []).forEach(c => {
        if (c.health === 'unhealthy') unhealthy.push(c.name);
      });
    }
  } catch(e) {}

  const total = containers.length;
  const withLatest = containers.filter(c => c.local_tag === 'latest').length;
  const pinned     = total - withLatest;

  const unhealthyBanner = unhealthy.length ? `
    <div style="display:flex;align-items:center;gap:6px;padding:5px 8px;background:rgba(239,68,68,.08);border:1px solid var(--red-b);border-radius:4px;margin-bottom:6px">
      <i class="ti ti-alert-triangle" style="font-size:11px;color:var(--red-b);flex-shrink:0"></i>
      <span style="font-size:8px;color:var(--red-b);font-weight:700">${unhealthy.length} CONTENEUR(S) UNHEALTHY : ${unhealthy.slice(0,3).join(', ')}${unhealthy.length>3?'…':''}</span>
    </div>` : '';

  const rows = containers.slice(0, 8).map(c => {
    const tagColor = c.local_tag === 'latest' ? 'var(--text3)' : 'var(--blue)';
    return `<div style="display:flex;align-items:center;gap:6px;padding:3px 0;border-bottom:1px solid var(--border)">
      <i class="ti ti-brand-docker" style="font-size:9px;color:var(--text3);flex-shrink:0"></i>
      <span style="font-size:8px;color:var(--text1);flex:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(c.container)}</span>
      <span style="font-size:7px;font-family:monospace;color:${tagColor};flex-shrink:0">${escapeHtml(c.local_image.split('/').pop())}:<b>${escapeHtml(c.local_tag)}</b></span>
      <button class="btn-sm" style="font-size:7px;padding:1px 5px" title="Tirer la dernière version"
        onclick="pullImage('${escapeHtml(c.local_image+':'+c.local_tag)}','${escapeHtml(c.container)}')">
        <i class="ti ti-download" style="font-size:8px"></i>
      </button>
    </div>`;
  }).join('');

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;overflow:hidden">
      <div style="display:flex;align-items:center;justify-content:space-between;padding:6px 10px;border-bottom:1px solid var(--border)">
        <span style="font-size:8px;font-weight:700;color:var(--text2)"><i class="ti ti-versions" style="font-size:9px;margin-right:4px"></i>${total} IMAGE(S) · ${pinned} ÉPINGLÉE(S)</span>
        <button class="btn-sm" style="font-size:7px" onclick="goSection('stats')" title="Stats Docker">
          <i class="ti ti-external-link" style="font-size:8px"></i>
        </button>
      </div>
      <div style="padding:6px 10px">
        ${unhealthyBanner}
        ${rows || '<div style="font-size:8px;color:var(--text3);padding:4px 0">Aucun conteneur détecté.</div>'}
        ${containers.length > 8 ? `<div style="font-size:7px;color:var(--text3);text-align:center;margin-top:4px">+${containers.length-8} autres</div>` : ''}
      </div>
    </div>`;
}

async function pullImage(image, container) {
  if (!confirm(`Tirer la dernière version de "${image}" ?\nLe service peut redémarrer si vous recréez le conteneur après.`)) return;
  notify(`Pull de ${image} en cours…`, 'info');
  try {
    const r = await fetch('/sys/docker-pull', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ image, container }),
    });
    const d = await r.json().catch(() => ({}));
    if (d.success) {
      notify(`Image ${image} mise à jour`, 'ok');
    } else {
      notify(`Erreur pull : ${d.error || 'inconnue'}`, 'err');
    }
  } catch(e) {
    notify('Erreur réseau lors du pull', 'err');
  }
}

// ── Backup status widget ──────────────────────────────────────────────────────

async function loadDashBackupWidget() {
  const w = document.getElementById('dash-backup-widget');
  if (!w) return;
  let data = null;
  try {
    const r = await fetch('/sys/backup-status');
    if (r.ok) data = await r.json();
  } catch(e) {}

  // Aussi chercher les tâches planifiées depuis le daemon
  let tasks = [];
  try {
    const rt = await api.get('/api/v1/tasks');
    tasks = (rt?.data || rt || []).filter(t => t.type === 'backup');
  } catch(e) {}

  const taskInfo = tasks.length ? `
    <div style="font-size:8px;color:var(--text3);margin-top:4px">
      <i class="ti ti-calendar-event" style="font-size:9px;margin-right:3px"></i>
      ${tasks.length} tâche(s) planifiée(s)
    </div>` : '';

  if (!data || !data.configured) {
    const reason = data?.reason;
    const msg = reason === 'no_restic' ? 'restic non installé' :
                reason === 'no_repo'   ? 'Aucun dépôt configuré' :
                'Sauvegardes non configurées';
    w.innerHTML = `
      <div style="display:flex;align-items:center;gap:8px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
        <i class="ti ti-archive-off" style="font-size:13px;color:var(--text3);flex-shrink:0"></i>
        <div style="flex:1">
          <div style="font-size:9px;color:var(--text3)">${msg}</div>
          ${taskInfo}
        </div>
        <button class="btn-sm" onclick="goSection('backups')"><i class="ti ti-external-link" style="font-size:9px"></i></button>
      </div>`;
    return;
  }

  const lastTime = data.last_time ? new Date(data.last_time) : null;
  const timeAgo = lastTime ? _timeAgo(lastTime) : '—';
  const paths = (data.last_paths || []).slice(0, 2).map(p => p.split('/').pop()).join(', ') || '—';

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;overflow:hidden">
      <div style="display:flex;align-items:center;justify-content:space-between;padding:6px 10px;border-bottom:1px solid var(--border)">
        <span style="font-size:8px;font-weight:700;color:var(--text2)"><i class="ti ti-archive" style="font-size:9px;margin-right:4px"></i>SAUVEGARDES RESTIC</span>
        <button class="btn-sm" style="font-size:7px" onclick="goSection('backups')"><i class="ti ti-external-link" style="font-size:8px"></i></button>
      </div>
      <div style="padding:6px 10px;display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px;text-align:center">
        <div>
          <div style="font-size:15px;font-weight:700;color:var(--ok)">${data.snapshots || 0}</div>
          <div style="font-size:7px;color:var(--text3)">SNAPSHOTS</div>
        </div>
        <div>
          <div style="font-size:11px;font-weight:700;color:var(--text1)">${timeAgo}</div>
          <div style="font-size:7px;color:var(--text3)">DERNIÈRE SAUVEG.</div>
        </div>
        <div>
          <div style="font-size:9px;font-weight:700;color:var(--blue);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(paths)}</div>
          <div style="font-size:7px;color:var(--text3)">CHEMINS</div>
        </div>
      </div>
      ${taskInfo ? `<div style="padding:0 10px 6px">${taskInfo}</div>` : ''}
    </div>`;
}

function _timeAgo(date) {
  const diff = Math.floor((Date.now() - date.getTime()) / 1000);
  if (diff < 60) return 'à l\'instant';
  if (diff < 3600) return `il y a ${Math.floor(diff/60)} min`;
  if (diff < 86400) return `il y a ${Math.floor(diff/3600)} h`;
  return `il y a ${Math.floor(diff/86400)} j`;
}

async function loadDashScrutinyWidget() {
  const w = document.getElementById('dash-scrutiny-widget');
  if (!w) return;
  let devices = [];
  try {
    const r = await fetch('/ui/proxy/scrutiny/api/summary');
    if (r.ok) {
      const d = await r.json();
      devices = Object.values(d?.data?.Devices || {});
    }
  } catch(e) {}
  const failed = devices.filter(dev => dev.device?.smart_status === 'failed').length;
  const passed = devices.filter(dev => dev.device?.smart_status === 'passed').length;
  const unknown = devices.length - failed - passed;
  const appDomain = S.apps.find(a => a.id === 'scrutiny')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-device-desktop-analytics" style="font-size:10px;margin-right:4px"></i>SCRUTINY — ${devices.length} DISQUE${devices.length !== 1 ? 'S' : ''}</span>
        ${openBtn}
      </div>
      ${devices.length === 0 ? `<div style="color:var(--text3);font-size:9px">Aucun disque scanné</div>` : `
      <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px">
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--ok)">${passed}</div>
          <div style="font-size:8px;color:var(--text3)">OK</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--err)">${failed}</div>
          <div style="font-size:8px;color:var(--text3)">ÉCHEC</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--text3)">${unknown}</div>
          <div style="font-size:8px;color:var(--text3)">INCONNU</div>
        </div>
      </div>`}
    </div>`;
}

async function loadDashJellyseerrWidget() {
  const w = document.getElementById('dash-jellyseerr-widget');
  if (!w) return;
  let pending = 0, approved = 0, available = 0, version = '';
  try {
    const [rStatus, rCount] = await Promise.all([
      fetch('/ui/proxy/jellyseerr/api/v1/status'),
      fetch('/ui/proxy/jellyseerr/api/v1/request/count')
    ]);
    if (rStatus.ok) { const d = await rStatus.json(); version = d.version || ''; }
    if (rCount.ok) { const d = await rCount.json(); pending = d.pending || 0; approved = d.approved || 0; available = d.available || 0; }
    else if (rCount.status === 401 || rCount.status === 403) {
      w.innerHTML = `<div style="color:var(--text3);font-size:9px;padding:4px 0">API key non configurée dans Jellyseerr</div>`;
      return;
    }
  } catch(e) {}
  const appDomain = S.apps.find(a => a.id === 'jellyseerr')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-movie" style="font-size:10px;margin-right:4px"></i>JELLYSEERR${version ? ' v'+version : ''}</span>
        ${openBtn}
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px">
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:${pending > 0 ? 'var(--warn)' : 'var(--text3)'}">${pending}</div>
          <div style="font-size:8px;color:var(--text3)">EN ATTENTE</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--blue)">${approved}</div>
          <div style="font-size:8px;color:var(--text3)">APPROUVÉES</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--ok)">${available}</div>
          <div style="font-size:8px;color:var(--text3)">DISPONIBLES</div>
        </div>
      </div>
    </div>`;
}

async function loadDashN8nWidget() {
  const w = document.getElementById('dash-n8n-widget');
  if (!w) return;
  let workflows = [], active = 0;
  try {
    const r = await fetch('/ui/proxy/n8n/api/v1/workflows?active=true&limit=250');
    if (r.ok) {
      const d = await r.json();
      workflows = d.data || [];
      active = workflows.filter(wf => wf.active).length;
    }
  } catch(e) {}
  const health = await fetch('/ui/proxy/n8n/healthz').then(r => r.ok).catch(() => false);
  const appDomain = S.apps.find(a => a.id === 'n8n')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-arrows-shuffle" style="font-size:10px;margin-right:4px"></i>N8N — AUTOMATION</span>
        ${openBtn}
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:6px">
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--blue)">${workflows.length || '—'}</div>
          <div style="font-size:8px;color:var(--text3)">WORKFLOWS</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--ok)">${active || '—'}</div>
          <div style="font-size:8px;color:var(--text3)">ACTIFS</div>
        </div>
      </div>
      <div style="margin-top:6px;font-size:8px;color:${health ? 'var(--ok)' : 'var(--err)'}">
        <i class="ti ti-circle-filled" style="font-size:6px"></i> ${health ? 'Opérationnel' : 'Hors ligne'}
      </div>
    </div>`;
}

async function loadDashGhostWidget() {
  const w = document.getElementById('dash-ghost-widget');
  if (!w) return;
  let postCount = 0, memberCount = 0, title = '';
  try {
    const [rPosts, rSite] = await Promise.all([
      fetch('/ui/proxy/ghost/ghost/api/admin/posts/?limit=1&fields=id'),
      fetch('/ui/proxy/ghost/ghost/api/admin/site/')
    ]);
    if (rPosts.ok) { const d = await rPosts.json(); postCount = d.meta?.pagination?.total || 0; }
    if (rSite.ok) { const d = await rSite.json(); title = d.site?.title || ''; }
  } catch(e) {}
  const appDomain = S.apps.find(a => a.id === 'ghost')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-ghost" style="font-size:10px;margin-right:4px"></i>GHOST${title ? ' — '+title.substring(0,20) : ''}</span>
        ${openBtn}
      </div>
      <div style="display:flex;align-items:center;gap:16px">
        <div style="text-align:center">
          <div style="font-size:24px;font-weight:700;color:var(--blue)">${postCount}</div>
          <div style="font-size:8px;color:var(--text3)">ARTICLES</div>
        </div>
      </div>
    </div>`;
}

async function loadDashWordpressWidget() {
  const w = document.getElementById('dash-wordpress-widget');
  if (!w) return;
  let postCount = 0, pageCount = 0, commentCount = 0;
  try {
    const [rPosts, rPages, rComments] = await Promise.all([
      fetch('/ui/proxy/wordpress/wp-json/wp/v2/posts?per_page=1&status=publish'),
      fetch('/ui/proxy/wordpress/wp-json/wp/v2/pages?per_page=1'),
      fetch('/ui/proxy/wordpress/wp-json/wp/v2/comments?per_page=1&status=approve')
    ]);
    if (rPosts.ok) postCount = parseInt(rPosts.headers.get('X-WP-Total') || '0', 10);
    if (rPages.ok) pageCount = parseInt(rPages.headers.get('X-WP-Total') || '0', 10);
    if (rComments.ok) commentCount = parseInt(rComments.headers.get('X-WP-Total') || '0', 10);
  } catch(e) {}
  const appDomain = S.apps.find(a => a.id === 'wordpress')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-brand-wordpress" style="font-size:10px;margin-right:4px"></i>WORDPRESS</span>
        ${openBtn}
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px">
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--blue)">${postCount}</div>
          <div style="font-size:8px;color:var(--text3)">ARTICLES</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--text2)">${pageCount}</div>
          <div style="font-size:8px;color:var(--text3)">PAGES</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--text2)">${commentCount}</div>
          <div style="font-size:8px;color:var(--text3)">COMMENTAIRES</div>
        </div>
      </div>
    </div>`;
}

async function loadDashMealieWidget() {
  const w = document.getElementById('dash-mealie-widget');
  if (!w) return;
  let version = '', recipeCount = 0, userCount = 0;
  try {
    const rAbout = await fetch('/ui/proxy/mealie/api/app/about');
    if (rAbout.ok) { const d = await rAbout.json(); version = d.version || ''; }
    const rStats = await fetch('/ui/proxy/mealie/api/recipes/summary/untagged?page=1&perPage=1');
    if (rStats.ok) {
      const rAll = await fetch('/ui/proxy/mealie/api/recipes?page=1&perPage=1');
      if (rAll.ok) { const d = await rAll.json(); recipeCount = d.total || 0; }
    }
  } catch(e) {}
  const appDomain = S.apps.find(a => a.id === 'mealie')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-salad" style="font-size:10px;margin-right:4px"></i>MEALIE${version ? ' v'+version : ''}</span>
        ${openBtn}
      </div>
      <div style="display:flex;align-items:center;gap:16px">
        <div style="text-align:center">
          <div style="font-size:24px;font-weight:700;color:var(--blue)">${recipeCount || '—'}</div>
          <div style="font-size:8px;color:var(--text3)">RECETTES</div>
        </div>
      </div>
    </div>`;
}

async function loadDashSyncthingWidget() {
  const w = document.getElementById('dash-syncthing-widget');
  if (!w) return;
  let folders = [], connected = 0;
  try {
    const [rFolders, rConnections] = await Promise.all([
      fetch('/ui/proxy/syncthing/rest/config/folders'),
      fetch('/ui/proxy/syncthing/rest/system/connections')
    ]);
    if (rFolders.ok) folders = await rFolders.json();
    if (rConnections.ok) {
      const d = await rConnections.json();
      connected = Object.values(d.connections || {}).filter(c => c.connected).length;
    }
  } catch(e) {}
  const appDomain = S.apps.find(a => a.id === 'syncthing')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-refresh" style="font-size:10px;margin-right:4px"></i>SYNCTHING</span>
        ${openBtn}
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:6px">
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--blue)">${folders.length || '—'}</div>
          <div style="font-size:8px;color:var(--text3)">DOSSIERS</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:${connected > 0 ? 'var(--ok)' : 'var(--text3)'}">${connected}</div>
          <div style="font-size:8px;color:var(--text3)">APPAREILS CONNECTÉS</div>
        </div>
      </div>
    </div>`;
}

async function loadDashGlpiWidget() {
  const w = document.getElementById('dash-glpi-widget');
  if (!w) return;
  let newTickets = 0, inProgress = 0, pendingCount = 0;
  try {
    const rTickets = await fetch('/ui/proxy/glpi/apirest.php/Ticket?searchText[status]=1,2,3&range=0-1');
    if (rTickets.ok) {
      const d = await rTickets.json();
      if (Array.isArray(d)) {
        newTickets = d.filter(t => t.status === 1).length;
        inProgress = d.filter(t => t.status === 2).length;
        pendingCount = d.filter(t => t.status === 3).length;
      }
    }
  } catch(e) {}
  const appDomain = S.apps.find(a => a.id === 'glpi')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';
  w.innerHTML = `
    <div class="card" style="padding:10px 12px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <span style="font-size:9px;font-weight:700;color:var(--text2)"><i class="ti ti-ticket" style="font-size:10px;margin-right:4px"></i>GLPI — TICKETS</span>
        ${openBtn}
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px">
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--warn)">${newTickets}</div>
          <div style="font-size:8px;color:var(--text3)">NOUVEAUX</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--blue)">${inProgress}</div>
          <div style="font-size:8px;color:var(--text3)">EN COURS</div>
        </div>
        <div style="text-align:center">
          <div style="font-size:18px;font-weight:700;color:var(--text3)">${pendingCount}</div>
          <div style="font-size:8px;color:var(--text3)">EN ATTENTE</div>
        </div>
      </div>
    </div>`;
}

async function loadDashContainersWidget() {
  const w = document.getElementById('dash-containers-widget');
  if (!w) return;
  let stats = null;
  try {
    const r = await fetch('/sys/docker-stats');
    if (r.ok) { const d = await r.json(); stats = d.stats || []; }
  } catch(e) {}

  if (!stats || !stats.length) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <i class="ti ti-brand-docker" style="color:var(--text3);font-size:13px"></i>
      <span style="font-size:9px;color:var(--text3)">Aucun conteneur actif</span>
    </div>`;
    return;
  }

  const parseMem = s => {
    if (!s) return 0;
    const m = s.match(/([\d.]+)\s*(GiB|MiB|KiB|GB|MB|KB)/i);
    if (!m) return 0;
    const v = parseFloat(m[1]);
    const u = m[2].toUpperCase();
    if (u.startsWith('G')) return v * 1024;
    if (u.startsWith('M')) return v;
    if (u.startsWith('K')) return v / 1024;
    return v;
  };
  const parseCpu = s => parseFloat(s?.replace('%','') || '0');

  const sorted = [...stats].sort((a, b) => parseMem(b.mem?.split('/')[0]) - parseMem(a.mem?.split('/')[0])).slice(0, 6);

  const rows = sorted.map(s => {
    const cpu = parseCpu(s.cpu);
    const memUsed = s.mem?.split('/')[0]?.trim() || '—';
    const cpuColor = cpu > 80 ? 'var(--red-b)' : cpu > 50 ? 'var(--warn)' : 'var(--text2)';
    const name = s.name.replace(/^\//, '');
    return `<tr style="border-bottom:1px solid var(--border)">
      <td style="padding:4px 6px;font-size:8px;color:var(--text1);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:120px">${escapeHtml(name)}</td>
      <td style="padding:4px 6px;font-size:8px;color:${cpuColor};font-family:monospace;text-align:right">${s.cpu || '—'}</td>
      <td style="padding:4px 6px;font-size:8px;color:var(--text2);font-family:monospace;text-align:right">${escapeHtml(memUsed)}</td>
    </tr>`;
  }).join('');

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;overflow:hidden">
      <div style="display:flex;align-items:center;justify-content:space-between;padding:6px 10px;border-bottom:1px solid var(--border)">
        <span style="font-size:8px;color:var(--text3)">${stats.length} conteneur(s) actif(s)</span>
        <button class="btn-sm" style="font-size:8px" onclick="goSection('stats')" title="Voir les stats détaillées">
          <i class="ti ti-cpu" style="font-size:9px"></i>
        </button>
      </div>
      <table style="width:100%;border-collapse:collapse">
        <thead><tr style="border-bottom:1px solid var(--border)">
          <th style="padding:4px 6px;font-size:7px;color:var(--text3);letter-spacing:.5px;font-weight:700;text-align:left">NOM</th>
          <th style="padding:4px 6px;font-size:7px;color:var(--text3);letter-spacing:.5px;font-weight:700;text-align:right">CPU</th>
          <th style="padding:4px 6px;font-size:7px;color:var(--text3);letter-spacing:.5px;font-weight:700;text-align:right">RAM</th>
        </tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
}

async function loadDashMemosWidget() {
  const w = document.getElementById('dash-memos-widget');
  if (!w) return;
  let memos = null;
  try {
    const r = await fetch('/ui/proxy/memos/api/v1/memos?pageSize=5');
    if (r.ok) { const d = await r.json(); memos = Array.isArray(d) ? d : (d.memos || []); }
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'memos')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  if (!memos) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <i class="ti ti-note-off" style="color:var(--text3);font-size:13px"></i>
      <span style="font-size:9px;color:var(--text3)">Memos indisponible</span>
      <div style="margin-left:auto">${openBtn}</div>
    </div>`;
    return;
  }

  const list = memos.slice(0, 3);
  const rows = list.length === 0
    ? `<div style="font-size:9px;color:var(--text3);text-align:center;padding:10px 0">Aucune note pour l'instant.</div>`
    : list.map(m => {
        const content = (m.content || '').replace(/\n/g, ' ').trim().slice(0, 90);
        const ts = m.createTime ? new Date(m.createTime).toLocaleDateString('fr-FR') :
                   m.createdTs  ? new Date(m.createdTs * 1000).toLocaleDateString('fr-FR') : '';
        return `<div style="padding:6px 8px;background:var(--card);border-radius:5px;border:1px solid var(--border);margin-bottom:4px">
          <div style="font-size:7px;color:var(--text3);margin-bottom:2px">${escapeHtml(ts)}</div>
          <div style="font-size:9px;color:var(--text2);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(content || '(vide)')}</div>
        </div>`;
      }).join('');

  w.innerHTML = `
    <div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">
      <span style="font-size:9px;color:var(--text3)">${memos.length > 3 ? '3 / ' + memos.length + ' notes' : memos.length + ' note(s)'}</span>
      <div style="margin-left:auto;display:flex;gap:4px">
        ${openBtn}
        <button class="btn-sm" style="font-size:8px" onclick="openQuickMemo()" title="Nouvelle note rapide">
          <i class="ti ti-plus" style="font-size:9px"></i> NOTE
        </button>
      </div>
    </div>
    ${rows}`;
}

async function loadDashNavidromeWidget() {
  const w = document.getElementById('dash-navidrome-widget');
  if (!w) return;
  let artistCount = 0, songCount = 0, albumCount = 0;
  try {
    const [rArt, rSong, rAlb] = await Promise.all([
      fetch('/ui/proxy/navidrome/api/artist?_start=0&_end=1'),
      fetch('/ui/proxy/navidrome/api/song?_start=0&_end=1'),
      fetch('/ui/proxy/navidrome/api/album?_start=0&_end=1'),
    ]);
    if (rArt.ok)  artistCount = parseInt(rArt.headers.get('X-Total-Count') || '0');
    if (rSong.ok) songCount   = parseInt(rSong.headers.get('X-Total-Count') || '0');
    if (rAlb.ok)  albumCount  = parseInt(rAlb.headers.get('X-Total-Count') || '0');
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'navidrome')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:10px 12px">
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:10px">
        <span style="font-size:13px">🎵</span>
        <span style="font-size:9px;font-weight:700">Navidrome</span>
        <div style="margin-left:auto;display:flex;gap:4px">
          ${openBtn}
          <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-navidrome-library')">
            <i class="ti ti-music" style="font-size:9px"></i>
          </button>
        </div>
      </div>
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:4px;text-align:center">
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:var(--accent)">${artistCount}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">ARTISTES</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:var(--accent)">${albumCount}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">ALBUMS</div>
        </div>
        <div style="background:var(--bg2);border-radius:4px;padding:6px 4px">
          <div style="font-size:16px;font-weight:800;color:var(--accent)">${songCount}</div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:.5px">PISTES</div>
        </div>
      </div>
    </div>`;
}

async function loadDashGiteaWidget() {
  const w = document.getElementById('dash-gitea-widget');
  if (!w) return;
  let repos = null;
  try {
    const r = await fetch('/ui/proxy/gitea/api/v1/repos/search?limit=5&sort=updated');
    if (r.ok) { const d = await r.json(); repos = d.data || d; }
  } catch(e) {}

  const appDomain = S.apps.find(a => a.id === 'gitea')?.domain;
  const openBtn = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px"><i class="ti ti-external-link" style="font-size:9px"></i></a>`
    : '';

  if (!Array.isArray(repos)) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <i class="ti ti-git-branch" style="color:var(--text3);font-size:13px"></i>
      <span style="font-size:9px;color:var(--text3)">Gitea indisponible</span>
      <div style="margin-left:auto">${openBtn}</div>
    </div>`;
    return;
  }

  const rows = repos.slice(0, 4).map(r => `
    <div style="display:flex;align-items:center;gap:8px;padding:5px 8px;background:var(--card);border-radius:5px;border:1px solid var(--border);margin-bottom:4px">
      <i class="ti ti-git-branch" style="font-size:11px;color:var(--vio);flex-shrink:0"></i>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(r.full_name || r.name || '—')}</div>
        <div style="font-size:7px;color:var(--text3)">${r.stars_count || 0} ★ · ${r.open_issues_count || 0} issues · ${escapeHtml(r.language || '')}</div>
      </div>
      ${r.html_url ? `<a href="${escapeHtml(r.html_url)}" target="_blank" rel="noopener" style="color:var(--text3);text-decoration:none;flex-shrink:0"><i class="ti ti-external-link" style="font-size:9px"></i></a>` : ''}
    </div>`).join('');

  w.innerHTML = `
    <div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">
      <span style="font-size:9px;color:var(--text3)">${repos.length} dépôt(s)</span>
      <div style="margin-left:auto;display:flex;gap:4px">
        ${openBtn}
        <button class="btn-sm" style="font-size:8px" onclick="goSection('panel-gitea-repos')">
          <i class="ti ti-layout-sidebar-right" style="font-size:9px"></i>
        </button>
      </div>
    </div>
    ${rows || '<div style="font-size:9px;color:var(--text3);text-align:center;padding:10px 0">Aucun dépôt.</div>'}`;
}

async function loadDashArrWidget() {
  const w = document.getElementById('dash-arr-widget');
  if (!w) return;
  const arrRunning = S.apps.some(a => a.id === 'arr-stack' && a.status === 'running');
  const radarrRunning = arrRunning;
  const sonarrRunning = arrRunning;

  let radarrMovies = null, sonarrHistory = null;
  try {
    const fetches = [];
    if (radarrRunning) fetches.push(
      fetch('/ui/proxy/arr-radarr/api/v3/movie?pageSize=100').then(r => r.ok ? r.json() : null).catch(() => null)
    );
    if (sonarrRunning) fetches.push(
      fetch('/ui/proxy/arr-sonarr/api/v3/history?pageSize=5&sortKey=date&sortDirection=descending').then(r => r.ok ? r.json() : null).catch(() => null)
    );
    const results = await Promise.all(fetches);
    let i = 0;
    if (radarrRunning) radarrMovies = results[i++];
    if (sonarrRunning) sonarrHistory = results[i++];
  } catch(e) {}

  const items = [];
  if (Array.isArray(radarrMovies)) {
    const recent = radarrMovies.filter(m => m.added && new Date(m.added) > new Date(Date.now() - 30 * 86400000))
      .sort((a, b) => new Date(b.added) - new Date(a.added)).slice(0, 2);
    recent.forEach(m => items.push({ emoji: '🎬', title: m.title, sub: String(m.year || ''), tag: 'Radarr' }));
  }
  if (sonarrHistory?.records) {
    sonarrHistory.records.slice(0, 2).forEach(e => {
      const series = e.series?.title || '';
      const ep = e.episode ? `S${String(e.episode.seasonNumber).padStart(2,'0')}E${String(e.episode.episodeNumber).padStart(2,'0')}` : '';
      items.push({ emoji: '📺', title: series || e.sourceTitle || '—', sub: ep, tag: 'Sonarr' });
    });
  }

  if (!items.length) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <span style="font-size:9px;color:var(--text3)">Aucune activité multimédia récente</span>
    </div>`;
    return;
  }

  const arrApp = S.apps.find(a => a.id === 'arr-stack');
  const arrDomain = arrApp?.domain;

  const rows = items.map(it => `
    <div style="display:flex;align-items:center;gap:8px;padding:5px 8px;background:var(--card);border-radius:5px;border:1px solid var(--border);margin-bottom:4px">
      <span style="font-size:12px;flex-shrink:0">${it.emoji}</span>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(it.title)}</div>
        <div style="font-size:7px;color:var(--text3)">${escapeHtml(it.sub)} <span style="color:var(--vio)">${it.tag}</span></div>
      </div>
    </div>`).join('');

  const arrLinks = [
    arrDomain ? `<a href="https://${arrDomain}" target="_blank" rel="noopener" class="btn-sm" style="text-decoration:none;font-size:8px">📡 ARR</a>` : '',
    `<button class="btn-sm" style="font-size:8px" onclick="goSection('panel-arr-radarr')"><i class="ti ti-movie" style="font-size:9px"></i></button>`,
    `<button class="btn-sm" style="font-size:8px" onclick="goSection('panel-arr-sonarr')"><i class="ti ti-device-tv" style="font-size:9px"></i></button>`,
  ].filter(Boolean).join('');

  w.innerHTML = `
    <div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">
      <span style="font-size:9px;color:var(--text3)">Ajouts récents</span>
      <div style="margin-left:auto;display:flex;gap:4px">${arrLinks}</div>
    </div>
    ${rows}`;
}

async function loadDashAuthentikWidget() {
  const w = document.getElementById('dash-authentik-widget');
  if (!w) return;

  let users = null, apps = null, providers = null;
  try {
    const [ur, ar, pr] = await Promise.all([
      fetch('/ui/proxy/authentik/api/v3/core/users/?page_size=1&type=internal'),
      fetch('/ui/proxy/authentik/api/v3/core/applications/?page_size=1'),
      fetch('/ui/proxy/authentik/api/v3/providers/all/?page_size=1'),
    ]);
    if (ur.ok) { const d = await ur.json(); users = d.pagination?.count ?? d.count ?? null; }
    if (ar.ok) { const d = await ar.json(); apps = d.pagination?.count ?? d.count ?? null; }
    if (pr.ok) { const d = await pr.json(); providers = d.pagination?.count ?? d.count ?? null; }
  } catch(e) {}

  if (users === null && apps === null) { w.innerHTML = ''; return; }

  const appDomain = S.apps.find(a => a.id === 'authentik')?.domain;
  const statItems = [
    { label: 'UTILISATEURS', val: users ?? '—', color: 'var(--vio-b)' },
    { label: 'APPLICATIONS', val: apps ?? '—', color: 'var(--blue)' },
    { label: 'PROVIDERS',    val: providers ?? '—', color: 'var(--accent)' },
  ];

  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:8px">
        ${statItems.map(it => `
          <div>
            <div style="font-size:8px;color:var(--text3)">${it.label}</div>
            <div style="font-size:18px;font-weight:700;color:${it.color}">${it.val}</div>
          </div>`).join('')}
      </div>
      <div style="display:flex;gap:4px;flex-wrap:wrap">
        <button class="btn-sm" onclick="goSection('panel-authentik-users')" style="font-size:8px">
          <i class="ti ti-users" style="font-size:9px"></i> ANNUAIRE
        </button>
        ${appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none">
          <i class="ti ti-external-link" style="font-size:9px"></i> OUVRIR
        </a>` : ''}
      </div>
    </div>`;
}

async function loadDashNextcloudWidget() {
  const w = document.getElementById('dash-nextcloud-widget');
  if (!w) return;

  let userCount = null, quota = null, version = null;
  try {
    const [ur, cr] = await Promise.all([
      fetch('/ui/proxy/nextcloud/ocs/v1.php/cloud/users?limit=1&format=json', {
        headers: { 'OCS-APIRequest': 'true' },
      }),
      fetch('/ui/proxy/nextcloud/ocs/v1.php/cloud/capabilities?format=json', {
        headers: { 'OCS-APIRequest': 'true' },
      }),
    ]);
    if (ur.ok) {
      const d = await ur.json();
      userCount = d?.ocs?.data?.users?.length ?? null;
      // NC returns meta.totalitems or we count the array
    }
    if (cr.ok) {
      const d = await cr.json();
      version = d?.ocs?.data?.version?.string ?? null;
    }
  } catch(e) {}

  // Try admin usercount via different endpoint
  if (userCount === null) {
    try {
      const r = await fetch('/ui/proxy/nextcloud/ocs/v2.php/cloud/users?format=json', {
        headers: { 'OCS-APIRequest': 'true' },
      });
      if (r.ok) {
        const d = await r.json();
        const users = d?.ocs?.data?.users;
        if (Array.isArray(users)) userCount = users.length;
      }
    } catch(e) {}
  }

  const appDomain = S.apps.find(a => a.id === 'nextcloud')?.domain;
  const statItems = [
    { label: 'UTILISATEURS', val: userCount ?? '—', color: 'var(--blue)' },
    { label: 'VERSION',      val: version ?? '—',   color: 'var(--text2)' },
  ];

  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:repeat(2,1fr);gap:8px;margin-bottom:8px">
        ${statItems.map(it => `
          <div>
            <div style="font-size:8px;color:var(--text3)">${it.label}</div>
            <div style="font-size:16px;font-weight:700;color:${it.color}">${escapeHtml(String(it.val))}</div>
          </div>`).join('')}
      </div>
      <div style="display:flex;gap:4px;flex-wrap:wrap">
        <button class="btn-sm" onclick="goSection('nextcloud')" style="font-size:8px">
          <i class="ti ti-layout-sidebar-right" style="font-size:9px"></i> PANEL
        </button>
        ${appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none">
          <i class="ti ti-external-link" style="font-size:9px"></i> OUVRIR
        </a>` : ''}
      </div>
    </div>`;
}

async function loadDashJellyfinWidget() {
  const w = document.getElementById('dash-jellyfin-widget');
  if (!w) return;

  let movies = null, series = null, episodes = null, users = null;
  try {
    const [mr, sr, ur] = await Promise.all([
      fetch('/ui/proxy/jellyfin/Items/Counts'),
      fetch('/ui/proxy/jellyfin/Users'),
    ]);
    if (mr.ok) {
      const d = await mr.json();
      movies = d.MovieCount ?? null;
      series = d.SeriesCount ?? null;
      episodes = d.EpisodeCount ?? null;
    }
    if (sr.ok) {
      const d = await sr.json();
      users = Array.isArray(d) ? d.length : null;
    }
  } catch(e) {}

  if (movies === null && series === null) { w.innerHTML = ''; return; }
  const appDomain = S.apps.find(a => a.id === 'jellyfin')?.domain;
  const stats = [
    { label: 'FILMS',    val: movies ?? '—',   color: 'var(--blue)' },
    { label: 'SÉRIES',   val: series ?? '—',   color: 'var(--vio-b)' },
    { label: 'ÉPISODES', val: episodes ?? '—', color: 'var(--accent)' },
    { label: 'USERS',    val: users ?? '—',    color: 'var(--text2)' },
  ];
  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-bottom:8px">
        ${stats.map(it => `<div>
          <div style="font-size:8px;color:var(--text3)">${it.label}</div>
          <div style="font-size:16px;font-weight:700;color:${it.color}">${it.val}</div>
        </div>`).join('')}
      </div>
      <div style="display:flex;gap:4px">
        <button class="btn-sm" onclick="goSection('jellyfin')" style="font-size:8px">
          <i class="ti ti-layout-sidebar-right" style="font-size:9px"></i> PANEL
        </button>
        ${appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none"><i class="ti ti-external-link" style="font-size:9px"></i> OUVRIR</a>` : ''}
      </div>
    </div>`;
}

async function loadDashImmichWidget() {
  const w = document.getElementById('dash-immich-widget');
  if (!w) return;

  let stats = null;
  try {
    const r = await fetch('/ui/proxy/immich/api/server/statistics');
    if (r.ok) stats = await r.json();
  } catch(e) {}
  if (!stats) { w.innerHTML = ''; return; }

  const photos = stats.photos ?? '—';
  const videos = stats.videos ?? '—';
  const usage  = stats.usage ? (stats.usage / 1073741824).toFixed(1) + ' Go' : '—';
  const appDomain = S.apps.find(a => a.id === 'immich')?.domain;

  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:8px">
        <div><div style="font-size:8px;color:var(--text3)">PHOTOS</div><div style="font-size:18px;font-weight:700;color:var(--blue)">${photos.toLocaleString()}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">VIDÉOS</div><div style="font-size:18px;font-weight:700;color:var(--vio-b)">${videos.toLocaleString()}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">STOCKAGE</div><div style="font-size:14px;font-weight:700;color:var(--text2)">${usage}</div></div>
      </div>
      <div style="display:flex;gap:4px">
        ${appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none"><i class="ti ti-external-link" style="font-size:9px"></i> OUVRIR</a>` : ''}
      </div>
    </div>`;
}

async function loadDashPiholeWidget() {
  const w = document.getElementById('dash-pihole-widget');
  if (!w) return;

  let data = null;
  try {
    const r = await fetch('/ui/proxy/pihole/api.php?summaryRaw&auth=');
    if (r.ok) data = await r.json();
  } catch(e) {}
  if (!data) { w.innerHTML = ''; return; }

  const queries = data.dns_queries_today ?? '—';
  const blocked = data.ads_blocked_today ?? '—';
  const pct = data.ads_percentage_today ? data.ads_percentage_today.toFixed(1)+'%' : '—';
  const domains = data.domains_being_blocked ?? '—';
  const appDomain = S.apps.find(a => a.id === 'pihole')?.domain;

  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-bottom:8px">
        <div><div style="font-size:8px;color:var(--text3)">REQUÊTES</div><div style="font-size:14px;font-weight:700;color:var(--blue)">${queries.toLocaleString()}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">BLOQUÉES</div><div style="font-size:14px;font-weight:700;color:var(--red-b)">${blocked.toLocaleString()}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">% BLOCKÉ</div><div style="font-size:14px;font-weight:700;color:var(--warn)">${pct}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">LISTES</div><div style="font-size:14px;font-weight:700;color:var(--text2)">${domains.toLocaleString()}</div></div>
      </div>
      ${appDomain ? `<a href="https://${appDomain}/admin" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none"><i class="ti ti-external-link" style="font-size:9px"></i> ADMIN</a>` : ''}
    </div>`;
}

async function loadDashPaperlessWidget() {
  const w = document.getElementById('dash-paperless-widget');
  if (!w) return;

  let docs = null, tags = null, correspondents = null;
  try {
    const [dr, tr, cr] = await Promise.all([
      fetch('/ui/proxy/paperless-ngx/api/documents/?page=1&page_size=1'),
      fetch('/ui/proxy/paperless-ngx/api/tags/?page=1&page_size=1'),
      fetch('/ui/proxy/paperless-ngx/api/correspondents/?page=1&page_size=1'),
    ]);
    if (dr.ok) { const d = await dr.json(); docs = d.count ?? null; }
    if (tr.ok) { const d = await tr.json(); tags = d.count ?? null; }
    if (cr.ok) { const d = await cr.json(); correspondents = d.count ?? null; }
  } catch(e) {}

  if (docs === null) { w.innerHTML = ''; return; }
  const appDomain = S.apps.find(a => a.id === 'paperless-ngx')?.domain;

  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:8px">
        <div><div style="font-size:8px;color:var(--text3)">DOCUMENTS</div><div style="font-size:18px;font-weight:700;color:var(--blue)">${docs}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">TAGS</div><div style="font-size:18px;font-weight:700;color:var(--vio-b)">${tags ?? '—'}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">CORRESPONDANTS</div><div style="font-size:14px;font-weight:700;color:var(--text2)">${correspondents ?? '—'}</div></div>
      </div>
      <div style="display:flex;gap:4px">
        ${appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none"><i class="ti ti-external-link" style="font-size:9px"></i> OUVRIR</a>` : ''}
      </div>
    </div>`;
}

async function loadDashNtfyWidget() {
  const w = document.getElementById('dash-ntfy-widget');
  if (!w) return;

  let topics = null, msgs = null;
  try {
    const r = await fetch('/ui/proxy/ntfy/v1/stats');
    if (r.ok) {
      const d = await r.json();
      topics = d.total_topics ?? d.topicsTotal ?? null;
      msgs   = d.total_messages ?? d.messagesPublished ?? null;
    }
  } catch(e) {}
  if (topics === null && msgs === null) { w.innerHTML = ''; return; }
  const appDomain = S.apps.find(a => a.id === 'ntfy')?.domain;

  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-bottom:8px">
        <div><div style="font-size:8px;color:var(--text3)">TOPICS</div><div style="font-size:18px;font-weight:700;color:var(--vio-b)">${topics ?? '—'}</div></div>
        <div><div style="font-size:8px;color:var(--text3)">MESSAGES</div><div style="font-size:18px;font-weight:700;color:var(--blue)">${msgs?.toLocaleString() ?? '—'}</div></div>
      </div>
      ${appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none"><i class="ti ti-external-link" style="font-size:9px"></i> OUVRIR</a>` : ''}
    </div>`;
}

async function loadDashAzuracastWidget() {
  const w = document.getElementById('dash-azuracast-widget');
  if (!w) return;

  let stations = null;
  try {
    const r = await fetch('/ui/proxy/azuracast/api/stations');
    if (r.ok) stations = await r.json();
  } catch(e) {}
  if (!stations) { w.innerHTML = ''; return; }

  const appDomain = S.apps.find(a => a.id === 'azuracast')?.domain;
  const stationRows = (Array.isArray(stations) ? stations : []).slice(0, 3).map(s => {
    const isLive = s.is_online !== false;
    const listeners = s.listeners?.current ?? s.listener_count ?? 0;
    const song = s.now_playing?.song;
    const nowPlayingText = song?.text || (song?.title ? [(song.artist || ''), song.title].filter(Boolean).join(' — ') : '');
    return `<div style="display:flex;align-items:center;gap:8px;padding:5px 0;border-bottom:1px solid var(--border)">
      <span style="display:inline-block;width:6px;height:6px;border-radius:50%;background:${isLive ? 'var(--green-b)' : 'var(--text3)'};flex-shrink:0;margin-top:2px"></span>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(s.name || s.short_name || '—')}</div>
        ${nowPlayingText ? `<div style="font-size:8px;color:var(--accent);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;margin-top:2px"><i class="ti ti-music" style="font-size:7px"></i> ${escapeHtml(nowPlayingText)}</div>` : ''}
        <div style="font-size:8px;color:var(--text3);margin-top:1px">${listeners} auditeur${listeners !== 1 ? 's' : ''}</div>
      </div>
      ${s.public_player_url ? `<a href="${s.public_player_url}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none" title="Ouvrir le player"><i class="ti ti-player-play" style="font-size:9px"></i></a>` : ''}
    </div>`;
  }).join('');

  w.innerHTML = `
    <div class="settings-card" style="padding:10px 12px">
      <div style="font-size:8px;color:var(--text3);margin-bottom:6px">${stations.length} STATION(S)</div>
      ${stationRows || '<div style="font-size:9px;color:var(--text3)">Aucune station configurée</div>'}
      <div style="margin-top:8px;display:flex;gap:4px">
        ${appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn-sm" style="font-size:8px;text-decoration:none"><i class="ti ti-external-link" style="font-size:9px"></i> ADMIN</a>` : ''}
      </div>
    </div>`;
}

async function loadDashJournalErrorsWidget() {
  const w = document.getElementById('dash-journal-errors-widget');
  if (!w) return;
  let data = null;
  try {
    const r = await fetch('/sys/journal?n=200');
    if (r.ok) data = await r.json();
  } catch(e) {}

  const errors = (data?.entries || []).filter(e =>
    e.priority === 'err' || e.priority === 'crit' || e.priority === 'alert' || e.priority === 'emerg'
  ).slice(-10).reverse();

  if (!errors.length) {
    w.innerHTML = `<div style="display:flex;align-items:center;gap:6px;padding:8px 10px;background:var(--card);border:1px solid var(--border);border-radius:6px;border-left:3px solid var(--green-b)">
      <i class="ti ti-circle-check" style="color:var(--green-b);font-size:13px"></i>
      <span style="font-size:9px;color:var(--text2)">Aucune erreur système récente</span>
    </div>`;
    return;
  }

  const rows = errors.map(e => {
    const ts = e.time ? new Date(parseInt(e.time.replace('.',''))/1000).toLocaleTimeString('fr-FR') : '';
    return `<div class="log-line" style="border-bottom:1px solid var(--border);padding:4px 10px">
      <span class="log-ts">${escapeHtml(ts)}</span>
      <span class="log-app" style="color:var(--blue)">${escapeHtml(e.unit || '—')}</span>
      <span class="log-level" style="color:var(--red-b)">${escapeHtml(e.priority || '')}</span>
      <span class="log-msg" style="color:var(--red-b)">${escapeHtml(e.message || '')}</span>
    </div>`;
  }).join('');

  w.innerHTML = `<div style="background:var(--card);border:1px solid var(--border);border-left:3px solid var(--red-b);border-radius:6px;overflow:hidden">
    <div style="display:flex;align-items:center;justify-content:space-between;padding:6px 10px;border-bottom:1px solid var(--border)">
      <span style="font-size:9px;font-weight:700;color:var(--red-b)">${errors.length} ERREUR(S)</span>
      <button class="btn-sm" onclick="goSection('journal')" style="font-size:8px">VOIR LE JOURNAL <i class="ti ti-arrow-right" style="font-size:9px"></i></button>
    </div>
    <div style="font-family:monospace;font-size:9px">${rows}</div>
  </div>`;
}

async function loadDashUptimeWidget() {
  const w = document.getElementById('dash-uptime-widget');
  if (!w) return;
  let monitors = null;
  try {
    const epCtrl = new AbortController();
    const epTimer = setTimeout(() => epCtrl.abort(), 4000);
    const epResp = await fetch('/ui/proxy/uptime-kuma/api/entry-page', { signal: epCtrl.signal }).catch(() => null);
    clearTimeout(epTimer);
    const epData = epResp?.ok ? await epResp.json().catch(() => null) : null;
    const slug = epData?.entryPage || null;
    if (slug) {
      const hbCtrl = new AbortController();
      const hbTimer = setTimeout(() => hbCtrl.abort(), 6000);
      const r = await fetch(`/ui/proxy/uptime-kuma/api/status-page/heartbeat/${slug}`, { signal: hbCtrl.signal }).catch(() => null);
      clearTimeout(hbTimer);
      if (r?.ok) {
        const d = await r.json();
        monitors = Object.values(d.heartbeatList || {}).map(beats => {
          const last = beats[beats.length - 1] || {};
          return { status: last.status, msg: last.msg || '' };
        });
      }
    }
  } catch(e) {}

  if (!monitors) {
    w.innerHTML = '';
    return;
  }

  const up   = monitors.filter(m => m.status === 1).length;
  const down = monitors.filter(m => m.status !== 1).length;
  const total = monitors.length;
  const allGood = down === 0;

  w.innerHTML = `
    <div style="display:flex;align-items:center;gap:10px;padding:8px 12px;background:var(--card);
        border-radius:6px;border:1px solid var(--border);border-left:3px solid ${allGood ? 'var(--green-b)' : 'var(--red-b)'}">
      <span style="font-size:16px">${allGood ? '✅' : '⚠️'}</span>
      <div style="flex:1">
        <div style="font-size:10px;font-weight:700;color:var(--text1)">
          ${allGood ? 'Tous les services sont opérationnels' : `${down} service(s) en anomalie`}
        </div>
        <div style="font-size:8px;color:var(--text3)">${up}/${total} moniteur(s) UP</div>
      </div>
      <button class="btn-sm" onclick="goSection('uptime-kuma')">
        <i class="ti ti-external-link" style="font-size:10px"></i> DÉTAILS
      </button>
    </div>`;
}

async function loadDashEventsWidget() {
  const w = document.getElementById('dash-events-widget');
  if (!w) return;
  let events = [];
  try {
    const r = await api.get('/api/v1/events?limit=8');
    events = r?.data || [];
  } catch(e) {}

  const EVENT_ICONS = {
    'app.installed': '📦', 'app.removed': '🗑️', 'app.started': '▶️',
    'app.stopped': '⏹️', 'app.restarted': '🔄', 'app.backed_up': '💾',
    'app.restored': '📤', 'app.failed': '❌',
  };

  if (!events.length) {
    w.innerHTML = `<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun événement récent.</div>`;
    return;
  }

  const rows = [...events].reverse().map(ev => {
    const ts = ev.timestamp ? new Date(ev.timestamp).toLocaleString('fr-FR', {month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}) : '';
    const evType = ev.event || ev.type || '';
    const evIcon = EVENT_ICONS[evType] || '•';
    const appId = ev.app || ev.app_id || '';
    const appName = appId ? (S.apps.find(a => a.id === appId)?.name || appId) : '';
    return `
      <div style="display:flex;align-items:center;gap:8px;padding:5px 8px;border-bottom:1px solid var(--border)">
        <span style="font-size:11px;width:18px;text-align:center">${evIcon}</span>
        <div style="flex:1;min-width:0">
          <span style="font-size:9px;font-weight:700;color:var(--text1)">${appName ? escapeHtml(appName)+' — ' : ''}</span>
          <span style="font-size:9px;color:var(--text2)">${escapeHtml(evType || '')}</span>
        </div>
        <span style="font-size:8px;color:var(--text3);white-space:nowrap">${escapeHtml(ts)}</span>
      </div>`;
  }).join('');

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;overflow:hidden">
      ${rows}
      <div style="padding:6px 10px;text-align:right">
        <button class="btn-sm" onclick="goSection('events')" style="font-size:8px">
          <i class="ti ti-history" style="font-size:9px"></i> VOIR TOUT
        </button>
      </div>
    </div>`;
}

// ── Dashboard: Gotify notifications widget ────────────────────────────────────
async function loadDashGotifyWidget() {
  const w = document.getElementById('dash-gotify-widget');
  if (!w) return;
  let msgs = null;
  try {
    const r = await fetch('/ui/proxy/gotify/message?limit=6');
    if (r.ok) msgs = (await r.json()).messages || [];
  } catch(e) {}

  if (!msgs || !msgs.length) {
    w.innerHTML = `<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune notification récente.</div>`;
    return;
  }

  const prioColor = p => p >= 8 ? 'var(--red-b)' : p >= 4 ? 'var(--warn)' : 'var(--text3)';
  const rows = msgs.map(m => {
    const ts = m.date ? new Date(m.date).toLocaleTimeString('fr-FR',{hour:'2-digit',minute:'2-digit'}) : '';
    return `<div style="display:flex;align-items:flex-start;gap:8px;padding:6px 10px;border-bottom:1px solid var(--border)">
      <span style="width:6px;height:6px;border-radius:50%;background:${prioColor(m.priority||0)};flex-shrink:0;margin-top:3px"></span>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:700;color:var(--text1);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(m.title||'Message')}</div>
        <div style="font-size:8px;color:var(--text3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml((m.message||'').slice(0,80))}</div>
      </div>
      <span style="font-size:8px;color:var(--text3);white-space:nowrap;flex-shrink:0">${escapeHtml(ts)}</span>
    </div>`;
  }).join('');

  w.innerHTML = `<div style="background:var(--card);border:1px solid var(--border);border-radius:6px;overflow:hidden">
    ${rows}
    <div style="padding:6px 10px;text-align:right">
      <button class="btn-sm" onclick="goSection('gotify')" style="font-size:8px">
        <i class="ti ti-bell" style="font-size:9px"></i> VOIR TOUT
      </button>
    </div>
  </div>`;
}

// ── Dashboard: CrowdSec security widget ──────────────────────────────────────
async function loadDashCrowdSecWidget() {
  const w = document.getElementById('dash-crowdsec-widget');
  if (!w) return;

  let decisions = null, alerts = null;
  try {
    const [dr, ar] = await Promise.all([
      fetch('/ui/proxy/crowdsec/v1/decisions?limit=1000'),
      fetch('/ui/proxy/crowdsec/v1/alerts?limit=100'),
    ]);
    if (dr.ok) { const t = await dr.text(); decisions = t ? JSON.parse(t) : []; }
    if (ar.ok) { const t = await ar.text(); alerts = t ? JSON.parse(t) : []; }
  } catch(e) {}

  if (!decisions && !alerts) { w.innerHTML = ''; return; }

  const totalBans = (decisions || []).length;
  const totalAlerts = (alerts || []).length;

  // Top scenarii
  const scenarioCounts = {};
  (decisions || []).forEach(d => {
    const s = d.scenario || d.reason || 'unknown';
    scenarioCounts[s] = (scenarioCounts[s] || 0) + 1;
  });
  const topScenarios = Object.entries(scenarioCounts)
    .sort((a, b) => b[1] - a[1]).slice(0, 3);

  const scenarioHtml = topScenarios.length
    ? topScenarios.map(([s, n]) => `
        <div style="display:flex;justify-content:space-between;align-items:center;padding:3px 0;border-bottom:1px solid var(--border)">
          <span style="font-size:8px;color:var(--text2)">${escapeHtml(s)}</span>
          <span class="badge badge-err" style="font-size:7px">${n}</span>
        </div>`).join('')
    : '';

  w.innerHTML = `
    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// SÉCURITÉ</div>
    <div class="settings-card" style="padding:10px 12px">
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-bottom:${scenarioHtml ? '10px' : '0'}">
        <div>
          <div style="font-size:8px;color:var(--text3)">BANS ACTIFS</div>
          <div style="font-size:18px;font-weight:700;color:${totalBans > 0 ? 'var(--red-b)' : 'var(--green-b)'}">${totalBans.toLocaleString()}</div>
        </div>
        <div>
          <div style="font-size:8px;color:var(--text3)">ALERTES</div>
          <div style="font-size:18px;font-weight:700;color:${totalAlerts > 0 ? 'var(--warn)' : 'var(--text2)'}">${totalAlerts}</div>
        </div>
      </div>
      ${scenarioHtml ? `<div style="font-size:8px;color:var(--text3);margin-bottom:5px">TOP SCENARII</div>${scenarioHtml}` : ''}
      <div style="margin-top:8px;text-align:right">
        <button class="btn-sm" onclick="goSection('panel-crowdsec-decisions')" style="font-size:8px">
          <i class="ti ti-shield-check" style="font-size:9px"></i> DÉTAILS
        </button>
      </div>
    </div>`;
}

// ── Dashboard: Resources widget ───────────────────────────────────────────────
async function loadDashResourcesWidget() {
  const w = document.getElementById('dash-resources-widget');
  if (!w) return;

  // Charger historique des stats pour sparklines (non-bloquant)
  let hist = null;
  try {
    const rh = await fetch('/sys/stats/history');
    if (rh.ok) { const dh = await rh.json(); hist = dh.samples || null; }
  } catch(e) {}

  let appStats = S.stats?.apps;
  if (!appStats || !appStats.length) {
    const r = await api.get('/api/v1/stats').catch(() => null);
    if (r?.data?.apps) { Object.assign(S.stats, r.data); appStats = r.data.apps; }
  }

  // ── Sparklines système ───────────────────────────────────────────────────────
  const ram  = S.stats.mem_total_mb  ? Math.round(S.stats.mem_used_mb  / S.stats.mem_total_mb  * 100) : 0;
  const disk = S.stats.disk_total_gb ? Math.round(S.stats.disk_used_gb / S.stats.disk_total_gb * 100) : 0;
  const cpu  = Math.round(S.stats.cpu_pct || 0);
  const cpuHist  = hist ? hist.map(s => s.cpu)  : [];
  const ramHist  = hist ? hist.map(s => s.ram)  : [];
  const diskHist = hist ? hist.map(s => s.disk) : [];

  const sysMetrics = [
    { label: 'CPU',  pct: cpu,  hist: cpuHist,  color: 'var(--vio-b)' },
    { label: 'RAM',  pct: ram,  hist: ramHist,  color: 'var(--blue)' },
    { label: 'DISK', pct: disk, hist: diskHist, color: disk > 85 ? 'var(--red-b)' : 'var(--ok)' },
  ].map(m => {
    const col = m.pct > 85 ? 'var(--red-b)' : m.pct > 70 ? 'var(--warn)' : m.color;
    const sparkSvg = _sparkline(m.hist, m.color, 56, 22);
    return `<div style="flex:1;min-width:0">
      <div style="display:flex;align-items:flex-end;justify-content:space-between;gap:6px">
        <div>
          <div style="font-size:7px;color:var(--text3);letter-spacing:1px;margin-bottom:2px">${m.label}</div>
          <div style="font-size:16px;font-weight:700;color:${col};line-height:1">${m.pct}%</div>
        </div>
        <div style="opacity:.8;flex-shrink:0">${sparkSvg}</div>
      </div>
      <div style="height:2px;background:var(--border);border-radius:1px;margin-top:4px">
        <div style="height:100%;width:${m.pct}%;background:${col};border-radius:1px;transition:width .3s"></div>
      </div>
    </div>`;
  }).join('<div style="width:1px;background:var(--border)"></div>');

  const sparkHtml = `
    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:0 0 10px">// RESSOURCES${hist && hist.length > 1 ? ` <span style="font-size:7px;font-weight:400;opacity:.5">· ${hist.length} pts / 1h</span>` : ''}</div>
    <div class="settings-card" style="padding:10px 12px;margin-bottom:14px">
      <div style="display:flex;gap:14px;align-items:stretch">${sysMetrics}</div>
    </div>`;

  // ── Top apps par RAM ─────────────────────────────────────────────────────────
  let appRows = '';
  if (appStats && appStats.length) {
    const topMem = [...appStats]
      .filter(a => a.memory_mb > 0 && a.status === 'running')
      .sort((a, b) => b.memory_mb - a.memory_mb)
      .slice(0, 6);
    if (topMem.length) {
      const maxMem = topMem[0].memory_mb;
      appRows = topMem.map(a => {
        const memPct = Math.round(a.memory_mb / maxMem * 100);
        const memStr = a.memory_mb >= 1024 ? (a.memory_mb / 1024).toFixed(1) + ' Go' : Math.round(a.memory_mb) + ' Mo';
        const cpuStr = a.cpu_percent > 0 ? a.cpu_percent.toFixed(1) + '%' : '';
        const barColor = memPct > 80 ? 'var(--red-b)' : memPct > 50 ? 'var(--warn)' : 'var(--accent)';
        return `
          <div style="display:grid;grid-template-columns:auto 1fr auto auto;align-items:center;gap:8px;padding:4px 0">
            <span style="font-size:14px;width:20px;text-align:center">${icon(a.app_id)}</span>
            <div>
              <div style="font-size:9px;font-weight:600;color:var(--text1);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(a.name || a.app_id)}</div>
              <div style="height:3px;background:var(--border);border-radius:2px;margin-top:2px">
                <div style="height:100%;width:${memPct}%;background:${barColor};border-radius:2px;transition:width .3s"></div>
              </div>
            </div>
            <span style="font-size:8px;color:var(--text2);font-family:monospace;white-space:nowrap">${memStr}</span>
            ${cpuStr ? `<span style="font-size:8px;color:var(--text3);font-family:monospace">${cpuStr}</span>` : '<span></span>'}
          </div>`;
      }).join('');
      appRows = `
        <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:14px 0 10px">// RESSOURCES APPS</div>
        <div class="settings-card" style="padding:8px 12px">${appRows}</div>`;
    }
  }

  w.innerHTML = sparkHtml + appRows;
}

// ── SECTION: TASKS ───────────────────────────────────────────────────────────

const TASK_TYPE_LABELS = { backup: 'Sauvegarde', upgrade: 'Mise à jour Caleope', update: 'Sync store' };
const TASK_SCOPE_LABELS = { all: 'Tout (data + config)', config: 'Config uniquement', data: 'Data uniquement' };
const DAY_LABELS = ['Dim', 'Lun', 'Mar', 'Mer', 'Jeu', 'Ven', 'Sam'];

async function loadTasks() {
  const c = document.getElementById('content-tasks');
  if (!c) return;

  c.innerHTML = `<div class="empty-state" style="padding-top:30px"><div class="empty-icon"><i class="ti ti-loader" style="animation:spin .8s linear infinite"></i></div><div class="empty-title" style="font-size:10px">CHARGEMENT...</div></div>`;

  const data = await api.get('/api/v1/tasks');
  const tasks = data?.data || [];

  if (!tasks.length) {
    c.innerHTML = `
      <div class="empty-state">
        <div class="empty-icon"><i class="ti ti-clock"></i></div>
        <div class="empty-title">AUCUNE TÂCHE PLANIFIÉE</div>
        <div class="empty-sub">Créez des sauvegardes automatiques, des mises à jour nocturnes, ou synchronisez le store régulièrement.</div>
      </div>`;
    return;
  }

  c.innerHTML = `<div class="settings-list">${tasks.map(taskRow).join('')}</div>`;
}

function computeNextRun(t) {
  if (!t.enabled || !t.schedule) return null;
  const now = new Date();
  const h = t.schedule.hour ?? 0;
  const m = t.schedule.minute ?? 0;
  const days = t.schedule.days; // undefined = every day, or [0-6]

  // Start from today
  for (let offset = 0; offset <= 7; offset++) {
    const candidate = new Date(now);
    candidate.setDate(now.getDate() + offset);
    candidate.setHours(h, m, 0, 0);
    if (candidate <= now) continue;
    if (!days?.length || days.includes(candidate.getDay())) {
      return candidate;
    }
  }
  return null;
}

function formatRelTime(d) {
  if (!d) return null;
  const diff = d - Date.now();
  if (diff < 0) return 'maintenant';
  const h = Math.floor(diff / 3600000);
  const min = Math.floor((diff % 3600000) / 60000);
  if (h > 23) return `dans ${Math.floor(h/24)}j ${h%24}h`;
  if (h > 0) return `dans ${h}h ${min}m`;
  return `dans ${min}m`;
}

function taskRow(t) {
  const typeLabel  = TASK_TYPE_LABELS[t.type]  || t.type;
  const scopeLabel = t.scope ? ` — ${TASK_SCOPE_LABELS[t.scope] || t.scope}` : '';
  const appLabel   = t.app   ? ` › ${t.app}` : '';
  const days       = t.schedule?.days?.length
    ? t.schedule.days.map(d => DAY_LABELS[d] ?? d).join(' ')
    : 'Chaque jour';
  const hour       = String(t.schedule?.hour ?? 0).padStart(2, '0');
  const min        = String(t.schedule?.minute ?? 0).padStart(2, '0');
  const lastRun    = t.last_run && t.last_run !== '0001-01-01T00:00:00Z'
    ? new Date(t.last_run).toLocaleString('fr-FR') : '—';
  const lastStatus = t.last_status || '';
  const statusDot  = lastStatus === 'ok' ? 'dot-ok' : lastStatus === 'error' ? 'dot-err' : 'dot-idle';
  const nextRun    = computeNextRun(t);
  const nextRunStr = nextRun ? nextRun.toLocaleString('fr-FR', {weekday:'short',hour:'2-digit',minute:'2-digit'}) + ` (${formatRelTime(nextRun)})` : '—';

  return `
    <div class="settings-card" style="display:flex;align-items:center;gap:10px;padding:10px 12px">
      <div style="flex:1;min-width:0">
        <div style="display:flex;align-items:center;gap:6px;margin-bottom:3px">
          <span style="font-size:10px;font-weight:700;letter-spacing:.8px">${escapeHtml(typeLabel)}${escapeHtml(appLabel)}${escapeHtml(scopeLabel)}</span>
          ${t.enabled
            ? '<span class="badge badge-ok" style="font-size:7px">ACTIF</span>'
            : '<span class="badge badge-warn" style="font-size:7px">INACTIF</span>'}
        </div>
        <div style="font-size:9px;color:var(--text3)">
          <i class="ti ti-clock" style="font-size:9px"></i>&nbsp;${escapeHtml(hour)}:${escapeHtml(min)} &nbsp;·&nbsp;
          <i class="ti ti-calendar" style="font-size:9px"></i>&nbsp;${escapeHtml(days)}
        </div>
        <div style="font-size:8px;color:var(--text3);margin-top:2px">
          Dernière exécution : ${escapeHtml(lastRun)}
          ${lastStatus ? `&nbsp;<span class="srv-dot ${statusDot}" style="display:inline-block;margin-left:2px"></span>` : ''}
        </div>
        ${nextRun ? `<div style="font-size:8px;color:var(--accent);margin-top:1px"><i class="ti ti-clock-play" style="font-size:8px"></i> Prochaine : ${escapeHtml(nextRunStr)}</div>` : ''}
      </div>
      <div style="display:flex;gap:6px;flex-shrink:0">
        <button class="btn-sm" title="Exécuter maintenant" onclick="runTaskNow('${escapeHtml(t.id)}')">
          <i class="ti ti-player-play"></i>
        </button>
        <button class="btn-sm ${t.enabled ? 'danger' : ''}" title="${t.enabled ? 'Désactiver' : 'Activer'}" onclick="toggleTask('${escapeHtml(t.id)}', ${!t.enabled})">
          <i class="ti ti-${t.enabled ? 'pause' : 'play'}"></i>
        </button>
        <button class="btn-sm danger" title="Supprimer" onclick="deleteTask('${escapeHtml(t.id)}')">
          <i class="ti ti-trash"></i>
        </button>
      </div>
    </div>`;
}

async function runTaskNow(id) {
  const r = await api.post(`/api/v1/tasks/${id}/run`);
  if (r?.success !== false) notify('Tâche lancée', 'ok');
  else notify(r?.error || 'Erreur', 'err');
}

async function toggleTask(id, enable) {
  const r = await api.patch(`/api/v1/tasks/${id}/toggle`, { enabled: enable });
  if (r?.success !== false) { notify(enable ? 'Tâche activée' : 'Tâche désactivée', 'ok'); loadTasks(); }
  else notify(r?.error || 'Erreur', 'err');
}

async function deleteTask(id) {
  if (!confirm('Supprimer cette tâche planifiée ?')) return;
  const r = await api.delete(`/api/v1/tasks/${id}`);
  if (r?.success !== false) { notify('Tâche supprimée', 'ok'); loadTasks(); }
  else notify(r?.error || 'Erreur', 'err');
}

function openTaskModal() {
  document.getElementById('task-modal')?.classList.add('open');
  document.getElementById('task-type')?.dispatchEvent(new Event('change'));
}

function closeTaskModal() {
  document.getElementById('task-modal')?.classList.remove('open');
  document.getElementById('task-form')?.reset();
}

function onTaskTypeChange() {
  const type = document.getElementById('task-type')?.value;
  document.getElementById('task-app-row').style.display   = type === 'backup' ? '' : 'none';
  document.getElementById('task-scope-row').style.display = type === 'backup' ? '' : 'none';
}

async function submitTaskForm() {
  const type  = document.getElementById('task-type').value;
  const app   = document.getElementById('task-app').value.trim();
  const scope = document.getElementById('task-scope').value;
  const time  = document.getElementById('task-time').value || '02:00';
  const daysChecked = [...document.querySelectorAll('.task-day-cb:checked')].map(cb => parseInt(cb.value));

  const [hh, mm] = time.split(':').map(Number);

  const body = {
    type,
    app:      type === 'backup' ? app : '',
    scope:    type === 'backup' ? scope : '',
    enabled:  true,
    schedule: { hour: hh || 0, minute: mm || 0, days: daysChecked },
  };

  const r = await api.post('/api/v1/tasks', body);
  if (r?.success !== false) {
    notify('Tâche créée', 'ok');
    closeTaskModal();
    loadTasks();
  } else {
    notify(r?.error || 'Erreur création tâche', 'err');
  }
}

// ── Navigation ────────────────────────────────────────────────────────────────
const SECTIONS = {
  dashboard: { label: 'TABLEAU DE BORD', num: '/00', load: loadDashboard,  content: 'content-dashboard',  btn: null },
  apps:      { label: 'APPLICATIONS',    num: '/01', load: loadApps,       content: 'content-apps',       btn: { icon: 'ti-plus',          label: 'INSTALLER',     action: "openInstallModal(S.catalog[0]?.id)" } },
  logs:      { label: 'LOGS',            num: '/02', load: loadLogs,       content: 'content-logs',       btn: null },
  backups:   { label: 'SAUVEGARDES',     num: '/03', load: loadBackups,    content: 'content-backups',    btn: { icon: 'ti-device-floppy', label: 'SAUVEGARDER',   action: "triggerBackup()" } },
  secrets:   { label: 'SECRETS',         num: '/04', load: loadSecrets,    content: 'content-secrets',    btn: { icon: 'ti-lock-open',     label: 'DÉVERROUILLER', action: "unlockSecrets()" } },
  locations: { label: 'EMPLACEMENTS',    num: '/05', load: loadLocations,  content: 'content-locations',  btn: { icon: 'ti-plus',          label: 'AJOUTER',       action: "openAddLocationModal()" } },
  tasks:     { label: 'TÂCHES',           num: '/06', load: loadTasks,      content: 'content-tasks',      btn: { icon: 'ti-plus', label: 'NOUVELLE TÂCHE', action: 'openTaskModal()' } },
  events:    { label: 'ÉVÉNEMENTS',       num: '/07', load: loadEvents,     content: 'content-events',     btn: null },
  audit:     { label: 'AUDIT',           num: '/08', load: loadAudit,      content: 'content-audit',      btn: null },
  settings:  { label: 'PARAMÈTRES',      num: '/09', load: loadSettings,   content: 'content-settings',   btn: null },
  stats:     { label: 'RESSOURCES',      num: '/10', load: loadStats,      content: 'content-stats',      btn: null },
  terminal:  { label: 'TERMINAL',        num: '/11', load: loadTerminal,   content: 'content-terminal',   btn: null },
  services:  { label: 'SERVICES',        num: '/12', load: loadServices,   content: 'content-services',   btn: null },
  network:   { label: 'RÉSEAU',          num: '/13', load: loadNetwork,    content: 'content-network',    btn: null },
  storage:   { label: 'STOCKAGE',        num: '/14', load: loadStorage,    content: 'content-storage',    btn: null },
  journal:   { label: 'JOURNAL',         num: '/15', load: loadJournal,    content: 'content-journal',    btn: null },
  integrations: { label: 'INTÉGRATIONS', num: '/INT', load: buildDynamicNav, content: 'content-integrations', btn: null },
};

// ── Intégrations apps (panels embarqués) ─────────────────────────────────────
// Définit les sections de nav ajoutées dynamiquement selon les apps installées.
const APP_PANELS = {
  'authentik': {
    group: '// IDENTITÉS',
    icon: 'ti-shield-check',
    panels: [
      { id: 'panel-authentik-users',  label: 'ANNUAIRE',   icon: 'ti-users',   load: loadAuthentikUsers  },
      { id: 'panel-authentik-groups', label: 'GROUPES',    icon: 'ti-sitemap', load: loadAuthentikGroups },
    ],
  },
  'nextcloud': {
    group: '// CLOUD',
    icon: 'ti-cloud',
    panels: [
      { id: 'panel-nextcloud-files', label: 'FICHIERS', icon: 'ti-files',      load: loadNextcloudFiles },
      { id: 'panel-nextcloud-users', label: 'COMPTES',  icon: 'ti-users',      load: loadNextcloudUsers },
    ],
  },
  'gitea': {
    group: '// DEV',
    icon: 'ti-git-merge',
    panels: [
      { id: 'panel-gitea-repos',  label: 'DÉPÔTS',  icon: 'ti-book',       load: loadGiteaRepos  },
      { id: 'panel-gitea-issues', label: 'ISSUES',  icon: 'ti-circle-dot', load: loadGiteaIssues },
    ],
  },
  'vaultwarden': {
    group: '// SÉCURITÉ',
    icon: 'ti-lock',
    panels: [
      { id: 'panel-vaultwarden-users', label: 'COMPTES', icon: 'ti-users', load: loadVaultwardenUsers },
    ],
  },
  'arr-stack': {
    group: '// MÉDIAS',
    icon: 'ti-movie',
    panels: [
      { id: 'panel-arr-queue',  label: 'QUEUE',   icon: 'ti-list',        load: loadArrQueue  },
      { id: 'panel-arr-series', label: 'SÉRIES',  icon: 'ti-device-tv',   load: loadArrSeries },
      { id: 'panel-arr-films',  label: 'FILMS',   icon: 'ti-movie',       load: loadArrFilms  },
    ],
  },
  'azuracast': {
    group: '// RADIO',
    icon: 'ti-radio',
    panels: [
      { id: 'panel-azuracast', label: 'RADIO', icon: 'ti-broadcast', load: loadAzuraCast },
    ],
  },
  'gaiverland-radio': {
    group: '// RADIO',
    icon: 'ti-music',
    panels: [
      { id: 'panel-gaiverland-live',     label: 'EN DIRECT',    icon: 'ti-broadcast',   load: loadGaiverlandLive     },
      { id: 'panel-gaiverland-schedule', label: 'HORAIRES',     icon: 'ti-clock',       load: loadGaiverlandSchedule },
      { id: 'panel-gaiverland-styles',   label: 'PROGRAMMATION', icon: 'ti-adjustments', load: loadGaiverlandStyles   },
    ],
  },
  'pterodactyl': {
    group: '// JEUX',
    icon: 'ti-device-gamepad',
    panels: [
      { id: 'panel-pterodactyl', label: 'SERVEURS', icon: 'ti-server-2', load: loadPterodactyl },
    ],
  },
  'prometheus-grafana': {
    group: '// MONITORING',
    icon: 'ti-chart-line',
    panels: [
      { id: 'panel-grafana-dashboards', label: 'DASHBOARDS', icon: 'ti-layout-dashboard', load: loadGrafanaDashboards },
    ],
  },
  'jellyfin': {
    group: '// MÉDIAS',
    icon: 'ti-player-play',
    panels: [
      { id: 'panel-jellyfin-libraries', label: 'BIBLIOTHÈQUES', icon: 'ti-movie',   load: loadJellyfinLibraries },
      { id: 'panel-jellyfin-recent',    label: 'RÉCENTS',        icon: 'ti-clock',  load: loadJellyfinRecent    },
    ],
  },
  'immich': {
    group: '// PHOTOS',
    icon: 'ti-photo',
    panels: [
      { id: 'panel-immich-stats',  label: 'STATISTIQUES', icon: 'ti-chart-bar',    load: loadImmichStats  },
      { id: 'panel-immich-albums', label: 'ALBUMS',        icon: 'ti-photo-album', load: loadImmichAlbums },
    ],
  },
  'wikijs': {
    group: '// WIKI',
    icon: 'ti-book',
    panels: [
      { id: 'panel-wikijs-pages', label: 'PAGES', icon: 'ti-file-text', load: loadWikiPages },
    ],
  },
  'ghost': {
    group: '// BLOG',
    icon: 'ti-ghost',
    panels: [
      { id: 'panel-ghost-posts', label: 'ARTICLES', icon: 'ti-writing', load: loadGhostPosts },
    ],
  },
  'wordpress': {
    group: '// WORDPRESS',
    icon: 'ti-world-www',
    panels: [
      { id: 'panel-wp-posts', label: 'ARTICLES', icon: 'ti-writing', load: loadWPPosts },
      { id: 'panel-wp-pages', label: 'PAGES',    icon: 'ti-layout',  load: loadWPPages },
    ],
  },
  'glpi': {
    group: '// HELPDESK',
    icon: 'ti-ticket',
    panels: [
      { id: 'panel-glpi-tickets', label: 'TICKETS', icon: 'ti-ticket', load: loadGLPITickets },
    ],
  },
  'pihole': {
    group: '// DNS',
    icon: 'ti-shield-bolt',
    panels: [
      { id: 'panel-pihole-stats', label: 'STATISTIQUES', icon: 'ti-chart-bar', load: loadPiholeStats },
      { id: 'panel-pihole-lists', label: 'BLOCKLISTS',   icon: 'ti-list',      load: loadPiholeLists },
    ],
  },
  'adguard': {
    group: '// DNS',
    icon: 'ti-shield-bolt',
    panels: [
      { id: 'panel-adguard-stats', label: 'STATISTIQUES', icon: 'ti-chart-bar', load: loadAdGuardStats },
    ],
  },
  'uptime-kuma': {
    group: '// MONITORING',
    icon: 'ti-heartbeat',
    panels: [
      { id: 'panel-uptime-monitors',  label: 'MONITEURS',    icon: 'ti-activity',  load: loadUptimeMonitors },
      { id: 'panel-uptime-incidents', label: 'INCIDENTS',    icon: 'ti-alert-circle', load: loadUptimeIncidents },
    ],
  },
  'memos': {
    group: '// NOTES',
    icon: 'ti-notes',
    panels: [
      { id: 'panel-memos-recent', label: 'NOTES RÉCENTES', icon: 'ti-file-text', load: loadMemosRecent },
    ],
  },
  'linkding': {
    group: '// PRODUCTIVITÉ',
    icon: 'ti-bookmark',
    panels: [
      { id: 'panel-linkding-bookmarks', label: 'FAVORIS',      icon: 'ti-bookmark',     load: loadLinkdingBookmarks },
      { id: 'panel-linkding-tags',      label: 'ÉTIQUETTES',   icon: 'ti-tag',          load: loadLinkdingTags },
    ],
  },
  'paperless-ngx': {
    group: '// PRODUCTIVITÉ',
    icon: 'ti-file-invoice',
    panels: [
      { id: 'panel-paperless-docs',   label: 'DOCUMENTS',     icon: 'ti-files',        load: loadPaperlessDocs },
      { id: 'panel-paperless-inbox',  label: 'BOÎTE ENTRÉE',  icon: 'ti-inbox',        load: loadPaperlessInbox },
    ],
  },
  'freshrss': {
    group: '// MÉDIAS',
    icon: 'ti-rss',
    panels: [
      { id: 'panel-freshrss-feeds',    label: 'FLUX RSS',    icon: 'ti-rss',       load: loadFreshRssFeeds },
      { id: 'panel-freshrss-articles', label: 'NON LUS',     icon: 'ti-article',   load: loadFreshRssUnread },
    ],
  },
  'syncthing': {
    group: '// STOCKAGE',
    icon: 'ti-refresh',
    panels: [
      { id: 'panel-syncthing-status', label: 'ÉTAT SYNC', icon: 'ti-refresh', load: loadSyncthingStatus },
    ],
  },
  'stirling-pdf': {
    group: '// OUTILS',
    icon: 'ti-file-type-pdf',
    panels: [
      { id: 'panel-stirling-pdf', label: 'STIRLING PDF', icon: 'ti-file-type-pdf', load: loadStirlingPDF },
    ],
  },
  'ntfy': {
    group: '// OUTILS',
    icon: 'ti-bell',
    panels: [
      { id: 'panel-ntfy-topics', label: 'TOPICS', icon: 'ti-bell', load: loadNtfyTopics },
    ],
  },
  'n8n': {
    group: '// AUTOMATION',
    icon: 'ti-workflow',
    panels: [
      { id: 'panel-n8n-workflows', label: 'WORKFLOWS', icon: 'ti-workflow', load: loadN8nWorkflows },
    ],
  },
  'filebrowser': {
    group: '// OUTILS',
    icon: 'ti-folder-open',
    panels: [
      { id: 'panel-filebrowser', label: 'FICHIERS', icon: 'ti-folder-open', load: loadFileBrowser },
    ],
  },
  'mealie': {
    group: '// LIFESTYLE',
    icon: 'ti-salad',
    panels: [
      { id: 'panel-mealie-recipes', label: 'RECETTES', icon: 'ti-salad', load: loadMealieRecipes },
    ],
  },
  'changedetection': {
    group: '// MONITORING',
    icon: 'ti-eye',
    panels: [
      { id: 'panel-changedetection-watches', label: 'SURVEILLANCES', icon: 'ti-eye', load: loadChangedetectionWatches },
    ],
  },
  'wg-easy': {
    group: '// RÉSEAU',
    icon: 'ti-vpn',
    panels: [
      { id: 'panel-wgeasy-peers', label: 'PAIRS VPN', icon: 'ti-vpn', load: loadWgEasyPeers },
    ],
  },
  'crowdsec': {
    group: '// SÉCURITÉ',
    icon: 'ti-shield-check',
    panels: [
      { id: 'panel-crowdsec-decisions', label: 'DÉCISIONS', icon: 'ti-shield-check', load: loadCrowdsecDecisions },
      { id: 'panel-crowdsec-alerts',    label: 'ALERTES',   icon: 'ti-alert-triangle', load: loadCrowdsecAlerts },
    ],
  },
  'gotify': {
    group: '// OUTILS',
    icon: 'ti-bell-ringing',
    panels: [
      { id: 'panel-gotify-messages', label: 'MESSAGES', icon: 'ti-bell-ringing', load: loadGotifyMessages },
    ],
  },
  'homarr': {
    group: '// OUTILS',
    icon: 'ti-layout-dashboard',
    panels: [
      { id: 'panel-homarr-dashboard', label: 'DASHBOARD', icon: 'ti-layout-dashboard', load: loadHomarrDashboard },
    ],
  },
  'grocy': {
    group: '// LIFESTYLE',
    icon: 'ti-shopping-cart',
    panels: [
      { id: 'panel-grocy-stock', label: 'STOCK', icon: 'ti-shopping-cart', load: loadGrocyStock },
      { id: 'panel-grocy-tasks', label: 'TÂCHES', icon: 'ti-checklist', load: loadGrocyTasks },
    ],
  },
  'jellyseerr': {
    group: '// MÉDIAS',
    icon: 'ti-ticket',
    panels: [
      { id: 'panel-jellyseerr-requests', label: 'DEMANDES', icon: 'ti-ticket', load: loadJellyseerrRequests },
    ],
  },
  'home-assistant': {
    group: '// SMART HOME',
    icon: 'ti-home-2',
    panels: [
      { id: 'panel-ha-dashboard', label: 'TABLEAU DE BORD', icon: 'ti-home-2', load: loadHADashboard },
    ],
  },
  'calibre-web': {
    group: '// MÉDIAS',
    icon: 'ti-book',
    panels: [
      { id: 'panel-calibre-books', label: 'BIBLIOTHÈQUE', icon: 'ti-book', load: loadCalibreBooks },
    ],
  },
  'navidrome': {
    group: '// MÉDIAS',
    icon: 'ti-music',
    panels: [
      { id: 'panel-navidrome-library', label: 'BIBLIOTHÈQUE', icon: 'ti-music', load: loadNavidromeLibrary },
    ],
  },
  'photoprism': {
    group: '// MÉDIAS',
    icon: 'ti-camera',
    panels: [
      { id: 'panel-photoprism-stats', label: 'STATISTIQUES', icon: 'ti-chart-bar', load: loadPhotoprismStats },
    ],
  },
  'kavita': {
    group: '// MÉDIAS',
    icon: 'ti-books',
    panels: [
      { id: 'panel-kavita-library', label: 'BIBLIOTHÈQUE', icon: 'ti-books', load: loadKavitaLibrary },
    ],
  },
  'komga': {
    group: '// LECTURE',
    icon: 'ti-notebook',
    panels: [
      { id: 'panel-komga-library', label: 'BIBLIOTHÈQUE', icon: 'ti-books', load: loadKomgaLibrary },
    ],
  },
  'code-server': {
    group: '// DEV',
    icon: 'ti-code',
    panels: [
      { id: 'panel-code-server', label: 'ÉDITEUR', icon: 'ti-code', load: loadCodeServer },
    ],
  },
  'scrutiny': {
    group: '// SYSTÈME',
    icon: 'ti-scan',
    panels: [
      { id: 'panel-scrutiny-disks', label: 'DISQUES', icon: 'ti-database', load: loadScrutinyDisks },
    ],
  },
  'traefik': {
    group: '// RÉSEAU',
    icon: 'ti-route',
    panels: [
      { id: 'panel-traefik-routes',   label: 'ROUTES',   icon: 'ti-route',   load: loadTraefikRoutes   },
      { id: 'panel-traefik-services', label: 'SERVICES', icon: 'ti-server',  load: loadTraefikServices },
    ],
  },
};

// ── Sidebar collapsible ───────────────────────────────────────────────────────
function toggleSbGroup(gid) {
  const toggle = document.querySelector(`[data-gid="${gid}"]`);
  const items  = document.getElementById(`sb-items-${gid}`);
  if (!toggle || !items) return;
  const collapsed = items.classList.toggle('collapsed');
  toggle.classList.toggle('collapsed', collapsed);
  try { localStorage.setItem('sb-col-' + gid, collapsed ? '1' : '0'); } catch(e) {}
}

function restoreSbGroups() {
  document.querySelectorAll('[data-gid]').forEach(toggle => {
    const gid = toggle.dataset.gid;
    try {
      if (localStorage.getItem('sb-col-' + gid) === '1') {
        const items = document.getElementById(`sb-items-${gid}`);
        if (items) { items.classList.add('collapsed'); toggle.classList.add('collapsed'); }
      }
    } catch(e) {}
  });
}

// ── App pinning (localStorage) ────────────────────────────────────────────────
function getPins() {
  try { return JSON.parse(localStorage.getItem('caleope-pins') || '[]'); } catch(e) { return []; }
}
function savePins(pins) {
  try { localStorage.setItem('caleope-pins', JSON.stringify(pins)); } catch(e) {}
}
function togglePinApp(appId) {
  const pins = getPins();
  const idx = pins.indexOf(appId);
  if (idx >= 0) pins.splice(idx, 1); else pins.push(appId);
  savePins(pins);
  buildPinnedSection();
  renderApps();
}
function buildPinnedSection() {
  const list = document.getElementById('sb-pins-list');
  const section = document.getElementById('sb-section-pins');
  if (!list || !section) return;
  const pins = getPins();
  const pinned = pins.map(id => S.apps.find(a => a.id === id)).filter(Boolean);
  if (!pinned.length) { section.style.display = 'none'; return; }
  section.style.display = '';
  list.innerHTML = pinned.map(a => {
    const domain = a.domain ? `https://${a.domain}` : null;
    const isRun = a.status === 'running';
    const dot = `<span style="display:inline-block;width:5px;height:5px;border-radius:50%;background:${isRun ? 'var(--green-b)' : 'var(--red-b)'};margin-right:5px;flex-shrink:0"></span>`;
    if (domain) {
      return `<a href="${domain}" target="_blank" rel="noopener"
        class="nav-btn" style="text-decoration:none;display:flex;align-items:center">
        ${dot}<span style="font-size:11px">${icon(a.id)}</span>
        <span style="margin-left:5px">${escapeHtml(a.name || a.id)}</span>
        <i class="ti ti-external-link" style="font-size:9px;margin-left:auto;opacity:.4"></i>
      </a>`;
    }
    return `<button class="nav-btn" onclick="goSection('apps')" style="display:flex;align-items:center">
      ${dot}<span style="font-size:11px">${icon(a.id)}</span>
      <span style="margin-left:5px">${escapeHtml(a.name || a.id)}</span>
    </button>`;
  }).join('');
}

// Intégrations : au lieu d'empiler des sous-groupes déroulants dans la sidebar
// (qui prenaient tout l'espace vertical), on enregistre les panels puis on rend
// une PAGE dédiée (cartes par app). La sidebar garde un seul item "INTÉGRATIONS".
function buildDynamicNav() {
  const installedIds = new Set(S.apps.map(a => a.id));
  const content = document.querySelector('.content');
  let hasAny = false;
  const cards = [];

  Object.entries(APP_PANELS).forEach(([appId, app]) => {
    if (!installedIds.has(appId)) return;
    hasAny = true;
    const appObj = S.apps.find(a => a.id === appId);
    const isRunning = appObj?.status === 'running';

    // Enregistrer les sections de panels + créer leurs content divs
    // (goSection('panel-...') s'appuie dessus — inchangé).
    app.panels.forEach(panel => {
      if (!SECTIONS[panel.id]) {
        SECTIONS[panel.id] = {
          label: panel.label, num: '/INT', load: panel.load,
          content: `content-${panel.id}`, btn: null,
        };
      }
      if (content && !document.getElementById(`content-${panel.id}`)) {
        const el = document.createElement('div');
        el.id = `content-${panel.id}`;
        el.className = 'section-content hidden';
        content.appendChild(el);
      }
    });

    // Carte de l'app pour la page dédiée
    const dot = `<span style="display:inline-block;width:6px;height:6px;border-radius:50%;background:${isRunning ? 'var(--green-b)' : 'var(--red-b)'};flex-shrink:0"></span>`;
    const panelBtns = app.panels.map(p =>
      `<button class="btn" onclick="goSection('${p.id}')" style="justify-content:flex-start"><i class="ti ${p.icon}"></i>${p.label}</button>`
    ).join('');
    cards.push(`
      <div class="int-card" style="background:var(--bg2);border:1px solid var(--border);border-radius:10px;padding:16px">
        <div style="display:flex;align-items:center;gap:8px;margin-bottom:12px">
          <i class="ti ${app.icon}" style="font-size:18px;color:var(--vio-b)"></i>
          ${dot}
          <span style="font-weight:700;font-size:12px;letter-spacing:.3px">${escapeHtml(appObj?.name || appId)}</span>
          <span style="margin-left:auto;font-size:8px;color:var(--text3);letter-spacing:.5px">${app.group || ''}</span>
        </div>
        <div style="display:flex;flex-wrap:wrap;gap:6px">${panelBtns}</div>
      </div>`);
  });

  // Rendre la page dédiée
  const grid = document.getElementById('integrations-grid');
  if (grid) {
    grid.style.cssText = 'display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:14px';
    grid.innerHTML = hasAny ? cards.join('')
      : '<div style="padding:40px;text-align:center;color:var(--text3);font-size:10px">Aucune intégration disponible. Installe une app compatible (Authentik, Nextcloud, Gitea…) pour la gérer ici.</div>';
  }

  // Item de nav unique (visible seulement si au moins une intégration installée)
  const navItem = document.getElementById('nav-integrations');
  if (navItem) navItem.style.display = hasAny ? '' : 'none';

  // Neutraliser l'ancien groupe déroulant de la sidebar
  const oldGroup = document.getElementById('sb-section-integrations');
  if (oldGroup) oldGroup.style.display = 'none';

  // Re-marquer le bouton actif
  document.querySelectorAll('.nav-btn').forEach(b => {
    b.classList.toggle('active', b.dataset.section === S.section);
  });
}

// ── Authentik — Annuaire utilisateurs ────────────────────────────────────────
async function loadAuthentikUsers() {
  const c = document.getElementById('content-panel-authentik-users');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT ANNUAIRE...</div>`;

  const r = await fetch('/ui/proxy/authentik/api/v3/core/users/?page_size=50&ordering=username');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-users"></i></div>
      <div class="empty-title">ANNUAIRE INDISPONIBLE</div>
      <div class="empty-sub">Authentik doit être installé et le token disponible.</div></div>`;
    return;
  }
  const data = await r.json();
  // Filtrer les comptes de service Authentik (outposts, etc.)
  const users = (data.results || []).filter(u => u.type !== 'internal_service_account');
  const total = data.pagination?.count ?? data.results?.length ?? 0;

  if (!users.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-users"></i></div>
      <div class="empty-title">AUCUN UTILISATEUR</div></div>`;
    return;
  }

  const rows = users.map(u => {
    const initials = ((u.name || u.username || '?')[0]).toUpperCase();
    const active = u.is_active !== false;
    const lastLogin = u.last_login ? new Date(u.last_login).toLocaleDateString('fr-FR') : '—';
    return `
      <div class="loc-row" style="gap:12px">
        <div style="width:28px;height:28px;border-radius:2px;background:var(--vio-dim);color:var(--vio-b);
          display:flex;align-items:center;justify-content:center;font-size:12px;font-weight:700;flex-shrink:0">${escapeHtml(initials)}</div>
        <div style="flex:1;min-width:0">
          <div style="font-size:10px;font-weight:700">${escapeHtml(u.username || '—')}</div>
          <div style="font-size:9px;color:var(--text3)">${escapeHtml(u.email || '—')}</div>
        </div>
        <div style="text-align:right;flex-shrink:0">
          <div>${active
            ? '<span class="badge badge-run" style="font-size:7px">ACTIF</span>'
            : '<span class="badge badge-err" style="font-size:7px">INACTIF</span>'}</div>
          <div style="font-size:8px;color:var(--text3);margin-top:2px">${escapeHtml(lastLogin)}</div>
        </div>
      </div>`;
  }).join('');

  const appUrl = S.apps.find(a => a.id === 'authentik')?.domain;
  const adminLink = appUrl ? `<a href="https://${appUrl}/if/admin/#/identity/users" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>GÉRER</a>` : '';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-users" style="font-size:12px"></i> UTILISATEURS
        <span style="color:var(--text3);font-size:9px">${users.length}</span>
        ${adminLink}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── Authentik — Groupes ───────────────────────────────────────────────────────
async function loadAuthentikGroups() {
  const c = document.getElementById('content-panel-authentik-groups');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT GROUPES...</div>`;

  const r = await fetch('/ui/proxy/authentik/api/v3/core/groups/?page_size=50&ordering=name');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-sitemap"></i></div>
      <div class="empty-title">GROUPES INDISPONIBLES</div></div>`;
    return;
  }
  const data = await r.json();
  const groups = data.results || [];

  if (!groups.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-sitemap"></i></div>
      <div class="empty-title">AUCUN GROUPE</div></div>`;
    return;
  }

  const rows = groups.map(g => `
    <div class="loc-row">
      <div style="width:28px;height:28px;border-radius:2px;background:var(--bg3);
        display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="ti ti-users-group" style="font-size:12px;color:var(--text2)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(g.name || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${(g.users?.length ?? g.users_obj?.length ?? 0)} membre(s)</div>
      </div>
      ${g.is_superuser ? '<span class="badge badge-warn" style="font-size:7px">SUPER</span>' : ''}
    </div>`).join('');

  const appUrl = S.apps.find(a => a.id === 'authentik')?.domain;
  const adminLink = appUrl ? `<a href="https://${appUrl}/if/admin/#/identity/groups" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>GÉRER</a>` : '';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-sitemap" style="font-size:12px"></i> GROUPES
        <span style="color:var(--text3);font-size:9px">${groups.length} groupe(s)</span>
        ${adminLink}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── Nextcloud — Fichiers récents ──────────────────────────────────────────────
async function loadNextcloudFiles() {
  const c = document.getElementById('content-panel-nextcloud-files');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT FICHIERS...</div>`;

  const appDomain = S.apps.find(a => a.id === 'nextcloud')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR</a>` : '';

  // OCS Files API — activité récente
  const r = await fetch('/ui/proxy/nextcloud/ocs/v2.php/apps/activity/api/v2/activity/all?limit=20&format=json', {
    headers: { 'OCS-APIRequest': 'true' },
  });
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-files"></i></div>
      <div class="empty-title">FICHIERS INDISPONIBLES</div>
      <div class="empty-sub">API activité Nextcloud non accessible.</div>
      <div style="margin-top:12px">${adminLink}</div></div>`;
    return;
  }
  const data = await r.json();
  const activities = data?.ocs?.data || [];
  if (!activities.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-files"></i></div>
      <div class="empty-title">AUCUNE ACTIVITÉ</div></div>`;
    return;
  }

  const rows = activities.map(a => {
    const icon = a.type === 'file_created' ? 'ti-file-plus' : a.type === 'file_deleted' ? 'ti-file-minus' : 'ti-file';
    const color = a.type === 'file_created' ? 'var(--ok)' : a.type === 'file_deleted' ? 'var(--err)' : 'var(--text2)';
    const ts = a.datetime ? new Date(a.datetime).toLocaleDateString('fr-FR') : '';
    const subject = a.subject || a.message || '—';
    return `<div class="loc-row" style="gap:10px">
      <div style="width:26px;height:26px;border-radius:2px;background:var(--bg3);flex-shrink:0;
        display:flex;align-items:center;justify-content:center">
        <i class="ti ${escapeHtml(icon)}" style="font-size:11px;color:${color}"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(subject)}</div>
        <div style="font-size:8px;color:var(--text3)">${escapeHtml(a.user || '—')}</div>
      </div>
      <div style="font-size:8px;color:var(--text3);flex-shrink:0">${escapeHtml(ts)}</div>
    </div>`;
  }).join('');

  c.innerHTML = `<div class="settings-card" style="padding:0">
    <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
      <i class="ti ti-files" style="font-size:12px"></i> ACTIVITÉ RÉCENTE
      <span style="color:var(--text3);font-size:9px">${activities.length} actions</span>
      ${adminLink}
    </div>
    <div style="padding:0 12px 12px">${rows}</div>
  </div>`;
}

// ── Nextcloud — Utilisateurs ──────────────────────────────────────────────────
async function loadNextcloudUsers() {
  const c = document.getElementById('content-panel-nextcloud-users');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT COMPTES...</div>`;

  const appDomain = S.apps.find(a => a.id === 'nextcloud')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/index.php/settings/users" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>GÉRER</a>` : '';

  const r = await fetch('/ui/proxy/nextcloud/ocs/v1.php/cloud/users?limit=50&format=json', {
    headers: { 'OCS-APIRequest': 'true' },
  });
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-users"></i></div>
      <div class="empty-title">COMPTES INDISPONIBLES</div></div>`;
    return;
  }
  const data = await r.json();
  const users = data?.ocs?.data?.users || [];

  const rows = users.map(u => `
    <div class="loc-row" style="gap:10px">
      <div style="width:26px;height:26px;border-radius:2px;background:var(--vio-dim);color:var(--vio-b);
        display:flex;align-items:center;justify-content:center;font-size:11px;font-weight:700;flex-shrink:0">
        ${escapeHtml((u[0]||'?').toUpperCase())}</div>
      <div style="font-size:10px;font-weight:700">${escapeHtml(u)}</div>
    </div>`).join('');

  c.innerHTML = `<div class="settings-card" style="padding:0">
    <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
      <i class="ti ti-users" style="font-size:12px"></i> COMPTES
      <span style="color:var(--text3);font-size:9px">${users.length}</span>
      ${adminLink}
    </div>
    <div style="padding:0 12px 12px">${rows}</div>
  </div>`;
}

// ── Gitea — Dépôts ────────────────────────────────────────────────────────────
async function loadGiteaRepos() {
  const c = document.getElementById('content-panel-gitea-repos');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT DÉPÔTS...</div>`;

  const appDomain = S.apps.find(a => a.id === 'gitea')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/explore/repos" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>EXPLORER</a>` : '';

  const r = await fetch('/ui/proxy/gitea/api/v1/repos/search?limit=30&sort=updated');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-book"></i></div>
      <div class="empty-title">DÉPÔTS INDISPONIBLES</div>
      <div class="empty-sub">API Gitea non accessible.</div></div>`;
    return;
  }
  const data = await r.json();
  const repos = data?.data || [];

  if (!repos.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-book"></i></div>
      <div class="empty-title">AUCUN DÉPÔT</div></div>`;
    return;
  }

  const rows = repos.map(r => {
    const updated = r.updated ? new Date(r.updated).toLocaleDateString('fr-FR') : '—';
    const lang = r.language ? `<span style="font-size:7px;padding:1px 4px;border-radius:1px;background:var(--bg3);color:var(--text2)">${escapeHtml(r.language)}</span>` : '';
    return `<div class="loc-row" style="gap:10px">
      <div style="width:26px;height:26px;border-radius:2px;background:var(--bg3);flex-shrink:0;
        display:flex;align-items:center;justify-content:center">
        <i class="ti ${r.fork ? 'ti-git-fork' : 'ti-book'}" style="font-size:11px;color:var(--text2)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(r.full_name || r.name || '—')}</div>
        <div style="display:flex;gap:4px;align-items:center;margin-top:2px">${lang}
          ${r.private ? '<span style="font-size:7px;padding:1px 4px;background:var(--warn-dim);color:var(--warn-b);border-radius:1px">PRIVÉ</span>' : ''}
          <span style="font-size:8px;color:var(--text3)">${r.stars_count||0} ★</span>
        </div>
      </div>
      <div style="font-size:8px;color:var(--text3);flex-shrink:0">${escapeHtml(updated)}</div>
    </div>`;
  }).join('');

  c.innerHTML = `<div class="settings-card" style="padding:0">
    <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
      <i class="ti ti-book" style="font-size:12px"></i> DÉPÔTS
      <span style="color:var(--text3);font-size:9px">${repos.length}</span>
      ${adminLink}
    </div>
    <div style="padding:0 12px 12px">${rows}</div>
  </div>`;
}

// ── Gitea — Issues ouvertes ───────────────────────────────────────────────────
async function loadGiteaIssues() {
  const c = document.getElementById('content-panel-gitea-issues');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT ISSUES...</div>`;

  const appDomain = S.apps.find(a => a.id === 'gitea')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/issues" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>VOIR TOUT</a>` : '';

  const r = await fetch('/ui/proxy/gitea/api/v1/repos/search?limit=50&token=');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-circle-dot"></i></div>
      <div class="empty-title">ISSUES INDISPONIBLES</div></div>`;
    return;
  }
  const reposData = await r.json();
  const repos = reposData?.data || [];

  // Récupérer les issues ouvertes de tous les dépôts en parallèle (max 10)
  const issueResults = await Promise.all(
    repos.slice(0, 10).map(repo =>
      fetch(`/ui/proxy/gitea/api/v1/repos/${encodeURIComponent(repo.full_name)}/issues?state=open&limit=5&type=issues`)
        .then(r => r.ok ? r.json() : [])
        .catch(() => [])
    )
  );
  const issues = issueResults.flat().filter(Boolean);

  if (!issues.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-circle-check"></i></div>
      <div class="empty-title">AUCUNE ISSUE OUVERTE</div></div>`;
    return;
  }

  const rows = issues.slice(0, 30).map(i => {
    const ts = i.created_at ? new Date(i.created_at).toLocaleDateString('fr-FR') : '—';
    return `<div class="loc-row" style="gap:10px">
      <div style="width:26px;height:26px;border-radius:2px;background:var(--ok-dim);flex-shrink:0;
        display:flex;align-items:center;justify-content:center">
        <i class="ti ti-circle-dot" style="font-size:11px;color:var(--ok)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(i.title||'—')}</div>
        <div style="font-size:8px;color:var(--text3)">${escapeHtml(i.repository?.full_name||'—')} · #${i.number}</div>
      </div>
      <div style="font-size:8px;color:var(--text3);flex-shrink:0">${escapeHtml(ts)}</div>
    </div>`;
  }).join('');

  c.innerHTML = `<div class="settings-card" style="padding:0">
    <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
      <i class="ti ti-circle-dot" style="font-size:12px;color:var(--ok)"></i> ISSUES OUVERTES
      <span style="color:var(--text3);font-size:9px">${issues.length}</span>
      ${adminLink}
    </div>
    <div style="padding:0 12px 12px">${rows}</div>
  </div>`;
}

// ── Vaultwarden — Comptes ────────────────────────────────────────────────────
async function loadVaultwardenUsers() {
  const c = document.getElementById('content-panel-vaultwarden-users');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT COMPTES...</div>`;

  const appDomain = S.apps.find(a => a.id === 'vaultwarden')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/admin" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>ADMIN</a>` : '';

  try {
    const r = await fetch('/ui/proxy/vaultwarden/admin/users/overview');
    if (!r.ok) throw new Error(`HTTP ${r.status}`);

    const html = await r.text();
    const doc = new DOMParser().parseFromString(html, 'text/html');
    const emailRx = /[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}/;
    const rows = [...doc.querySelectorAll('table tbody tr')];
    const users = rows.map(tr => {
      const cells = tr.querySelectorAll('td');
      if (cells.length < 2) return null;
      // Chercher l'email dans les 3 premières cellules via regex
      let email = '', name = '';
      for (let i = 0; i < Math.min(cells.length, 3); i++) {
        const text = cells[i]?.textContent || '';
        const m = text.match(emailRx);
        if (m && !email) {
          email = m[0];
          // Le nom est la ligne de texte avant l'email dans la même cellule
          const lines = text.split(/[\n\r]+/).map(l => l.trim()).filter(l => l && !l.includes('@'));
          name = lines[0] || '';
        }
      }
      const statusText = [...cells].slice(0, 5).map(td => td.textContent).join(' ');
      const status = /invited/i.test(statusText) ? 'invited' : /disabled/i.test(statusText) ? 'disabled' : 'active';
      return email ? { email, name, status } : null;
    }).filter(Boolean);

    if (!users.length) {
      c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-lock"></i></div>
        <div class="empty-title">AUCUN COMPTE</div>
        <div style="margin-top:12px">${adminLink}</div></div>`;
      return;
    }

    const rowsHtml = users.map(u => {
      const initials = (u.email[0] || '?').toUpperCase();
      return `<div class="loc-row" style="gap:10px">
        <div style="width:26px;height:26px;border-radius:2px;background:var(--vio-dim);color:var(--vio-b);
          display:flex;align-items:center;justify-content:center;font-size:11px;font-weight:700;flex-shrink:0">
          ${escapeHtml(initials)}</div>
        <div style="flex:1;min-width:0">
          <div style="font-size:10px;font-weight:700">${escapeHtml(u.email)}</div>
          ${u.name ? `<div style="font-size:8px;color:var(--text3)">${escapeHtml(u.name)}</div>` : ''}
        </div>
        <div>
          ${u.status === 'active'
            ? '<span class="badge badge-run" style="font-size:7px">ACTIF</span>'
            : u.status === 'invited'
            ? '<span class="badge" style="font-size:7px;background:var(--warn-dim);color:var(--warn-b)">INVITÉ</span>'
            : '<span class="badge badge-err" style="font-size:7px">DÉSACTIVÉ</span>'}
        </div>
      </div>`;
    }).join('');

    c.innerHTML = `<div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-lock" style="font-size:12px"></i> COMPTES VAULTWARDEN
        <span style="color:var(--text3);font-size:9px">${users.length}</span>
        ${adminLink}
      </div>
      <div style="padding:0 12px 12px">${rowsHtml}</div>
    </div>`;
  } catch (err) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-lock"></i></div>
      <div class="empty-title">COMPTES INDISPONIBLES</div>
      <div class="empty-sub">${escapeHtml(String(err.message))}</div>
      <div style="margin-top:12px">${adminLink}</div></div>`;
  }
}

// ── Arr-stack — Queue de téléchargement ──────────────────────────────────────
async function loadArrQueue() {
  const c = document.getElementById('content-panel-arr-queue');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT QUEUE...</div>`;

  // Récupère les queues Sonarr + Radarr en parallèle
  const [sonarrQ, radarrQ] = await Promise.all([
    fetch('/ui/proxy/arr-sonarr/api/v3/queue?pageSize=20').then(r => r.ok ? r.json() : null).catch(() => null),
    fetch('/ui/proxy/arr-radarr/api/v3/queue?pageSize=20').then(r => r.ok ? r.json() : null).catch(() => null),
  ]);

  const sonarrItems = (sonarrQ?.records || []).map(i => ({...i, _src: 'SONARR'}));
  const radarrItems = (radarrQ?.records || []).map(i => ({...i, _src: 'RADARR'}));
  const all = [...sonarrItems, ...radarrItems];

  if (!all.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-check"></i></div>
      <div class="empty-title">QUEUE VIDE</div>
      <div class="empty-sub">Aucun téléchargement en cours.</div></div>`;
    return;
  }

  const rows = all.map(i => {
    const pct = i.sizeleft != null && i.size ? Math.round((1 - i.sizeleft / i.size) * 100) : 0;
    const status = i.status || '—';
    const statusColor = status === 'downloading' ? 'var(--ok)' : status === 'queued' ? 'var(--text3)' : 'var(--warn)';
    const title = i.title || i.series?.title || i.movie?.title || '—';
    return `<div class="loc-row" style="gap:10px;flex-direction:column;align-items:stretch;padding:6px 0">
      <div style="display:flex;gap:10px;align-items:center">
        <span style="font-size:7px;padding:1px 4px;background:var(--bg3);color:var(--text3);border-radius:1px;flex-shrink:0">${escapeHtml(i._src)}</span>
        <div style="flex:1;min-width:0;font-size:9px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(title)}</div>
        <span style="font-size:8px;color:${statusColor};flex-shrink:0">${escapeHtml(status)}</span>
      </div>
      <div style="height:3px;background:var(--bg3);border-radius:1px;overflow:hidden">
        <div style="height:100%;width:${pct}%;background:var(--ok);border-radius:1px;transition:.3s"></div>
      </div>
    </div>`;
  }).join('');

  c.innerHTML = `<div class="settings-card" style="padding:0">
    <div class="settings-title" style="padding:10px 12px">
      <i class="ti ti-list" style="font-size:12px"></i> QUEUE
      <span style="color:var(--text3);font-size:9px">${all.length} élément(s)</span>
    </div>
    <div style="padding:0 12px 12px">${rows}</div>
  </div>`;
}

// ── Arr-stack — Séries surveillées (Sonarr) ───────────────────────────────────
async function loadArrSeries() {
  const c = document.getElementById('content-panel-arr-series');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT SÉRIES...</div>`;

  const r = await fetch('/ui/proxy/arr-sonarr/api/v3/series?sortKey=added&sortDir=desc');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-device-tv"></i></div>
      <div class="empty-title">SONARR INDISPONIBLE</div></div>`;
    return;
  }
  const series = await r.json();
  if (!series.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-device-tv"></i></div>
      <div class="empty-title">AUCUNE SÉRIE</div></div>`;
    return;
  }

  const rows = series.slice(0, 30).map(s => {
    const pct = s.episodeCount > 0 ? Math.round(s.episodeFileCount / s.episodeCount * 100) : 0;
    const statusColor = s.monitored ? 'var(--ok)' : 'var(--text3)';
    return `<div class="loc-row" style="gap:10px">
      <div style="width:26px;height:26px;border-radius:2px;background:var(--bg3);flex-shrink:0;
        display:flex;align-items:center;justify-content:center">
        <i class="ti ti-device-tv" style="font-size:11px;color:${statusColor}"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(s.title||'—')}</div>
        <div style="font-size:8px;color:var(--text3)">${s.episodeFileCount||0}/${s.episodeCount||0} épisodes · ${pct}%</div>
      </div>
      <span style="font-size:7px;padding:1px 4px;background:var(--bg3);color:var(--text3);border-radius:1px;flex-shrink:0">${s.seasons?.length||0} S</span>
    </div>`;
  }).join('');

  c.innerHTML = `<div class="settings-card" style="padding:0">
    <div class="settings-title" style="padding:10px 12px">
      <i class="ti ti-device-tv" style="font-size:12px"></i> SÉRIES
      <span style="color:var(--text3);font-size:9px">${series.length}</span>
    </div>
    <div style="padding:0 12px 12px">${rows}</div>
  </div>`;
}

// ── Arr-stack — Films surveillés (Radarr) ─────────────────────────────────────
async function loadArrFilms() {
  const c = document.getElementById('content-panel-arr-films');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT FILMS...</div>`;

  const r = await fetch('/ui/proxy/arr-radarr/api/v3/movie?sortKey=added&sortDir=desc');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-movie"></i></div>
      <div class="empty-title">RADARR INDISPONIBLE</div></div>`;
    return;
  }
  const movies = await r.json();
  if (!movies.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-movie"></i></div>
      <div class="empty-title">AUCUN FILM</div></div>`;
    return;
  }

  const downloaded = movies.filter(m => m.hasFile);
  const missing = movies.filter(m => m.monitored && !m.hasFile);

  const renderFilm = m => {
    const year = m.year ? ` (${m.year})` : '';
    const hasFile = m.hasFile;
    return `<div class="loc-row" style="gap:10px">
      <div style="width:26px;height:26px;border-radius:2px;background:var(--bg3);flex-shrink:0;
        display:flex;align-items:center;justify-content:center">
        <i class="ti ti-movie" style="font-size:11px;color:${hasFile ? 'var(--ok)' : 'var(--warn)'}"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:9px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml((m.title||'—')+year)}</div>
        <div style="font-size:8px;color:var(--text3)">${escapeHtml(m.studio||m.genres?.[0]||'—')}</div>
      </div>
      <span class="badge ${hasFile ? 'badge-run' : 'badge-warn'}" style="font-size:7px;flex-shrink:0">${hasFile ? 'OK' : 'MANQUANT'}</span>
    </div>`;
  };

  const allRows = [...missing.slice(0,10), ...downloaded.slice(0,20)].map(renderFilm).join('');

  c.innerHTML = `<div class="settings-card" style="padding:0">
    <div class="settings-title" style="padding:10px 12px">
      <i class="ti ti-movie" style="font-size:12px"></i> FILMS
      <span style="color:var(--text3);font-size:9px">${downloaded.length} dispo · ${missing.length} manquants</span>
    </div>
    <div style="padding:0 12px 12px">${allRows}</div>
  </div>`;
}

// ── AzuraCast — Stations ──────────────────────────────────────────────────────
async function loadAzuraCast() {
  const c = document.getElementById('content-panel-azuracast');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'azuracast')?.domain;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT RADIO...</div>`;

  // AzuraCast : token via login avec les credentials stockés
  const r = await fetch('/ui/proxy/azuracast/api/stations');
  if (!r.ok) {
    // Fallback : lien vers l'interface admin
    const link = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
      class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR AZURACAST</a>` : '';
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-radio"></i></div>
      <div class="empty-title">RADIO INDISPONIBLE</div>
      <div class="empty-sub">Clé API non disponible — configurer AZURACAST_API_KEY dans les secrets.</div>
      <div style="margin-top:12px">${link}</div></div>`;
    return;
  }
  const stations = await r.json();

  const rows = (Array.isArray(stations) ? stations : []).map(s => {
    const online = s.is_online;
    const listeners = s.listeners?.current ?? '—';
    return `
      <div class="loc-row">
        <div style="width:28px;height:28px;border-radius:2px;background:var(--bg3);
          display:flex;align-items:center;justify-content:center;flex-shrink:0">
          <i class="ti ti-broadcast" style="font-size:12px;color:var(--text2)"></i>
        </div>
        <div style="flex:1;min-width:0">
          <div style="font-size:10px;font-weight:700">${escapeHtml(s.name || '—')}</div>
          <div style="font-size:9px;color:var(--text3)">${escapeHtml(s.short_name || '')}
            ${online ? `· ${listeners} auditeur(s)` : ''}</div>
        </div>
        ${online
          ? '<span class="badge badge-run" style="font-size:7px">EN LIGNE</span>'
          : '<span class="badge" style="font-size:7px;opacity:.6">HORS LIGNE</span>'}
      </div>`;
  }).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune station configurée.</div>';

  const adminLink = appDomain ? `<a href="https://${appDomain}/admin" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>GÉRER</a>` : '';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-broadcast" style="font-size:12px"></i> STATIONS
        ${adminLink}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── Gaiverland Radio — En direct ─────────────────────────────────────────────
async function loadGaiverlandLive() {
  const c = document.getElementById('content-panel-gaiverland-live');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/gaiverland-radio/nowplaying');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-music-off"></i></div>
      <div class="empty-title">RADIO INDISPONIBLE</div>
      <div class="empty-sub">Le moteur de playlist Gaiverland ne répond pas.</div></div>`;
    return;
  }
  const d = await r.json();
  const np = d.now_playing || {};
  const lib = d.library || {};

  const moodColors = { festival: 'var(--vio)', intense: 'var(--red-b)', energique: 'var(--blue)',
                       melodique: 'var(--green-b)', nocturne: '#8b7cf6' };
  const moodColor = moodColors[d.mood] || 'var(--text2)';

  const artHtml = (np.art && np.art.startsWith('http'))
    ? `<img src="${np.art}" style="width:56px;height:56px;border-radius:4px;object-fit:cover;flex-shrink:0" onerror="this.style.display='none'">`
    : `<div style="width:56px;height:56px;border-radius:4px;background:var(--bg3);display:flex;align-items:center;justify-content:center;flex-shrink:0"><i class="ti ti-music" style="font-size:20px;color:var(--text3)"></i></div>`;

  const progress = np.duration > 0
    ? `<div style="height:2px;background:var(--bg3);border-radius:1px;margin-top:8px;overflow:hidden">
        <div style="height:100%;background:var(--vio);width:${Math.min(100, Math.round(np.elapsed/np.duration*100))}%;transition:width 1s linear"></div>
       </div>` : '';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card" style="flex:1">
        <div class="stat-val">${lib.analyzed ?? '—'}</div>
        <div class="stat-lbl">TRACKS ANALYSÉES</div>
      </div>
      <div class="stat-card" style="flex:1">
        <div class="stat-val">${d.listeners ?? '—'}</div>
        <div class="stat-lbl">AUDITEURS</div>
      </div>
      <div class="stat-card" style="flex:1">
        <div class="stat-val" style="color:${moodColor};text-transform:uppercase">${d.mood || '—'}</div>
        <div class="stat-lbl">MOOD</div>
      </div>
    </div>

    <div class="settings-card" style="padding:12px">
      <div style="display:flex;align-items:center;gap:10px">
        ${artHtml}
        <div style="flex:1;min-width:0">
          <div style="font-size:11px;font-weight:800;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">
            ${escapeHtml(np.title || 'Aucune lecture en cours')}
          </div>
          <div style="font-size:9px;color:var(--text3);margin-top:2px">${escapeHtml(np.artist || '')}</div>
          ${d.current_playlist ? `<div style="font-size:8px;color:var(--vio);margin-top:4px;text-transform:uppercase">${escapeHtml(d.current_playlist)}</div>` : ''}
          ${progress}
        </div>
        ${d.is_online ? '<span class="badge badge-run" style="font-size:7px;flex-shrink:0">EN LIGNE</span>'
                      : '<span class="badge badge-stop" style="font-size:7px;flex-shrink:0">HORS LIGNE</span>'}
      </div>
    </div>

    <div style="margin-top:8px;text-align:right">
      <button class="btn-sm" onclick="loadGaiverlandLive()" style="font-size:8px">
        <i class="ti ti-refresh" style="font-size:9px"></i> ACTUALISER
      </button>
    </div>`;
}

// ── Gaiverland Radio — Horaires ───────────────────────────────────────────────
async function loadGaiverlandSchedule() {
  const c = document.getElementById('content-panel-gaiverland-schedule');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/gaiverland-radio/config');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-clock-off"></i></div>
      <div class="empty-title">CONFIGURATION INDISPONIBLE</div></div>`;
    return;
  }
  const cfg = await r.json();

  const dayLabels = ['', 'Lun', 'Mar', 'Mer', 'Jeu', 'Ven', 'Sam', 'Dim'];
  const daysHtml = [1,2,3,4,5,6,7].map(d => {
    const on = (cfg.work_days || [1,2,3,4,5]).includes(d);
    return `<label style="display:flex;align-items:center;gap:4px;cursor:pointer;font-size:9px">
      <input type="checkbox" id="gwr-day-${d}" ${on ? 'checked' : ''} style="accent-color:var(--vio)">
      ${dayLabels[d]}
    </label>`;
  }).join('');

  const weights = cfg.playlist_weights || {};
  const weightRow = (key, label, icon) => {
    const w = weights[key] ?? 1;
    return `<div style="display:flex;align-items:center;gap:8px;padding:6px 0;border-bottom:1px solid var(--bd)">
      <i class="ti ${icon}" style="font-size:11px;color:var(--text3);flex-shrink:0"></i>
      <div style="flex:1;font-size:9px;font-weight:700">${label}</div>
      <input type="range" id="gwr-w-${key}" min="0" max="5" value="${w}" step="1"
        style="width:80px;accent-color:var(--vio)" oninput="document.getElementById('gwr-wv-${key}').textContent=this.value">
      <span id="gwr-wv-${key}" style="font-size:10px;font-weight:800;color:var(--vio);min-width:12px;text-align:right">${w}</span>
    </div>`;
  };

  c.innerHTML = `
    <div class="settings-card" style="padding:0;margin-bottom:12px">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-clock" style="font-size:11px"></i> HORAIRES MODE TRAVAIL
      </div>
      <div style="padding:0 12px 12px;display:flex;flex-direction:column;gap:10px">
        <div style="display:flex;align-items:center;gap:8px">
          <span style="font-size:9px;color:var(--text3);width:50px">DÉBUT</span>
          <input type="time" id="gwr-start" value="${cfg.work_start || '08:20'}"
            style="background:var(--bg2);border:1px solid var(--bd);color:var(--text1);
                   border-radius:4px;padding:4px 6px;font-size:10px;font-family:inherit">
        </div>
        <div style="display:flex;align-items:center;gap:8px">
          <span style="font-size:9px;color:var(--text3);width:50px">FIN</span>
          <input type="time" id="gwr-end" value="${cfg.work_end || '17:00'}"
            style="background:var(--bg2);border:1px solid var(--bd);color:var(--text1);
                   border-radius:4px;padding:4px 6px;font-size:10px;font-family:inherit">
        </div>
        <div style="display:flex;align-items:center;gap:8px">
          <span style="font-size:9px;color:var(--text3);width:50px">DÉCALAGE</span>
          <input type="range" id="gwr-offset" min="-60" max="60" value="${cfg.work_offset_min || 0}" step="5"
            style="width:100px;accent-color:var(--vio)" oninput="document.getElementById('gwr-offset-v').textContent=(this.value>0?'+':'')+this.value+'min'">
          <span id="gwr-offset-v" style="font-size:9px;color:var(--vio);min-width:40px">${cfg.work_offset_min > 0 ? '+' : ''}${cfg.work_offset_min || 0}min</span>
        </div>
        <div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap">
          <span style="font-size:9px;color:var(--text3);width:50px">JOURS</span>
          ${daysHtml}
        </div>
      </div>
    </div>

    <div class="settings-card" style="padding:0;margin-bottom:12px">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-playlist" style="font-size:11px"></i> POIDS DES PLAYLISTS (MODE TRAVAIL)
      </div>
      <div style="padding:0 12px 12px">
        ${weightRow('decouverte',   'Travail Découverte', 'ti-sparkles')}
        ${weightRow('bien_francais','Bien Français',      'ti-flag')}
        ${weightRow('gaiverland_ia','Gaiverland IA',      'ti-robot')}
        <div style="margin-top:8px;font-size:8px;color:var(--text3)">
          Poids 0 = désactivé · 5 = prioritaire maximum
        </div>
      </div>
    </div>

    <button class="btn btn-vio" style="width:100%;font-size:9px" onclick="saveGaiverlandSchedule()">
      <i class="ti ti-device-floppy"></i> ENREGISTRER
    </button>`;
}

async function saveGaiverlandSchedule() {
  const days = [1,2,3,4,5,6,7].filter(d => document.getElementById(`gwr-day-${d}`)?.checked);
  const weights = {};
  ['decouverte', 'bien_francais', 'gaiverland_ia'].forEach(k => {
    const el = document.getElementById(`gwr-w-${k}`);
    if (el) weights[k] = parseInt(el.value);
  });
  const payload = {
    work_start:       document.getElementById('gwr-start')?.value,
    work_end:         document.getElementById('gwr-end')?.value,
    work_offset_min:  parseInt(document.getElementById('gwr-offset')?.value || '0'),
    work_days:        days,
    playlist_weights: weights,
  };
  const r = await fetch('/ui/proxy/gaiverland-radio/config', {
    method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload),
  });
  if (r.ok) { notify('Horaires enregistrés', 'ok'); }
  else { notify('Erreur lors de la sauvegarde', 'err'); }
}

// ── Gaiverland Radio — Styles / Programmation ─────────────────────────────────
async function loadGaiverlandStyles() {
  const c = document.getElementById('content-panel-gaiverland-styles');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/gaiverland-radio/config');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-adjustments-off"></i></div>
      <div class="empty-title">CONFIGURATION INDISPONIBLE</div></div>`;
    return;
  }
  const cfg = await r.json();

  const pausedMoods  = cfg.paused_moods  || [];
  const pausedGenres = cfg.paused_genres || [];

  const ALL_MOODS = [
    { name: 'festival',  icon: 'ti-confetti',  label: 'Festival' },
    { name: 'intense',   icon: 'ti-bolt',       label: 'Intense' },
    { name: 'energique', icon: 'ti-flame',      label: 'Énergique' },
    { name: 'melodique', icon: 'ti-wave-sine',  label: 'Mélodique' },
    { name: 'nocturne',  icon: 'ti-moon',       label: 'Nocturne' },
  ];
  const ALL_GENRES = [
    { name: 'Hardstyle',       label: 'Hardstyle'      },
    { name: 'Hard Techno',     label: 'Hard Techno'    },
    { name: 'Hardcore',        label: 'Hardcore'       },
    { name: 'Drum and Bass',   label: 'Drum & Bass'    },
    { name: 'Dubstep',         label: 'Dubstep'        },
    { name: 'French House',    label: 'French House'   },
    { name: 'Deep House',      label: 'Deep House'     },
    { name: 'Tech House',      label: 'Tech House'     },
    { name: 'Progressive House', label: 'Progressive House' },
    { name: 'Trance',          label: 'Trance'         },
    { name: 'Synthwave',       label: 'Synthwave'      },
  ];

  const pausedMoodNames  = new Set(pausedMoods.map(p => p.name));
  const pausedGenreNames = new Set(pausedGenres.map(p => p.name));

  const pauseUntilSel = (id) => `
    <select id="days-${id}" style="background:var(--bg2);border:1px solid var(--bd);color:var(--text1);
      border-radius:3px;padding:2px 4px;font-size:8px;font-family:inherit">
      <option value="1">1 jour</option>
      <option value="3">3 jours</option>
      <option value="7" selected>7 jours</option>
      <option value="14">14 jours</option>
      <option value="30">1 mois</option>
    </select>`;

  const moodRows = ALL_MOODS.map(m => {
    const paused = pausedMoodNames.has(m.name);
    const until  = pausedMoods.find(p => p.name === m.name)?.until || '';
    return `<div class="loc-row" style="gap:8px">
      <i class="ti ${m.icon}" style="font-size:13px;color:var(--text3);flex-shrink:0;width:18px"></i>
      <div style="flex:1;font-size:9px;font-weight:700">${m.label}</div>
      ${paused
        ? `<span style="font-size:8px;color:var(--text3)">jusqu'au ${until}</span>
           <button class="btn-sm" style="font-size:7px;color:var(--green-b)"
             onclick="resumeGaiverlandPause('mood','${m.name}')">
             <i class="ti ti-player-play" style="font-size:8px"></i> RÉACTIVER
           </button>`
        : `${pauseUntilSel('mood-'+m.name)}
           <button class="btn-sm danger" style="font-size:7px"
             onclick="pauseGaiverlandStyle('mood','${m.name}','mood-${m.name}')">
             <i class="ti ti-pause" style="font-size:8px"></i> PAUSE
           </button>`
      }
    </div>`;
  }).join('');

  const genreRows = ALL_GENRES.map(g => {
    const paused = pausedGenreNames.has(g.name);
    const until  = pausedGenres.find(p => p.name === g.name)?.until || '';
    return `<div class="loc-row" style="gap:8px">
      <i class="ti ti-tag" style="font-size:11px;color:var(--text3);flex-shrink:0;width:18px"></i>
      <div style="flex:1;font-size:9px;font-weight:700">${g.label}</div>
      ${paused
        ? `<span style="font-size:8px;color:var(--text3)">jusqu'au ${until}</span>
           <button class="btn-sm" style="font-size:7px;color:var(--green-b)"
             onclick="resumeGaiverlandPause('genre','${g.name}')">
             <i class="ti ti-player-play" style="font-size:8px"></i> RÉACTIVER
           </button>`
        : `${pauseUntilSel('genre-'+g.name)}
           <button class="btn-sm danger" style="font-size:7px"
             onclick="pauseGaiverlandStyle('genre','${g.name}','genre-${g.name}')">
             <i class="ti ti-pause" style="font-size:8px"></i> PAUSE
           </button>`
      }
    </div>`;
  }).join('');

  c.innerHTML = `
    <div class="settings-card" style="padding:0;margin-bottom:12px">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-mood-happy" style="font-size:11px"></i> MOODS
        <span style="font-size:8px;color:var(--text3);margin-left:4px;font-weight:400">
          Suspend un mood de la rotation
        </span>
      </div>
      <div style="padding:0 12px 12px">${moodRows}</div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-tag" style="font-size:11px"></i> GENRES
        <span style="font-size:8px;color:var(--text3);margin-left:4px;font-weight:400">
          Exclut un genre du moteur de playlist
        </span>
      </div>
      <div style="padding:0 12px 12px">${genreRows}</div>
    </div>`;
}

async function pauseGaiverlandStyle(type, name, selId) {
  const days = parseInt(document.getElementById('days-'+selId)?.value || '7');
  const r = await fetch(`/ui/proxy/gaiverland-radio/config/pause?type=${type}&name=${encodeURIComponent(name)}&days=${days}`, { method: 'POST' });
  if (r.ok) { notify(`${name} mis en pause ${days}j`, 'ok'); loadGaiverlandStyles(); }
  else { notify('Erreur', 'err'); }
}

async function resumeGaiverlandPause(type, name) {
  const r = await fetch(`/ui/proxy/gaiverland-radio/config/pause?type=${type}&name=${encodeURIComponent(name)}`, { method: 'DELETE' });
  if (r.ok) { notify(`${name} réactivé`, 'ok'); loadGaiverlandStyles(); }
  else { notify('Erreur', 'err'); }
}

// ── Pterodactyl — Serveurs de jeux ────────────────────────────────────────────
async function loadPterodactyl() {
  const c = document.getElementById('content-panel-pterodactyl');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'pterodactyl')?.domain;
  const link = appDomain ? `<a href="https://${appDomain}/admin" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR LE PANEL</a>` : '';
  c.innerHTML = `
    <div class="empty-state">
      <div class="empty-icon"><i class="ti ti-device-gamepad-2"></i></div>
      <div class="empty-title">PTERODACTYL</div>
      <div class="empty-sub">Gestion des serveurs de jeux via le panel web.</div>
      <div style="margin-top:12px">${link}</div>
    </div>`;
}

// ── Grafana — Dashboards ──────────────────────────────────────────────────────
async function loadGrafanaDashboards() {
  const c = document.getElementById('content-panel-grafana-dashboards');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT GRAFANA...</div>`;

  const appDomain = S.apps.find(a => a.id === 'prometheus-grafana')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR</a>` : '';

  const [rDash, rDs] = await Promise.all([
    fetch('/ui/proxy/grafana/api/search?type=dash-db&limit=50'),
    fetch('/ui/proxy/grafana/api/datasources'),
  ]);

  if (!rDash.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-chart-line"></i></div>
      <div class="empty-title">GRAFANA INDISPONIBLE</div>
      <div class="empty-sub">Vérifier GRAFANA_ADMIN_USER/PASSWORD dans les secrets.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const dashboards = await rDash.json();
  const datasources = rDs.ok ? await rDs.json() : [];

  const statCards = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${dashboards.length}</div><div class="stat-lbl">DASHBOARDS</div></div>
      <div class="stat-card"><div class="stat-val">${datasources.length}</div><div class="stat-lbl">SOURCES</div></div>
    </div>`;

  const rows = dashboards.slice(0, 20).map(d => `
    <div class="loc-row">
      <div style="width:28px;height:28px;border-radius:2px;background:var(--bg3);
        display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="ti ti-layout-dashboard" style="font-size:12px;color:var(--vio)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(d.title || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(d.folderTitle || 'Général')}</div>
      </div>
      ${appDomain ? `<a href="https://${appDomain}${d.url}" target="_blank" rel="noopener"
        style="font-size:9px;color:var(--blue);text-decoration:none"><i class="ti ti-external-link"></i></a>` : ''}
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun dashboard.</div>';

  c.innerHTML = `${statCards}
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-layout-dashboard" style="font-size:12px"></i> DASHBOARDS
        ${adminLink}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── Jellyfin — Bibliothèques ──────────────────────────────────────────────────
async function loadJellyfinLibraries() {
  const c = document.getElementById('content-panel-jellyfin-libraries');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT JELLYFIN...</div>`;

  const appDomain = S.apps.find(a => a.id === 'jellyfin')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/web" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR JELLYFIN</a>` : '';

  const r = await fetch('/ui/proxy/jellyfin/Library/VirtualFolders');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-player-play"></i></div>
      <div class="empty-title">JELLYFIN INDISPONIBLE</div>
      <div class="empty-sub">Générer JELLYFIN_API_KEY via la reconfiguration de l'app.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const libs = await r.json();
  const iconFor = type => ({movies:'ti-movie',tvshows:'ti-device-tv',music:'ti-music',books:'ti-book',photos:'ti-photo'}[type] || 'ti-folder');

  const rows = (Array.isArray(libs) ? libs : []).map(lib => `
    <div class="loc-row">
      <div style="width:28px;height:28px;border-radius:2px;background:var(--bg3);
        display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="ti ${iconFor(lib.CollectionType)}" style="font-size:12px;color:var(--text2)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(lib.Name || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(lib.CollectionType || 'mixte')}</div>
      </div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune bibliothèque.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-movie" style="font-size:12px"></i> BIBLIOTHÈQUES
        ${adminLink ? `<a href="https://${appDomain}/web" target="_blank" rel="noopener"
          class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>GÉRER</a>` : ''}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

async function loadJellyfinRecent() {
  const c = document.getElementById('content-panel-jellyfin-recent');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/jellyfin/Items/Latest?Limit=10&IncludeItemTypes=Movie,Episode');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-clock"></i></div>
      <div class="empty-title">NON DISPONIBLE</div>
      <div class="empty-sub">Clé API Jellyfin requise.</div></div>`;
    return;
  }

  const items = await r.json();
  const rows = (Array.isArray(items) ? items : []).map(item => `
    <div class="loc-row">
      <div style="width:28px;height:28px;border-radius:2px;background:var(--bg3);
        display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="ti ${item.Type === 'Movie' ? 'ti-movie' : 'ti-device-tv'}" style="font-size:12px;color:var(--text2)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(item.Name || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(item.Type || '')}${item.ProductionYear ? ' · ' + item.ProductionYear : ''}</div>
      </div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun contenu récent.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-clock" style="font-size:12px"></i> AJOUTS RÉCENTS
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── Immich — Statistiques ────────────────────────────────────────────────────
async function loadImmichStats() {
  const c = document.getElementById('content-panel-immich-stats');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT IMMICH...</div>`;

  const appDomain = S.apps.find(a => a.id === 'immich')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR IMMICH</a>` : '';

  const r = await fetch('/ui/proxy/immich/api/server/statistics');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-photo"></i></div>
      <div class="empty-title">IMMICH INDISPONIBLE</div>
      <div class="empty-sub">Vérifier IMMICH_ADMIN_EMAIL/PASS dans les secrets.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const stats = await r.json();
  const fmtSize = bytes => {
    if (!bytes) return '—';
    const gb = bytes / 1073741824;
    return gb >= 1 ? gb.toFixed(1) + ' Go' : (bytes / 1048576).toFixed(0) + ' Mo';
  };

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${(stats.photos || 0).toLocaleString()}</div><div class="stat-lbl">PHOTOS</div></div>
      <div class="stat-card"><div class="stat-val">${(stats.videos || 0).toLocaleString()}</div><div class="stat-lbl">VIDÉOS</div></div>
      <div class="stat-card"><div class="stat-val">${fmtSize(stats.usage)}</div><div class="stat-lbl">STOCKAGE</div></div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

async function loadImmichAlbums() {
  const c = document.getElementById('content-panel-immich-albums');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT ALBUMS...</div>`;

  const r = await fetch('/ui/proxy/immich/api/albums?shared=false');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-photo-album"></i></div>
      <div class="empty-title">NON DISPONIBLE</div></div>`;
    return;
  }

  const albums = await r.json();
  const rows = (Array.isArray(albums) ? albums.slice(0,20) : []).map(a => `
    <div class="loc-row">
      <div style="width:28px;height:28px;border-radius:2px;background:var(--bg3);
        display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="ti ti-photo-album" style="font-size:12px;color:var(--text2)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(a.albumName || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${a.assetCount ?? 0} média(s)</div>
      </div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun album.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-photo-album" style="font-size:12px"></i> ALBUMS (${(Array.isArray(albums) ? albums.length : 0)})
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── WikiJS — Pages ────────────────────────────────────────────────────────────
async function loadWikiPages() {
  const c = document.getElementById('content-panel-wikijs-pages');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT WIKI...</div>`;

  const appDomain = S.apps.find(a => a.id === 'wikijs')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR WIKI</a>` : '';

  const query = JSON.stringify({ query: '{ pages { list(orderBy: UPDATED) { id title path updatedAt } } }' });
  const r = await fetch('/ui/proxy/wikijs/graphql', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: query,
  });

  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-book"></i></div>
      <div class="empty-title">WIKI.JS INDISPONIBLE</div>
      <div class="empty-sub">Générer WIKIJS_API_TOKEN dans Wiki.js → Admin → Developer Tools → API Access.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const data = await r.json();
  const pages = data?.data?.pages?.list || [];

  const rows = pages.slice(0, 20).map(p => `
    <div class="loc-row">
      <div style="width:28px;height:28px;border-radius:2px;background:var(--bg3);
        display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="ti ti-file-text" style="font-size:12px;color:var(--text2)"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(p.title || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(p.path || '')}</div>
      </div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune page.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${pages.length}</div><div class="stat-lbl">PAGES</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-file-text" style="font-size:12px"></i> PAGES RÉCENTES
        ${adminLink ? `<a href="https://${appDomain}/a" target="_blank" rel="noopener"
          class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>ADMIN</a>` : ''}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── Ghost — Articles ──────────────────────────────────────────────────────────
async function loadGhostPosts() {
  const c = document.getElementById('content-panel-ghost-posts');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT GHOST...</div>`;

  const appDomain = S.apps.find(a => a.id === 'ghost')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/ghost" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR GHOST</a>` : '';

  const r = await fetch('/ui/proxy/ghost/ghost/api/admin/posts/?limit=20&fields=id,title,status,published_at');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-ghost"></i></div>
      <div class="empty-title">GHOST INDISPONIBLE</div>
      <div class="empty-sub">Générer GHOST_ADMIN_API_KEY dans Ghost admin → Intégrations → Ajouter une intégration personnalisée.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const data = await r.json();
  const posts = data.posts || [];
  const published = posts.filter(p => p.status === 'published').length;
  const draft     = posts.filter(p => p.status === 'draft').length;

  const statusBadge = s => s === 'published'
    ? '<span class="badge badge-run" style="font-size:7px">PUBLIÉ</span>'
    : '<span class="badge" style="font-size:7px;opacity:.6">BROUILLON</span>';

  const rows = posts.slice(0, 15).map(p => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(p.title || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${p.published_at ? new Date(p.published_at).toLocaleDateString('fr') : '—'}</div>
      </div>
      ${statusBadge(p.status)}
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun article.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${published}</div><div class="stat-lbl">PUBLIÉS</div></div>
      <div class="stat-card"><div class="stat-val">${draft}</div><div class="stat-lbl">BROUILLONS</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-writing" style="font-size:12px"></i> ARTICLES
        ${adminLink ? `<a href="https://${appDomain}/ghost" target="_blank" rel="noopener"
          class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>ADMIN</a>` : ''}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── WordPress — Articles & Pages ──────────────────────────────────────────────
async function loadWPPosts() {
  const c = document.getElementById('content-panel-wp-posts');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT WORDPRESS...</div>`;

  const appDomain = S.apps.find(a => a.id === 'wordpress')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/wp-admin" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>WP ADMIN</a>` : '';

  const r = await fetch('/ui/proxy/wordpress/wp-json/wp/v2/posts?status=publish,draft&per_page=20&_fields=id,title,status,date,link');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-world-www"></i></div>
      <div class="empty-title">WORDPRESS INDISPONIBLE</div>
      <div class="empty-sub">Vérifier WP_ADMIN_USER/PASS dans les secrets. Activer l'authentification REST si nécessaire.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const posts = await r.json();
  const published = (Array.isArray(posts) ? posts : []).filter(p => p.status === 'publish').length;
  const draft     = (Array.isArray(posts) ? posts : []).filter(p => p.status === 'draft').length;

  const rows = (Array.isArray(posts) ? posts.slice(0,15) : []).map(p => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(p.title?.rendered || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${p.date ? new Date(p.date).toLocaleDateString('fr') : '—'}</div>
      </div>
      ${p.status === 'publish'
        ? '<span class="badge badge-run" style="font-size:7px">PUBLIÉ</span>'
        : '<span class="badge" style="font-size:7px;opacity:.6">BROUILLON</span>'}
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun article.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${published}</div><div class="stat-lbl">PUBLIÉS</div></div>
      <div class="stat-card"><div class="stat-val">${draft}</div><div class="stat-lbl">BROUILLONS</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-writing" style="font-size:12px"></i> ARTICLES
        ${adminLink ? `<a href="https://${appDomain}/wp-admin/edit.php" target="_blank" rel="noopener"
          class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>GÉRER</a>` : ''}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

async function loadWPPages() {
  const c = document.getElementById('content-panel-wp-pages');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/wordpress/wp-json/wp/v2/pages?status=publish,draft&per_page=20&_fields=id,title,status,date');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-layout"></i></div>
      <div class="empty-title">NON DISPONIBLE</div></div>`;
    return;
  }

  const pages = await r.json();
  const rows = (Array.isArray(pages) ? pages.slice(0,15) : []).map(p => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(p.title?.rendered || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${p.date ? new Date(p.date).toLocaleDateString('fr') : '—'}</div>
      </div>
      ${p.status === 'publish'
        ? '<span class="badge badge-run" style="font-size:7px">PUBLIÉE</span>'
        : '<span class="badge" style="font-size:7px;opacity:.6">BROUILLON</span>'}
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune page.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-layout" style="font-size:12px"></i> PAGES (${Array.isArray(pages) ? pages.length : 0})
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── GLPI — Tickets ────────────────────────────────────────────────────────────
async function loadGLPITickets() {
  const c = document.getElementById('content-panel-glpi-tickets');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT GLPI...</div>`;

  const appDomain = S.apps.find(a => a.id === 'glpi')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR GLPI</a>` : '';

  const r = await fetch('/ui/proxy/glpi/apirest.php/Ticket?range=0-20&is_deleted=0&order=DESC&sort=15&criteria[0][field]=12&criteria[0][searchtype]=equals&criteria[0][value]=notclosed');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-ticket"></i></div>
      <div class="empty-title">GLPI INDISPONIBLE</div>
      <div class="empty-sub">Activer l'API REST dans GLPI : Administration → Paramètres généraux → API, puis ajouter GLPI_ADMIN_USER dans les secrets.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const tickets = await r.json();
  const list = Array.isArray(tickets) ? tickets : [];

  const statusLabel = s => ({1:'NOUVEAU',2:'EN COURS',3:'EN ATTENTE',4:'RÉSOLU',5:'FERMÉ',6:'ACCEPTÉ'}[s] || s);
  const statusBadge = s => {
    const cls = [4,5].includes(s) ? 'badge-run' : s === 1 ? '' : '';
    const bg  = s === 1 ? 'background:var(--warn-dim);color:var(--warn-b)' : '';
    return `<span class="badge ${cls}" style="font-size:7px;${bg}">${statusLabel(s)}</span>`;
  };

  const rows = list.slice(0,15).map(t => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(t.name || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">#${t.id || '?'} · ${t.date ? new Date(t.date).toLocaleDateString('fr') : '—'}</div>
      </div>
      ${statusBadge(t.status)}
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun ticket ouvert.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="display:flex;align-items:center;gap:6px;padding:10px 12px">
        <i class="ti ti-ticket" style="font-size:12px"></i> TICKETS OUVERTS
        ${adminLink ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
          class="btn-sm" style="margin-left:auto;text-decoration:none"><i class="ti ti-external-link"></i>GÉRER</a>` : ''}
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── Pi-hole — Stats DNS ───────────────────────────────────────────────────────
async function loadPiholeStats() {
  const c = document.getElementById('content-panel-pihole-stats');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT PI-HOLE...</div>`;

  const appDomain = S.apps.find(a => a.id === 'pihole')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}/admin" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR PI-HOLE</a>` : '';

  const r = await fetch('/ui/proxy/pihole/admin/api.php?summaryRaw');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-shield-bolt"></i></div>
      <div class="empty-title">PI-HOLE INDISPONIBLE</div>
      <div class="empty-sub">Vérifier PIHOLE_API_TOKEN dans les secrets.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const s = await r.json();
  if (s.status === undefined && !s.dns_queries_today) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-shield-bolt"></i></div>
      <div class="empty-title">TOKEN INVALIDE</div>
      <div class="empty-sub">Régénérer PIHOLE_API_TOKEN (sha256 double du WEBPASSWORD).</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const pct = s.ads_percentage_today ? parseFloat(s.ads_percentage_today).toFixed(1) : '0';
  const enabled = s.status === 'enabled';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card">
        <div class="stat-val">${(s.dns_queries_today || 0).toLocaleString()}</div>
        <div class="stat-lbl">REQUÊTES</div>
      </div>
      <div class="stat-card">
        <div class="stat-val">${(s.ads_blocked_today || 0).toLocaleString()}</div>
        <div class="stat-lbl">BLOQUÉES</div>
      </div>
      <div class="stat-card">
        <div class="stat-val">${pct}%</div>
        <div class="stat-lbl">BLOQUÉES %</div>
      </div>
    </div>
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card">
        <div class="stat-val">${(s.domains_being_blocked || 0).toLocaleString()}</div>
        <div class="stat-lbl">DOMAINES BLOQUÉS</div>
      </div>
      <div class="stat-card">
        <div class="stat-val">${(s.unique_clients || 0)}</div>
        <div class="stat-lbl">CLIENTS</div>
      </div>
      <div class="stat-card">
        <div class="stat-val">
          ${enabled
            ? '<span style="color:var(--green-b)">ACTIF</span>'
            : '<span style="color:var(--red-b)">INACTIF</span>'}
        </div>
        <div class="stat-lbl">STATUT</div>
      </div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:4px">${adminLink}</div>` : ''}`;
}

async function loadPiholeLists() {
  const c = document.getElementById('content-panel-pihole-lists');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/pihole/admin/api.php?list=black');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-list"></i></div>
      <div class="empty-title">INDISPONIBLE</div></div>`;
    return;
  }

  const data = await r.json();
  const list = data.data || [];

  const rows = list.slice(0,20).map(entry => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(Array.isArray(entry) ? entry[0] : (entry.domain || entry))}</div>
      </div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune entrée.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-list" style="font-size:12px"></i> LISTE NOIRE (${list.length})
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── AdGuard Home — Stats DNS ──────────────────────────────────────────────────
async function loadAdGuardStats() {
  const c = document.getElementById('content-panel-adguard-stats');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT ADGUARD...</div>`;

  const appDomain = S.apps.find(a => a.id === 'adguard')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR ADGUARD</a>` : '';

  const [rStats, rStatus] = await Promise.all([
    fetch('/ui/proxy/adguard/control/stats'),
    fetch('/ui/proxy/adguard/control/status'),
  ]);

  if (!rStats.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-shield-bolt"></i></div>
      <div class="empty-title">ADGUARD INDISPONIBLE</div>
      <div class="empty-sub">Vérifier ADGUARD_USERNAME/PASSWORD dans les secrets.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const stats  = await rStats.json();
  const status = rStatus.ok ? await rStatus.json() : {};
  const total  = stats.num_dns_queries || 0;
  const blocked = stats.num_blocked_filtering || 0;
  const pct = total > 0 ? ((blocked / total) * 100).toFixed(1) : '0';

  const topBlocked = (stats.top_blocked_domains || []).slice(0, 5);
  const topRows = topBlocked.map(([domain, count]) => `
    <div class="loc-row">
      <div style="flex:1;min-width:0"><div style="font-size:10px;font-weight:700">${escapeHtml(domain)}</div></div>
      <div style="font-size:9px;color:var(--text3)">${count}</div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune donnée.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${total.toLocaleString()}</div><div class="stat-lbl">REQUÊTES</div></div>
      <div class="stat-card"><div class="stat-val">${blocked.toLocaleString()}</div><div class="stat-lbl">BLOQUÉES</div></div>
      <div class="stat-card"><div class="stat-val">${pct}%</div><div class="stat-lbl">TAUX</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-list" style="font-size:12px"></i> TOP DOMAINES BLOQUÉS
      </div>
      <div style="padding:0 12px 12px">${topRows}</div>
    </div>
    ${status.protection_enabled !== undefined ? `
    <div style="margin-top:8px;font-size:9px;color:var(--text3);text-align:center">
      Protection : ${status.protection_enabled
        ? '<span style="color:var(--green-b)">ACTIVE</span>'
        : '<span style="color:var(--red-b)">INACTIVE</span>'}
    </div>` : ''}
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

// ── Uptime Kuma — Moniteurs ───────────────────────────────────────────────────
async function loadUptimeMonitors() {
  const c = document.getElementById('content-panel-uptime-monitors');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT MONITEURS...</div>`;

  const appDomain = S.apps.find(a => a.id === 'uptime-kuma')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR UPTIME KUMA</a>` : '';

  const epCtrl = new AbortController();
  const epTimer = setTimeout(() => epCtrl.abort(), 4000);
  const epResp = await fetch('/ui/proxy/uptime-kuma/api/entry-page', { signal: epCtrl.signal }).catch(() => null);
  clearTimeout(epTimer);
  const epData = epResp?.ok ? await epResp.json().catch(() => null) : null;
  const ukSlug = epData?.entryPage || null;
  if (!ukSlug) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-heartbeat"></i></div>
      <div class="empty-title">AUCUNE PAGE DE STATUT</div>
      <div class="empty-sub">Créer un compte admin puis une page de statut dans Uptime Kuma.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }
  const spCtrl = new AbortController();
  const spTimer = setTimeout(() => spCtrl.abort(), 8000);
  const r = await fetch(`/ui/proxy/uptime-kuma/api/status-page/${ukSlug}`, { signal: spCtrl.signal }).catch(() => null);
  clearTimeout(spTimer);
  if (!r?.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-heartbeat"></i></div>
      <div class="empty-title">UPTIME KUMA INDISPONIBLE</div>
      <div class="empty-sub">Vérifier que le service est démarré.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const data = await r.json();
  const monitors = data.monitorList || {};
  const list = Object.values(monitors);

  const up = list.filter(m => m.active && m.heartbeat?.status === 1).length;
  const down = list.filter(m => m.active && m.heartbeat?.status !== 1).length;
  const paused = list.filter(m => !m.active).length;

  const statusColor = s => s === 1 ? 'var(--green-b)' : (s === 0 ? 'var(--red-b)' : 'var(--text3)');
  const statusLabel = s => s === 1 ? 'UP' : (s === 0 ? 'DOWN' : 'INCONNU');

  const rows = list.map(m => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(m.name || m.url || '?')}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(m.url || m.hostname || '')}</div>
      </div>
      <div style="display:flex;align-items:center;gap:6px">
        ${m.heartbeat?.ping ? `<span style="font-size:9px;color:var(--text3)">${m.heartbeat.ping}ms</span>` : ''}
        <span style="font-size:9px;font-weight:700;color:${statusColor(m.heartbeat?.status)}">
          ${statusLabel(m.heartbeat?.status)}
        </span>
      </div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun moniteur.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val" style="color:var(--green-b)">${up}</div><div class="stat-lbl">UP</div></div>
      <div class="stat-card"><div class="stat-val" style="color:var(--red-b)">${down}</div><div class="stat-lbl">DOWN</div></div>
      <div class="stat-card"><div class="stat-val" style="color:var(--text3)">${paused}</div><div class="stat-lbl">PAUSÉS</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-activity" style="font-size:12px"></i> MONITEURS (${list.length})
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

async function loadUptimeIncidents() {
  const c = document.getElementById('content-panel-uptime-incidents');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const incEpCtrl = new AbortController();
  const incEpTimer = setTimeout(() => incEpCtrl.abort(), 4000);
  const incEpResp = await fetch('/ui/proxy/uptime-kuma/api/entry-page', { signal: incEpCtrl.signal }).catch(() => null);
  clearTimeout(incEpTimer);
  const incEpData = incEpResp?.ok ? await incEpResp.json().catch(() => null) : null;
  const incSlug = incEpData?.entryPage || null;
  if (!incSlug) {
    c.innerHTML = `<div class="empty-state">
      <div class="empty-icon" style="color:var(--green-b)"><i class="ti ti-circle-check"></i></div>
      <div class="empty-title" style="color:var(--green-b)">AUCUN INCIDENT</div>
      <div class="empty-sub">Configurer Uptime Kuma pour activer le suivi d'incidents.</div>
    </div>`;
    return;
  }
  const incCtrl = new AbortController();
  const incTimer = setTimeout(() => incCtrl.abort(), 8000);
  const r = await fetch(`/ui/proxy/uptime-kuma/api/status-page/${incSlug}`, { signal: incCtrl.signal }).catch(() => null);
  clearTimeout(incTimer);
  if (!r?.ok) { c.innerHTML = `<div class="empty-msg">Indisponible.</div>`; return; }

  const data = await r.json();
  const incident = data.incident;

  if (!incident) {
    c.innerHTML = `<div class="empty-state">
      <div class="empty-icon" style="color:var(--green-b)"><i class="ti ti-circle-check"></i></div>
      <div class="empty-title" style="color:var(--green-b)">AUCUN INCIDENT</div>
      <div class="empty-sub">Tous les services fonctionnent normalement.</div>
    </div>`;
    return;
  }

  c.innerHTML = `
    <div class="settings-card" style="border-left:3px solid var(--red-b);padding:12px">
      <div style="font-size:11px;font-weight:700;color:var(--red-b);margin-bottom:4px">
        ${escapeHtml(incident.title || 'Incident en cours')}
      </div>
      <div style="font-size:9px;color:var(--text2)">${escapeHtml(incident.content || '')}</div>
    </div>`;
}

// ── Memos — Notes récentes ────────────────────────────────────────────────────
async function loadMemosRecent() {
  const c = document.getElementById('content-panel-memos-recent');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT NOTES...</div>`;

  const appDomain = S.apps.find(a => a.id === 'memos')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR MEMOS</a>` : '';

  const r = await fetch('/ui/proxy/memos/api/v1/memos?pageSize=20');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-notes"></i></div>
      <div class="empty-title">MEMOS INDISPONIBLE</div>
      <div class="empty-sub">MEMOS_API_TOKEN manquant ou service arrêté.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const memos = await r.json();
  const list = Array.isArray(memos) ? memos : (memos.memos || []);

  window._memosAll = list;

  const memoRow = m => {
    const ts = m.createTime || m.createdAt;
    const date = ts ? new Date(typeof ts === 'number' ? ts * 1000 : ts).toLocaleDateString('fr-FR') : '';
    const preview = (m.content || '').slice(0, 140).replace(/\n/g, ' ');
    const memoId = m.name || m.uid || m.id;
    return `
      <div class="loc-row" style="flex-direction:column;align-items:flex-start;gap:2px;position:relative" data-memo-id="${escapeHtml(String(memoId))}">
        <div style="display:flex;align-items:center;gap:6px;width:100%">
          <span style="font-size:8px;color:var(--text3);flex:1">${escapeHtml(date)}</span>
          <button class="btn-sm danger" title="Supprimer" style="padding:1px 5px;font-size:8px"
            onclick="deleteMemo('${escapeHtml(String(memoId))}')">
            <i class="ti ti-trash" style="font-size:9px"></i>
          </button>
        </div>
        <div style="font-size:10px;color:var(--text2)">${escapeHtml(preview)}${preview.length >= 140 ? '…' : ''}</div>
      </div>`;
  };

  const rows = list.slice(0, 20).map(memoRow).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune note.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:12px;margin-bottom:12px">
      <div class="settings-title" style="margin-bottom:8px">NOUVELLE NOTE</div>
      <textarea id="memos-new-content" rows="3" placeholder="Écrire une note (Markdown supporté)..."
        style="width:100%;font-size:10px;padding:6px 8px;background:var(--bg);border:1px solid var(--border);
               border-radius:4px;color:var(--text1);resize:vertical;font-family:inherit;box-sizing:border-box"></textarea>
      <div style="display:flex;gap:6px;margin-top:6px">
        <select id="memos-visibility" style="font-size:9px;padding:3px 6px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text1)">
          <option value="PRIVATE">PRIVÉE</option>
          <option value="PROTECTED">PROTÉGÉE</option>
          <option value="PUBLIC">PUBLIQUE</option>
        </select>
        <button class="btn-sm" style="margin-left:auto" onclick="createMemo()">
          <i class="ti ti-send" style="font-size:10px"></i> CRÉER
        </button>
      </div>
    </div>
    <div class="settings-card" style="padding:0">
      <div style="display:flex;align-items:center;gap:8px;padding:10px 12px 6px">
        <div class="settings-title" style="flex:1;margin:0">
          <i class="ti ti-file-text" style="font-size:12px"></i> NOTES (${list.length})
        </div>
        <input id="memos-search" type="search" placeholder="Rechercher…"
          oninput="filterMemos(this.value)"
          style="font-size:9px;padding:3px 7px;background:var(--card);border:1px solid var(--border);
                 border-radius:4px;color:var(--text1);width:120px;outline:none">
        <button class="btn-sm" onclick="loadMemosRecent()"><i class="ti ti-refresh" style="font-size:9px"></i></button>
      </div>
      <div id="memos-rows" style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

function filterMemos(q) {
  const el = document.getElementById('memos-rows');
  if (!el || !window._memosAll) return;
  const qLow = q.toLowerCase();
  const filtered = qLow
    ? window._memosAll.filter(m => (m.content || '').toLowerCase().includes(qLow))
    : window._memosAll;

  const memoRow = m => {
    const ts = m.createTime || m.createdAt;
    const date = ts ? new Date(typeof ts === 'number' ? ts * 1000 : ts).toLocaleDateString('fr-FR') : '';
    const preview = (m.content || '').slice(0, 140).replace(/\n/g, ' ');
    const memoId = m.name || m.uid || m.id;
    return `<div class="loc-row" style="flex-direction:column;align-items:flex-start;gap:2px" data-memo-id="${escapeHtml(String(memoId))}">
      <div style="display:flex;align-items:center;gap:6px;width:100%">
        <span style="font-size:8px;color:var(--text3);flex:1">${escapeHtml(date)}</span>
        <button class="btn-sm danger" title="Supprimer" style="padding:1px 5px;font-size:8px"
          onclick="deleteMemo('${escapeHtml(String(memoId))}')">
          <i class="ti ti-trash" style="font-size:9px"></i>
        </button>
      </div>
      <div style="font-size:10px;color:var(--text2)">${escapeHtml(preview)}${preview.length >= 140 ? '…' : ''}</div>
    </div>`;
  };

  el.innerHTML = filtered.slice(0, 20).map(memoRow).join('') ||
    '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun résultat.</div>';
}

async function deleteMemo(memoId) {
  if (!confirm('Supprimer cette note ?')) return;
  // memos v0.29+ uses name like "memos/123", DELETE /api/v1/{name}
  // v0.x uses numeric id, DELETE /api/v1/memo/{id}
  const url = memoId.includes('/') ? `/ui/proxy/memos/api/v1/${memoId}` : `/ui/proxy/memos/api/v1/memo/${memoId}`;
  const r = await fetch(url, { method: 'DELETE' });
  if (r.ok) { notify('Note supprimée', 'ok'); loadMemosRecent(); }
  else { notify('Erreur suppression', 'err'); }
}

async function createMemo() {
  const content = document.getElementById('memos-new-content')?.value?.trim();
  if (!content) { notify('Contenu vide', 'err'); return; }
  const visibility = document.getElementById('memos-visibility')?.value || 'PRIVATE';
  const r = await fetch('/ui/proxy/memos/api/v1/memos', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content, visibility }),
  });
  if (r.ok) {
    notify('Note créée', 'ok');
    const ta = document.getElementById('memos-new-content');
    if (ta) ta.value = '';
    loadMemosRecent();
  } else {
    notify('Erreur création note', 'err');
  }
}

// ── Linkding — Favoris ───────────────────────────────────────────────────────
async function loadLinkdingBookmarks() {
  const c = document.getElementById('content-panel-linkding-bookmarks');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT FAVORIS...</div>`;

  const appDomain = S.apps.find(a => a.id === 'linkding')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR LINKDING</a>` : '';

  const searchQ = document.getElementById('linkding-search')?.value?.trim() || '';
  const url = `/ui/proxy/linkding/api/bookmarks/?limit=30${searchQ ? '&q=' + encodeURIComponent(searchQ) : ''}`;
  const r = await fetch(url);
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-bookmark"></i></div>
      <div class="empty-title">LINKDING INDISPONIBLE</div>
      <div class="empty-sub">${r.status === 503 ? 'Service non démarré.' : 'LINKDING_API_TOKEN manquant dans les secrets.'}</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const data = await r.json();
  const bookmarks = data.results || [];

  const rows = bookmarks.map(b => `
    <div class="loc-row" style="gap:8px">
      <div style="flex:1;min-width:0">
        <a href="${escapeHtml(b.url)}" target="_blank" rel="noopener"
          style="font-size:10px;font-weight:700;color:var(--accent);text-decoration:none;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;display:block">
          ${escapeHtml(b.title || b.website_title || b.url)}</a>
        <div style="font-size:8px;color:var(--text3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
          ${escapeHtml(b.url)}</div>
        ${b.tag_names?.length ? `<div style="display:flex;gap:3px;margin-top:2px;flex-wrap:wrap">${b.tag_names.map(t => `<span style="font-size:7px;background:var(--vio-g);color:var(--vio-b);padding:1px 5px;border-radius:8px">${escapeHtml(t)}</span>`).join('')}</div>` : ''}
      </div>
      <button class="btn-sm danger" title="Supprimer" style="padding:2px 5px;flex-shrink:0"
        onclick="deleteLinkdingBookmark(${b.id})">
        <i class="ti ti-trash" style="font-size:9px"></i>
      </button>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun résultat.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:12px;margin-bottom:12px">
      <div class="settings-title" style="margin-bottom:8px">AJOUTER UN FAVORI</div>
      <div style="display:flex;gap:6px;margin-bottom:6px">
        <input id="linkding-url-input" type="url" placeholder="https://..." autocomplete="url"
          style="flex:1;font-size:9px;padding:4px 8px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text1)">
      </div>
      <div style="display:flex;gap:6px">
        <input id="linkding-title-input" type="text" placeholder="Titre (optionnel)"
          style="flex:1;font-size:9px;padding:4px 8px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text1)">
        <input id="linkding-tags-input" type="text" placeholder="tags, séparés, par, virgule"
          style="flex:1;font-size:9px;padding:4px 8px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text1)">
        <button class="btn-sm" onclick="addLinkdingBookmark()">
          <i class="ti ti-plus" style="font-size:10px"></i> AJOUTER
        </button>
      </div>
    </div>
    <div class="settings-card" style="padding:0">
      <div style="display:flex;align-items:center;gap:8px;padding:10px 12px 6px">
        <div class="settings-title" style="flex:1;margin:0">
          <i class="ti ti-bookmark" style="font-size:12px"></i> FAVORIS (${data.count || bookmarks.length})
        </div>
        <input id="linkding-search" type="search" placeholder="Rechercher…"
          onkeydown="if(event.key==='Enter')loadLinkdingBookmarks()"
          style="font-size:9px;padding:3px 7px;background:var(--card);border:1px solid var(--border);
                 border-radius:4px;color:var(--text1);width:120px;outline:none">
        <button class="btn-sm" onclick="loadLinkdingBookmarks()"><i class="ti ti-search" style="font-size:9px"></i></button>
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

async function addLinkdingBookmark() {
  const url   = document.getElementById('linkding-url-input')?.value?.trim();
  const title = document.getElementById('linkding-title-input')?.value?.trim();
  const tags  = document.getElementById('linkding-tags-input')?.value?.trim();
  if (!url) { notify('URL requise', 'err'); return; }
  const body = { url, title: title || '', tag_names: tags ? tags.split(',').map(t => t.trim()).filter(Boolean) : [] };
  const r = await fetch('/ui/proxy/linkding/api/bookmarks/', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (r.ok) {
    notify('Favori ajouté', 'ok');
    document.getElementById('linkding-url-input').value = '';
    document.getElementById('linkding-title-input').value = '';
    document.getElementById('linkding-tags-input').value = '';
    loadLinkdingBookmarks();
  } else {
    const err = await r.json().catch(() => ({}));
    notify(err.url?.[0] || 'Erreur ajout favori', 'err');
  }
}

async function deleteLinkdingBookmark(id) {
  if (!confirm('Supprimer ce favori ?')) return;
  const r = await fetch(`/ui/proxy/linkding/api/bookmarks/${id}/`, { method: 'DELETE' });
  if (r.ok || r.status === 204) { notify('Favori supprimé', 'ok'); loadLinkdingBookmarks(); }
  else { notify('Erreur suppression', 'err'); }
}

async function loadLinkdingTags() {
  const c = document.getElementById('content-panel-linkding-tags');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/linkding/api/tags/?limit=50');
  if (!r.ok) { c.innerHTML = `<div class="empty-msg">Linkding indisponible.</div>`; return; }

  const data = await r.json();
  const tags = data.results || [];

  const tagHtml = tags.map(t => `
    <span style="display:inline-block;background:var(--bg3);border-radius:4px;padding:2px 8px;font-size:9px;font-weight:700;color:var(--blue);margin:2px">
      ${escapeHtml(t.name)}
    </span>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune étiquette.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:12px">
      <div class="settings-title" style="margin-bottom:10px">
        <i class="ti ti-tag" style="font-size:12px"></i> ÉTIQUETTES (${tags.length})
      </div>
      <div>${tagHtml}</div>
    </div>`;
}

// ── Paperless-NGX ─────────────────────────────────────────────────────────────
async function loadPaperlessDocs() {
  const c = document.getElementById('content-panel-paperless-docs');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT DOCUMENTS...</div>`;

  const appDomain = S.apps.find(a => a.id === 'paperless-ngx')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR PAPERLESS</a>` : '';

  const [rDocs, rStats] = await Promise.all([
    fetch('/ui/proxy/paperless-ngx/api/documents/?page_size=10&ordering=-created'),
    fetch('/ui/proxy/paperless-ngx/api/statistics/'),
  ]);

  if (!rDocs.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-file-invoice"></i></div>
      <div class="empty-title">PAPERLESS INDISPONIBLE</div>
      <div class="empty-sub">${rDocs.status === 503 ? 'Service non démarré.' : 'PAPERLESS_API_TOKEN manquant.'}</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const docs = await rDocs.json();
  const stats = rStats.ok ? await rStats.json() : {};
  const list = docs.results || [];

  const rows = list.map(d => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(d.title || '(sans titre)')}</div>
        <div style="font-size:9px;color:var(--text3)">
          ${d.created ? new Date(d.created).toLocaleDateString('fr-FR') : ''}
          ${d.correspondent ? ` · ${escapeHtml(d.correspondent?.name || '')}` : ''}
        </div>
      </div>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun document.</div>';

  const totalDocs = stats.documents_total || docs.count || 0;
  const inboxCount = stats.inbox_count || 0;

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${totalDocs}</div><div class="stat-lbl">TOTAL</div></div>
      <div class="stat-card"><div class="stat-val" style="color:${inboxCount > 0 ? 'var(--warn)' : 'var(--green-b)'}">${inboxCount}</div><div class="stat-lbl">À TRAITER</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-files" style="font-size:12px"></i> DOCUMENTS RÉCENTS
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

async function loadPaperlessInbox() {
  const c = document.getElementById('content-panel-paperless-inbox');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  // Récupérer les docs sans correspondant ni tag (typiquement la boîte entrée)
  const r = await fetch('/ui/proxy/paperless-ngx/api/documents/?page_size=10&ordering=-created&is_inbox_document=true');
  if (!r.ok) { c.innerHTML = `<div class="empty-msg">Paperless indisponible.</div>`; return; }

  const data = await r.json();
  const list = data.results || [];

  if (list.length === 0) {
    c.innerHTML = `<div class="empty-state">
      <div class="empty-icon" style="color:var(--green-b)"><i class="ti ti-circle-check"></i></div>
      <div class="empty-title" style="color:var(--green-b)">BOÎTE VIDE</div>
      <div class="empty-sub">Tous les documents ont été traités.</div>
    </div>`;
    return;
  }

  const rows = list.map(d => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(d.title || '(sans titre)')}</div>
        <div style="font-size:9px;color:var(--warn)">À TRAITER · ${d.created ? new Date(d.created).toLocaleDateString('fr-FR') : ''}</div>
      </div>
    </div>`).join('');

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px;color:var(--warn)">
        <i class="ti ti-inbox" style="font-size:12px"></i> BOÎTE ENTRÉE (${data.count})
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>`;
}

// ── FreshRSS — Flux RSS ───────────────────────────────────────────────────────
async function loadFreshRssFeeds() {
  const c = document.getElementById('content-panel-freshrss-feeds');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT FLUX RSS...</div>`;

  const appDomain = S.apps.find(a => a.id === 'freshrss')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR FRESHRSS</a>` : '';

  const r = await fetch('/ui/proxy/freshrss/api/greader.php/reader/api/0/subscription/list?output=json');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-rss"></i></div>
      <div class="empty-title">FRESHRSS INDISPONIBLE</div>
      <div class="empty-sub">${r.status === 503 ? 'Service non démarré.' : 'Erreur d\'accès à l\'API FreshRSS.'}</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const data = await r.json();
  const feeds = data.subscriptions || [];

  const rows = feeds.map(f => {
    const unread = f.unread_count || 0;
    return `
      <div class="loc-row">
        <div style="flex:1;min-width:0">
          <div style="font-size:10px;font-weight:700">${escapeHtml(f.title || '?')}</div>
          <div style="font-size:9px;color:var(--text3)">${escapeHtml((f.categories?.[0]?.label) || '')}</div>
        </div>
        ${unread > 0 ? `<span style="font-size:9px;font-weight:700;color:var(--blue)">${unread}</span>` : ''}
      </div>`;
  }).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun flux.</div>';

  const totalUnread = feeds.reduce((s, f) => s + (f.unread_count || 0), 0);

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${feeds.length}</div><div class="stat-lbl">FLUX</div></div>
      <div class="stat-card"><div class="stat-val" style="color:${totalUnread > 0 ? 'var(--blue)' : 'var(--text3)'}">${totalUnread}</div><div class="stat-lbl">NON LUS</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-rss" style="font-size:12px"></i> ABONNEMENTS (${feeds.length})
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

// ── FreshRSS — Articles non lus ──────────────────────────────────────────────
async function loadFreshRssUnread() {
  const c = document.getElementById('content-panel-freshrss-articles');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT ARTICLES...</div>`;

  const appDomain = S.apps.find(a => a.id === 'freshrss')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR FRESHRSS</a>`
    : '';

  // GReader API: récupérer les articles non lus
  let items = null;
  try {
    const r = await fetch('/ui/proxy/freshrss/api/greader.php/reader/api/0/stream/contents/user/-/state/com.google/reading-list?output=json&n=30&xt=user/-/state/com.google/read');
    if (r.ok) {
      const d = await r.json();
      items = d.items || [];
    }
  } catch(e) {}

  if (!items) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}</div>
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-rss"></i></div>
        <div class="empty-title">FRESHRSS INDISPONIBLE</div>
        <div class="empty-sub">Erreur d'accès à l'API GReader.</div></div>`;
    return;
  }

  const rows = items.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-check"></i></div>
        <div class="empty-title">TOUT LU !</div>
        <div class="empty-sub">Aucun article non lu pour l'instant.</div></div>`
    : items.map(item => {
        const title = item.title || '(sans titre)';
        const source = item.origin?.title || '';
        const ts = item.published ? new Date(item.published * 1000).toLocaleString('fr-FR') : '';
        const url = item.alternate?.[0]?.href || item.canonical?.[0]?.href || '';
        return `
          <div style="padding:7px 8px;background:var(--card);border-radius:6px;border:1px solid var(--border);margin-bottom:5px">
            <div style="display:flex;align-items:baseline;gap:6px;margin-bottom:2px">
              ${source ? `<span style="font-size:7px;color:var(--text3);letter-spacing:.5px;flex-shrink:0">${escapeHtml(source)}</span>` : ''}
              <span style="font-size:8px;color:var(--text3);margin-left:auto;flex-shrink:0">${escapeHtml(ts)}</span>
            </div>
            ${url
              ? `<a href="${escapeHtml(url)}" target="_blank" rel="noopener"
                  style="font-size:10px;font-weight:600;color:var(--text1);text-decoration:none;line-height:1.3;display:block">
                  ${escapeHtml(title)}</a>`
              : `<div style="font-size:10px;font-weight:600;color:var(--text1)">${escapeHtml(title)}</div>`
            }
          </div>`;
      }).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      ${adminLink}
      <span style="font-size:9px;color:var(--text3)">${items.length} article(s) non lu(s)</span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadFreshRssUnread()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}`;
}

// ── Changedetection.io — Surveillances ───────────────────────────────────────
async function loadChangedetectionWatches() {
  const c = document.getElementById('content-panel-changedetection-watches');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'changedetection')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR CHANGEDETECTION</a>` : '';

  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const r = await fetch('/ui/proxy/changedetection/api/v1/watch?limit=20', {
    headers: { 'Accept': 'application/json' }
  });
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-eye"></i></div>
      <div class="empty-title">CHANGEDETECTION INDISPONIBLE</div>
      <div class="empty-sub">Vérifier que le service est démarré.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }
  const data = await r.json();
  const watches = Array.isArray(data) ? data : Object.values(data);

  const changed = watches.filter(w => w.last_changed > 0).length;
  const rows = watches.slice(0, 10).map(w => {
    const lastCheck = w.last_checked ? new Date(w.last_checked * 1000).toLocaleString('fr-FR') : '—';
    const hasChanged = w.last_changed > 0;
    return `
      <div class="loc-row">
        <div style="flex:1;min-width:0">
          <div style="font-size:10px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
            ${escapeHtml(w.title || w.url || '?')}</div>
          <div style="font-size:9px;color:var(--text3)">${escapeHtml(lastCheck)}</div>
        </div>
        ${hasChanged ? `<span style="font-size:9px;font-weight:700;color:var(--warn)">MODIFIÉ</span>` : ''}
      </div>`;
  }).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune surveillance.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${watches.length}</div><div class="stat-lbl">SURVEILLÉES</div></div>
      <div class="stat-card"><div class="stat-val" style="color:${changed > 0 ? 'var(--warn)' : 'var(--green-b)'}">${changed}</div><div class="stat-lbl">MODIFIÉES</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-eye" style="font-size:12px"></i> PAGES SURVEILLÉES
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

// ── Mealie — Recettes ────────────────────────────────────────────────────────
async function loadMealieRecipes() {
  const c = document.getElementById('content-panel-mealie-recipes');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'mealie')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR MEALIE</a>` : '';

  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT RECETTES...</div>`;

  // Mealie nécessite auth — on essaie l'API publique (pas de token nécessaire pour les recettes publiques)
  const r = await fetch('/ui/proxy/mealie/api/recipes?page=1&perPage=20');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-salad"></i></div>
      <div class="empty-title">MEALIE INDISPONIBLE</div>
      <div class="empty-sub">Vérifier que le service est démarré.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }
  const data = await r.json();
  const recipes = data.items || [];
  const total = data.total || recipes.length;

  const rows = recipes.slice(0, 10).map(recipe => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(recipe.name || '?')}</div>
        ${recipe.description ? `<div style="font-size:9px;color:var(--text3)">${escapeHtml(recipe.description.slice(0,80))}${recipe.description.length>80?'…':''}</div>` : ''}
      </div>
      ${recipe.rating ? `<span style="font-size:9px;color:var(--warn)">★ ${recipe.rating}</span>` : ''}
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune recette.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${total}</div><div class="stat-lbl">RECETTES</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-salad" style="font-size:12px"></i> DERNIÈRES RECETTES
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

// ── ntfy — Topics et messages ─────────────────────────────────────────────────
async function loadNtfyTopics() {
  const c = document.getElementById('content-panel-ntfy-topics');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'ntfy')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR NTFY</a>` : '';

  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT NTFY...</div>`;

  const r = await fetch('/ui/proxy/ntfy/v1/stats');
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-bell"></i></div>
      <div class="empty-title">NTFY INDISPONIBLE</div>
      <div class="empty-sub">Vérifier que le service est démarré.</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }
  const stats = await r.json();
  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${stats.messages_published ?? '—'}</div><div class="stat-lbl">MESSAGES</div></div>
      <div class="stat-card"><div class="stat-val">${stats.open_subscriptions ?? '—'}</div><div class="stat-lbl">ABONNEMENTS</div></div>
    </div>
    <div class="settings-card">
      <div class="settings-title">ENVOYER UN TEST</div>
      <div style="display:flex;gap:6px;margin:8px 0">
        <input id="ntfy-topic-input" type="text" placeholder="Topic (ex: test)" style="flex:1;font-size:9px;padding:4px 8px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text1)">
        <input id="ntfy-msg-input" type="text" placeholder="Message" style="flex:2;font-size:9px;padding:4px 8px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text1)">
        <button class="btn-sm" onclick="sendNtfyTest()"><i class="ti ti-send"></i>ENVOYER</button>
      </div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

async function sendNtfyTest() {
  const topic = document.getElementById('ntfy-topic-input')?.value?.trim() || 'test';
  const msg   = document.getElementById('ntfy-msg-input')?.value?.trim() || 'Test depuis Caleope UI';
  const r = await fetch(`/ui/proxy/ntfy/${encodeURIComponent(topic)}`, {
    method: 'POST', body: msg, headers: { 'Title': 'Caleope' }
  });
  if (r.ok) notify(`Message envoyé sur "${topic}"`, 'ok');
  else notify('Erreur envoi ntfy', 'err');
}

// ── n8n — Workflows ───────────────────────────────────────────────────────────
async function loadN8nWorkflows() {
  const c = document.getElementById('content-panel-n8n-workflows');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'n8n')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR N8N</a>` : '';

  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT N8N...</div>`;

  const r = await fetch('/ui/proxy/n8n/api/v1/workflows?limit=25', {
    headers: { 'Accept': 'application/json' }
  });
  if (!r.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-workflow"></i></div>
      <div class="empty-title">N8N INDISPONIBLE</div>
      <div class="empty-sub">Vérifier que le service est démarré.<br>L'API publique n8n nécessite un token API (n8n settings → API).</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }
  const data = await r.json();
  const workflows = data.data || [];
  const rows = workflows.map(w => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(w.name || '?')}</div>
        <div style="font-size:9px;color:var(--text3)">${w.nodes?.length || 0} nœuds</div>
      </div>
      <span style="font-size:9px;font-weight:700;color:${w.active ? 'var(--green-b)' : 'var(--text3)'}">
        ${w.active ? 'ACTIF' : 'INACTIF'}
      </span>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun workflow.</div>';

  c.innerHTML = `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-workflow" style="font-size:12px"></i> WORKFLOWS (${workflows.length})
      </div>
      <div style="padding:0 12px 12px">${rows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

// ── File Browser — Gestionnaire de fichiers iframe ────────────────────────────
function loadFileBrowser() {
  const c = document.getElementById('content-panel-filebrowser');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'filebrowser')?.domain;
  c.innerHTML = `
    <div style="display:flex;flex-direction:column;height:calc(100vh - 120px);min-height:400px;gap:8px">
      <div style="display:flex;gap:8px;align-items:center">
        ${appDomain ? `<a class="btn btn-vio" href="https://${appDomain}" target="_blank" rel="noopener"
          style="text-decoration:none;font-size:9px"><i class="ti ti-external-link"></i>OUVRIR DANS NOUVEL ONGLET</a>` : ''}
        <span style="font-size:9px;color:var(--text3)">Identifiants par défaut : admin / admin</span>
      </div>
      <iframe src="/ui/proxy/filebrowser/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
        allow="fullscreen" title="File Browser"></iframe>
    </div>`;
}

// ── Stirling-PDF — Embed iframe ───────────────────────────────────────────────
function loadStirlingPDF() {
  const c = document.getElementById('content-panel-stirling-pdf');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'stirling-pdf')?.domain;
  const src = '/ui/proxy/stirling-pdf/';
  c.innerHTML = `
    <div style="display:flex;flex-direction:column;height:calc(100vh - 120px);min-height:400px;gap:8px">
      <div style="display:flex;gap:8px;align-items:center">
        ${appDomain ? `<a class="btn btn-vio" href="https://${appDomain}" target="_blank" rel="noopener"
          style="text-decoration:none;font-size:9px"><i class="ti ti-external-link"></i>OUVRIR DANS NOUVEL ONGLET</a>` : ''}
        <span style="font-size:9px;color:var(--text3)">Outil de manipulation PDF — traitement côté serveur</span>
      </div>
      <iframe src="${src}" style="flex:1;border:none;border-radius:6px;background:var(--card)"
        allow="fullscreen" title="Stirling PDF"></iframe>
    </div>`;
}

// ── WG-Easy — Pairs VPN ───────────────────────────────────────────────────────
async function loadWgEasyPeers() {
  const c = document.getElementById('content-panel-wgeasy-peers');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT WG-EASY...</div>`;

  const appDomain = S.apps.find(a => a.id === 'wg-easy')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR WG-EASY</a>`
    : '';

  let clients = null;
  try {
    const r = await fetch('/ui/proxy/wg-easy/api/wireguard/client');
    if (r.ok) clients = await r.json();
  } catch(e) {}

  if (!clients) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}</div>
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-vpn"></i></div>
        <div class="empty-title">WG-EASY INDISPONIBLE</div>
        <div class="empty-sub">Vérifiez que WG-Easy est démarré et accessible.</div></div>`;
    return;
  }

  const online = clients.filter(cl => cl.endpoint).length;
  const rows = clients.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-vpn"></i></div>
        <div class="empty-title">AUCUN PAIR</div>
        <div class="empty-sub">Créez un pair depuis l'interface WG-Easy.</div></div>`
    : `<table style="width:100%;border-collapse:collapse;font-size:9px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="text-align:left;padding:4px 6px">NOM</th>
          <th style="text-align:left;padding:4px 6px">IP</th>
          <th style="text-align:left;padding:4px 6px">STATUT</th>
          <th style="text-align:left;padding:4px 6px">TRANSFERT ↑↓</th>
          <th style="text-align:left;padding:4px 6px">DERNIÈRE CONNEXION</th>
        </tr></thead>
        <tbody>${clients.map(cl => {
          const isConn = !!cl.endpoint;
          const tx = cl.transferTx ? `${(cl.transferTx/1024/1024).toFixed(1)} MB` : '—';
          const rx = cl.transferRx ? `${(cl.transferRx/1024/1024).toFixed(1)} MB` : '—';
          const lastHS = cl.latestHandshakeAt ? new Date(cl.latestHandshakeAt).toLocaleString('fr-FR') : '—';
          return `<tr style="border-bottom:1px solid var(--border);opacity:${isConn?1:0.65}">
            <td style="padding:5px 6px;font-weight:600;color:var(--text1)">${escapeHtml(cl.name || cl.id)}</td>
            <td style="padding:5px 6px;color:var(--text2)">${escapeHtml(cl.address || '—')}</td>
            <td style="padding:5px 6px">
              <span class="badge ${isConn ? 'badge-run' : 'badge-stop'}" style="font-size:7px">
                ${isConn ? 'CONNECTÉ' : 'HORS LIGNE'}</span></td>
            <td style="padding:5px 6px;color:var(--text3)">↑ ${tx} / ↓ ${rx}</td>
            <td style="padding:5px 6px;color:var(--text3)">${escapeHtml(lastHS)}</td>
          </tr>`;
        }).join('')}</tbody>
      </table>`;

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      ${adminLink}
      <span style="font-size:9px;color:var(--text3)">${clients.length} pair(s) — <span style="color:var(--green-b)">${online} connecté(s)</span></span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadWgEasyPeers()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}`;
}

// ── CrowdSec — Décisions (bans actifs) ───────────────────────────────────────
async function loadCrowdsecDecisions() {
  const c = document.getElementById('content-panel-crowdsec-decisions');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT CROWDSEC...</div>`;

  let decisions = null;
  try {
    const r = await fetch('/ui/proxy/crowdsec/v1/decisions?limit=50');
    if (r.ok) { const t = await r.text(); decisions = t ? JSON.parse(t) : []; }
  } catch(e) {}

  if (decisions === null) {
    c.innerHTML = `
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-shield"></i></div>
        <div class="empty-title">CROWDSEC INDISPONIBLE</div>
        <div class="empty-sub">Vérifiez que CrowdSec est démarré et que la clé bouncer est configurée.</div></div>`;
    return;
  }

  const rows = !decisions || decisions.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-shield-check"></i></div>
        <div class="empty-title">AUCUN BAN ACTIF</div>
        <div class="empty-sub">Aucune décision active pour l'instant.</div></div>`
    : `<table style="width:100%;border-collapse:collapse;font-size:9px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="text-align:left;padding:4px 6px">IP / SCOPE</th>
          <th style="text-align:left;padding:4px 6px">TYPE</th>
          <th style="text-align:left;padding:4px 6px">RAISON</th>
          <th style="text-align:left;padding:4px 6px">DURÉE</th>
          <th style="text-align:left;padding:4px 6px">ORIGINE</th>
          <th style="padding:4px 6px"></th>
        </tr></thead>
        <tbody>${decisions.map(d => `
          <tr style="border-bottom:1px solid var(--border)">
            <td style="padding:5px 6px;font-family:monospace;color:var(--text1)">${escapeHtml(d.value || '—')}</td>
            <td style="padding:5px 6px"><span class="badge badge-err" style="font-size:7px">${escapeHtml(d.type || 'ban')}</span></td>
            <td style="padding:5px 6px;color:var(--text2)">${escapeHtml(d.scenario || d.reason || '—')}</td>
            <td style="padding:5px 6px;color:var(--text3)">${escapeHtml(d.duration || '—')}</td>
            <td style="padding:5px 6px;color:var(--text3)">${escapeHtml(d.origin || '—')}</td>
            <td style="padding:5px 6px">
              <button class="btn-sm" style="font-size:7px;color:var(--warn)" title="Débloquer cette IP"
                onclick="crowdsecUnban(${d.id}, '${escapeHtml(d.value || '')}')">
                <i class="ti ti-ban" style="font-size:9px"></i> LEVER
              </button>
            </td>
          </tr>`).join('')}
        </tbody>
      </table>`;

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      <span style="font-size:9px;color:var(--text3)">${(decisions || []).length} décision(s) active(s)</span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="openCrowdsecBanModal()">
        <i class="ti ti-shield-x"></i> BANNIR IP</button>
      <button class="btn" style="font-size:9px" onclick="loadCrowdsecDecisions()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}
    <div id="crowdsec-ban-form" style="display:none;margin-top:12px;padding:12px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <div style="font-size:9px;font-weight:700;letter-spacing:1px;color:var(--text3);margin-bottom:8px">BANNIR UNE IP MANUELLEMENT</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        <input id="cs-ban-ip" class="field-input" placeholder="IP ou CIDR (ex: 1.2.3.4)" style="flex:2;min-width:120px;font-size:10px;padding:5px 8px">
        <input id="cs-ban-duration" class="field-input" placeholder="Durée (ex: 24h)" value="24h" style="flex:1;min-width:80px;font-size:10px;padding:5px 8px">
        <input id="cs-ban-reason" class="field-input" placeholder="Raison" value="manual" style="flex:2;min-width:100px;font-size:10px;padding:5px 8px">
        <button class="btn btn-vio" style="font-size:9px" onclick="crowdsecBan()"><i class="ti ti-shield-x"></i> BANNIR</button>
        <button class="btn" style="font-size:9px" onclick="document.getElementById('crowdsec-ban-form').style.display='none'">ANNULER</button>
      </div>
    </div>`;
}

async function loadCrowdsecAlerts() {
  const c = document.getElementById('content-panel-crowdsec-alerts');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT ALERTES...</div>`;

  let alerts = null;
  try {
    const r = await fetch('/ui/proxy/crowdsec/v1/alerts?limit=30');
    if (r.ok) { const t = await r.text(); alerts = t ? JSON.parse(t) : []; }
  } catch(e) {}

  if (alerts === null) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-alert-triangle"></i></div>
      <div class="empty-title">CROWDSEC INDISPONIBLE</div></div>`;
    return;
  }

  const rows = !alerts || alerts.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-shield-check"></i></div>
        <div class="empty-title">AUCUNE ALERTE</div>
        <div class="empty-sub">Aucune alerte récente détectée.</div></div>`
    : alerts.map(a => {
        const src = a.source?.ip || a.source?.range || '—';
        const country = a.source?.cn ? ` (${a.source.cn})` : '';
        const date = a.created_at ? new Date(a.created_at).toLocaleString('fr-FR') : '';
        const count = a.decisions?.length || 0;
        return `
          <div style="padding:8px;background:var(--card);border-radius:6px;border:1px solid var(--border);margin-bottom:6px">
            <div style="display:flex;align-items:center;gap:8px;margin-bottom:4px">
              <span style="font-family:monospace;font-size:10px;color:var(--text1)">${escapeHtml(src)}${escapeHtml(country)}</span>
              ${count ? `<span class="badge badge-err" style="font-size:7px">${count} décision(s)</span>` : ''}
              <span style="font-size:8px;color:var(--text3);margin-left:auto">${escapeHtml(date)}</span>
            </div>
            <div style="font-size:9px;color:var(--text2)">${escapeHtml(a.scenario || a.reason || '—')}</div>
          </div>`;
      }).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      <span style="font-size:9px;color:var(--text3)">${(alerts || []).length} alerte(s)</span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadCrowdsecAlerts()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}`;
}

// ── CrowdSec — Unban / Ban manuel ────────────────────────────────────────────
function openCrowdsecBanModal() {
  const form = document.getElementById('crowdsec-ban-form');
  if (form) form.style.display = form.style.display === 'none' ? 'block' : 'none';
}

async function crowdsecUnban(decisionId, ip) {
  if (!confirm(`Lever le ban de ${ip} ?`)) return;
  try {
    const r = await fetch(`/ui/proxy/crowdsec/v1/decisions/${decisionId}`, { method: 'DELETE' });
    if (r.ok) {
      notify(`Ban levé pour ${ip}`, 'ok');
      loadCrowdsecDecisions();
    } else {
      notify(`Erreur lors du déblocage (${r.status})`, 'err');
    }
  } catch(e) {
    notify('Erreur réseau', 'err');
  }
}

async function crowdsecBan() {
  const ip       = document.getElementById('cs-ban-ip')?.value?.trim();
  const duration = document.getElementById('cs-ban-duration')?.value?.trim() || '24h';
  const reason   = document.getElementById('cs-ban-reason')?.value?.trim() || 'manual';

  if (!ip) { notify('IP requise', 'err'); return; }

  const scope = ip.includes('/') ? 'Range' : 'Ip';
  const body = [{ value: ip, type: 'ban', duration, reason: reason, scope, origin: 'caleope-ui', simulated: false }];

  try {
    const r = await fetch('/ui/proxy/crowdsec/v1/decisions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (r.ok) {
      notify(`${ip} banni pour ${duration}`, 'ok');
      document.getElementById('cs-ban-ip').value = '';
      document.getElementById('crowdsec-ban-form').style.display = 'none';
      loadCrowdsecDecisions();
    } else {
      const t = await r.text();
      notify(`Erreur ban (${r.status}): ${t.slice(0, 80)}`, 'err');
    }
  } catch(e) {
    notify('Erreur réseau', 'err');
  }
}

// ── Gotify — Messages récents ─────────────────────────────────────────────────
async function loadGotifyMessages() {
  const c = document.getElementById('content-panel-gotify-messages');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT GOTIFY...</div>`;

  const appDomain = S.apps.find(a => a.id === 'gotify')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR GOTIFY</a>`
    : '';

  let r;
  try {
    r = await fetch('/ui/proxy/gotify/message?limit=20');
  } catch(e) { r = null; }

  if (!r || !r.ok) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}</div>
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-bell-off"></i></div>
        <div class="empty-title">GOTIFY INDISPONIBLE</div>
        <div class="empty-sub">Vérifiez que Gotify est démarré et configuré.</div></div>`;
    return;
  }

  const data = await r.json();
  const messages = data.messages || [];

  const rows = messages.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-bell"></i></div>
        <div class="empty-title">AUCUN MESSAGE</div>
        <div class="empty-sub">Aucune notification reçue pour l'instant.</div></div>`
    : messages.map(m => {
        const prioCls = m.priority >= 8 ? 'badge-err' : m.priority >= 4 ? 'badge-warn' : 'badge-run';
        const prioLabel = m.priority >= 8 ? 'URGENT' : m.priority >= 4 ? 'NORMAL' : 'INFO';
        const date = m.date ? new Date(m.date).toLocaleString('fr-FR') : '';
        return `
          <div style="padding:8px;background:var(--card);border-radius:6px;border:1px solid var(--border);margin-bottom:6px">
            <div style="display:flex;align-items:center;gap:8px;margin-bottom:4px">
              <span class="badge ${prioCls}" style="font-size:8px">${prioLabel}</span>
              <span style="font-size:9px;font-weight:600;color:var(--text1)">${escapeHtml(m.appid ? String(m.appid) : 'App ' + m.appid)}</span>
              <span style="font-size:8px;color:var(--text3);margin-left:auto">${escapeHtml(date)}</span>
            </div>
            <div style="font-size:10px;font-weight:600;color:var(--text1)">${escapeHtml(m.title || '')}</div>
            ${m.message ? `<div style="font-size:9px;color:var(--text2);margin-top:2px">${escapeHtml(m.message)}</div>` : ''}
          </div>`;
      }).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      ${adminLink}
      <span style="font-size:9px;color:var(--text3)">${messages.length} message(s)</span>
      <button class="btn" style="font-size:9px" onclick="sendGotifyTest()">
        <i class="ti ti-send"></i> TEST</button>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadGotifyMessages()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    <div id="gotify-send-form" style="display:none;margin-bottom:12px;padding:10px 12px;background:var(--card);border:1px solid var(--border);border-radius:6px">
      <div style="font-size:9px;font-weight:700;letter-spacing:1px;color:var(--text3);margin-bottom:8px">ENVOYER UN MESSAGE TEST</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        <input id="gotify-test-title" class="field-input" placeholder="Titre" value="Test Caleope" style="flex:2;min-width:120px;font-size:10px;padding:5px 8px">
        <input id="gotify-test-msg" class="field-input" placeholder="Message" value="Message de test depuis Caleope UI" style="flex:3;min-width:150px;font-size:10px;padding:5px 8px">
        <select id="gotify-test-prio" class="field-input" style="flex:1;min-width:80px;font-size:10px;padding:5px 8px">
          <option value="2">INFO</option>
          <option value="5" selected>NORMAL</option>
          <option value="8">URGENT</option>
        </select>
        <button class="btn btn-vio" style="font-size:9px" onclick="confirmGotifyTest()"><i class="ti ti-send"></i> ENVOYER</button>
        <button class="btn" style="font-size:9px" onclick="document.getElementById('gotify-send-form').style.display='none'">ANNULER</button>
      </div>
    </div>
    ${rows}`;
}

function sendGotifyTest() {
  const form = document.getElementById('gotify-send-form');
  if (form) form.style.display = form.style.display === 'none' ? 'block' : 'none';
}

async function confirmGotifyTest() {
  const title = document.getElementById('gotify-test-title')?.value || 'Test';
  const msg   = document.getElementById('gotify-test-msg')?.value || 'Test';
  const prio  = parseInt(document.getElementById('gotify-test-prio')?.value || '5');

  try {
    const r = await fetch('/ui/proxy/gotify/message', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title, message: msg, priority: prio }),
    });
    if (r.ok) {
      notify('Message Gotify envoyé', 'ok');
      document.getElementById('gotify-send-form').style.display = 'none';
      setTimeout(loadGotifyMessages, 500);
    } else {
      notify(`Erreur Gotify (${r.status})`, 'err');
    }
  } catch(e) {
    notify('Erreur réseau', 'err');
  }
}

// ── Homarr — Dashboard iframe ─────────────────────────────────────────────────
function loadHomarrDashboard() {
  const c = document.getElementById('content-panel-homarr-dashboard');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'homarr')?.domain;
  c.innerHTML = `
    <div style="display:flex;flex-direction:column;height:calc(100vh - 120px);min-height:400px;gap:8px">
      <div style="display:flex;gap:8px;align-items:center">
        ${appDomain ? `<a class="btn btn-vio" href="https://${appDomain}" target="_blank" rel="noopener"
          style="text-decoration:none;font-size:9px"><i class="ti ti-external-link"></i>OUVRIR DANS NOUVEL ONGLET</a>` : ''}
        <span style="font-size:9px;color:var(--text3)">Dashboard de liens — cliquer sur l'icône crayon pour éditer</span>
      </div>
      <iframe src="/ui/proxy/homarr/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
        allow="fullscreen" title="Homarr"></iframe>
    </div>`;
}

// ── Home Assistant — Dashboard iframe ────────────────────────────────────────
function loadHADashboard() {
  const c = document.getElementById('content-panel-ha-dashboard');
  if (!c) return;
  const appDomain = S.apps.find(a => a.id === 'home-assistant')?.domain;
  c.innerHTML = `
    <div style="display:flex;flex-direction:column;height:calc(100vh - 120px);min-height:400px;gap:8px">
      <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
        ${appDomain ? `<a class="btn btn-vio" href="https://${appDomain}" target="_blank" rel="noopener"
          style="text-decoration:none;font-size:9px"><i class="ti ti-external-link"></i>OUVRIR DANS NOUVEL ONGLET</a>` : ''}
        <span style="font-size:9px;color:var(--text3)">Domotique — interface intégrée</span>
      </div>
      <iframe src="/ui/proxy/home-assistant/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
        allow="fullscreen" title="Home Assistant"></iframe>
    </div>`;
}

// ── Calibre-Web — Bibliothèque ebooks ─────────────────────────────────────────
async function loadCalibreBooks() {
  const c = document.getElementById('content-panel-calibre-books');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT CALIBRE-WEB...</div>`;

  const appDomain = S.apps.find(a => a.id === 'calibre-web')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR CALIBRE-WEB</a>`
    : '';

  let books = null;
  try {
    const r = await fetch('/ui/proxy/calibre-web/opds/new');
    if (r.ok) {
      const xml = await r.text();
      const parser = new DOMParser();
      const doc = parser.parseFromString(xml, 'application/xml');
      const entries = Array.from(doc.querySelectorAll('entry'));
      books = entries.slice(0, 20).map(e => ({
        title: e.querySelector('title')?.textContent || '—',
        author: e.querySelector('author name')?.textContent || '—',
        updated: e.querySelector('updated')?.textContent || '',
      }));
    }
  } catch(e) {}

  if (!books) {
    // Fallback: iframe embed
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:10px">${adminLink}</div>
      <div style="display:flex;flex-direction:column;height:calc(100vh - 180px);min-height:300px">
        <iframe src="/ui/proxy/calibre-web/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
          allow="fullscreen" title="Calibre-Web"></iframe>
      </div>`;
    return;
  }

  const rows = books.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-book"></i></div>
        <div class="empty-title">BIBLIOTHÈQUE VIDE</div>
        <div class="empty-sub">Ajoutez des livres dans le dossier books.</div></div>`
    : books.map(b => `
        <div style="display:flex;align-items:center;gap:8px;padding:6px 8px;background:var(--card);
            border-radius:6px;border:1px solid var(--border);margin-bottom:5px">
          <span style="font-size:16px">📖</span>
          <div style="flex:1;min-width:0">
            <div style="font-size:10px;font-weight:600;color:var(--text1);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(b.title)}</div>
            <div style="font-size:8px;color:var(--text3)">${escapeHtml(b.author)}</div>
          </div>
        </div>`).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      ${adminLink}
      <span style="font-size:9px;color:var(--text3)">${books.length} livre(s) récents</span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadCalibreBooks()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}`;
}

// ── Kavita — Bibliothèque manga/comics ───────────────────────────────────────
// ── Komga — Bibliothèque comics/mangas ───────────────────────────────────────
async function loadKomgaLibrary() {
  const c = document.getElementById('content-panel-komga-library');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT KOMGA...</div>`;

  const appDomain = S.apps.find(a => a.id === 'komga')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR KOMGA</a>`
    : '';

  // Komga API v1: lister les séries récentes
  let series = null;
  try {
    const r = await fetch('/ui/proxy/komga/api/v1/series?page=0&size=10&sort=lastModified,desc');
    if (r.ok) { const d = await r.json(); series = d?.content; }
  } catch(e) {}

  if (!Array.isArray(series)) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:10px">${adminLink}</div>
      <div style="display:flex;flex-direction:column;height:calc(100vh - 180px);min-height:300px">
        <iframe src="/ui/proxy/komga/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
          allow="fullscreen" title="Komga"></iframe>
      </div>`;
    return;
  }

  const rows = series.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-book"></i></div>
        <div class="empty-title">BIBLIOTHÈQUE VIDE</div>
        <div class="empty-sub">Placez vos comics/mangas dans les dossiers Komga.</div></div>`
    : series.map(s => `
        <div style="display:flex;align-items:center;gap:10px;padding:8px 10px;background:var(--card);
            border-radius:6px;border:1px solid var(--border);margin-bottom:5px">
          <span style="font-size:18px">📔</span>
          <div style="flex:1;min-width:0">
            <div style="font-size:11px;color:var(--text1);font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(s.name || '—')}</div>
            <div style="font-size:9px;color:var(--text3);margin-top:2px">${s.booksCount || 0} tome${s.booksCount > 1 ? 's' : ''}</div>
          </div>
          <span style="font-size:9px;color:${s.booksUnreadCount > 0 ? 'var(--vio)' : 'var(--text3)'}">${s.booksUnreadCount > 0 ? s.booksUnreadCount + ' non lu' : '✓'}</span>
        </div>`).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}
      <span style="font-size:9px;color:var(--text3);margin-left:auto">${series.length} SÉRIE${series.length > 1 ? 'S' : ''}</span>
    </div>
    ${rows}`;
}

// ── Code Server — Éditeur web ─────────────────────────────────────────────────
async function loadCodeServer() {
  const c = document.getElementById('content-panel-code-server');
  if (!c) return;

  const appDomain = S.apps.find(a => a.id === 'code-server')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR ÉDITEUR</a>`
    : '';

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:10px">${adminLink}</div>
    <div style="display:flex;flex-direction:column;height:calc(100vh - 180px);min-height:300px">
      <iframe src="/ui/proxy/code-server/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
        allow="fullscreen" title="Code Server"></iframe>
    </div>`;
}

// ── Scrutiny — Monitoring disques SMART ──────────────────────────────────────
async function loadScrutinyDisks() {
  const c = document.getElementById('content-panel-scrutiny-disks');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT SCRUTINY...</div>`;

  const appDomain = S.apps.find(a => a.id === 'scrutiny')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR SCRUTINY</a>`
    : '';

  // Scrutiny API: /api/devices
  let devices = null;
  try {
    const r = await fetch('/ui/proxy/scrutiny/api/devices');
    if (r.ok) { const d = await r.json(); devices = d?.data; }
  } catch(e) {}

  if (!Array.isArray(devices)) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:10px">${adminLink}</div>
      <div style="display:flex;flex-direction:column;height:calc(100vh - 180px);min-height:300px">
        <iframe src="/ui/proxy/scrutiny/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
          allow="fullscreen" title="Scrutiny"></iframe>
      </div>`;
    return;
  }

  const rows = devices.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-database"></i></div>
        <div class="empty-title">AUCUN DISQUE DÉTECTÉ</div>
        <div class="empty-sub">Vérifiez la configuration des devices dans le docker-compose.</div></div>`
    : devices.map(d => {
        const passed = d.device_status === 0;
        const statusColor = passed ? 'var(--ok)' : 'var(--err)';
        const statusLabel = passed ? 'PASSED' : 'FAILED';
        const temp = d.temp !== undefined ? `${d.temp}°C` : '—';
        return `
        <div style="display:flex;align-items:center;gap:12px;padding:10px 14px;background:var(--card);
            border-radius:6px;border:1px solid ${passed ? 'var(--border)' : 'var(--err)'};margin-bottom:6px">
          <i class="ti ti-device-floppy" style="font-size:18px;color:${statusColor}"></i>
          <div style="flex:1">
            <div style="font-size:11px;color:var(--text1);font-weight:600">${escapeHtml(d.device_name || d.wwn || '—')}</div>
            <div style="font-size:9px;color:var(--text3);margin-top:2px">${escapeHtml(d.model_name || '')} · ${escapeHtml(d.capacity || '')}</div>
          </div>
          <div style="text-align:right">
            <div style="font-size:10px;color:${statusColor};font-weight:700">${statusLabel}</div>
            <div style="font-size:9px;color:var(--text3)">${temp}</div>
          </div>
        </div>`;
      }).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}
      <span style="font-size:9px;color:var(--text3);margin-left:auto">${devices.length} DISQUE${devices.length > 1 ? 'S' : ''}</span>
    </div>
    ${rows}`;
}

async function loadTraefikRoutes() {
  const c = document.getElementById('content-panel-traefik-routes');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;
  let routers = null;
  try {
    const r = await fetch('/sys/traefik-routes');
    if (r.ok) routers = await r.json();
  } catch(e) {}

  if (!Array.isArray(routers)) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-route"></i></div><div class="empty-title">ROUTES INDISPONIBLES</div><div class="empty-sub">Traefik API non accessible (port 8080).</div></div>`;
    return;
  }

  const dockerRouters = routers.filter(r => r.provider === 'docker' || r.provider?.startsWith('docker'));
  const internalRouters = routers.filter(r => r.provider === 'internal' || r.provider === 'file');
  const errRouters = routers.filter(r => r.status !== 'enabled');

  const routerRow = r => {
    const host = r.rule?.match(/Host\(`([^`]+)`\)/)?.[1] || '';
    const ep = (r.entryPoints || []).join(', ');
    const hasTLS = !!r.tls;
    const ok = r.status === 'enabled';
    return `<tr style="border-bottom:1px solid var(--border)">
      <td style="padding:5px 12px;font-weight:700;color:var(--text1);font-size:9px">${escapeHtml(r.name || r.service || '—')}</td>
      <td style="padding:5px 8px;font-size:9px">
        ${host ? `<span style="color:var(--accent)">${escapeHtml(host)}</span>` : `<span style="color:var(--text3);font-size:8px;font-family:monospace">${escapeHtml(r.rule || '—')}</span>`}
      </td>
      <td style="padding:5px 8px;font-size:8px;color:var(--text3)">${escapeHtml(ep)}</td>
      <td style="padding:5px 8px;text-align:center">
        ${hasTLS ? '<i class="ti ti-lock" style="font-size:10px;color:var(--green-b)" title="TLS"></i>' : '<i class="ti ti-lock-open" style="font-size:10px;color:var(--text3)" title="No TLS"></i>'}
      </td>
      <td style="padding:5px 12px 5px 8px;text-align:center">
        <span style="font-size:8px;color:${ok ? 'var(--ok)' : 'var(--err)'}">${ok ? '●' : '✗'}</span>
      </td>
    </tr>`;
  };

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      <div style="font-size:9px;color:var(--text3)">${routers.length} ROUTE${routers.length !== 1 ? 'S' : ''}</div>
      ${errRouters.length ? `<span style="font-size:8px;color:var(--err);font-weight:700">${errRouters.length} EN ERREUR</span>` : ''}
      <button class="btn-sm" style="margin-left:auto" onclick="loadTraefikRoutes()"><i class="ti ti-refresh"></i>REFRESH</button>
    </div>
    ${dockerRouters.length ? `
    <div class="settings-card" style="padding:0;margin-bottom:8px">
      <div class="settings-title" style="padding:8px 12px">DOCKER (${dockerRouters.length})</div>
      <table style="width:100%;border-collapse:collapse;font-size:9px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="padding:4px 12px;text-align:left">NOM</th>
          <th style="padding:4px 8px;text-align:left">HÔTE / RÈGLE</th>
          <th style="padding:4px 8px;text-align:left">ENTRY</th>
          <th style="padding:4px 8px;text-align:center">TLS</th>
          <th style="padding:4px 12px 4px 8px;text-align:center">ÉTAT</th>
        </tr></thead>
        <tbody>${dockerRouters.map(routerRow).join('')}</tbody>
      </table>
    </div>` : ''}
    ${internalRouters.length ? `
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:8px 12px">INTERNES / FICHIER (${internalRouters.length})</div>
      <table style="width:100%;border-collapse:collapse;font-size:9px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="padding:4px 12px;text-align:left">NOM</th>
          <th style="padding:4px 8px;text-align:left">HÔTE / RÈGLE</th>
          <th style="padding:4px 8px;text-align:left">ENTRY</th>
          <th style="padding:4px 8px;text-align:center">TLS</th>
          <th style="padding:4px 12px 4px 8px;text-align:center">ÉTAT</th>
        </tr></thead>
        <tbody>${internalRouters.map(routerRow).join('')}</tbody>
      </table>
    </div>` : ''}`;
}

async function loadTraefikServices() {
  const c = document.getElementById('content-panel-traefik-services');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;
  let services = null;
  try {
    const r = await fetch('/sys/traefik-services');
    if (r.ok) services = await r.json();
  } catch(e) {}

  if (!Array.isArray(services)) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-server"></i></div><div class="empty-title">SERVICES INDISPONIBLES</div></div>`;
    return;
  }

  const dockerSvcs = services.filter(s => s.provider === 'docker');
  const rows = dockerSvcs.map(s => {
    const servers = s.loadBalancer?.servers || [];
    const healthy = servers.filter(sv => sv.status === 'UP' || !sv.status).length;
    return `<tr style="border-bottom:1px solid var(--border)">
      <td style="padding:5px 12px;font-weight:700;font-size:9px;color:var(--text1)">${escapeHtml(s.name || '—')}</td>
      <td style="padding:5px 8px;font-size:8px;font-family:monospace;color:var(--text2)">${servers.map(sv => escapeHtml(sv.url || '')).join('<br>')}</td>
      <td style="padding:5px 8px;text-align:center;font-size:9px;color:${healthy === servers.length ? 'var(--ok)' : 'var(--err)'}">${healthy}/${servers.length}</td>
      <td style="padding:5px 12px 5px 8px;font-size:8px;color:var(--text3)">${escapeHtml(s.type || '—')}</td>
    </tr>`;
  }).join('');

  c.innerHTML = `
    <div style="font-size:9px;color:var(--text3);margin-bottom:12px">${dockerSvcs.length} SERVICE${dockerSvcs.length !== 1 ? 'S' : ''} DOCKER</div>
    <div class="settings-card" style="padding:0">
      <table style="width:100%;border-collapse:collapse;font-size:9px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="padding:4px 12px;text-align:left">NOM</th>
          <th style="padding:4px 8px;text-align:left">SERVEURS</th>
          <th style="padding:4px 8px;text-align:center">SANTÉ</th>
          <th style="padding:4px 12px 4px 8px;text-align:left">TYPE</th>
        </tr></thead>
        <tbody>${rows || '<tr><td colspan="4" style="padding:12px;text-align:center;color:var(--text3)">Aucun service Docker</td></tr>'}</tbody>
      </table>
    </div>`;
}

async function loadKavitaLibrary() {
  const c = document.getElementById('content-panel-kavita-library');
  if (!c) return;

  const appDomain = S.apps.find(a => a.id === 'kavita')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR KAVITA</a>`
    : '';

  // Kavita n'expose pas d'API public sans auth — on affiche juste un iframe
  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:10px">${adminLink}</div>
    <div style="display:flex;flex-direction:column;height:calc(100vh - 180px);min-height:300px">
      <iframe src="/ui/proxy/kavita/" style="flex:1;border:none;border-radius:6px;background:var(--card)"
        allow="fullscreen" title="Kavita"></iframe>
    </div>`;
}

// ── Navidrome — Bibliothèque musicale ────────────────────────────────────────
async function loadNavidromeLibrary() {
  const c = document.getElementById('content-panel-navidrome-library');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT NAVIDROME...</div>`;

  const appDomain = S.apps.find(a => a.id === 'navidrome')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR NAVIDROME</a>`
    : '';

  // Navidrome REST API: albums récents (Bearer JWT via proxy)
  let albums = null, artistCount = 0, songCount = 0;
  try {
    const [rAlbum, rStats] = await Promise.all([
      fetch('/ui/proxy/navidrome/api/album?_start=0&_end=12&_order=DESC&_sort=updatedAt'),
      fetch('/ui/proxy/navidrome/api/artist?_start=0&_end=1'),
    ]);
    if (rAlbum.ok) albums = await rAlbum.json();
    if (rStats.ok) {
      const countHeader = rStats.headers.get('X-Total-Count');
      artistCount = countHeader ? parseInt(countHeader) : 0;
    }
    const rSong = await fetch('/ui/proxy/navidrome/api/song?_start=0&_end=1');
    if (rSong.ok) songCount = parseInt(rSong.headers.get('X-Total-Count') || '0');
  } catch(e) {}

  if (!Array.isArray(albums)) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:10px">${adminLink}</div>
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-music-off"></i></div>
        <div class="empty-title">NAVIDROME INDISPONIBLE</div>
        <div class="empty-sub">Vérifiez les credentials dans app-config/navidrome/secrets.env</div></div>`;
    return;
  }

  const rows = albums.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-music"></i></div>
        <div class="empty-title">BIBLIOTHÈQUE VIDE</div>
        <div class="empty-sub">Placez votre musique dans le dossier music/ de Navidrome.</div></div>`
    : albums.map(a => `
        <div style="display:flex;align-items:center;gap:8px;padding:6px 8px;background:var(--card);
            border-radius:6px;border:1px solid var(--border);margin-bottom:5px">
          <span style="font-size:16px">🎵</span>
          <div style="flex:1;min-width:0">
            <div style="font-size:10px;font-weight:600;color:var(--text1);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${escapeHtml(a.name || '—')}</div>
            <div style="font-size:8px;color:var(--text3)">${escapeHtml(a.albumArtist || '—')} · ${a.songCount || 0} piste(s)</div>
          </div>
          <span style="font-size:8px;color:var(--text3)">${escapeHtml(String(a.minYear || ''))}</span>
        </div>`).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      ${adminLink}
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadNavidromeLibrary()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    <div style="display:flex;gap:8px;margin-bottom:12px">
      <div class="stat-mini"><span class="stat-val">${artistCount}</span><span class="stat-lbl">ARTISTES</span></div>
      <div class="stat-mini"><span class="stat-val">${songCount}</span><span class="stat-lbl">PISTES</span></div>
      <div class="stat-mini"><span class="stat-val">${albums.length}</span><span class="stat-lbl">ALBUMS RÉCENTS</span></div>
    </div>
    <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin-bottom:8px">// DERNIERS ALBUMS</div>
    ${rows}`;
}

// ── PhotoPrism — Statistiques ─────────────────────────────────────────────────
async function loadPhotoprismStats() {
  const c = document.getElementById('content-panel-photoprism-stats');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT PHOTOPRISM...</div>`;

  const appDomain = S.apps.find(a => a.id === 'photoprism')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR PHOTOPRISM</a>`
    : '';

  let photos = null;
  try {
    const r = await fetch('/ui/proxy/photoprism/api/v1/photos?count=12&offset=0&merged=true&quality=1&public=true');
    if (r.ok) photos = await r.json();
  } catch(e) {}

  if (!photos) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}</div>
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-camera"></i></div>
        <div class="empty-title">PHOTOPRISM INDISPONIBLE</div>
        <div class="empty-sub">Vérifiez que PhotoPrism est démarré. Une session est requise.</div></div>`;
    return;
  }

  const count = Array.isArray(photos) ? photos.length : 0;
  const grid = Array.isArray(photos) && photos.length > 0
    ? `<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:6px;margin-top:10px">
        ${photos.slice(0, 12).map(p => {
          const thumb = `/ui/proxy/photoprism/api/v1/t/${p.Hash}/public/tile_224`;
          return `<div style="aspect-ratio:1;border-radius:4px;overflow:hidden;background:var(--card);border:1px solid var(--border)">
            <img src="${escapeHtml(thumb)}" style="width:100%;height:100%;object-fit:cover"
              onerror="this.parentElement.innerHTML='<div style=\'display:flex;align-items:center;justify-content:center;height:100%\'>📷</div>'" alt="">
          </div>`;
        }).join('')}
      </div>`
    : `<div class="empty-state"><div class="empty-icon"><i class="ti ti-camera"></i></div>
        <div class="empty-title">AUCUNE PHOTO</div>
        <div class="empty-sub">Lancez une indexation depuis PhotoPrism.</div></div>`;

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      ${adminLink}
      <span style="font-size:9px;color:var(--text3)">${count} photo(s) récentes</span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadPhotoprismStats()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${grid}`;
}

// ── Jellyseerr — Demandes médias ─────────────────────────────────────────────
async function loadJellyseerrRequests() {
  const c = document.getElementById('content-panel-jellyseerr-requests');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT JELLYSEERR...</div>`;

  const appDomain = S.apps.find(a => a.id === 'jellyseerr')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR JELLYSEERR</a>`
    : '';

  let data = null;
  try {
    const r = await fetch('/ui/proxy/jellyseerr/api/v1/request?take=20&skip=0&sort=added&requestedBy=0');
    if (r.ok) data = await r.json();
  } catch(e) {}

  if (!data) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}</div>
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-ticket"></i></div>
        <div class="empty-title">JELLYSEERR INDISPONIBLE</div>
        <div class="empty-sub">Vérifiez que Jellyseerr est démarré et configuré.</div></div>`;
    return;
  }

  const requests = data.results || [];
  const pending  = requests.filter(r => r.status === 1).length;
  const approved = requests.filter(r => r.status === 2).length;

  const statusLabel = s => ({ 1: 'EN ATTENTE', 2: 'APPROUVÉE', 3: 'REFUSÉE', 4: 'DISPONIBLE', 5: 'TRAITÉE' }[s] || String(s));
  const statusCls   = s => ({ 1: 'badge-warn', 2: 'badge-run', 3: 'badge-err', 4: 'badge-run', 5: 'badge-stop' }[s] || 'badge-stop');

  const rows = requests.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-check"></i></div>
        <div class="empty-title">AUCUNE DEMANDE</div>
        <div class="empty-sub">Aucune demande de média pour l'instant.</div></div>`
    : requests.map(req => {
        const media = req.media || {};
        const title = media.originalTitle || req.media?.title || `Media #${media.tmdbId || '?'}`;
        const type = req.type === 'movie' ? '🎬' : '📺';
        const date = req.createdAt ? new Date(req.createdAt).toLocaleDateString('fr-FR') : '';
        const requester = req.requestedBy?.displayName || req.requestedBy?.email || '—';
        return `
          <div style="display:flex;align-items:center;gap:8px;padding:6px 8px;background:var(--card);
              border-radius:6px;border:1px solid var(--border);margin-bottom:5px">
            <span style="font-size:14px">${type}</span>
            <div style="flex:1">
              <div style="font-size:10px;font-weight:600;color:var(--text1)">${escapeHtml(title)}</div>
              <div style="font-size:8px;color:var(--text3)">${escapeHtml(requester)} · ${escapeHtml(date)}</div>
            </div>
            <span class="badge ${statusCls(req.status)}" style="font-size:7px">${statusLabel(req.status)}</span>
          </div>`;
      }).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap">
      ${adminLink}
      <span class="badge badge-warn" style="font-size:7px">${pending} en attente</span>
      <span class="badge badge-run" style="font-size:7px">${approved} approuvée(s)</span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadJellyseerrRequests()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}`;
}

// ── Grocy — Stock et tâches ───────────────────────────────────────────────────
async function loadGrocyStock() {
  const c = document.getElementById('content-panel-grocy-stock');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT GROCY...</div>`;

  const appDomain = S.apps.find(a => a.id === 'grocy')?.domain;
  const adminLink = appDomain
    ? `<a href="https://${appDomain}" target="_blank" rel="noopener" class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR GROCY</a>`
    : '';

  let stock = null;
  try {
    const r = await fetch('/ui/proxy/grocy/api/stock');
    if (r.ok) stock = await r.json();
  } catch(e) {}

  if (!stock) {
    c.innerHTML = `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">${adminLink}</div>
      <div class="empty-state"><div class="empty-icon"><i class="ti ti-shopping-cart"></i></div>
        <div class="empty-title">GROCY INDISPONIBLE</div>
        <div class="empty-sub">Vérifiez que Grocy est démarré. La clé API est requise dans la configuration.</div></div>`;
    return;
  }

  const expiringSoon = stock.filter(p => {
    if (!p.best_before_date) return false;
    const days = (new Date(p.best_before_date) - new Date()) / 86400000;
    return days >= 0 && days <= 7;
  });
  const outOfStock = stock.filter(p => parseFloat(p.amount) <= 0);

  const rows = stock.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-shopping-cart"></i></div>
        <div class="empty-title">STOCK VIDE</div>
        <div class="empty-sub">Ajoutez des produits depuis l'interface Grocy.</div></div>`
    : `<table style="width:100%;border-collapse:collapse;font-size:9px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="text-align:left;padding:4px 6px">PRODUIT</th>
          <th style="text-align:left;padding:4px 6px">QUANTITÉ</th>
          <th style="text-align:left;padding:4px 6px">DLC</th>
        </tr></thead>
        <tbody>${stock.slice(0, 30).map(p => {
          const dlc = p.best_before_date || '—';
          const days = p.best_before_date ? Math.ceil((new Date(p.best_before_date) - new Date()) / 86400000) : null;
          const dlcCls = days !== null && days <= 3 ? 'color:var(--red-b)' : days !== null && days <= 7 ? 'color:var(--warn)' : 'color:var(--text3)';
          return `<tr style="border-bottom:1px solid var(--border)">
            <td style="padding:5px 6px;color:var(--text1)">${escapeHtml(p.product?.name || String(p.product_id))}</td>
            <td style="padding:5px 6px;color:var(--text2)">${parseFloat(p.amount || 0).toFixed(1)} ${escapeHtml(p.quantity_unit?.name || '')}</td>
            <td style="padding:5px 6px;${dlcCls}">${escapeHtml(dlc)}</td>
          </tr>`;
        }).join('')}</tbody>
      </table>`;

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:8px;flex-wrap:wrap">
      ${adminLink}
      <span style="font-size:9px;color:var(--text3)">${stock.length} produit(s)</span>
      ${expiringSoon.length ? `<span class="badge badge-warn" style="font-size:7px">${expiringSoon.length} exp. bientôt</span>` : ''}
      ${outOfStock.length ? `<span class="badge badge-err" style="font-size:7px">${outOfStock.length} rupture(s)</span>` : ''}
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadGrocyStock()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}`;
}

async function loadGrocyTasks() {
  const c = document.getElementById('content-panel-grocy-tasks');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT TÂCHES...</div>`;

  let tasks = null;
  try {
    const r = await fetch('/ui/proxy/grocy/api/tasks?done=0');
    if (r.ok) tasks = await r.json();
  } catch(e) {}

  if (!tasks) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-checklist"></i></div>
      <div class="empty-title">GROCY INDISPONIBLE</div></div>`;
    return;
  }

  const rows = tasks.length === 0
    ? `<div class="empty-state"><div class="empty-icon"><i class="ti ti-check"></i></div>
        <div class="empty-title">AUCUNE TÂCHE</div>
        <div class="empty-sub">Toutes les tâches sont accomplies !</div></div>`
    : tasks.map(t => {
        const due = t.due_date || null;
        const overdue = due && new Date(due) < new Date();
        return `
          <div style="display:flex;align-items:center;gap:8px;padding:6px 8px;background:var(--card);
              border-radius:6px;border:1px solid var(--border);margin-bottom:5px">
            <i class="ti ti-circle" style="color:var(--text3);font-size:12px"></i>
            <span style="flex:1;font-size:10px;color:var(--text1)">${escapeHtml(t.name || '—')}</span>
            ${due ? `<span style="font-size:8px;color:${overdue ? 'var(--red-b)' : 'var(--text3)'}">${escapeHtml(due)}</span>` : ''}
          </div>`;
      }).join('');

  c.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:12px">
      <span style="font-size:9px;color:var(--text3)">${tasks.length} tâche(s) en cours</span>
      <button class="btn" style="margin-left:auto;font-size:9px" onclick="loadGrocyTasks()">
        <i class="ti ti-refresh"></i> RAFRAÎCHIR</button>
    </div>
    ${rows}`;
}

// ── Syncthing — Statut synchronisation ────────────────────────────────────────
async function loadSyncthingStatus() {
  const c = document.getElementById('content-panel-syncthing-status');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT SYNCTHING...</div>`;

  const appDomain = S.apps.find(a => a.id === 'syncthing')?.domain;
  const adminLink = appDomain ? `<a href="https://${appDomain}" target="_blank" rel="noopener"
    class="btn btn-vio" style="text-decoration:none"><i class="ti ti-external-link"></i>OUVRIR SYNCTHING</a>` : '';

  const [rStatus, rFolders, rConnections] = await Promise.all([
    fetch('/ui/proxy/syncthing/rest/system/status'),
    fetch('/ui/proxy/syncthing/rest/config/folders'),
    fetch('/ui/proxy/syncthing/rest/system/connections'),
  ]);

  if (!rStatus.ok) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-refresh"></i></div>
      <div class="empty-title">SYNCTHING INDISPONIBLE</div>
      <div class="empty-sub">${rStatus.status === 503 ? 'Service non démarré.' : 'Erreur d\'accès à l\'API Syncthing.'}</div>
      ${adminLink ? `<div style="margin-top:12px">${adminLink}</div>` : ''}</div>`;
    return;
  }

  const status = await rStatus.json();
  const folders = rFolders.ok ? await rFolders.json() : [];
  const connections = rConnections.ok ? await rConnections.json() : {};
  const connList = Object.values(connections.connections || {});
  const connectedDevices = connList.filter(d => d.connected).length;

  const folderRows = folders.slice(0, 5).map(f => `
    <div class="loc-row">
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(f.label || f.id)}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(f.path || '')}</div>
      </div>
      <span style="font-size:9px;color:var(--text3)">${f.type === 'sendonly' ? 'ENVOI' : f.type === 'receiveonly' ? 'RÉCEPTION' : 'SYNC'}</span>
    </div>`).join('') || '<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucun dossier partagé.</div>';

  c.innerHTML = `
    <div class="dash-row" style="gap:8px;margin-bottom:12px">
      <div class="stat-card"><div class="stat-val">${folders.length}</div><div class="stat-lbl">DOSSIERS</div></div>
      <div class="stat-card"><div class="stat-val" style="color:${connectedDevices > 0 ? 'var(--green-b)' : 'var(--text3)'}">${connectedDevices}</div><div class="stat-lbl">CONNECTÉS</div></div>
    </div>
    <div class="settings-card" style="padding:0">
      <div class="settings-title" style="padding:10px 12px">
        <i class="ti ti-refresh" style="font-size:12px"></i> DOSSIERS PARTAGÉS
      </div>
      <div style="padding:0 12px 12px">${folderRows}</div>
    </div>
    ${adminLink ? `<div style="display:flex;justify-content:center;margin-top:8px">${adminLink}</div>` : ''}`;
}

function goSection(id) {
  S.section = id;
  pushRecentSection(id);

  // Nav buttons
  document.querySelectorAll('.nav-btn').forEach(b => {
    b.classList.toggle('active', b.dataset.section === id);
  });

  // Title
  const sec = SECTIONS[id];
  document.getElementById('tb-section').textContent = sec?.num || '';
  document.getElementById('tb-title').textContent   = sec?.label || '';

  // Bouton contextuel
  const tbBtn = document.getElementById('tb-primary-btn');
  if (tbBtn) {
    if (sec?.btn) {
      tbBtn.style.display = '';
      tbBtn.innerHTML = `<i class="ti ${sec.btn.icon}"></i>${sec.btn.label}`;
      tbBtn.setAttribute('onclick', sec.btn.action);
    } else {
      tbBtn.style.display = 'none';
    }
  }

  // Sections
  document.querySelectorAll('.section-content').forEach(el => el.classList.add('hidden'));
  const target = document.getElementById(sec?.content);
  if (target) target.classList.remove('hidden');

  // Scroll en haut à chaque changement de section
  const contentEl = document.querySelector('.content');
  if (contentEl) contentEl.scrollTop = 0;

  // Contrôle d'app dans la topbar pour les panels d'intégration
  const tbCtrl = document.getElementById('tb-app-ctrl');
  if (tbCtrl) {
    const panelAppId = Object.keys(APP_PANELS).find(aid =>
      id.startsWith('panel-' + aid + '-') || id === 'panel-' + aid
    );
    if (panelAppId) {
      const appObj = S.apps.find(a => a.id === panelAppId);
      if (appObj) {
        const isRun = appObj.status === 'running';
        const openLink = appObj.domain ? `<a href="https://${appObj.domain}" target="_blank" rel="noopener"
          class="btn-sm" title="Ouvrir ${escapeHtml(appObj.name || panelAppId)}" style="text-decoration:none">
          <i class="ti ti-external-link" style="font-size:10px"></i></a>` : '';
        tbCtrl.style.display = 'inline-flex';
        tbCtrl.style.alignItems = 'center';
        tbCtrl.style.gap = '6px';
        tbCtrl.innerHTML = `
          <span style="display:inline-block;width:6px;height:6px;border-radius:50%;background:${isRun ? 'var(--green-b)' : 'var(--red-b)'}"></span>
          <span style="font-size:9px;color:${isRun ? 'var(--green-b)' : 'var(--red-b)'}">${isRun ? 'ACTIF' : 'ARRÊTÉ'}</span>
          ${isRun ? `<button class="btn-sm" onclick="restartApp('${panelAppId}')" title="Redémarrer ${appObj.name}">
            <i class="ti ti-refresh" style="font-size:10px"></i>
          </button>
          <button class="btn-sm" onclick="stopApp('${panelAppId}')" title="Arrêter ${appObj.name}">
            <i class="ti ti-player-pause" style="font-size:10px"></i>
          </button>` : `<button class="btn-sm" onclick="startApp('${panelAppId}')" title="Démarrer ${appObj.name}">
            <i class="ti ti-player-play" style="font-size:10px"></i>
          </button>`}
          ${openLink}`;
      } else {
        tbCtrl.style.display = 'none';
        tbCtrl.innerHTML = '';
      }
    } else {
      tbCtrl.style.display = 'none';
      tbCtrl.innerHTML = '';
    }
  }

  // Load data
  if (sec?.load) sec.load();
}

// ── SECTION: ÉVÉNEMENTS ──────────────────────────────────────────────────────

const EVENT_ICONS = {
  'app.installed':   'ti-package',
  'app.removed':     'ti-trash',
  'app.started':     'ti-player-play',
  'app.stopped':     'ti-player-stop',
  'app.restarted':   'ti-refresh',
  'app.backed_up':   'ti-device-floppy',
  'app.restored':    'ti-history',
  'app.failed':      'ti-alert-circle',
};

async function pollEventsBadge() {
  if (S.section === 'events') { clearEventsBadge(); return; }
  try {
    const resp = await api.get('/api/v1/events?limit=20');
    const evts = resp?.data || [];
    if (!evts.length) return;
    const lastSeen = localStorage.getItem('caleope-events-seen') || '';
    const newest = evts[evts.length - 1]?.timestamp || evts[0]?.timestamp || '';
    if (lastSeen && newest <= lastSeen) return;
    const unseen = evts.filter(e => (e.timestamp || '') > lastSeen).length;
    const badge = document.getElementById('events-badge');
    if (badge && unseen > 0) {
      badge.textContent = unseen > 9 ? '9+' : String(unseen);
      badge.style.display = '';
    }
  } catch(e) {}
}

function clearEventsBadge() {
  const badge = document.getElementById('events-badge');
  if (badge) badge.style.display = 'none';
  try {
    const resp = S.events_last_ts;
    if (resp) localStorage.setItem('caleope-events-seen', resp);
    else api.get('/api/v1/events?limit=1').then(r => {
      const ts = r?.data?.[0]?.timestamp || r?.data?.[r?.data?.length-1]?.timestamp;
      if (ts) localStorage.setItem('caleope-events-seen', ts);
    });
  } catch(e) {}
}

async function loadEvents() {
  const c = document.getElementById('content-events');
  if (!c) return;
  c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;

  const resp = await api.get('/api/v1/events?limit=100');
  const evts = resp?.data || [];

  // Marquer les événements comme vus
  if (evts.length) {
    const newest = evts[evts.length - 1]?.timestamp || evts[0]?.timestamp;
    if (newest) {
      try { localStorage.setItem('caleope-events-seen', newest); } catch(e) {}
    }
    clearEventsBadge();
  }

  if (!evts.length) {
    c.innerHTML = `<div class="empty-state">
      <div class="empty-icon"><i class="ti ti-history"></i></div>
      <div class="empty-title">AUCUN ÉVÉNEMENT</div>
      <div class="empty-sub">Les installations, suppressions, démarrages et arrêts d'applications apparaîtront ici.</div>
    </div>`;
    return;
  }

  const rows = [...evts].reverse().map(e => {
    const type = e.event || e.type || '';
    const appId = e.app || e.app_id || e.appId || '';
    const ico = EVENT_ICONS[type] || 'ti-circle';
    const isErr = type.includes('failed') || type.includes('error');
    const dotCls = isErr ? 'var(--err)' : type.includes('removed') ? 'var(--warn)' : 'var(--ok)';
    const ts = e.timestamp ? new Date(e.timestamp).toLocaleString('fr-FR') : '';
    return `<div class="loc-row" style="gap:12px">
      <div style="width:30px;height:30px;border-radius:2px;background:var(--bg3);flex-shrink:0;
        display:flex;align-items:center;justify-content:center">
        <i class="ti ${ico}" style="font-size:13px;color:${dotCls}"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(type || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(appId || '—')}</div>
      </div>
      <div style="text-align:right;flex-shrink:0;font-size:8px;color:var(--text3)">${escapeHtml(ts)}</div>
    </div>`;
  }).join('');

  const types = [...new Set(evts.map(e => e.event || e.type || '').filter(Boolean))].sort();
  const filterBtns = `<div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin-bottom:10px">
    <button class="tab-btn active" style="font-size:8px;padding:2px 7px" onclick="filterEvents(this,'')">TOUT</button>
    ${types.map(t => `<button class="tab-btn" style="font-size:8px;padding:2px 7px" onclick="filterEvents(this,'${escapeHtml(t)}')">${escapeHtml(t.replace('app.','').toUpperCase())}</button>`).join('')}
    <input type="search" id="events-search" placeholder="Rechercher app..." autocomplete="off"
      oninput="filterEventsSearch()"
      style="margin-left:auto;font-size:9px;padding:3px 8px;background:var(--card);border:1px solid var(--border);
             border-radius:4px;color:var(--text1);width:150px;outline:none">
  </div>`;

  c.innerHTML = `${filterBtns}<div class="settings-card" style="padding:0">
    <div class="settings-title" style="padding:10px 12px">HISTORIQUE DES ÉVÉNEMENTS
      <span style="color:var(--text3);font-size:9px;margin-left:6px">${evts.length}</span>
    </div>
    <div id="events-rows" style="padding:0 12px 12px">${rows}</div>
  </div>`;
  window._evtRows = [...evts].reverse();
  window._evtTypeFilter = '';
}

// ── Audit ────────────────────────────────────────────────────────────────────
async function loadAudit() {
  const c = document.getElementById('content-audit');
  if (!c) return;
  const resp = await api.get('/api/v1/audit');
  // Format réponse daemon: {data: {lines: string[], count: N}}
  const rawLines = resp?.data?.lines || resp?.lines || [];
  if (!rawLines.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-clipboard-list"></i></div><div class="empty-title">AUCUNE ENTRÉE</div><div class="empty-sub">Les actions (install, suppression, secrets) apparaîtront ici.</div></div>`;
    return;
  }
  // Format brut : "2026-06-21T12:00:00Z ACTION app=ID result=RESULT"
  const lines = [...rawLines].reverse().map(line => {
    const parts = line.split(' ');
    const ts = parts[0] || '';
    const action = parts[1] || '';
    const rest = parts.slice(2).join(' ');
    const appMatch = rest.match(/app=(\S+)/);
    const resultMatch = rest.match(/result=(\S+)/);
    const appId = appMatch?.[1] || '';
    const result = resultMatch?.[1] || '';
    const isOk = result.startsWith('OK');
    const isErr = action === 'ERROR' || result.startsWith('DENIED');
    const tagCls = isErr ? 'log-err' : isOk ? 'log-ok' : 'log-step';
    const dateStr = ts ? new Date(ts).toLocaleString('fr-FR') : '';
    return `<div class="log-line">
      <span class="log-ts">${escapeHtml(dateStr)}</span>
      <span class="log-tag ${tagCls}">[${escapeHtml(action)}]</span>
      <span class="log-txt">${escapeHtml(appId)}${result ? ' — ' + escapeHtml(result) : ''}</span>
    </div>`;
  }).join('');
  c.innerHTML = `
    <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px;flex-wrap:wrap">
      <div style="font-size:9px;color:var(--text3)">${rawLines.length} ENTRÉES</div>
      <input type="search" id="audit-search" placeholder="Rechercher..." autocomplete="off"
        oninput="filterAudit()"
        style="flex:1;min-width:120px;max-width:220px;font-size:9px;padding:3px 8px;background:var(--card);
               border:1px solid var(--border);border-radius:4px;color:var(--text1);outline:none">
      <div style="display:flex;gap:4px;flex-wrap:wrap">
        <button class="tab-btn active" style="font-size:8px;padding:2px 7px" onclick="setAuditFilter(this,'')">TOUT</button>
        <button class="tab-btn" style="font-size:8px;padding:2px 7px" onclick="setAuditFilter(this,'INSTALL')">INSTALL</button>
        <button class="tab-btn" style="font-size:8px;padding:2px 7px" onclick="setAuditFilter(this,'REMOVE')">REMOVE</button>
        <button class="tab-btn" style="font-size:8px;padding:2px 7px" onclick="setAuditFilter(this,'START')">START</button>
        <button class="tab-btn" style="font-size:8px;padding:2px 7px" onclick="setAuditFilter(this,'STOP')">STOP</button>
        <button class="tab-btn" style="font-size:8px;padding:2px 7px" onclick="setAuditFilter(this,'ERROR')">ERREUR</button>
      </div>
      <button class="btn-sm" onclick="exportAudit()" title="Exporter le journal d'audit" style="margin-left:auto">
        <i class="ti ti-download" style="font-size:10px"></i> EXPORTER
      </button>
    </div>
    <div class="log-wrap"><div class="log-body" id="audit-body">${lines}</div></div>`;
  // Store raw lines for export and filter
  window._auditLines = rawLines;
  window._auditFilter = '';
}

function filterEventsSearch() {
  filterEvents(null, window._evtTypeFilter || '');
}

function filterEvents(btn, type) {
  if (btn) document.querySelectorAll('#content-events .tab-btn').forEach(b => b.classList.toggle('active', b === btn));
  window._evtTypeFilter = type;
  const evts = window._evtRows || [];
  const container = document.getElementById('events-rows');
  if (!container) return;
  const search = document.getElementById('events-search')?.value?.toLowerCase() || '';
  const filtered = evts.filter(e => {
    const evType = e.event || e.type || '';
    const appId = e.app || e.app_id || e.appId || '';
    if (type && evType !== type) return false;
    if (search && !appId.toLowerCase().includes(search) && !evType.toLowerCase().includes(search)) return false;
    return true;
  });
  container.innerHTML = filtered.map(e => {
    const evType = e.event || e.type || '';
    const appId = e.app || e.app_id || e.appId || '';
    const ico = EVENT_ICONS[evType] || 'ti-circle';
    const isErr = evType.includes('failed') || evType.includes('error');
    const dotCls = isErr ? 'var(--err)' : evType.includes('removed') ? 'var(--warn)' : 'var(--ok)';
    const ts = e.timestamp ? new Date(e.timestamp).toLocaleString('fr-FR') : '';
    return `<div class="loc-row" style="gap:12px">
      <div style="width:30px;height:30px;border-radius:2px;background:var(--bg3);flex-shrink:0;
        display:flex;align-items:center;justify-content:center">
        <i class="ti ${ico}" style="font-size:13px;color:${dotCls}"></i>
      </div>
      <div style="flex:1;min-width:0">
        <div style="font-size:10px;font-weight:700">${escapeHtml(evType || '—')}</div>
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(appId || '—')}</div>
      </div>
      <div style="text-align:right;flex-shrink:0;font-size:8px;color:var(--text3)">${escapeHtml(ts)}</div>
    </div>`;
  }).join('') || '<div style="font-size:9px;color:var(--text3);padding:12px">Aucun événement de ce type.</div>';
}

function exportAudit() {
  const lines = window._auditLines || [];
  if (!lines.length) { notify('Aucune donnée à exporter', 'err'); return; }
  const blob = new Blob([lines.join('\n')], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `caleope-audit-${new Date().toISOString().slice(0,10)}.log`;
  a.click();
  URL.revokeObjectURL(url);
  notify('Export téléchargé', 'ok');
}

function setAuditFilter(btn, action) {
  window._auditFilter = action;
  document.querySelectorAll('#content-audit .tab-btn').forEach(b => b.classList.toggle('active', b === btn));
  filterAudit();
}

function filterAudit() {
  const body = document.getElementById('audit-body');
  if (!body) return;
  const rawLines = window._auditLines || [];
  const search = document.getElementById('audit-search')?.value?.toLowerCase() || '';
  const actionFilter = window._auditFilter || '';
  const filtered = [...rawLines].reverse().filter(line => {
    const parts = line.split(' ');
    const action = parts[1] || '';
    if (actionFilter && action !== actionFilter) return false;
    if (search && !line.toLowerCase().includes(search)) return false;
    return true;
  });
  body.innerHTML = filtered.map(line => {
    const parts = line.split(' ');
    const ts = parts[0] || '';
    const action = parts[1] || '';
    const rest = parts.slice(2).join(' ');
    const appMatch = rest.match(/app=(\S+)/);
    const resultMatch = rest.match(/result=(\S+)/);
    const appId = appMatch?.[1] || '';
    const result = resultMatch?.[1] || '';
    const isOk = result.startsWith('OK');
    const isErr = action === 'ERROR' || result.startsWith('DENIED');
    const tagCls = isErr ? 'log-err' : isOk ? 'log-ok' : 'log-step';
    const dateStr = ts ? new Date(ts).toLocaleString('fr-FR') : '';
    return `<div class="log-line">
      <span class="log-ts">${escapeHtml(dateStr)}</span>
      <span class="log-tag ${tagCls}">[${escapeHtml(action)}]</span>
      <span class="log-txt">${escapeHtml(appId)}${result ? ' — ' + escapeHtml(result) : ''}</span>
    </div>`;
  }).join('') || '<div class="empty-msg">Aucune entrée correspondante</div>';
}

// ── SSO / OIDC ────────────────────────────────────────────────────────────────
async function checkOidcConfig() {
  try {
    const r = await fetch('/auth/oidc/config');
    if (!r.ok) return;
    const d = await r.json();
    const sect = document.getElementById('sso-section');
    const lbl = document.getElementById('sso-btn-label');
    if (sect) sect.style.display = d.enabled ? '' : 'none';
    if (lbl && d.name) lbl.textContent = d.name;
  } catch(e) {}
}

function loginWithSSO() {
  window.location.href = '/auth/oidc/start';
}

async function loadOidcSettings() {
  const badge = document.getElementById('oidc-status-badge');
  const loading = document.getElementById('oidc-loading');
  if (loading) loading.style.display = '';
  try {
    const r = await fetch('/auth/oidc/config');
    const d = r.ok ? await r.json() : {};
    if (badge) {
      badge.innerHTML = d.enabled
        ? `<span class="badge badge-ok" style="font-size:8px"><span style="width:5px;height:5px;background:var(--ok);display:inline-block;border-radius:50%;margin-right:3px"></span>SSO ACTIF — ${escapeHtml(d.name || 'OIDC')}</span>`
        : `<span style="font-size:8px;color:var(--text3)">SSO non configuré</span>`;
    }
    // Pré-remplir les champs si on a une config existante (lire le fichier via API si besoin)
    // Les champs secrets ne sont pas renvoyés par /auth/oidc/config pour la sécurité
  } catch(e) {}
  if (loading) loading.style.display = 'none';
}

async function saveOidcSettings() {
  const issuer = document.getElementById('oidc-issuer')?.value?.trim() || '';
  const clientId = document.getElementById('oidc-client-id')?.value?.trim() || '';
  const clientSecret = document.getElementById('oidc-client-secret')?.value?.trim() || '';
  const name = document.getElementById('oidc-name')?.value?.trim() || 'SSO';
  const redirectUri = document.getElementById('oidc-redirect-uri')?.value?.trim() || '';

  if (!issuer || !clientId) { notify('Issuer et Client ID requis', 'err'); return; }

  notify('Sauvegarde...', 'info');
  try {
    const r = await fetch('/auth/oidc/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ issuer, client_id: clientId, client_secret: clientSecret, name, redirect_uri: redirectUri })
    });
    const d = await r.json().catch(() => ({}));
    if (r.ok && d.status === 'ok') {
      notify('Configuration SSO sauvegardée', 'ok');
      loadOidcSettings();
      checkOidcConfig();
    } else {
      notify(d.error || 'Erreur sauvegarde SSO', 'err');
    }
  } catch(e) {
    notify('Erreur réseau', 'err');
  }
}

async function clearOidcSettings() {
  if (!confirm('Désactiver le SSO ? Le bouton de connexion SSO sera retiré de l\'écran de login.')) return;
  ['oidc-name','oidc-issuer','oidc-client-id','oidc-client-secret','oidc-redirect-uri'].forEach(id => {
    const el = document.getElementById(id);
    if (el) el.value = '';
  });
  await saveOidcSettings().catch(() => {});
  // Sauvegarder avec champs vides = désactive
  try {
    await fetch('/auth/oidc/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ issuer: '', client_id: '', client_secret: '', name: '', redirect_uri: '' })
    });
    notify('SSO désactivé', 'ok');
    loadOidcSettings();
    checkOidcConfig();
  } catch(e) {}
}

// ── TOTP 2FA settings ─────────────────────────────────────────────────────────

async function loadTOTPSettings() {
  const el = document.getElementById('totp-settings-content');
  if (!el) return;
  let status = { enabled: false, has_secret: false };
  try {
    const r = await fetch('/auth/totp/status');
    if (r.ok) status = await r.json();
  } catch(e) {}
  _renderTOTPSettings(status);
}

function _renderTOTPSettings(status) {
  const el = document.getElementById('totp-settings-content');
  if (!el) return;
  if (status.enabled) {
    el.innerHTML = `
      <div style="display:flex;align-items:center;gap:10px;margin-bottom:10px">
        <div style="width:28px;height:28px;background:rgba(52,211,153,.12);border:1px solid var(--ok);border-radius:50%;display:flex;align-items:center;justify-content:center;flex-shrink:0">
          <i class="ti ti-shield-check" style="font-size:14px;color:var(--ok)"></i>
        </div>
        <div>
          <div style="font-size:10px;font-weight:700;color:var(--ok)">2FA ACTIVÉ</div>
          <div style="font-size:9px;color:var(--text3)">La double authentification est requise à chaque connexion</div>
        </div>
      </div>
      <div style="font-size:9px;color:var(--text3);margin-bottom:8px">Pour désactiver, saisissez votre code TOTP actuel :</div>
      <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">
        <input type="text" id="totp-disable-code" class="param-input" style="width:110px;text-align:center;font-size:14px;letter-spacing:4px"
          placeholder="123456" maxlength="6" inputmode="numeric">
        <button class="btn btn-sm danger" onclick="disableTOTP()"><i class="ti ti-lock-open"></i>DÉSACTIVER</button>
      </div>`;
  } else {
    el.innerHTML = `
      <div style="font-size:9px;color:var(--text3);margin-bottom:10px">
        Ajoutez une couche de sécurité supplémentaire. Après activation, chaque connexion demandera un code TOTP généré par votre application (Google Authenticator, Aegis, Bitwarden…).
      </div>
      <div id="totp-setup-area" style="display:none">
        <div style="margin-bottom:10px;padding:10px 12px;background:var(--bg3);border:1px solid var(--border);border-radius:6px">
          <div style="font-size:8px;color:var(--text3);margin-bottom:4px;letter-spacing:.5px">SECRET (à entrer manuellement dans votre app)</div>
          <code id="totp-secret-display" style="font-size:12px;letter-spacing:3px;color:var(--text1);word-break:break-all"></code>
          <div style="margin-top:8px;font-size:8px;color:var(--text3)">
            <i class="ti ti-info-circle" style="font-size:9px"></i>
            Dans Google Authenticator / Aegis : Ajouter → Entrer manuellement → coller le secret ci-dessus.
          </div>
        </div>
        <div style="font-size:9px;color:var(--text3);margin-bottom:6px">Confirmez en entrant le code affiché par votre app :</div>
        <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">
          <input type="text" id="totp-enable-code" class="param-input" style="width:110px;text-align:center;font-size:14px;letter-spacing:4px"
            placeholder="123456" maxlength="6" inputmode="numeric">
          <button class="btn btn-vio" onclick="enableTOTP()"><i class="ti ti-shield-check"></i>ACTIVER LE 2FA</button>
        </div>
      </div>
      <button class="btn" id="totp-gen-btn" onclick="setupTOTP()"><i class="ti ti-key"></i>GÉNÉRER UN SECRET TOTP</button>`;
  }
}

async function setupTOTP() {
  const btn = document.getElementById('totp-gen-btn');
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="spinner"></span>&nbsp;GÉNÉRATION...'; }
  try {
    const r = await fetch('/auth/totp/setup', { method: 'POST' });
    const d = await r.json();
    if (!r.ok) { notify(d.error || 'Erreur génération TOTP', 'err'); return; }
    const area = document.getElementById('totp-setup-area');
    const secretEl = document.getElementById('totp-secret-display');
    if (area) area.style.display = '';
    if (secretEl) secretEl.textContent = d.secret || '';
    if (btn) btn.style.display = 'none';
  } catch(e) {
    notify('Erreur réseau', 'err');
  }
}

async function enableTOTP() {
  const code = document.getElementById('totp-enable-code')?.value?.trim();
  if (!code || code.length !== 6) { notify('Code TOTP à 6 chiffres requis', 'err'); return; }
  const r = await fetch('/auth/totp/enable', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  });
  const d = await r.json().catch(() => ({}));
  if (r.ok && d.status === 'ok') {
    notify('2FA activé avec succès', 'ok');
    _renderTOTPSettings({ enabled: true });
  } else {
    notify(d.error || 'Code invalide', 'err');
  }
}

async function disableTOTP() {
  const code = document.getElementById('totp-disable-code')?.value?.trim();
  if (!code || code.length !== 6) { notify('Code TOTP à 6 chiffres requis', 'err'); return; }
  const r = await fetch('/auth/totp/disable', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  });
  const d = await r.json().catch(() => ({}));
  if (r.ok && d.status === 'ok') {
    notify('2FA désactivé', 'ok');
    _renderTOTPSettings({ enabled: false });
  } else {
    notify(d.error || 'Code invalide', 'err');
  }
}

// ── Login screen ──────────────────────────────────────────────────────────────
function showLogin() {
  document.getElementById('app').classList.remove('visible');
  document.getElementById('login-screen').style.display = 'flex';
  // Vérifier les erreurs OIDC dans l'URL (callback redirect)
  const params = new URLSearchParams(window.location.search);
  const oidcErr = params.get('oidc_error');
  if (oidcErr) {
    const errEl = document.getElementById('login-error');
    if (errEl) errEl.textContent = 'Erreur SSO : ' + oidcErr.replace(/_/g,' ');
    history.replaceState(null, '', '/');
  }
}

let _dashRefreshInterval = null;

function updateTbSysbar() {
  const el = document.getElementById('tb-sysbar');
  if (!el) return;
  const ram  = S.stats.mem_total_mb  ? Math.round(S.stats.mem_used_mb  / S.stats.mem_total_mb  * 100) : null;
  const disk = S.stats.disk_total_gb ? Math.round(S.stats.disk_used_gb / S.stats.disk_total_gb * 100) : null;
  if (ram === null && disk === null) { el.style.display = 'none'; return; }

  const pill = (label, pct) => {
    const col = pct > 85 ? 'var(--red-b)' : pct > 70 ? 'var(--warn)' : 'var(--green-b)';
    return `<span style="display:inline-flex;align-items:center;gap:3px">
      <span style="color:var(--text3)">${label}</span>
      <span style="font-weight:700;color:${col}">${pct}%</span>
    </span>`;
  };

  el.style.display = 'inline-flex';
  el.innerHTML = [
    ram  !== null ? pill('RAM',   ram)  : '',
    disk !== null ? pill('DISK',  disk) : '',
  ].filter(Boolean).join('<span style="color:var(--border1)">·</span>');
}

function showApp() {
  document.getElementById('login-screen').style.display = 'none';
  document.getElementById('app').classList.add('visible');
  restoreSbGroups();
  startClock();
  goSection('dashboard');
  _startDashRefresh();
  pollEventsBadge();
  checkLicenseOnLogin();
}

async function checkLicenseOnLogin() {
  try {
    const r = await fetch('/api/v1/license');
    if (!r.ok) return;
    const d = await r.json();
    // La réponse API est enveloppée : {data:{activated,...},success}. On lit d.data.
    const lic = (d && d.data) ? d.data : d;
    if (!lic || !lic.activated) showLicenseModal();
  } catch(e) {}
}

function showLicenseModal() {
  let m = document.getElementById('license-modal');
  if (!m) {
    m = document.createElement('div');
    m.id = 'license-modal';
    m.style.cssText = 'position:fixed;inset:0;z-index:9999;background:rgba(0,0,0,.75);display:flex;align-items:center;justify-content:center;backdrop-filter:blur(4px)';
    m.innerHTML = `
      <div style="background:var(--bg2);border:1px solid var(--border1);border-radius:12px;padding:32px;max-width:420px;width:90%;box-shadow:0 8px 40px rgba(0,0,0,.5)">
        <div style="display:flex;align-items:center;gap:10px;margin-bottom:20px">
          <i class="ti ti-license" style="font-size:20px;color:var(--vio-b)"></i>
          <div>
            <div style="font-size:11px;font-weight:700;letter-spacing:.5px;color:var(--text1)">ACTIVATION REQUISE</div>
            <div style="font-size:9px;color:var(--text3);margin-top:2px">Caleope n'est pas encore activé sur ce serveur</div>
          </div>
        </div>
        <div style="font-size:9px;color:var(--text3);margin-bottom:14px">Entrez votre clé de licence (Community ou Pro) pour activer. Sans licence, l'installation d'apps est désactivée.</div>
        <input id="lic-key-input" class="param-input" type="text" placeholder="CALP-XXXX-XXXX-XXXX" style="width:100%;margin-bottom:10px;font-family:monospace;text-transform:uppercase">
        <div id="lic-modal-err" style="display:none;font-size:9px;color:var(--red-b);margin-bottom:8px"></div>
        <div style="display:flex;gap:8px">
          <button class="btn btn-vio" onclick="activateLicenseModal()" style="flex:1"><i class="ti ti-check"></i>ACTIVER</button>
          <button class="btn" onclick="document.getElementById('license-modal').style.display='none'" style="font-size:9px">Plus tard</button>
        </div>
        <div style="margin-top:14px;padding-top:12px;border-top:1px solid var(--border1);font-size:8px;color:var(--text3)">
          Pas de licence ? <a href="https://caleope-pay.gaiver-it.fr" target="_blank" style="color:var(--vio-b)">Obtenir une clé</a> — Community est gratuit.
        </div>
      </div>`;
    document.body.appendChild(m);
  }
  m.style.display = 'flex';
  setTimeout(() => document.getElementById('lic-key-input')?.focus(), 50);
}

async function activateLicenseModal() {
  const key = document.getElementById('lic-key-input')?.value.trim().toUpperCase();
  const errEl = document.getElementById('lic-modal-err');
  errEl.style.display = 'none';
  if (!key || !key.startsWith('CALP-')) { errEl.textContent = 'Format invalide (CALP-XXXX-XXXX-XXXX)'; errEl.style.display = 'block'; return; }
  const btn = document.querySelector('#license-modal .btn-vio');
  btn.disabled = true; btn.innerHTML = '<span class="spinner"></span>';
  const r = await fetch('/api/v1/license/activate', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({license_key: key})
  });
  const d = await r.json();
  if (!r.ok) {
    errEl.textContent = d.error || 'Erreur activation';
    errEl.style.display = 'block';
    btn.disabled = false; btn.innerHTML = '<i class="ti ti-check"></i>ACTIVER';
    return;
  }
  document.getElementById('license-modal').style.display = 'none';
  notify(`Licence ${(d.edition||'').toUpperCase()} activée ✓`, 'ok');
}

function _startDashRefresh() {
  if (_dashRefreshInterval) clearInterval(_dashRefreshInterval);
  const interval = parseInt(localStorage.getItem('caleope-refresh-interval') || '30');
  if (!interval) return;
  _dashRefreshInterval = setInterval(async () => {
    if (S.section === 'dashboard') loadDashboard();
    else if (S.section === 'stats') loadStats();
    else {
      const r = await api.get('/api/v1/stats').catch(() => null);
      if (r?.data) { Object.assign(S.stats, r.data); updateTbSysbar(); }
    }
    pollEventsBadge();
  }, interval * 1000);
}

function setRefreshInterval(seconds) {
  try { localStorage.setItem('caleope-refresh-interval', String(seconds)); } catch(e) {}
  _startDashRefresh();
  loadSettings();
  notify(seconds ? `Actualisation : ${seconds < 60 ? seconds+'s' : seconds/60+'m'}` : 'Actualisation désactivée', 'ok');
}

function refreshSection() {
  const btn = document.getElementById('refresh-btn');
  if (btn) { btn.classList.add('spinning'); setTimeout(() => btn.classList.remove('spinning'), 800); }
  if (SECTIONS[S.section]?.load) SECTIONS[S.section].load();
}

// ── Init ──────────────────────────────────────────────────────────────────────
async function init() {
  loadSavedTheme();
  loadSavedSidebar();
  checkOidcConfig();
  const ok = await checkAuth();
  if (ok) { showApp(); }
  else    { showLogin(); }

  // Ctrl+K / Cmd+K → quick nav (enregistré une seule fois au démarrage)
  let _gChordActive = false;
  let _gChordTimer = null;
  document.addEventListener('keydown', e => {
    const tag = document.activeElement?.tagName?.toLowerCase();
    const inInput = tag === 'input' || tag === 'textarea' || tag === 'select' || document.activeElement?.isContentEditable;

    // Ctrl+K: quick nav
    if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
      e.preventDefault();
      const overlay = document.getElementById('quicknav-overlay');
      if (overlay && overlay.style.display !== 'none') closeQuickNav();
      else openQuickNav();
      return;
    }

    if (inInput) return;

    // Escape: close modals / return to dashboard
    if (e.key === 'Escape') {
      if (document.getElementById('help-overlay')?.style.display !== 'none') { closeHelp(); return; }
      if (document.getElementById('quicknav-overlay')?.style.display !== 'none') { closeQuickNav(); return; }
      const openModal = document.querySelector('.modal-overlay.open');
      if (openModal) { openModal.classList.remove('open'); return; }
      if (S.section !== 'dashboard') { goSection('dashboard'); }
      return;
    }

    // ?: help modal
    if (e.key === '?') { openHelp(); return; }

    // R: refresh current section
    if (e.key === 'r' || e.key === 'R') { refreshSection(); return; }

    // G+x chords for section navigation
    if (e.key === 'g' || e.key === 'G') {
      _gChordActive = true;
      clearTimeout(_gChordTimer);
      _gChordTimer = setTimeout(() => { _gChordActive = false; }, 1200);
      return;
    }
    if (_gChordActive) {
      _gChordActive = false;
      clearTimeout(_gChordTimer);
      const CHORD_MAP = { d: 'dashboard', a: 'apps', l: 'logs', b: 'backups', s: 'stats', t: 'terminal', n: 'network', j: 'journal', e: 'events', k: 'secrets', v: 'services', x: 'storage', p: 'settings', u: 'audit', f: 'locations', m: 'tasks' };
      const dest = CHORD_MAP[e.key.toLowerCase()];
      if (dest) { e.preventDefault(); goSection(dest); }
    }
  });

  // Login form handler
  document.getElementById('login-form')?.addEventListener('submit', async e => {
    e.preventDefault();
    const pw  = document.getElementById('login-pw').value;
    const btn = document.getElementById('login-btn');
    const err = document.getElementById('login-error');

    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span>&nbsp;CONNEXION...';
    err.classList.remove('show');

    const res = await login(pw);
    if (res.ok) {
      showApp();
    } else if (res.totp_required) {
      // Basculer vers l'étape TOTP
      document.getElementById('login-step-pw').style.display = 'none';
      document.getElementById('login-step-totp').style.display = '';
      document.getElementById('login-totp').focus();
      btn.disabled = false;
      btn.innerHTML = '<i class="ti ti-arrow-right"></i>CONNECTER';
    } else {
      err.textContent = 'MOT DE PASSE INCORRECT';
      err.classList.add('show');
      btn.disabled = false;
      btn.innerHTML = '<i class="ti ti-arrow-right"></i>CONNECTER';
    }
  });
}

// ── TOTP login ────────────────────────────────────────────────────────────────

function resetLoginStep() {
  document.getElementById('login-step-pw').style.display = '';
  document.getElementById('login-step-totp').style.display = 'none';
  document.getElementById('login-totp').value = '';
  document.getElementById('login-totp-error').classList.remove('show');
  document.getElementById('login-pw').focus();
}

async function submitTOTP() {
  const code = document.getElementById('login-totp').value.trim();
  const btn  = document.getElementById('login-totp-btn');
  const err  = document.getElementById('login-totp-error');
  if (!code || code.length !== 6) {
    err.textContent = 'Entrez un code à 6 chiffres';
    err.classList.add('show');
    return;
  }
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span>&nbsp;VÉRIFICATION...';
  err.classList.remove('show');

  try {
    const r = await fetch('/auth/totp', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    });
    const d = await r.json().catch(() => ({}));
    if (r.ok && d.status === 'ok') {
      showApp();
    } else {
      err.textContent = d.error || 'CODE TOTP INVALIDE';
      err.classList.add('show');
      document.getElementById('login-totp').value = '';
      document.getElementById('login-totp').focus();
    }
  } catch(e) {
    err.textContent = 'ERREUR RÉSEAU';
    err.classList.add('show');
  }
  btn.disabled = false;
  btn.innerHTML = '<i class="ti ti-shield-check"></i>VÉRIFIER';
}

// ══════════════════════════════════════════════════════════════════════════════
// TERMINAL
// ══════════════════════════════════════════════════════════════════════════════

let _term = null, _termWs = null, _termFitted = false;

function loadTerminal() {
  const container = document.getElementById('xterm-container');
  if (!container) return;

  // Si le terminal existe déjà et la connexion est ouverte, rien à faire
  if (_term && _termWs && _termWs.readyState === WebSocket.OPEN) return;

  // Nettoyer une ancienne instance
  if (_term) { _term.dispose(); _term = null; }
  if (_termWs) { _termWs.close(); _termWs = null; }
  container.innerHTML = '';

  _term = new Terminal({
    cursorBlink:    true,
    fontSize:       13,
    fontFamily:     "'JetBrains Mono', 'Fira Code', 'Courier New', monospace",
    theme: {
      background:   '#0a0a0f',
      foreground:   '#e0e0e0',
      cursor:       '#00D4FF',
      selectionBackground: 'rgba(0,212,255,0.25)',
      black:   '#000000', brightBlack:   '#555555',
      red:     '#ff5555', brightRed:     '#ff7777',
      green:   '#50fa7b', brightGreen:   '#69ff94',
      yellow:  '#f1fa8c', brightYellow:  '#ffffa5',
      blue:    '#6272a4', brightBlue:    '#8be9fd',
      magenta: '#ff79c6', brightMagenta: '#ff92df',
      cyan:    '#8be9fd', brightCyan:    '#a4ffff',
      white:   '#bfbfbf', brightWhite:   '#ffffff',
    },
  });

  _term.open(container);
  fitTerminal();

  // WebSocket — même host, port 8766
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  _termWs = new WebSocket(`${proto}//${location.host}/ws/terminal`);
  _termWs.binaryType = 'arraybuffer';

  _termWs.onopen = () => {
    fitTerminal();
  };
  _termWs.onmessage = e => {
    if (e.data instanceof ArrayBuffer) {
      _term.write(new Uint8Array(e.data));
    } else {
      _term.write(e.data);
    }
  };
  _termWs.onclose = () => {
    _term?.write('\r\n\x1b[33m[connexion terminée]\x1b[0m\r\n');
  };
  _termWs.onerror = () => {
    _term?.write('\r\n\x1b[31m[erreur WebSocket]\x1b[0m\r\n');
  };

  _term.onData(data => {
    if (_termWs?.readyState === WebSocket.OPEN) _termWs.send(data);
  });

  _term.onResize(({ cols, rows }) => {
    if (_termWs?.readyState === WebSocket.OPEN) {
      _termWs.send(JSON.stringify({ r: { c: cols, r: rows } }));
    }
  });

  // Redimensionner quand la fenêtre change
  window.addEventListener('resize', fitTerminal);
}

function fitTerminal() {
  if (!_term) return;
  const container = document.getElementById('xterm-container');
  if (!container) return;

  // Calculer cols/rows à partir des dimensions du container
  const wrap = container.closest('.term-wrap');
  if (!wrap) return;
  const w = wrap.clientWidth  - 24; // padding 12px de chaque côté
  const h = wrap.clientHeight - 24;

  // Dimensions approximatives d'une cellule à fontSize=13 (monospace)
  const cellW = 7.8;
  const cellH = 17;

  const cols = Math.max(80, Math.floor(w / cellW));
  const rows = Math.max(24, Math.floor(h / cellH));

  _term.resize(cols, rows);
  if (_termWs?.readyState === WebSocket.OPEN) {
    _termWs.send(JSON.stringify({ r: { c: cols, r: rows } }));
  }
}

// ══════════════════════════════════════════════════════════════════════════════
// SERVICES
// ══════════════════════════════════════════════════════════════════════════════

async function loadServices() {
  const el = document.getElementById('content-services');
  if (!el) return;
  el.innerHTML = '<div class="loading-msg"><span class="spinner"></span>Chargement des services...</div>';

  const data = await fetch('/sys/services').then(r => r.json()).catch(() => null);
  if (!data?.services) { el.innerHTML = '<div class="empty-msg">Impossible de charger les services</div>'; return; }

  const stateColor = s => s === 'active' ? 'var(--ok)' : s === 'failed' ? 'var(--err)' : s === 'inactive' ? 'var(--text3)' : 'var(--warn)';
  const stateLabel = s => s === 'active' ? 'ACTIF' : s === 'failed' ? 'ERREUR' : s === 'inactive' ? 'INACTIF' : s?.toUpperCase() || '—';

  const rows = data.services.map(s => `
    <div class="svc-row" data-svc-state="${escapeHtml(s.active || '')}">
      <div class="svc-dot" style="background:${stateColor(s.active)}"></div>
      <div class="svc-info">
        <div class="svc-name">${escapeHtml(s.name)}</div>
        <div class="svc-desc">${escapeHtml(s.description || '—')}</div>
      </div>
      <div class="svc-badges">
        <span class="badge" style="color:${stateColor(s.active)};border-color:${stateColor(s.active)}20">${stateLabel(s.active)}</span>
        ${s.sub && s.sub !== s.active ? `<span class="badge" style="color:var(--text3)">${escapeHtml(s.sub)}</span>` : ''}
      </div>
      <div class="svc-actions">
        <button class="btn-sm" title="Démarrer"  onclick="svcAction('${s.name}','start')"><i class="ti ti-player-play"></i></button>
        <button class="btn-sm" title="Arrêter"   onclick="svcAction('${s.name}','stop')"><i class="ti ti-player-stop"></i></button>
        <button class="btn-sm" title="Redémarrer" onclick="svcAction('${s.name}','restart')"><i class="ti ti-refresh"></i></button>
      </div>
    </div>`).join('');

  const counts = { all: data.services.length, active: 0, failed: 0, inactive: 0 };
  data.services.forEach(s => {
    if (s.active === 'active') counts.active++;
    else if (s.active === 'failed') counts.failed++;
    else counts.inactive++;
  });

  el.innerHTML = `
    <div style="display:flex;gap:4px;flex-wrap:wrap;margin-bottom:10px">
      <button class="tab-btn active" onclick="filterServices(this,'all')" data-svc-filter="all">
        TOUS <span style="margin-left:3px;font-size:8px;color:var(--text3)">${counts.all}</span>
      </button>
      <button class="tab-btn" onclick="filterServices(this,'active')" data-svc-filter="active" style="color:var(--ok)">
        ACTIFS <span style="margin-left:3px;font-size:8px;color:var(--text3)">${counts.active}</span>
      </button>
      ${counts.failed ? `<button class="tab-btn" onclick="filterServices(this,'failed')" data-svc-filter="failed" style="color:var(--red-b)">
        ERREUR <span style="margin-left:3px;font-size:8px;color:var(--text3)">${counts.failed}</span>
      </button>` : ''}
      <button class="tab-btn" onclick="filterServices(this,'inactive')" data-svc-filter="inactive" style="color:var(--text3)">
        INACTIFS <span style="margin-left:3px;font-size:8px;color:var(--text3)">${counts.inactive}</span>
      </button>
    </div>
    <div class="svc-list" id="svc-list">${rows}</div>`;
}

function filterServices(btn, state) {
  document.querySelectorAll('[data-svc-filter]').forEach(b => b.classList.toggle('active', b === btn));
  document.querySelectorAll('.svc-row').forEach(row => {
    const rowState = row.dataset.svcState || '';
    row.style.display = (state === 'all' || rowState === state) ? '' : 'none';
  });
}

async function svcAction(name, action) {
  const r = await fetch(`/sys/services/${name}/${action}`, { method: 'POST' }).then(r => r.json()).catch(() => null);
  if (r?.status === 'ok') {
    notify(`${name} : ${action}`, 'ok');
    setTimeout(loadServices, 1200);
  } else {
    notify(`Erreur : ${r?.error || 'inconnue'}`, 'err');
  }
}

// ══════════════════════════════════════════════════════════════════════════════
// RÉSEAU
// ══════════════════════════════════════════════════════════════════════════════

async function loadNetwork() {
  const el = document.getElementById('content-network');
  if (!el) return;
  el.innerHTML = '<div class="loading-msg"><span class="spinner"></span>Chargement du réseau...</div>';

  const data = await fetch('/sys/network').then(r => r.json()).catch(() => null);
  if (!data?.interfaces) { el.innerHTML = '<div class="empty-msg">Impossible de charger les infos réseau</div>'; return; }

  // Le serveur filtre déjà les interfaces virtuelles, on affiche directement
  const ifaces = Array.isArray(data.interfaces) ? data.interfaces : [];
  const ifaceCards = ifaces.map(i => {
    const addrs = (i.addr_info || []).map(a =>
      `<div class="net-addr"><span class="net-ip">${escapeHtml(a.local)}</span><span class="net-prefix">/${a.prefixlen}</span><span class="net-scope">${escapeHtml(a.scope || '')}</span></div>`
    ).join('');
    const up = (i.flags || []).includes('UP');
    const speed = i.linkinfo?.info_data?.speed ? `${i.linkinfo.info_data.speed} Mb/s` : '';
    return `<div class="net-card">
      <div class="net-header">
        <div class="svc-dot" style="background:${up ? 'var(--ok)' : 'var(--text3)'}"></div>
        <div class="net-ifname">${escapeHtml(i.ifname)}</div>
        <div class="net-mac">${escapeHtml(i.address || '')}${speed ? `<span style="margin-left:8px;opacity:.6">${speed}</span>` : ''}</div>
        <span class="badge" style="color:${up ? 'var(--ok)' : 'var(--text3)'}">${up ? 'UP' : 'DOWN'}</span>
      </div>
      ${addrs || '<div class="net-addr" style="color:var(--text3)">aucune adresse</div>'}
    </div>`;
  }).join('');

  const routes = Array.isArray(data.routes) ? data.routes : [];
  const routeRows = routes.map(r =>
    `<tr><td>${escapeHtml(r.dst || 'default')}</td><td>${escapeHtml(r.gateway || '—')}</td><td>${escapeHtml(r.dev || '—')}</td><td>${escapeHtml(String(r.metric ?? ''))}</td></tr>`
  ).join('');

  // Ports exposés par les apps installées (depuis docker ps -a)
  let appPortsHtml = '';
  if (S.apps.length) {
    const portsData = await fetch('/sys/ports').then(r => r.json()).catch(() => null);
    if (portsData?.ports?.length) {
      const portRows = portsData.ports.map(p => `
        <tr>
          <td>${escapeHtml(p.container || '—')}</td>
          <td>${escapeHtml(p.host_port || '—')}</td>
          <td>${escapeHtml(p.container_port || '—')}</td>
          <td><span style="color:${p.state === 'running' ? 'var(--green-b)' : 'var(--text3)'}">${escapeHtml(p.state || '—')}</span></td>
        </tr>`).join('');
      appPortsHtml = `<div class="net-section-title" style="margin-top:24px">// PORTS APPS</div>
      <div class="table-wrap">
        <table class="sys-table"><thead><tr><th>CONTENEUR</th><th>PORT HÔTE</th><th>PORT CONTAINER</th><th>ÉTAT</th></tr></thead>
        <tbody>${portRows}</tbody></table>
      </div>`;
    } else {
      // Fallback: afficher les domaines apps depuis S.apps
      const appRows = S.apps.filter(a => a.domain).map(a => `
        <tr>
          <td>${escapeHtml(a.name || a.id)}</td>
          <td>${escapeHtml(a.domain || '—')}</td>
          <td><span style="color:${a.status==='running'?'var(--green-b)':'var(--text3)'}">${a.status?.toUpperCase()}</span></td>
        </tr>`).join('');
      if (appRows) {
        appPortsHtml = `<div class="net-section-title" style="margin-top:24px">// SERVICES EXPOSÉS</div>
        <div class="table-wrap">
          <table class="sys-table"><thead><tr><th>APP</th><th>DOMAINE</th><th>ÉTAT</th></tr></thead>
          <tbody>${appRows}</tbody></table>
        </div>`;
      }
    }
  }

  // IP publique (tentative non-bloquante)
  let publicIp = null;
  try {
    const ipResp = await Promise.race([
      fetch('https://api.ipify.org?format=json').then(r => r.json()),
      new Promise((_, reject) => setTimeout(reject, 3000)),
    ]);
    publicIp = ipResp?.ip || null;
  } catch(e) {}

  el.innerHTML = `
    ${publicIp ? `<div class="settings-card" style="margin-bottom:16px;padding:10px 14px">
      <div style="display:flex;align-items:center;gap:10px">
        <i class="ti ti-world" style="color:var(--accent);font-size:16px;flex-shrink:0"></i>
        <div style="flex:1">
          <div style="font-size:8px;color:var(--text3);letter-spacing:1px">ADRESSE IP PUBLIQUE</div>
          <div style="font-size:13px;font-weight:700;font-family:monospace;color:var(--text1)">${escapeHtml(publicIp)}</div>
        </div>
        <button class="btn-sm" onclick="navigator.clipboard.writeText('${escapeHtml(publicIp)}').then(()=>notify('IP copiée','ok'))" title="Copier">
          <i class="ti ti-copy" style="font-size:10px"></i>
        </button>
      </div>
    </div>` : ''}
    <div class="net-section-title">// INTERFACES PHYSIQUES</div>
    <div class="net-grid">${ifaceCards || '<div class="empty-msg">Aucune interface physique détectée</div>'}</div>
    ${routeRows ? `<div class="net-section-title" style="margin-top:24px">// ROUTES</div>
    <div class="table-wrap">
      <table class="sys-table"><thead><tr><th>DESTINATION</th><th>PASSERELLE</th><th>INTERFACE</th><th>MÉTRIQUE</th></tr></thead>
      <tbody>${routeRows}</tbody></table>
    </div>` : ''}
    ${appPortsHtml}
    <div id="net-bandwidth-widget" style="margin-top:24px"></div>
    <div id="net-firewall-widget" style="margin-top:24px"></div>
    <div style="margin-top:24px">
      <div class="net-section-title">// OUTILS RÉSEAU</div>
      <div class="settings-card" style="padding:12px">
        <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:10px">
          <input id="net-tool-host" class="param-input" placeholder="hôte / IP (ex: 1.1.1.1)"
            style="flex:1;min-width:160px;max-width:240px"
            onkeydown="if(event.key==='Enter')runNetTool()">
          <select id="net-tool-type" class="log-select" style="width:auto">
            <option value="ping">PING</option>
            <option value="dns">DNS LOOKUP</option>
            <option value="trace">TRACEROUTE</option>
          </select>
          <button class="btn" onclick="runNetTool()"><i class="ti ti-terminal"></i>EXÉCUTER</button>
          <button class="btn-sm" onclick="clearNetTool()"><i class="ti ti-eraser"></i></button>
        </div>
        <pre id="net-tool-output" style="font-family:monospace;font-size:9px;color:var(--text2);background:var(--bg1);
          padding:8px;border-radius:4px;border:1px solid var(--border);min-height:40px;max-height:200px;
          overflow-y:auto;white-space:pre-wrap;display:none"></pre>
      </div>
    </div>`;

  loadNetBandwidthWidget();
  loadNetFirewallWidget();
}

async function runNetTool() {
  const host = document.getElementById('net-tool-host')?.value?.trim();
  const type = document.getElementById('net-tool-type')?.value || 'ping';
  const out  = document.getElementById('net-tool-output');
  if (!out) return;
  if (!host) { notify('Saisissez un hôte', 'err'); return; }
  out.style.display = 'block';
  out.textContent = `Exécution de ${type} vers ${host}…`;
  try {
    const r = await fetch(`/sys/nettool?type=${encodeURIComponent(type)}&host=${encodeURIComponent(host)}`);
    const d = await r.json();
    out.textContent = d.output || d.error || 'Aucun résultat';
  } catch(e) {
    out.textContent = `Erreur : ${e.message}`;
  }
}

function clearNetTool() {
  const out = document.getElementById('net-tool-output');
  if (out) { out.textContent = ''; out.style.display = 'none'; }
  const inp = document.getElementById('net-tool-host');
  if (inp) inp.value = '';
}

async function loadNetBandwidthWidget() {
  const w = document.getElementById('net-bandwidth-widget');
  if (!w) return;

  // Deux lectures espacées de 1s pour calculer le débit
  let s1 = null, s2 = null;
  try {
    const r1 = await fetch('/sys/netstat');
    if (r1.ok) s1 = await r1.json();
    await new Promise(res => setTimeout(res, 1000));
    const r2 = await fetch('/sys/netstat');
    if (r2.ok) s2 = await r2.json();
  } catch(e) {}

  if (!s1?.ifaces?.length || !s2?.ifaces?.length) return;

  const rates = s1.ifaces.map(i1 => {
    const i2 = s2.ifaces.find(i => i.name === i1.name);
    if (!i2) return null;
    const rxBps = Math.max(0, i2.rx_bytes - i1.rx_bytes);
    const txBps = Math.max(0, i2.tx_bytes - i1.tx_bytes);
    return { name: i1.name, rxBps, txBps, rxTotal: i2.rx_bytes, txTotal: i2.tx_bytes };
  }).filter(Boolean);

  const fmtBytes = b => {
    if (b >= 1073741824) return (b / 1073741824).toFixed(2) + ' Go/s';
    if (b >= 1048576)    return (b / 1048576).toFixed(1) + ' Mo/s';
    if (b >= 1024)       return (b / 1024).toFixed(0) + ' Ko/s';
    return b + ' o/s';
  };
  const fmtTotal = b => {
    if (b >= 1073741824) return (b / 1073741824).toFixed(1) + ' Go';
    if (b >= 1048576)    return (b / 1048576).toFixed(0) + ' Mo';
    if (b >= 1024)       return (b / 1024).toFixed(0) + ' Ko';
    return b + ' o';
  };

  w.innerHTML = `
    <div class="net-section-title">// BANDE PASSANTE (1s)</div>
    <div style="display:flex;flex-direction:column;gap:6px">
      ${rates.map(r => `
        <div class="settings-card" style="padding:8px 12px">
          <div style="font-size:9px;font-weight:700;color:var(--text3);margin-bottom:6px">${escapeHtml(r.name)}</div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
            <div>
              <div style="font-size:8px;color:var(--text3)">↓ RX</div>
              <div style="font-size:11px;font-weight:700;color:var(--green-b);font-family:monospace">${fmtBytes(r.rxBps)}</div>
              <div style="font-size:8px;color:var(--text3)">${fmtTotal(r.rxTotal)} total</div>
            </div>
            <div>
              <div style="font-size:8px;color:var(--text3)">↑ TX</div>
              <div style="font-size:11px;font-weight:700;color:var(--accent);font-family:monospace">${fmtBytes(r.txBps)}</div>
              <div style="font-size:8px;color:var(--text3)">${fmtTotal(r.txTotal)} total</div>
            </div>
          </div>
        </div>`).join('')}
    </div>`;
}

async function loadNetFirewallWidget() {
  const w = document.getElementById('net-firewall-widget');
  if (!w) return;

  let data = null;
  try {
    const r = await fetch('/sys/firewall');
    if (r.ok) data = await r.json();
  } catch(e) {}

  if (!data?.rules?.length) { w.innerHTML = ''; return; }

  const actionColor = a => {
    const up = a.toUpperCase();
    if (up.includes('ALLOW')) return 'var(--green-b)';
    if (up.includes('DENY') || up.includes('REJECT')) return 'var(--red-b)';
    if (up.includes('LIMIT')) return 'var(--warn)';
    return 'var(--text2)';
  };

  w.innerHTML = `
    <div class="net-section-title">// PARE-FEU (UFW) — ${escapeHtml(data.status || 'inconnu')}</div>
    <div class="settings-card" style="padding:0">
      <table style="width:100%;border-collapse:collapse;font-size:9px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="padding:5px 12px;text-align:left">PORT / SERVICE</th>
          <th style="padding:5px 8px;text-align:left">ACTION</th>
          <th style="padding:5px 8px;text-align:left">SOURCE</th>
          <th style="padding:5px 12px 5px 8px;text-align:left">NOTE</th>
        </tr></thead>
        <tbody>${data.rules.map(r => `
          <tr style="border-bottom:1px solid var(--border)">
            <td style="padding:4px 12px;font-family:monospace;color:var(--text1)">${escapeHtml(r.to || '—')}</td>
            <td style="padding:4px 8px"><span style="font-size:8px;font-weight:700;color:${actionColor(r.action)}">${escapeHtml(r.action || '—')}</span></td>
            <td style="padding:4px 8px;color:var(--text2);font-family:monospace">${escapeHtml(r.from || 'Anywhere')}</td>
            <td style="padding:4px 12px 4px 8px;color:var(--text3);font-style:italic">${escapeHtml(r.note || '')}</td>
          </tr>`).join('')}
        </tbody>
      </table>
    </div>`;
}

// ══════════════════════════════════════════════════════════════════════════════
// STOCKAGE
// ══════════════════════════════════════════════════════════════════════════════

async function loadStorage() {
  const el = document.getElementById('content-storage');
  if (!el) return;
  el.innerHTML = '<div class="loading-msg"><span class="spinner"></span>Chargement du stockage...</div>';

  const data = await fetch('/sys/storage').then(r => r.json()).catch(() => null);
  if (!data?.disks) { el.innerHTML = '<div class="empty-msg">Impossible de charger le stockage</div>'; return; }

  const diskRows = (data.disks || []).map(d => {
    const pct = parseInt(d.use_pct) || 0;
    const barColor = pct > 90 ? 'var(--err)' : pct > 75 ? 'var(--warn)' : 'var(--ok)';
    return `<div class="disk-row">
      <div class="disk-info">
        <div class="disk-target">${escapeHtml(d.target)}</div>
        <div class="disk-source">${escapeHtml(d.source)}</div>
      </div>
      <div class="disk-bar-wrap">
        <div class="disk-bar"><div class="disk-bar-fill" style="width:${pct}%;background:${barColor}"></div></div>
        <div class="disk-stats">
          <span style="color:var(--text1);font-weight:600">${escapeHtml(d.avail)} libre</span>
          <span style="color:var(--text3)"> · ${escapeHtml(d.used)} utilisé / ${escapeHtml(d.size)}</span>
          <span style="color:${barColor};margin-left:6px">${escapeHtml(d.use_pct)}</span>
        </div>
      </div>
    </div>`;
  }).join('');

  // Disques disponibles pour montage
  const available = data.available || [];
  const availCards = available.map(d =>
    `<div class="disk-avail-row">
      <div class="disk-info">
        <div class="disk-target">${escapeHtml(d.model || d.name)}</div>
        <div class="disk-source">${escapeHtml(d.path)} · ${escapeHtml(d.size)} · ${escapeHtml(d.fstype || 'inconnu')}</div>
      </div>
      <button class="btn-sm" onclick="openDiskMountModal('${escapeHtml(d.path)}','${escapeHtml(d.model||d.name)}','${escapeHtml(d.size)}')">
        <i class="ti ti-plus"></i>MONTER
      </button>
    </div>`
  ).join('');

  el.innerHTML = `
    <div class="storage-header">
      <div class="net-section-title" style="margin:0">// SYSTÈMES DE FICHIERS</div>
      <button class="btn-sm" onclick="openDiskMountModal()"><i class="ti ti-plus"></i>AJOUTER UN DISQUE</button>
    </div>
    <div class="disk-list">${diskRows || '<div class="empty-msg">Aucune partition montée</div>'}</div>
    ${availCards ? `
    <div class="net-section-title" style="margin-top:24px">// DISQUES DISPONIBLES</div>
    <div class="disk-list">${availCards}</div>` : ''}
    <div class="net-section-title" style="margin-top:24px">// DOCKER</div>
    <div class="settings-card" style="padding:10px 14px">
      <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
        <div>
          <div style="font-size:9px;font-weight:700;color:var(--text1)">NETTOYAGE DOCKER</div>
          <div style="font-size:8px;color:var(--text3);margin-top:2px">Supprimer les images, volumes et réseaux non utilisés</div>
        </div>
        <button class="btn" id="docker-prune-btn" onclick="runDockerPrune()" style="font-size:9px;flex-shrink:0">
          <i class="ti ti-trash"></i> PRUNE
        </button>
      </div>
      <div id="docker-prune-result" style="display:none;margin-top:10px;font-size:9px;font-family:monospace;color:var(--text2);background:var(--bg1);padding:8px;border-radius:4px;max-height:120px;overflow-y:auto;white-space:pre-wrap"></div>
    </div>
    <div id="docker-volumes-widget" style="margin-top:16px"></div>
    <div class="net-section-title" style="margin-top:24px">// DONNÉES PAR APPLICATION</div>
    <div id="app-sizes-widget"><div class="dash-loading"><span class="spinner"></span> CALCUL EN COURS...</div></div>`;
  loadDockerVolumesWidget();
  loadAppSizesWidget();
}

async function loadAppSizesWidget() {
  const w = document.getElementById('app-sizes-widget');
  if (!w) return;
  let sizes = null;
  try {
    const r = await fetch('/sys/app-sizes');
    if (r.ok) { const d = await r.json(); sizes = d.sizes; }
  } catch(e) {}

  if (!sizes || !sizes.length) {
    w.innerHTML = `<div style="font-size:9px;color:var(--text3);padding:8px 0">Aucune donnée d'application trouvée.</div>`;
    return;
  }

  const totalBytes = sizes.reduce((s, x) => s + x.bytes, 0);
  const rows = sizes.map(s => {
    const pct = totalBytes > 0 ? Math.round(s.bytes / totalBytes * 100) : 0;
    const barColor = pct > 40 ? 'var(--warn)' : 'var(--accent)';
    return `<div style="padding:6px 0;border-bottom:1px solid var(--border)">
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:3px">
        <span style="font-size:10px">${icon(s.app_id)}</span>
        <span style="font-size:9px;font-weight:600;flex:1">${escapeHtml(s.app_id)}</span>
        <span style="font-size:9px;font-family:monospace;color:var(--text2)">${escapeHtml(s.size_str)}</span>
        <span style="font-size:8px;color:var(--text3);min-width:28px;text-align:right">${pct}%</span>
      </div>
      <div style="height:3px;background:var(--border);border-radius:2px">
        <div style="height:100%;width:${pct}%;background:${barColor};border-radius:2px;transition:width .3s"></div>
      </div>
    </div>`;
  }).join('');

  const totalStr = sizes[0] ? sizes.reduce((s, x) => s + x.bytes, 0) : 0;
  const fmtTotal = totalStr > 1073741824 ? (totalStr / 1073741824).toFixed(1) + ' GiB'
                 : totalStr > 1048576   ? (totalStr / 1048576).toFixed(0) + ' MiB'
                 : (totalStr / 1024).toFixed(0) + ' KiB';

  w.innerHTML = `
    <div style="background:var(--card);border:1px solid var(--border);border-radius:6px;padding:12px 14px">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
        <span style="font-size:9px;color:var(--text3)">${sizes.length} app(s) · Total : ${fmtTotal}</span>
        <button class="btn-sm" style="font-size:8px" onclick="loadAppSizesWidget()">
          <i class="ti ti-refresh" style="font-size:9px"></i>
        </button>
      </div>
      ${rows}
    </div>`;
}

async function runDockerPrune() {
  const btn = document.getElementById('docker-prune-btn');
  const res = document.getElementById('docker-prune-result');
  if (!btn) return;
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> EN COURS...';
  try {
    const r = await fetch('/sys/docker-prune', { method: 'POST' });
    const d = await r.json();
    if (res) {
      res.style.display = 'block';
      res.textContent = [
        d.images ? '▸ Images:\n' + d.images.trim() : '',
        d.volumes ? '▸ Volumes:\n' + d.volumes.trim() : '',
        d.networks ? '▸ Réseaux:\n' + d.networks.trim() : '',
      ].filter(Boolean).join('\n\n') || 'Rien à supprimer.';
    }
    notify('Nettoyage Docker terminé', 'ok');
  } catch(e) {
    notify('Erreur Docker prune', 'err');
  } finally {
    btn.disabled = false;
    btn.innerHTML = '<i class="ti ti-trash"></i> PRUNE';
  }
}

async function loadDockerVolumesWidget() {
  const w = document.getElementById('docker-volumes-widget');
  if (!w) return;
  let data = null;
  try {
    const r = await fetch('/sys/docker-volumes');
    if (r.ok) data = await r.json();
  } catch(e) {}
  if (!data?.volumes?.length) { w.innerHTML = ''; return; }
  w.innerHTML = `
    <div class="net-section-title">// VOLUMES DOCKER (${data.volumes.length})</div>
    <div class="settings-card" style="padding:0">
      <table style="width:100%;border-collapse:collapse;font-size:8px">
        <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
          <th style="padding:4px 12px;text-align:left">NOM</th>
          <th style="padding:4px 8px;text-align:left">DRIVER</th>
          <th style="padding:4px 8px;text-align:left">SCOPE</th>
          <th style="padding:4px 12px 4px 8px;text-align:left">POINT DE MONTAGE</th>
        </tr></thead>
        <tbody>${data.volumes.map(v => `
          <tr style="border-bottom:1px solid var(--border)">
            <td style="padding:4px 12px;font-family:monospace;color:var(--text1);word-break:break-all">${escapeHtml(v.name)}</td>
            <td style="padding:4px 8px;color:var(--text2)">${escapeHtml(v.driver || '—')}</td>
            <td style="padding:4px 8px;color:var(--text3)">${escapeHtml(v.scope || '—')}</td>
            <td style="padding:4px 12px 4px 8px;color:var(--text3);font-family:monospace;font-size:7px;word-break:break-all">${escapeHtml(v.mountpoint || '—')}</td>
          </tr>`).join('')}
        </tbody>
      </table>
    </div>`;
}

function openDiskMountModal(device, label, size) {
  document.getElementById('disk-mount-device').value = device || '';
  document.getElementById('disk-mount-name').value = '';
  document.getElementById('disk-mount-label').textContent = device
    ? `${label || device} (${size || ''})`
    : 'Choisissez un périphérique dans la liste';
  document.getElementById('disk-mount-modal').classList.add('open');
  if (!device) document.getElementById('disk-mount-name').focus();
}

async function confirmDiskMount() {
  const device = document.getElementById('disk-mount-device').value.trim();
  const name   = document.getElementById('disk-mount-name').value.trim();
  if (!name) { notify('Nom requis', 'err'); return; }
  if (!device) { notify('Périphérique requis', 'err'); return; }

  const btn = document.querySelector('#disk-mount-modal .btn-vio');
  btn.disabled = true;

  // On passe par l'API locations du daemon (type=local, device=...)
  const resp = await fetch('/api/v1/locations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, type: 'local', device }),
  }).then(r => r.json()).catch(() => null);

  btn.disabled = false;
  if (!resp || resp.error) {
    notify(resp?.error || 'Erreur montage', 'err');
    return;
  }

  document.getElementById('disk-mount-modal').classList.remove('open');
  notify(`Disque "${name}" monté — disponible via --storage ${name}`, 'ok');
  loadStorage();
}

// ══════════════════════════════════════════════════════════════════════════════
// JOURNAL
// ══════════════════════════════════════════════════════════════════════════════

const JOURNAL_UNITS = [
  'all', 'caleoped', 'caleope-ui', 'docker', 'traefik', 'crowdsec',
  'ssh', 'fail2ban', 'ufw', 'kernel',
];

function initJournalSelect() {
  const sel = document.getElementById('journal-unit-select');
  if (!sel || sel.dataset.init) return;
  sel.innerHTML = JOURNAL_UNITS.map(u =>
    `<option value="${u}">${u === 'all' ? '— Toutes les unités —' : u}</option>`
  ).join('');
  sel.dataset.init = '1';
}

async function loadJournal() {
  initJournalSelect();
  const el = document.getElementById('journal-body');
  if (!el) return;
  el.innerHTML = '<div class="loading-msg"><span class="spinner"></span></div>';

  const unit = document.getElementById('journal-unit-select')?.value || 'all';
  const url  = `/sys/journal?n=300${unit !== 'all' ? '&unit=' + encodeURIComponent(unit) : ''}`;
  const data = await fetch(url).then(r => r.json()).catch(() => null);
  if (!data?.entries) { el.innerHTML = '<div class="empty-msg">Journal non disponible</div>'; return; }

  const priColor = p => ({ err:'var(--err)', warning:'var(--warn)', crit:'var(--err)', alert:'var(--err)', emerg:'var(--err)', debug:'var(--text3)' }[p] || 'var(--text2)');

  window._journalEntries = data.entries;
  renderJournalLines();
}

function _journalLineHtml(e) {
  const priColor = p => ({ err:'var(--err)', warning:'var(--warn)', crit:'var(--err)', alert:'var(--err)', emerg:'var(--err)', debug:'var(--text3)' }[p] || 'var(--text2)');
  const ts = e.time ? new Date(parseInt(e.time.replace('.',''))/1000).toLocaleTimeString('fr-FR') : '';
  return `<div class="log-line" data-pri="${escapeHtml(e.priority || '')}">
    <span class="log-ts">${escapeHtml(ts)}</span>
    <span class="log-app" style="color:var(--blue)">${escapeHtml(e.unit || '—')}</span>
    <span class="log-level" style="color:${priColor(e.priority)}">${escapeHtml(e.priority || '')}</span>
    <span class="log-msg">${escapeHtml(e.message || '')}</span>
  </div>`;
}

let _journalPriFilter = '';

function renderJournalLines() {
  const el = document.getElementById('journal-body');
  if (!el) return;
  const entries = window._journalEntries || [];
  const search  = document.getElementById('journal-search')?.value?.toLowerCase() || '';
  const filtered = entries.filter(e => {
    if (_journalPriFilter && e.priority !== _journalPriFilter) return false;
    if (search && !(e.message || '').toLowerCase().includes(search) && !(e.unit || '').toLowerCase().includes(search)) return false;
    return true;
  });
  el.innerHTML = filtered.map(_journalLineHtml).join('') || '<div class="empty-msg">Aucune entrée</div>';
  el.scrollTop = el.scrollHeight;
}

function setJournalPriFilter(btn, pri) {
  _journalPriFilter = pri;
  document.querySelectorAll('#journal-pri-filters .tab-btn').forEach(b => b.classList.toggle('active', b === btn));
  renderJournalLines();
}

function filterJournalLines() {
  renderJournalLines();
}

function exportJournal() {
  const entries = window._journalEntries || [];
  const search  = document.getElementById('journal-search')?.value?.toLowerCase() || '';
  const filtered = entries.filter(e => {
    if (_journalPriFilter && e.priority !== _journalPriFilter) return false;
    if (search && !(e.message || '').toLowerCase().includes(search) && !(e.unit || '').toLowerCase().includes(search)) return false;
    return true;
  });
  const lines = filtered.map(e => {
    const ts = e.time ? new Date(parseInt(e.time.replace('.',''))/1000).toISOString() : '';
    return `${ts} [${e.priority || ''}] ${e.unit || ''}: ${e.message || ''}`;
  });
  const blob = new Blob([lines.join('\n')], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `caleope-journal-${new Date().toISOString().slice(0,10)}.log`;
  a.click();
  URL.revokeObjectURL(url);
  notify(`Journal exporté (${lines.length} lignes)`, 'ok');
}

// ── Utils ─────────────────────────────────────────────────────────────────────
function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// ══════════════════════════════════════════════════════════════════════════════
// QUICK NAV (Ctrl+K)
// ══════════════════════════════════════════════════════════════════════════════

let _qnCursor = -1;

function openQuickNav() {
  const overlay = document.getElementById('quicknav-overlay');
  if (!overlay) return;
  overlay.style.display = 'flex';
  const inp = document.getElementById('quicknav-input');
  if (inp) { inp.value = ''; inp.focus(); }
  _qnCursor = -1;
  updateQuickNav('');
}

function closeQuickNav() {
  const overlay = document.getElementById('quicknav-overlay');
  if (overlay) overlay.style.display = 'none';
  _qnCursor = -1;
}

function _buildQuickNavItems() {
  const items = [];

  // Sections principales
  const SECTION_ICONS = {
    dashboard: 'ti-layout-dashboard', apps: 'ti-layout-grid', logs: 'ti-file-text',
    backups: 'ti-device-floppy', secrets: 'ti-lock', locations: 'ti-map-pin',
    tasks: 'ti-checklist', events: 'ti-bell', audit: 'ti-shield',
    settings: 'ti-settings', stats: 'ti-cpu', terminal: 'ti-terminal-2',
    services: 'ti-server', network: 'ti-network', storage: 'ti-database',
    journal: 'ti-notebook',
  };
  Object.entries(SECTIONS).forEach(([id, sec]) => {
    items.push({
      type: 'section', id, label: sec.label,
      icon: SECTION_ICONS[id] || 'ti-chevron-right', sub: 'SECTION ' + sec.num,
    });
  });

  // Panels d'intégration installés
  Object.entries(APP_PANELS).forEach(([appId, appDef]) => {
    const installed = S.apps.find(a => a.id === appId);
    if (!installed) return;
    const emoji = APP_ICONS[appId] || '';
    appDef.panels.forEach(p => {
      items.push({
        type: 'panel', id: p.id, label: p.label,
        icon: p.icon, sub: (emoji ? emoji + ' ' : '') + (installed.name || appId).toUpperCase(),
        appId,
      });
    });
  });

  // Apps installées avec URL externe (ouverture directe)
  S.apps.filter(a => a.domain && a.status === 'running').forEach(a => {
    const emoji = APP_ICONS[a.id] || '';
    items.push({
      type: 'app-url', id: a.id, label: 'Ouvrir ' + (a.name || a.id),
      icon: 'ti-external-link',
      sub: (emoji ? emoji + ' ' : '') + (a.domain || '').toUpperCase(),
      url: 'https://' + a.domain,
    });
  });

  return items;
}

function updateQuickNav(q) {
  const list = document.getElementById('quicknav-list');
  if (!list) return;
  _qnCursor = -1;

  const items = _buildQuickNavItems();
  const query = q.trim().toLowerCase();
  const filtered = query
    ? items.filter(it => it.label.toLowerCase().includes(query) || it.sub.toLowerCase().includes(query))
    : items;

  if (!filtered.length) {
    list.innerHTML = `<div style="padding:20px;text-align:center;color:var(--text3);font-size:11px">AUCUN RÉSULTAT</div>`;
    return;
  }

  list.innerHTML = filtered.map((it, i) => `
    <div class="qn-item" data-idx="${i}" data-id="${escapeHtml(it.id)}"
      style="display:flex;align-items:center;gap:12px;padding:9px 16px;cursor:pointer;transition:background .12s"
      onmouseenter="qnHighlight(${i})" onclick="qnGo('${escapeHtml(it.id)}')">
      <i class="ti ${it.icon}" style="font-size:14px;color:var(--vio);flex-shrink:0;width:16px;text-align:center"></i>
      <span style="flex:1;font-size:12px;color:var(--text1)">${escapeHtml(it.label)}</span>
      <span style="font-size:9px;color:var(--text3);font-family:monospace">${escapeHtml(it.sub)}</span>
    </div>
  `).join('');
}

function qnHighlight(idx) {
  _qnCursor = idx;
  document.querySelectorAll('.qn-item').forEach((el, i) => {
    el.style.background = i === idx ? 'var(--bg3)' : '';
    el.style.color = i === idx ? 'var(--vio)' : '';
  });
}

function quickNavKey(e) {
  const items = document.querySelectorAll('.qn-item');
  if (e.key === 'Escape') { closeQuickNav(); return; }
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    qnHighlight(Math.min(_qnCursor + 1, items.length - 1));
    items[_qnCursor]?.scrollIntoView({ block: 'nearest' });
    return;
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault();
    qnHighlight(Math.max(_qnCursor - 1, 0));
    items[_qnCursor]?.scrollIntoView({ block: 'nearest' });
    return;
  }
  if (e.key === 'Enter') {
    e.preventDefault();
    const active = _qnCursor >= 0 ? items[_qnCursor] : items[0];
    if (active) { const id = active.dataset.id; qnGo(id); }
    return;
  }
}

function qnGo(id) {
  closeQuickNav();
  const items = _buildQuickNavItems();
  const it = items.find(x => x.id === id);
  if (it?.type === 'app-url' && it.url) {
    window.open(it.url, '_blank', 'noopener');
  } else {
    goSection(id);
  }
}

// ── Container inspect modal ───────────────────────────────────────────────────
let _inspectData = null;

async function openInspect(appId) {
  const overlay = document.getElementById('inspect-overlay');
  const title   = document.getElementById('inspect-title');
  const body    = document.getElementById('inspect-body');
  const tab     = document.getElementById('inspect-tab');
  if (!overlay) return;

  overlay.style.display = 'flex';
  title.textContent = `// INSPECT — ${appId.toUpperCase()}`;
  if (tab) tab.value = 'summary';
  body.innerHTML = `<div class="dash-loading"><span class="spinner"></span> Chargement...</div>`;
  _inspectData = null;

  try {
    const r = await fetch(`/sys/docker-inspect/${encodeURIComponent(appId)}`);
    if (r.ok) {
      const d = await r.json();
      _inspectData = d.container;
    }
  } catch(e) {}

  if (!_inspectData) {
    body.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-alert-circle"></i></div><div class="empty-title">INTROUVABLE</div><div class="empty-sub">Le conteneur ${appId} n'est pas trouvable via Docker inspect.</div></div>`;
    return;
  }

  renderInspectTab('summary');
}

function closeInspect() {
  const overlay = document.getElementById('inspect-overlay');
  if (overlay) overlay.style.display = 'none';
  _inspectData = null;
}

function switchInspectTab(tab) {
  renderInspectTab(tab);
}

function renderInspectTab(tab) {
  const body = document.getElementById('inspect-body');
  if (!body || !_inspectData) return;
  const d = _inspectData;

  if (tab === 'summary') {
    const state  = d.State || {};
    const config = d.Config || {};
    const net    = d.NetworkSettings || {};
    const created = d.Created ? new Date(d.Created).toLocaleString('fr-FR') : '—';
    const started = state.StartedAt ? new Date(state.StartedAt).toLocaleString('fr-FR') : '—';
    const upSince = state.StartedAt ? formatRelTime(new Date(Date.now() + (Date.now() - new Date(state.StartedAt).getTime()) * -1) * -1) : '—';
    const stateColor = state.Running ? 'var(--green-b)' : 'var(--red-b)';
    const stateLabel = state.Running ? 'EN COURS' : state.Status?.toUpperCase() || '—';
    const image = config.Image || '—';
    const hostname = config.Hostname || '—';
    const restartCount = d.RestartCount ?? '—';
    const networks = Object.entries(net.Networks || {}).map(([name, info]) =>
      `<div class="setting-row"><span>${escapeHtml(name)}</span><span class="setting-val" style="font-family:monospace">${escapeHtml(info.IPAddress || '—')}</span></div>`
    ).join('');

    body.innerHTML = `
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:12px">
        <span style="display:inline-block;width:10px;height:10px;border-radius:50%;background:${stateColor}"></span>
        <span style="font-size:11px;font-weight:700;color:${stateColor}">${stateLabel}</span>
        <span style="font-size:8px;color:var(--text3)">· ${escapeHtml(d.Name?.replace(/^\//,'') || '?')}</span>
      </div>
      <div class="settings-card" style="margin-bottom:8px">
        <div class="settings-title">INFOS</div>
        <div class="setting-row"><span>IMAGE</span><span class="setting-val" style="font-family:monospace;font-size:8px">${escapeHtml(image)}</span></div>
        <div class="setting-row"><span>HOSTNAME</span><span class="setting-val">${escapeHtml(hostname)}</span></div>
        <div class="setting-row"><span>CRÉÉ</span><span class="setting-val">${escapeHtml(created)}</span></div>
        <div class="setting-row"><span>DÉMARRÉ</span><span class="setting-val">${escapeHtml(started)}</span></div>
        <div class="setting-row"><span>REDÉMARRAGES</span><span class="setting-val${restartCount > 3 ? ';color:var(--warn)' : ''}">${restartCount}</span></div>
      </div>
      ${networks ? `<div class="settings-card"><div class="settings-title">RÉSEAUX</div>${networks}</div>` : ''}`;
  } else if (tab === 'env') {
    const envVars = (_inspectData.Config?.Env || []).sort();
    if (!envVars.length) { body.innerHTML = `<div class="empty-state"><div class="empty-title">AUCUNE VARIABLE</div></div>`; return; }
    body.innerHTML = `<div style="display:flex;flex-direction:column;gap:3px">
      ${envVars.map(e => {
        const [key, ...rest] = e.split('=');
        const val = rest.join('=');
        const isSensitive = /password|secret|key|token|api/i.test(key);
        return `<div style="display:flex;gap:8px;padding:4px 0;border-bottom:1px solid var(--border);align-items:baseline">
          <span style="font-family:monospace;font-size:8px;color:var(--accent);flex-shrink:0;min-width:120px;overflow:hidden;text-overflow:ellipsis">${escapeHtml(key)}</span>
          <span style="font-family:monospace;font-size:8px;color:${isSensitive?'var(--text3)':'var(--text2)'};word-break:break-all">${isSensitive?'••••••••':escapeHtml(val)}</span>
        </div>`;
      }).join('')}
    </div>`;
  } else if (tab === 'mounts') {
    const mounts = _inspectData.Mounts || [];
    if (!mounts.length) { body.innerHTML = `<div class="empty-state"><div class="empty-title">AUCUN MONTAGE</div></div>`; return; }
    body.innerHTML = `<div style="display:flex;flex-direction:column;gap:4px">
      ${mounts.map(m => `
        <div style="background:var(--bg3);border-radius:4px;padding:6px 10px;border:1px solid var(--border)">
          <div style="font-size:8px;color:var(--text3);margin-bottom:2px">${escapeHtml(m.Type || 'bind').toUpperCase()} · ${m.RW ? 'RW' : 'RO'}</div>
          <div style="font-family:monospace;font-size:8px;color:var(--text2)">
            <span style="color:var(--text3)">HÔTE:</span> ${escapeHtml(m.Source || '—')}<br>
            <span style="color:var(--text3)">CONT:</span> ${escapeHtml(m.Destination || '—')}
          </div>
        </div>`).join('')}
    </div>`;
  } else if (tab === 'ports') {
    const ports = _inspectData.NetworkSettings?.Ports || {};
    const portEntries = Object.entries(ports);
    if (!portEntries.length) { body.innerHTML = `<div class="empty-state"><div class="empty-title">AUCUN PORT EXPOSÉ</div></div>`; return; }
    body.innerHTML = `<table style="width:100%;border-collapse:collapse;font-size:9px">
      <thead><tr style="color:var(--text3);border-bottom:1px solid var(--border)">
        <th style="padding:4px 8px;text-align:left">PORT CONTAINER</th>
        <th style="padding:4px 8px;text-align:left">PORT HÔTE</th>
      </tr></thead>
      <tbody>${portEntries.map(([cPort, bindings]) => `
        <tr style="border-bottom:1px solid var(--border)">
          <td style="padding:4px 8px;font-family:monospace;color:var(--text1)">${escapeHtml(cPort)}</td>
          <td style="padding:4px 8px;font-family:monospace;color:var(--text2)">${(bindings||[]).map(b=>escapeHtml(b.HostPort||'?')).join(', ') || '—'}</td>
        </tr>`).join('')}
      </tbody>
    </table>`;
  } else if (tab === 'raw') {
    body.innerHTML = `<pre style="font-size:8px;color:var(--text2);overflow:auto;white-space:pre-wrap;word-break:break-all">${escapeHtml(JSON.stringify(_inspectData, null, 2))}</pre>`;
  }
}

// ── Help modal ────────────────────────────────────────────────────────────────
function openHelp() {
  const overlay = document.getElementById('help-overlay');
  if (overlay) { overlay.style.display = 'flex'; }
}
function closeHelp() {
  const overlay = document.getElementById('help-overlay');
  if (overlay) { overlay.style.display = 'none'; }
}

// ── Quick Memo ────────────────────────────────────────────────────────────────

function openQuickMemo() {
  const overlay = document.getElementById('quickmemo-overlay');
  if (!overlay) return;
  overlay.style.display = 'flex';
  // Populate app selector
  const sel = document.getElementById('quickmemo-app');
  if (sel && S.apps.length) {
    sel.innerHTML = '<option value="">— Aucune app —</option>' +
      S.apps.map(a => `<option value="${escapeHtml(a.id)}">${escapeHtml(a.name || a.id)}</option>`).join('');
  }
  document.getElementById('quickmemo-title')?.focus();
}

function closeQuickMemo() {
  const overlay = document.getElementById('quickmemo-overlay');
  if (overlay) overlay.style.display = 'none';
  const t = document.getElementById('quickmemo-title');
  const b = document.getElementById('quickmemo-body');
  if (t) t.value = '';
  if (b) b.value = '';
}

async function saveQuickMemo() {
  const title  = document.getElementById('quickmemo-title')?.value?.trim() || '';
  const body   = document.getElementById('quickmemo-body')?.value?.trim() || '';
  const appId  = document.getElementById('quickmemo-app')?.value || '';
  if (!body) { notify('Le contenu ne peut pas être vide', 'err'); return; }
  const payload = { title: title || 'Note rapide', content: body };
  if (appId) payload.app_id = appId;
  const r = await api.post('/api/v1/memos', payload);
  if (r?.data?.id || r?.id) {
    notify('Note sauvegardée', 'ok');
    closeQuickMemo();
    if (S.section === 'locations') loadLocations();
  } else {
    notify(r?.error || 'Erreur sauvegarde', 'err');
  }
}

document.addEventListener('DOMContentLoaded', init);
