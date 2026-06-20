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
  logApp: null,
  logStream: null,
  installTarget: null,
  installParams: [],
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
  get:    (p)    => api.req('GET',    p),
  post:   (p, b) => api.req('POST',   p, b),
  del:    (p)    => api.req('DELETE', p),
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
  const [apps, store, stats] = await Promise.all([
    api.get('/api/v1/apps'),
    api.get('/api/v1/store'),
    api.get('/api/v1/stats'),
  ]);
  S.apps    = apps?.apps    || apps    || [];
  S.catalog = store?.apps   || store   || [];
  S.stats   = stats || {};
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
        <div class="mc-val">${diskUsed > 0 ? diskUsed + 'G' : '—'}</div>
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
  const domain = app.domain ? `https://${app.domain}` : '#';
  return `
    <div class="app-card ${isRunning ? 'running' : ''}">
      <div class="card-corner"></div>
      <div class="app-top">
        <div class="app-icon">${icon(app.id)}</div>
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
        ${app.domain ? `<a class="app-link" href="${domain}" target="_blank" rel="noopener"><i class="ti ti-external-link" style="font-size:10px"></i>OUVRIR</a>` : ''}
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
          <div class="cat-tag">${(app.categories || []).join(' · ').toUpperCase() || 'APP'}</div>
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
  const info = await api.get(`/api/v1/store/${appId}`);
  S.installParams = info?.params || [];

  document.getElementById('modal-app-name').textContent = appId.toUpperCase();
  const paramsEl = document.getElementById('modal-params');
  paramsEl.innerHTML = S.installParams.length === 0
    ? `<div style="font-size:10px;color:var(--text3)">Aucun paramètre requis pour cette app.</div>`
    : S.installParams.map(p => `
        <div class="field full">
          <div class="field-label">${p.label.toUpperCase()}${p.required ? ' *' : ''}</div>
          <input class="field-input" id="param-${p.id}" type="${p.type === 'secret' ? 'password' : 'text'}"
            placeholder="${p.description || ''}" value="${p.default || ''}" />
        </div>
      `).join('');

  document.getElementById('install-modal').classList.add('open');
}

async function confirmInstall() {
  const params = {};
  S.installParams.forEach(p => {
    const el = document.getElementById(`param-${p.id}`);
    if (el) params[p.id] = el.value;
  });

  document.getElementById('install-modal').classList.remove('open');
  notify(`Installation de ${S.installTarget}...`, 'info');

  const r = await api.post(`/api/v1/apps/${S.installTarget}/install`, {
    params,
    async: true,
  });

  if (r && r.success !== false) {
    notify(`${S.installTarget} — installation lancée`, 'ok');
    S.tab = 'installed';
    goSection('apps');
    setTimeout(loadApps, 2000);
  } else {
    notify(r?.error || 'Erreur installation', 'err');
  }
}

// ── SECTION: LOGS ─────────────────────────────────────────────────────────────
function openLogs(appId) {
  S.logApp = appId;
  goSection('logs');
}

async function loadLogs() {
  const select = document.getElementById('log-select');
  if (select && S.apps.length) {
    select.innerHTML = S.apps.map(a =>
      `<option value="${a.id}" ${a.id === S.logApp ? 'selected' : ''}>${a.id}</option>`
    ).join('');
  }

  const appId = S.logApp || S.apps[0]?.id;
  if (!appId) return;

  stopLogStream();
  clearLogs();

  // Logs initiaux (tail 100)
  const data = await api.get(`/api/v1/apps/${appId}/logs?tail=100`);
  if (data?.lines) {
    data.lines.forEach(l => appendLog(l));
  } else if (typeof data === 'string') {
    data.split('\n').filter(Boolean).forEach(l => appendLog(l));
  }

  appendLogCursor();
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

// ── SECTION: SECRETS ──────────────────────────────────────────────────────────
async function loadSecrets() {
  const c = document.getElementById('content-secrets');
  if (!c) return;
  c.innerHTML = `
    <div class="secret-alert">
      <i class="ti ti-shield-lock" style="font-size:14px;flex-shrink:0" aria-hidden="true"></i>
      AES-256-GCM — SAISIR LE MOT DE PASSE MAÎTRE POUR CONSULTER LES VALEURS
    </div>
    <div style="font-size:10px;color:var(--text3);text-align:center;padding:40px">
      Gestion des secrets à venir dans une prochaine version.
    </div>
  `;
}

// ── SECTION: STATS (dashboard) ────────────────────────────────────────────────
async function loadStats() {
  const data = await api.get('/api/v1/stats?disk=true');
  S.stats = data || {};
  renderStats();
}

function renderStats() {
  const c = document.getElementById('content-stats');
  if (!c) return;

  const cpu  = S.stats.cpu_pct  || 0;
  const ram  = S.stats.ram_pct  || 0;
  const disk = S.stats.disk_pct || 0;

  c.innerHTML = `
    <div class="settings-card">
      <div class="settings-title">RESSOURCES SYSTÈME</div>
      <div class="seg-wrap">
        <div class="seg-meta"><span>CPU</span><span>${cpu}%</span></div>
        <div class="seg-bar">${segBar(cpu)}</div>
      </div>
      <div class="seg-wrap">
        <div class="seg-meta"><span>RAM</span><span>${S.stats.ram_used_gb || '—'}G / ${S.stats.ram_total_gb || '—'}G</span></div>
        <div class="seg-bar">${segBar(ram)}</div>
      </div>
      <div class="seg-wrap">
        <div class="seg-meta"><span>DISQUE</span><span>${S.stats.disk_used_gb || '—'}G / ${S.stats.disk_total_gb || '—'}G</span></div>
        <div class="seg-bar">${segBar(disk, 20, 'on-ok')}</div>
      </div>
    </div>
    <div class="settings-card">
      <div class="settings-title">DAEMON</div>
      <div class="setting-row"><span>VERSION</span><span class="setting-val">${S.stats.version || '—'}</span></div>
      <div class="setting-row"><span>SOCKET</span><span class="setting-val">/run/caleoped.sock</span></div>
      <div class="setting-row"><span>UPTIME</span><span class="setting-val">${S.stats.uptime || '—'}</span></div>
      <div class="setting-row"><span>APPS ACTIVES</span><span class="setting-val text-vio">${S.apps.filter(a => a.status === 'running').length}</span></div>
    </div>
  `;
}

// ── SECTION: SETTINGS ─────────────────────────────────────────────────────────
async function loadSettings() {
  const data = await api.get('/api/v1/ping');
  const c = document.getElementById('content-settings');
  if (!c) return;
  c.innerHTML = `
    <div class="settings-card">
      <div class="settings-title">SERVEUR</div>
      <div class="setting-row"><span>DOMAINE</span><span class="setting-val">${data?.domain || '—'}</span></div>
      <div class="setting-row"><span>PROXY</span><span class="setting-val">${data?.proxy_mode || '—'}</span></div>
      <div class="setting-row"><span>CANAL</span><span class="badge badge-warn"><span style="width:5px;height:5px;background:var(--warn);display:inline-block"></span>&nbsp;${(data?.channel || 'stable').toUpperCase()}</span></div>
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
      <div class="settings-title">SESSION</div>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div style="font-size:10px;color:var(--text3)">CONNECTÉ À L'INTERFACE WEB</div>
        <button class="btn btn-sm danger" onclick="logout()"><i class="ti ti-logout"></i>SE DÉCONNECTER</button>
      </div>
    </div>
  `;
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

// ── Navigation ────────────────────────────────────────────────────────────────
const SECTIONS = {
  apps:      { label: 'APPLICATIONS', num: '/01', load: loadApps,     content: 'content-apps'      },
  logs:      { label: 'LOGS',         num: '/02', load: loadLogs,     content: 'content-logs'      },
  backups:   { label: 'SAUVEGARDES',  num: '/03', load: null,         content: 'content-backups'   },
  secrets:   { label: 'SECRETS',      num: '/04', load: loadSecrets,  content: 'content-secrets'   },
  locations: { label: 'EMPLACEMENTS', num: '/05', load: null,         content: 'content-locations' },
  audit:     { label: 'AUDIT',        num: '/06', load: loadAudit,    content: 'content-audit'     },
  settings:  { label: 'PARAMÈTRES',   num: '/07', load: loadSettings, content: 'content-settings'  },
  stats:     { label: 'SYSTÈME',      num: '/08', load: loadStats,    content: 'content-stats'     },
};

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

  // Sections
  document.querySelectorAll('.section-content').forEach(el => el.classList.add('hidden'));
  const target = document.getElementById(sec?.content);
  if (target) target.classList.remove('hidden');

  // Load data
  if (sec?.load) sec.load();
  else if (id === 'backups' || id === 'locations') renderPlaceholder(sec.content, id);
}

function renderPlaceholder(contentId, id) {
  const icons = { backups: 'ti-archive', locations: 'ti-network' };
  const labels = { backups: 'AUCUNE SAUVEGARDE', locations: 'AUCUN EMPLACEMENT' };
  const subs = { backups: 'Restic SFTP / S3 à configurer.', locations: 'NFS / SMB — Montage automatique.' };
  const el = document.getElementById(contentId);
  if (!el) return;
  el.innerHTML = `
    <div class="empty-state">
      <div class="empty-icon"><i class="ti ${icons[id]}" aria-hidden="true"></i></div>
      <div class="empty-title">${labels[id]}</div>
      <div class="empty-sub">${subs[id]}</div>
    </div>
  `;
}

// ── Audit ────────────────────────────────────────────────────────────────────
async function loadAudit() {
  const c = document.getElementById('content-audit');
  if (!c) return;
  const data = await api.get('/api/v1/audit');
  const entries = data?.entries || data || [];
  if (!entries.length) {
    c.innerHTML = `<div class="empty-state"><div class="empty-icon"><i class="ti ti-clipboard-list"></i></div><div class="empty-title">AUCUNE ENTRÉE</div></div>`;
    return;
  }
  const lines = entries.slice(-50).reverse().map(e => {
    const tagCls = e.action?.includes('install') ? 'log-ok' : e.action?.includes('error') ? 'log-err' : 'log-step';
    return `<div class="log-line">
      <span class="log-ts">${e.timestamp || ''}</span>
      <span class="log-tag ${tagCls}">[${(e.action || '').toUpperCase()}]</span>
      <span class="log-txt">${escapeHtml(e.target || '')} — ${escapeHtml(e.user || '')}</span>
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
  goSection('apps');
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

// ── Utils ─────────────────────────────────────────────────────────────────────
function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

document.addEventListener('DOMContentLoaded', init);
