# Status des Browser Tools SQL

## ✅ Ce qui est fait

### Architecture complète en SQL (12 tools)
Tous les tools sont définis dans `schemas/browser-tools.sql` et chargés dans `holow-mcp.lifecycle-tools.db` :

1. **browser_navigate** - Navigation vers URL
2. **browser_click** - Cliquer sur élément (CSS selector)
3. **browser_type** - Taper texte dans input
4. **browser_evaluate** - Exécuter JavaScript
5. **browser_screenshot** - Capture d'écran (PNG/JPEG)
6. **browser_extract_content** - Extraire titre/texte/headings
7. **browser_parse_dom** - Parser boutons/liens/inputs actionnables
8. **browser_console_logs** - Lire console.log/error/warn
9. **browser_network_logs** - Voir requêtes HTTP
10. **browser_cookies_get** - Lister cookies
11. **browser_cookies_set** - Définir cookie
12. **browser_cookies_delete** - Supprimer cookie

### Hot reload fonctionnel
- Trigger SQL automatique sur INSERT/UPDATE dans `tool_definitions`
- Polling 2s via `hot_reload_flag.tools_dirty`
- Les 14 tools (12 SQL + 2 hardcodés) sont exposés dans `tools/list`

### Tables CDP créées
- `cdp_console_logs` - Cache des messages console
- `cdp_network_requests` - Cache des requêtes HTTP
- `cdp_session_state` - État de la session browser
- `cdp_commands` - Queue de commandes CDP (workaround pour fonction SQL)

### Code CDP Manager
- `internal/chromium/cdp_sql.go` - Gestionnaire connexion WebSocket persistante
- Méthodes: `EnsureConnected()`, `Call()`, `ProcessPendingCommands()`

## ⚠️ Ce qui manque pour que ça fonctionne

### Problème #1: Fonction SQL cdp_call() non disponible
**Cause**: ncruces/go-sqlite3 ne supporte pas encore `RegisterFunc()` pour créer des fonctions SQL custom.

**Solution temporaire**: Utiliser la table `cdp_commands` comme queue
- SQL INSERT une commande: `INSERT INTO cdp_commands (method, params) VALUES ('Page.navigate', '{"url":"..."}')`
- Go process la queue en arrière-plan via `CDPManager.ProcessPendingCommands()`
- SQL lit le résultat: `SELECT result FROM cdp_commands WHERE id = last_insert_rowid()`

**Solution finale**: Attendre ncruces/go-sqlite3 v0.21+ avec RegisterFunc, ou passer à modernc.org/sqlite (mais HOROS a migré vers ncruces).

### Problème #2: CDPManager pas intégré au serveur
Le serveur doit:
1. Créer un `CDPManager` au démarrage
2. Lancer `ProcessPendingCommands()` en goroutine (toutes les 100ms)
3. Passer le CDPManager aux tools qui en ont besoin

### Problème #3: Tools SQL doivent utiliser cdp_commands
Actuellement les tools appellent `SELECT cdp_call('Page.navigate', ...)` qui échoue.

Il faut changer le pattern en 2 étapes:
```sql
-- Étape 1: Insérer commande
INSERT INTO cdp_commands (method, params, status)
VALUES ('Page.navigate', '{"url":"https://..."}', 'pending');

-- Étape 2: Attendre et récupérer résultat
SELECT result FROM cdp_commands
WHERE id = last_insert_rowid() AND status = 'success';
```

## 🔧 Prochaines étapes

1. **Modifier server.go** pour intégrer CDPManager
   - Créer CDPManager au démarrage
   - Lancer goroutine ProcessPendingCommands()
   - Connecter au browser au port 9222

2. **Réécrire browser-tools.sql** avec pattern cdp_commands
   - Remplacer tous les `SELECT cdp_call(...)`
   - Utiliser INSERT + SELECT sur cdp_commands

3. **Tester le workflow complet**
   - Lancer Chromium avec `--remote-debugging-port=9222`
   - Appeler `browser_navigate` via MCP
   - Vérifier que la page se charge
   - Appeler `browser_extract_content`
   - Vérifier qu'on récupère le contenu

## 📊 Avantages de cette architecture

✅ **100% SQL** - Aucun code Go à modifier pour ajouter des actions browser
✅ **Hot reload** - Modifie un tool SQL → rechargé en 2s
✅ **Persistance CDP** - Connexion WebSocket maintenue entre appels
✅ **Cache événements** - Console logs et network requests persistent
✅ **LLM-créable** - Tu peux créer de nouveaux tools via INSERT SQL

## 🎯 Usage final (une fois terminé)

```sql
-- Créer un nouveau tool dynamiquement
INSERT INTO tool_definitions (name, description, input_schema, category)
VALUES (
    'browser_get_links',
    'Extract all links from current page',
    '{"type": "object", "properties": {}}',
    'browser'
);

INSERT INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES (
    'browser_get_links', 1, 'extract', 'sql',
    'INSERT INTO cdp_commands (method, params) VALUES (''Runtime.evaluate'', ''{"expression": "JSON.stringify(Array.from(document.querySelectorAll(\"a\")).map(a => a.href))"}'')'
);

-- Le tool apparaît automatiquement en 2 secondes dans tools/list !
```
