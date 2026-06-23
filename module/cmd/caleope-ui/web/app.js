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
  locations: [],
  logApp: null,
  logStream: null,
  installTarget: null,
  installParams: [],
  backupApp: null,
  tasks: [],       // file de tâches style Proxmox
  taskSeq: 0,      // compteur d'ID de tâche
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
  return r && r.status === 'ok';
}

async function logout() {
  await fetch('/auth/logout', { method: 'POST' });
  showLogin();
}

// ── Notifications ─────────────────────────────────────────────────────────────
function notify(msg, type = 'info') {
  const stack = document.getElementById('notif-stack');
  const n = document.createElement('div');
  n.className = `notif notif-${type}`;
  const icon = { ok: 'ti-check', err: 'ti-alert-circle', info: 'ti-info-circle' }[type];
  n.innerHTML = `<i class="ti ${icon}" aria-hidden="true"></i>${msg}`;
  stack.appendChild(n);
  setTimeout(() => n.remove(), 4000);
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

// ── Clock ─────────────────────────────────────────────────────────────────────
function startClock() {
  const el = document.getElementById('clock');
  if (!el) return;
  const tick = () => { el.textContent = new Date().toLocaleTimeString('fr-FR', { hour12: false }); };
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
};

// ── Icons apps (défaut) ───────────────────────────────────────────────────────
const APP_ICONS = {
  jellyfin: '🎬', 'arr-stack': '📡', azuracast: '🎵', nextcloud: '☁️',
  vaultwarden: '🔒', authentik: '🔑', gitea: '🐙', immich: '📸',
  'prometheus-grafana': '📊', crowdsec: '🛡️', 'wg-easy': '🌐', ghost: '👻',
  wordpress: '📝', glpi: '🎫', 'wiki-js': '📚', pterodactyl: '🦕',
  tailscale: '🔐',
};
const icon = id => APP_ICONS[id] || '📦';

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
  S.apps    = apps?.data    || [];
  S.catalog = store?.data   || [];
  S.stats   = { ...(stats?.data || {}), version: ping?.data?.version };
  buildDynamicNav();
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

    <div class="tabs">
      <button class="tab-btn ${S.tab === 'installed' ? 'active' : ''}" onclick="switchTab('installed')">
        INSTALLÉES <span class="tab-count">${S.apps.length}</span>
      </button>
      <button class="tab-btn ${S.tab === 'catalog' ? 'active' : ''}" onclick="switchTab('catalog')">
        CATALOGUE <span class="tab-count">${S.catalog.length}</span>
      </button>
    </div>

    <div id="tab-installed" class="${S.tab !== 'installed' ? 'hidden' : ''}">
      ${S.apps.length === 0
        ? `<div class="empty-state">
            <div class="empty-icon"><i class="ti ti-apps" aria-hidden="true"></i></div>
            <div class="empty-title">AUCUNE APP INSTALLÉE</div>
            <div class="empty-sub">Ouvrez le catalogue pour installer votre première app.</div>
           </div>`
        : `<div class="apps-grid">${S.apps.map(appCard).join('')}</div>`
      }
    </div>

    <div id="tab-catalog" class="${S.tab !== 'catalog' ? 'hidden' : ''}">
      ${S.catalog.length === 0
        ? `<div class="empty-state">
            <div class="empty-icon"><i class="ti ti-store" aria-hidden="true"></i></div>
            <div class="empty-title">CATALOGUE VIDE</div>
            <div class="empty-sub">Vérifiez la connexion au store.</div>
           </div>`
        : `<div class="cat-grid">${S.catalog.map(catalogCard).join('')}</div>`
      }
    </div>
  `;
}

function appCard(app) {
  const isRunning = app.status === 'running';
  const domain = app.domain ? `https://${app.domain}` : null;
  const iconEl = domain
    ? `<a class="app-icon" href="${domain}" target="_blank" rel="noopener" title="Ouvrir ${escapeHtml(app.name || app.id)}">${icon(app.id)}</a>`
    : `<div class="app-icon">${icon(app.id)}</div>`;
  return `
    <div class="app-card ${isRunning ? 'running' : ''}">
      <div class="card-corner"></div>
      <div class="app-top">
        ${iconEl}
        <div class="app-meta">
          <div class="app-name">${app.name || app.id}</div>
          <div class="app-ver">${app.version || '—'}</div>
        </div>
        ${statusBadge(app.status)}
      </div>
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
          <button class="action-btn danger" onclick="removeApp('${app.id}')" title="Supprimer">
            <i class="ti ti-trash"></i>
            <span class="btn-label">SUPPRIMER</span>
          </button>
        </div>
        ${domain ? `<a class="app-link" href="${domain}" target="_blank" rel="noopener"><i class="ti ti-external-link" style="font-size:10px"></i>OUVRIR</a>` : ''}
      </div>
    </div>
  `;
}

function catalogCard(app) {
  const installed = S.apps.some(a => a.id === app.id);
  return `
    <div class="cat-card">
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
    : S.installParams.map(p => {
        let inner;
        if (p.type === 'bool') {
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
      }).join('');

  updateParamVisibility();
  document.getElementById('install-modal').classList.add('open');
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
    // Ne pas envoyer les champs masqués par depends_on
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

  document.getElementById('install-modal').classList.remove('open');
  const appLabel = S.catalog.find(a => a.id === S.installTarget)?.name || S.installTarget;
  const taskId = taskAdd('install', S.installTarget, `Installation — ${appLabel}`);

  const r = await api.post(`/api/v1/apps/${S.installTarget}/install`, {
    params,
    async: true,
  });

  if (r && r.success !== false) {
    taskDone(taskId, 'Installation terminée', true);
    notify(`${S.installTarget} — installation terminée`, 'ok');
    S.tab = 'installed';
    goSection('apps');
    setTimeout(loadApps, 1000);
  } else {
    taskDone(taskId, r?.error || 'Erreur inconnue', false);
    notify(r?.error || 'Erreur installation', 'err');
  }
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

  c.innerHTML = `
    <div style="display:flex;align-items:center;gap:8px;margin-bottom:14px">
      <span style="font-size:9px;color:var(--text3);letter-spacing:.5px">AFFICHER LES SAUVEGARDES DE</span>
      <select class="log-select" id="backup-select" onchange="changeBackupApp()" style="flex:0;min-width:160px">
        ${S.apps.map(a => `<option value="${a.id}" ${a.id === S.backupApp ? 'selected' : ''}>${a.name || a.id}</option>`).join('')}
      </select>
    </div>
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
          <div class="setting-row" style="font-family:monospace;font-size:9px">
            <span style="color:var(--text2)">${escapeHtml(k)}</span>
            <span class="setting-val" style="font-size:9px;word-break:break-all">${escapeHtml(v)}</span>
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
  const data = await api.get('/api/v1/stats?disk=true');
  S.stats = data?.data || {};
  renderStats();
}

function renderStats() {
  const c = document.getElementById('content-stats');
  if (!c) return;

  const ramUsedGb  = S.stats.mem_used_mb  ? (S.stats.mem_used_mb  / 1024).toFixed(1) : '—';
  const ramTotalGb = S.stats.mem_total_mb ? (S.stats.mem_total_mb / 1024).toFixed(1) : '—';
  const ram  = S.stats.mem_total_mb ? Math.round(S.stats.mem_used_mb  / S.stats.mem_total_mb  * 100) : 0;
  const disk = S.stats.disk_total_gb ? Math.round(S.stats.disk_used_gb / S.stats.disk_total_gb * 100) : 0;
  const diskUsedGb  = S.stats.disk_used_gb  ? S.stats.disk_used_gb.toFixed(1)  : '—';
  const diskTotalGb = S.stats.disk_total_gb ? S.stats.disk_total_gb.toFixed(1) : '—';

  c.innerHTML = `
    <div class="settings-card">
      <div class="settings-title">RESSOURCES SYSTÈME</div>
      <div class="seg-wrap">
        <div class="seg-meta"><span>RAM</span><span>${ramUsedGb}G / ${ramTotalGb}G</span></div>
        <div class="seg-bar">${segBar(ram)}</div>
      </div>
      <div class="seg-wrap">
        <div class="seg-meta"><span>DISQUE</span><span>${diskUsedGb}G / ${diskTotalGb}G</span></div>
        <div class="seg-bar">${segBar(disk, 20, 'on-ok')}</div>
      </div>
    </div>
    <div class="settings-card">
      <div class="settings-title">DAEMON</div>
      <div class="setting-row"><span>SOCKET</span><span class="setting-val">/run/caleoped.sock</span></div>
      <div class="setting-row"><span>APPS ACTIVES</span><span class="setting-val text-vio">${S.apps.filter(a => a.status === 'running').length}</span></div>
    </div>
  `;
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
    <div class="settings-card">
      <div class="settings-title">MISE À JOUR</div>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div style="font-size:10px;color:var(--text3)">VERSION ACTUELLE : <span class="setting-val">${data?.version || '—'}</span></div>
        <button class="btn" onclick="checkUpgrade()"><i class="ti ti-refresh"></i>VÉRIFIER</button>
      </div>
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
    <div class="settings-card">
      <div class="settings-title">SESSION</div>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div style="font-size:10px;color:var(--text3)">CONNECTÉ À L'INTERFACE WEB</div>
        <button class="btn btn-sm danger" onclick="logout()"><i class="ti ti-logout"></i>SE DÉCONNECTER</button>
      </div>
    </div>
  `;
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
  if (r?.update_available) {
    notify(`Mise à jour disponible : ${r.latest_version}`, 'ok');
  } else {
    notify('Caleope est à jour', 'ok');
  }
}

// ── SECTION: DASHBOARD ────────────────────────────────────────────────────────
async function loadDashboard() {
  const c = document.getElementById('content-dashboard');
  if (!c) return;

  // Charger les données si pas encore disponibles
  const needsLoad = !S.apps.length && !S.catalog.length;
  if (needsLoad) {
    c.innerHTML = `<div class="dash-loading"><span class="spinner"></span> CHARGEMENT...</div>`;
    await loadApps();
  }

  const running = S.apps.filter(a => a.status === 'running').length;
  const stopped = S.apps.length - running;
  const ram  = S.stats.mem_total_mb ? Math.round(S.stats.mem_used_mb / S.stats.mem_total_mb * 100) : 0;
  const disk = S.stats.disk_total_gb ? Math.round(S.stats.disk_used_gb / S.stats.disk_total_gb * 100) : 0;

  const shortcuts = [
    { id: 'apps',      icon: 'ti-layout-grid', label: 'APPLICATIONS', val: `${S.apps.length} installées` },
    { id: 'logs',      icon: 'ti-terminal-2',  label: 'LOGS',         val: 'Temps réel' },
    { id: 'backups',   icon: 'ti-archive',      label: 'SAUVEGARDES',  val: 'Restic SFTP' },
    { id: 'secrets',   icon: 'ti-lock',         label: 'SECRETS',      val: 'AES-256-GCM' },
    { id: 'locations', icon: 'ti-network',      label: 'EMPLACEMENTS', val: 'NFS / SMB' },
    { id: 'audit',     icon: 'ti-clipboard-list',label: 'AUDIT',       val: 'Historique' },
    { id: 'stats',     icon: 'ti-chart-bar',    label: 'SYSTÈME',      val: ram ? `RAM ${ram}%` : '—' },
    { id: 'settings',  icon: 'ti-settings',     label: 'PARAMÈTRES',   val: `v${S.stats.version || '—'}` },
  ];

  c.innerHTML = `
    <div class="metrics" style="margin-bottom:20px">
      <div class="mc mc-vio">
        <div class="mc-label">APPS ACTIVES</div>
        <div class="mc-val">${String(running).padStart(2,'0')}</div>
        <div class="mc-sub">${stopped} arrêtée${stopped !== 1 ? 's' : ''}</div>
      </div>
      <div class="mc">
        <div class="mc-label">INSTALLÉES</div>
        <div class="mc-val">${String(S.apps.length).padStart(2,'0')}</div>
        <div class="mc-sub">SUR ${S.catalog.length} DISPO</div>
      </div>
      <div class="mc">
        <div class="mc-label">RAM</div>
        <div class="mc-val ${ram > 85 ? 'mc-err' : ''}">${ram || '—'}${ram ? '%' : ''}</div>
        <div class="mc-sub"><div class="seg-bar" style="margin-top:4px">${segBar(ram, 12, ram > 85 ? 'on-err' : ram > 70 ? 'on-warn' : 'on')}</div></div>
      </div>
      <div class="mc">
        <div class="mc-label">DISQUE</div>
        <div class="mc-val ${disk > 85 ? 'mc-err' : ''}">${disk || '—'}${disk ? '%' : ''}</div>
        <div class="mc-sub"><div class="seg-bar" style="margin-top:4px">${segBar(disk, 12, disk > 85 ? 'on-err' : disk > 70 ? 'on-warn' : 'on-ok')}</div></div>
      </div>
    </div>

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

    ${S.apps.length ? `
      <div style="font-size:9px;color:var(--text3);letter-spacing:1.5px;font-weight:700;margin:20px 0 10px">// APPLICATIONS</div>
      <div class="apps-grid">
        ${S.apps.slice(0,6).map(a => {
          const domain = a.domain ? `https://${a.domain}` : null;
          return `<div class="app-card ${a.status === 'running' ? 'running' : ''}" style="cursor:pointer" onclick="goSection('apps')">
            <div class="card-corner"></div>
            <div class="app-top">
              ${domain ? `<a class="app-icon" href="${domain}" target="_blank" rel="noopener" onclick="event.stopPropagation()">${icon(a.id)}</a>` : `<div class="app-icon">${icon(a.id)}</div>`}
              <div class="app-meta">
                <div class="app-name">${escapeHtml(a.name || a.id)}</div>
                <div class="app-ver">${a.version || '—'}</div>
              </div>
              ${statusBadge(a.status)}
            </div>
          </div>`;
        }).join('')}
      </div>
    ` : ''}
  `;
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
  audit:     { label: 'AUDIT',           num: '/07', load: loadAudit,      content: 'content-audit',      btn: null },
  settings:  { label: 'PARAMÈTRES',      num: '/08', load: loadSettings,   content: 'content-settings',   btn: null },
  stats:     { label: 'SYSTÈME',         num: '/09', load: loadStats,      content: 'content-stats',      btn: null },
  terminal:  { label: 'TERMINAL',        num: '/10', load: loadTerminal,   content: 'content-terminal',   btn: null },
  services:  { label: 'SERVICES',        num: '/11', load: loadServices,   content: 'content-services',   btn: null },
  network:   { label: 'RÉSEAU',          num: '/12', load: loadNetwork,    content: 'content-network',    btn: null },
  storage:   { label: 'STOCKAGE',        num: '/13', load: loadStorage,    content: 'content-storage',    btn: null },
  journal:   { label: 'JOURNAL',         num: '/14', load: loadJournal,    content: 'content-journal',    btn: null },
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
  'azuracast': {
    group: '// MÉDIAS',
    icon: 'ti-radio',
    panels: [
      { id: 'panel-azuracast', label: 'RADIO', icon: 'ti-broadcast', load: loadAzuraCast },
    ],
  },
  'pterodactyl': {
    group: '// JEUX',
    icon: 'ti-device-gamepad',
    panels: [
      { id: 'panel-pterodactyl', label: 'SERVEURS', icon: 'ti-server-2', load: loadPterodactyl },
    ],
  },
};

function buildDynamicNav() {
  const sbInt = document.getElementById('sb-integrations');
  if (!sbInt) return;

  const installedIds = new Set(S.apps.map(a => a.id));
  const content = document.querySelector('.content');

  let html = '';
  Object.entries(APP_PANELS).forEach(([appId, app]) => {
    if (!installedIds.has(appId)) return;
    html += `<div class="sb-group">${app.group}</div>`;
    app.panels.forEach(panel => {
      html += `<button class="nav-btn" data-section="${panel.id}" onclick="goSection('${panel.id}')">
        <i class="ti ${panel.icon}" aria-hidden="true"></i>${panel.label}
        <span style="font-size:7px;color:var(--text3);margin-left:auto;letter-spacing:.3px">${appId.toUpperCase()}</span>
      </button>`;
      // Enregistrer dans SECTIONS si pas déjà présent
      if (!SECTIONS[panel.id]) {
        SECTIONS[panel.id] = {
          label: panel.label, num: '/INT', load: panel.load,
          content: `content-${panel.id}`, btn: null,
        };
      }
      // Créer le div de contenu si absent
      if (content && !document.getElementById(`content-${panel.id}`)) {
        const el = document.createElement('div');
        el.id = `content-${panel.id}`;
        el.className = 'section-content hidden';
        content.appendChild(el);
      }
    });
  });

  sbInt.innerHTML = html;

  // Re-marquer le bouton actif si on est déjà dans un panel
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
  const users = data.results || [];

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
        <span style="color:var(--text3);font-size:9px">${users.length} / ${data.pagination?.count || users.length}</span>
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
        <div style="font-size:9px;color:var(--text3)">${escapeHtml(g.users_obj?.length ? g.users_obj.length + ' membre(s)' : (g.num_pk ? g.num_pk + ' membre(s)' : '—'))}</div>
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

function goSection(id) {
  S.section = id;

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

  // Load data
  if (sec?.load) sec.load();
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
  c.innerHTML = `<div class="log-wrap"><div class="log-body">${lines}</div></div>`;
}

// ── Login screen ──────────────────────────────────────────────────────────────
function showLogin() {
  document.getElementById('app').classList.remove('visible');
  document.getElementById('login-screen').style.display = 'flex';
}

function showApp() {
  document.getElementById('login-screen').style.display = 'none';
  document.getElementById('app').classList.add('visible');
  startClock();
  goSection('dashboard');
}

function refreshSection() {
  const btn = document.getElementById('refresh-btn');
  if (btn) { btn.classList.add('spinning'); setTimeout(() => btn.classList.remove('spinning'), 800); }
  if (SECTIONS[S.section]?.load) SECTIONS[S.section].load();
}

// ── Init ──────────────────────────────────────────────────────────────────────
async function init() {
  const ok = await checkAuth();
  if (ok) { showApp(); }
  else    { showLogin(); }

  // Login form handler
  document.getElementById('login-form')?.addEventListener('submit', async e => {
    e.preventDefault();
    const pw  = document.getElementById('login-pw').value;
    const btn = document.getElementById('login-btn');
    const err = document.getElementById('login-error');

    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span>&nbsp;CONNEXION...';

    const ok = await login(pw);
    if (ok) {
      showApp();
    } else {
      err.textContent = 'MOT DE PASSE INCORRECT';
      err.classList.add('show');
      btn.disabled = false;
      btn.innerHTML = '<i class="ti ti-arrow-right"></i>CONNECTER';
    }
  });
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
    <div class="svc-row">
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

  el.innerHTML = `<div class="svc-list">${rows}</div>`;
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

  el.innerHTML = `
    <div class="net-section-title">// INTERFACES PHYSIQUES</div>
    <div class="net-grid">${ifaceCards || '<div class="empty-msg">Aucune interface physique détectée</div>'}</div>
    ${routeRows ? `<div class="net-section-title" style="margin-top:24px">// ROUTES</div>
    <div class="table-wrap">
      <table class="sys-table"><thead><tr><th>DESTINATION</th><th>PASSERELLE</th><th>INTERFACE</th><th>MÉTRIQUE</th></tr></thead>
      <tbody>${routeRows}</tbody></table>
    </div>` : ''}`;
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
    <div class="disk-list">${availCards}</div>` : ''}`;
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

  const lines = data.entries.map(e => {
    const ts = e.time ? new Date(parseInt(e.time.replace('.',''))/1000).toLocaleTimeString('fr-FR') : '';
    return `<div class="log-line">
      <span class="log-ts">${escapeHtml(ts)}</span>
      <span class="log-app" style="color:var(--blue)">${escapeHtml(e.unit || '—')}</span>
      <span class="log-level" style="color:${priColor(e.priority)}">${escapeHtml(e.priority || '')}</span>
      <span class="log-msg">${escapeHtml(e.message || '')}</span>
    </div>`;
  }).join('');

  el.innerHTML = lines || '<div class="empty-msg">Aucune entrée</div>';
  el.scrollTop = el.scrollHeight;
}

// ── Utils ─────────────────────────────────────────────────────────────────────
function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

document.addEventListener('DOMContentLoaded', init);
