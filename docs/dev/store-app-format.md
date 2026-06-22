# Format des paramètres d'installation — Store Caleope

> Documentation à destination des développeurs contribuant au store Caleope.  
> Mise à jour : 2026-06-21

---

## Vue d'ensemble

Chaque application du store peut déclarer des **paramètres d'installation** que l'interface web affiche sous forme de formulaire avant l'installation. Ces paramètres permettent de configurer l'application sans que l'utilisateur ait à éditer des fichiers Docker Compose ou des variables d'environnement à la main.

L'UI Caleope récupère les paramètres depuis :
1. `GET /api/v1/store/{id}` (source canonique — endpoint du daemon)
2. Fallback : `HARDCODED_PARAMS` dans `web/app.js` (valeurs de secours côté UI)

**Objectif : les deux sources doivent toujours être synchronisées.**

---

## Schéma d'un paramètre

```json
{
  "id": "admin_email",
  "label": "Email administrateur",
  "type": "text",
  "default": "",
  "required": true,
  "description": "Email du premier compte admin — utilisé pour la connexion",
  "depends_on": {
    "param": "smtp_enabled",
    "values": ["true"]
  }
}
```

### Champs

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `id` | string | ✅ | Identifiant unique du paramètre, en `snake_case`. Devient la clé dans le payload d'installation. |
| `label` | string | ✅ | Libellé affiché dans le formulaire (en français, en clair). L'UI l'affiche en majuscules. |
| `type` | string | ✅ | Type du champ — voir **Types** ci-dessous. |
| `default` | string | — | Valeur pré-remplie. Pour `bool`, utiliser `"true"` ou `"false"`. |
| `required` | bool | — | Affiche `*` dans le label, bloque la soumission si vide. |
| `description` | string | — | Texte d'aide affiché sous le champ. Expliquer le *pourquoi*, pas le *quoi*. |
| `options` | string[] | Pour `select` | Liste des valeurs possibles. La première est choisie par défaut si `default` absent. |
| `depends_on` | object | — | Masque le champ si la condition n'est pas remplie — voir **Visibilité conditionnelle**. |

---

## Types de champs

### `text`
Champ texte libre. À utiliser pour : noms de domaine, noms d'hôte, chemins non-système, régions, etc.

```json
{ "id": "site_title", "label": "Titre du site", "type": "text", "default": "Mon Site" }
```

### `secret`
Champ mot de passe (masqué). À utiliser pour : mots de passe, tokens API, clés privées, clés secrètes.  
**Ne jamais mettre de valeur par défaut pour un secret.** Laisser `default: ""`.

```json
{ "id": "admin_password", "label": "Mot de passe admin", "type": "secret", "required": true }
```

### `bool`
Deux boutons radio ACTIVÉ / DÉSACTIVÉ. À utiliser pour des fonctionnalités optionnelles.  
`default` doit être `"true"` ou `"false"` (string, pas booléen JSON).

```json
{ "id": "vpn_enabled", "label": "Kill-switch VPN", "type": "bool", "default": "true" }
```

### `select`
Menu déroulant. À utiliser dès que les valeurs sont connues et finies (protocole, provider, région...).  
**Préférer `select` à `text` quand les valeurs possibles sont énumérables.**

```json
{
  "id": "vpn_protocol",
  "label": "Protocole VPN",
  "type": "select",
  "default": "wireguard",
  "options": ["wireguard", "openvpn"]
}
```

### `location`
Menu déroulant qui liste les emplacements réseau configurés (NFS/SMB) **plus** une option "SYSTÈME" avec le chemin par défaut.  
À utiliser pour tous les chemins de stockage (médias, données, backups).  
`default` = chemin absolu local utilisé quand aucun emplacement réseau n'est sélectionné.

```json
{
  "id": "media_path",
  "label": "Stockage médias",
  "type": "location",
  "default": "/opt/gaiver-it/caleope/data/media"
}
```

---

## Visibilité conditionnelle (`depends_on`)

Un paramètre peut être **masqué automatiquement** selon la valeur d'un autre paramètre.

### Syntaxe

```json
"depends_on": {
  "param": "id_du_parametre_parent",
  "values": ["valeur1", "valeur2"]
}
```

Le champ est affiché si et seulement si le paramètre `param` a l'une des valeurs listées dans `values`.

### Conditions multiples (ET logique)

```json
"depends_on": [
  { "param": "vpn_enabled",  "values": ["true"] },
  { "param": "vpn_protocol", "values": ["wireguard"] }
]
```

### Exemple complet : VPN dans arr-stack

```json
[
  { "id": "vpn_enabled",  "type": "bool",   "default": "true" },
  { "id": "vpn_protocol", "type": "select",  "options": ["wireguard","openvpn"],
    "depends_on": { "param": "vpn_enabled", "values": ["true"] } },
  { "id": "wireguard_private_key", "type": "secret",
    "depends_on": { "param": "vpn_protocol", "values": ["wireguard"] } },
  { "id": "openvpn_user", "type": "text",
    "depends_on": { "param": "vpn_protocol", "values": ["openvpn"] } }
]
```

---

## Bonnes pratiques

### Ce qu'il faut toujours inclure

- **Tous les champs nécessaires au premier démarrage** : l'utilisateur ne devrait pas avoir à ouvrir un shell après l'installation.
- **Le stockage via `location`** pour tout ce qui est données persistantes, médias, uploads.
- **Les credentials admin** pour les apps qui ont une interface web (email + mot de passe).
- **SMTP comme section optionnelle** (`smtp_enabled: bool → false` + champs conditionnels) pour les apps qui envoient des emails.

### Ce qu'il ne faut pas inclure

- Les variables internes (identifiants de base de données générés, seeds internes, ports internes entre containers).
- Les options avancées que 95% des utilisateurs ne toucheront jamais — les exposer dans la documentation de l'app, pas dans le formulaire.
- Les valeurs qui peuvent être calculées automatiquement (ex: DB_PASSWORD peut être auto-généré si laissé vide).

### Secrets

- Ne jamais proposer de valeur par défaut pour un secret.
- Si l'app peut auto-générer un secret (ex: `SECRET_KEY` d'Authentik), documenter ce comportement dans `description` : `"Laisser vide pour générer automatiquement"`.
- Distinguer le type `secret` du type `text` même pour des tokens qui "ressemblent" à du texte.

### Labels et descriptions

- `label` : nom court du champ, compréhensible sans contexte. Max ~40 caractères.
- `description` : explique **pourquoi** ce champ existe ou **où trouver** la valeur. Exemples :
  - ✅ `"Clé depuis admin.tailscale.com > Settings > Keys"`
  - ✅ `"Généré dans Admin > Nodes > [nœud] > Configuration dans Pterodactyl Panel"`
  - ❌ `"La clé privée WireGuard"` (redit juste le label)

---

## Référence des apps du store

| App | Params notables | `depends_on` utilisé |
|-----|----------------|----------------------|
| `arr-stack` | vpn_protocol (wireguard/openvpn), clés WG, credentials OVPN, media_path | ✅ vpn_enabled → vpn_protocol → champs WG/OVPN |
| `authentik` | admin_email, admin_password, secret_key, SMTP | ✅ smtp_enabled → champs SMTP |
| `azuracast` | admin_email, admin_password | — |
| `crowdsec` | enroll_key (optionnel) | — |
| `ghost` | site_title, admin_email, admin_password, SMTP | ✅ smtp_enabled |
| `gitea` | admin_username, admin_email, admin_password, site_name, SMTP | ✅ smtp_enabled |
| `glpi` | aucun (setup via wizard web) | — |
| `immich` | upload_path, db_password (optionnel) | — |
| `jellyfin` | media_path, hw_transcoding | — |
| `nextcloud` | admin_user, admin_password, data_path, SMTP | ✅ smtp_enabled |
| `prometheus-grafana` | grafana_admin_user, grafana_admin_password | — |
| `pterodactyl-panel` | admin_email, admin_username, admin_password, app_name, SMTP | ✅ smtp_enabled |
| `pterodactyl-wings` | panel_url, wings_token, node_name | — |
| `restic` | repo_backend (sftp/s3/b2/local), repo_password, champs backend, schedule | ✅ repo_backend → champs SFTP/S3/B2/local |
| `tailscale` | auth_key, hostname, exit_node_enabled, accept_routes | — |
| `vaultwarden` | admin_token (optionnel), data_path, SMTP | ✅ smtp_enabled |
| `wg-easy` | wg_host (requis), wg_password, wg_port, wg_cidr, wg_dns | — |
| `wikijs` | admin_email, admin_password, site_title, SMTP | ✅ smtp_enabled |
| `wordpress` | site_title, admin_user, admin_email, admin_password, SMTP | ✅ smtp_enabled |

---

## Synchronisation UI ↔ Daemon

Le fallback `HARDCODED_PARAMS` dans `web/app.js` est une **sécurité** pour les versions du daemon qui ne renvoient pas encore les paramètres via l'API. Quand le daemon implémente `GET /api/v1/store/{id}` avec les paramètres, les deux doivent être identiques.

Processus recommandé lors de l'ajout d'une app :
1. Définir les paramètres dans le store daemon (source de vérité).
2. Ajouter l'entrée correspondante dans `HARDCODED_PARAMS` avec exactement les mêmes `id`.
3. Tester l'installation via l'UI sur un environnement de dev.
4. Mettre à jour ce document.
