# HOLOW-MCP

**Serveur MCP (Model Context Protocol) pour automatiser un navigateur Chrome/Chromium depuis Claude Code ou tout autre client MCP.**

## Qu'est-ce que c'est ?

HOLOW-MCP permet à un assistant IA (Claude, etc.) de :
- Ouvrir un navigateur web
- Naviguer vers des sites
- Cliquer sur des éléments
- Remplir des formulaires
- Prendre des captures d'écran
- Exécuter du JavaScript
- Gérer les cookies

Le tout via des commandes simples, sans que vous ayez besoin de coder.

---

## Installation rapide

### Prérequis

1. **Go 1.21+** installé ([télécharger](https://go.dev/dl/))
2. **Chrome ou Chromium** installé sur votre machine

### Étapes

```bash
# 1. Cloner le projet
git clone https://github.com/hazyhaar/holow_mcp.git
cd holow_mcp

# 2. Compiler
go build -o bin/holow-mcp ./cmd/holow-mcp

# 3. Initialiser (crée les bases de données)
./bin/holow-mcp -setup
```

Le setup interactif vous guidera pour :
- Choisir le dossier de données (par défaut `~/.holow-mcp`)
- Configurer les credentials (optionnel)
- Détecter Chrome/Chromium automatiquement

---

## Configuration avec Claude Code

### Méthode 1 : Fichier projet (recommandé)

Créez un fichier `.mcp.json` à la racine de votre projet :

```json
{
  "mcpServers": {
    "holow": {
      "command": "/chemin/vers/holow-mcp/bin/holow-mcp",
      "args": []
    }
  }
}
```

### Méthode 2 : Configuration globale

Le fichier est dans `~/.claude.json`. Ajoutez dans la section `projects` → votre projet → `mcpServers` :

```json
"holow": {
  "type": "stdio",
  "command": "/chemin/vers/holow-mcp/bin/holow-mcp",
  "args": []
}
```

### Vérification

Dans Claude Code, tapez `/mcp` pour voir si holow apparaît avec un ✓ vert.

---

## Utilisation

Une fois connecté, demandez simplement à Claude :

### Exemples de requêtes en langage naturel

**Navigation :**
> "Ouvre Google et cherche 'météo Paris'"

**Capture d'écran :**
> "Prends une capture d'écran de la page actuelle"

**Remplir un formulaire :**
> "Va sur le site X, remplis le champ email avec mon@email.com et clique sur Envoyer"

**Extraire du contenu :**
> "Lis le contenu de la page et donne-moi un résumé"

---

## Outils disponibles

### 1. `browser` - Contrôle du navigateur

L'outil principal avec 16 actions :

| Action | Description | Exemple |
|--------|-------------|---------|
| `launch` | Ouvre Chrome | `launch` avec `headless: false` pour voir la fenêtre |
| `navigate` | Va vers une URL | `navigate` avec `url: "https://google.com"` |
| `screenshot` | Capture d'écran | `screenshot` avec `full_page: true` pour page entière |
| `click` | Clique sur un élément | `click` avec `selector: "#bouton"` |
| `type` | Tape du texte | `type` avec `selector: "#champ"` et `text: "mon texte"` |
| `evaluate` | Exécute du JavaScript | `evaluate` avec `expression: "document.title"` |
| `get_html` | Récupère le HTML | Retourne le code source de la page |
| `get_url` | URL actuelle | Retourne l'URL courante |
| `get_title` | Titre de la page | Retourne le titre |
| `cookies` | Liste les cookies | Retourne tous les cookies |
| `set_cookie` | Définit un cookie | Avec `name`, `value`, `domain` |
| `wait` | Attend un élément | `wait` avec `selector: ".element"` et `timeout: 10` |
| `pdf` | Génère un PDF | Sauvegarde la page en PDF |
| `close` | Ferme le navigateur | Termine la session |
| `connect` | Se connecte à Chrome existant | Si Chrome est déjà ouvert en mode debug |
| `list_actions` | Liste toutes les actions | Aide-mémoire |

### 2. `brainloop` - Outils système

| Action | Description |
|--------|-------------|
| `audit_system` | État du serveur HOLOW |
| `get_metrics` | Métriques en temps réel |
| `list_tools` | Liste tous les outils |
| `get_tool` | Détails d'un outil |
| `create_tool` | Crée un nouvel outil SQL |

---

## Options de ligne de commande

```bash
# Aide
./bin/holow-mcp -h

# Setup interactif (première utilisation)
./bin/holow-mcp -setup

# Afficher la configuration
./bin/holow-mcp -config

# Initialiser les bases de données
./bin/holow-mcp -init -schemas schemas/

# Shell SQL intégré (pour debug)
./bin/holow-mcp -sql "SELECT * FROM tool_definitions"

# Statut des configurations MCP
./bin/holow-mcp -mcp-status
```

---

## Mode visible vs invisible (headless)

Par défaut, le navigateur s'ouvre en mode **invisible** (headless). Pour **voir** ce que fait l'IA :

Demandez à Claude :
> "Lance le navigateur en mode visible"

Ou techniquement : action `launch` avec paramètre `headless: false`

---

## Dépannage

### "Failed to reconnect to holow"

1. Vérifiez que le binaire existe : `ls -la /chemin/vers/bin/holow-mcp`
2. Testez manuellement : `echo '{}' | /chemin/vers/bin/holow-mcp`
3. Réinitialisez : `./bin/holow-mcp -setup`

### "HOLOW-MCP n'est pas initialisé"

Lancez le setup :
```bash
./bin/holow-mcp -setup
```

### Le navigateur ne s'ouvre pas

1. Vérifiez que Chrome/Chromium est installé
2. Testez : `which chromium` ou `which google-chrome`
3. Le setup détecte automatiquement le chemin

### Erreur "wasm error" ou corruption de base

Supprimez les bases et réinitialisez :
```bash
rm -rf ~/.holow-mcp/*.db*
./bin/holow-mcp -init -schemas schemas/
```

### Voir les logs

Les logs MCP sont dans :
```
~/.cache/claude-cli-nodejs/-workspace/mcp-logs-holow/
```

---

## Architecture technique

```
holow-mcp/
├── bin/holow-mcp          # Binaire compilé
├── cmd/holow-mcp/         # Point d'entrée
├── internal/
│   ├── chromium/          # Contrôle Chrome via CDP
│   ├── server/            # Serveur MCP JSON-RPC 2.0
│   ├── database/          # SQLite (modernc.org/sqlite)
│   ├── initcli/           # Setup interactif
│   └── brainloop/         # Outils système
├── schemas/               # Schémas SQL des bases
└── ~/.holow-mcp/          # Données utilisateur
    ├── config.json        # Configuration
    └── holow-mcp.*.db     # Bases SQLite
```

### Bases de données (pattern 6-BDD)

| Base | Contenu |
|------|---------|
| `input.db` | Sources d'entrée |
| `lifecycle-core.db` | Configuration runtime |
| `lifecycle-execution.db` | Logs d'exécution |
| `lifecycle-tools.db` | Définitions d'outils |
| `metadata.db` | Métriques système |
| `output.db` | Résultats et heartbeat |

### Protocole CDP (Chrome DevTools Protocol)

HOLOW communique avec Chrome via WebSocket sur le port 9222. Les commandes sont envoyées au format JSON-RPC.

---

## Fonctions SQL personnalisées

Pour les utilisateurs avancés, des fonctions SQL permettent d'appeler CDP directement :

```sql
-- Naviguer
SELECT cdp_call('Page.navigate', '{"url":"https://example.com"}');

-- Vérifier la connexion
SELECT cdp_connected();

-- Lister les pages ouvertes
SELECT cdp_list_pages();
```

---

## Contribuer

1. Fork le repo
2. Créez une branche : `git checkout -b ma-feature`
3. Commitez : `git commit -m "feat: ma feature"`
4. Pushez : `git push origin ma-feature`
5. Ouvrez une Pull Request

---

## Licence

MIT License - voir [LICENSE](LICENSE)

---

## Besoin d'aide ?

1. Ouvrez une issue sur GitHub
2. Décrivez votre problème avec :
   - Votre OS (Linux, Mac, Windows)
   - La commande qui échoue
   - Le message d'erreur complet
