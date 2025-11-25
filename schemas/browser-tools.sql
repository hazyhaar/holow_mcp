-- ============================================================================
-- TOOLS BROWSER pour HOLOW-MCP
-- Pattern: Tools SQL avec actions CDP via fonction Go cdp_call()
-- ============================================================================

-- Tool 1: browser_navigate
-- Navigation vers une URL avec attente de chargement
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_navigate',
    'Navigate to a URL and wait for page load',
    '{
        "type": "object",
        "properties": {
            "url": {"type": "string", "description": "URL to navigate to"},
            "wait_selector": {"type": "string", "description": "Optional CSS selector to wait for"}
        },
        "required": ["url"]
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_navigate', 1, 'enable_page', 'sql',
     'SELECT cdp_call(''Page.enable'', json_object())'),
    ('browser_navigate', 2, 'navigate', 'sql',
     'SELECT cdp_call(''Page.navigate'', json_object(''url'', ''{{url}}''))'),
    ('browser_navigate', 3, 'get_title', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(''expression'', ''document.title'', ''returnByValue'', 1))');

-- Tool 2: browser_screenshot
-- Capture d'écran de la page
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_screenshot',
    'Take a screenshot of the current page',
    '{
        "type": "object",
        "properties": {
            "format": {"type": "string", "enum": ["png", "jpeg"], "default": "png"},
            "quality": {"type": "integer", "minimum": 0, "maximum": 100, "default": 80},
            "full_page": {"type": "boolean", "default": false}
        }
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_screenshot', 1, 'capture', 'sql',
     'SELECT cdp_call(''Page.captureScreenshot'', json_object(''format'', ''{{format}}'', ''quality'', {{quality}}, ''captureBeyondViewport'', {{full_page}}))');

-- Tool 3: browser_evaluate
-- Exécuter du JavaScript sur la page
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_evaluate',
    'Execute JavaScript in the browser context',
    '{
        "type": "object",
        "properties": {
            "expression": {"type": "string", "description": "JavaScript code to execute"}
        },
        "required": ["expression"]
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_evaluate', 1, 'eval', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(''expression'', ''{{expression}}'', ''returnByValue'', 1))');

-- Tool 4: browser_console_logs
-- Récupérer les logs de la console (console.log, error, warn)
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_console_logs',
    'Get console logs (log/error/warn) from the browser',
    '{
        "type": "object",
        "properties": {
            "clear": {"type": "boolean", "default": false, "description": "Clear logs after reading"}
        }
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_console_logs', 1, 'enable_console', 'sql',
     'SELECT cdp_call(''Runtime.enable'', json_object())'),
    ('browser_console_logs', 2, 'get_logs', 'sql',
     'SELECT json_group_array(
         json_object(
             ''timestamp'', timestamp,
             ''level'', level,
             ''message'', message
         )
     ) FROM cdp_console_logs ORDER BY timestamp DESC LIMIT 100');

-- Tool 5: browser_network_logs
-- Récupérer les requêtes HTTP (Network panel)
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_network_logs',
    'Get HTTP requests from Network panel',
    '{
        "type": "object",
        "properties": {
            "filter": {"type": "string", "description": "Optional URL filter pattern"}
        }
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_network_logs', 1, 'enable_network', 'sql',
     'SELECT cdp_call(''Network.enable'', json_object())'),
    ('browser_network_logs', 2, 'get_requests', 'sql',
     'SELECT json_group_array(
         json_object(
             ''url'', url,
             ''method'', method,
             ''status'', status,
             ''timestamp'', timestamp
         )
     ) FROM cdp_network_requests
     WHERE ''{{filter}}'' = '''' OR url LIKE ''%{{filter}}%''
     ORDER BY timestamp DESC LIMIT 100');

-- Tool 6: browser_parse_dom
-- Parser le DOM pour extraire éléments actionnables (boutons, liens, forms)
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_parse_dom',
    'Parse DOM to extract actionable elements (buttons, links, forms)',
    '{
        "type": "object",
        "properties": {
            "element_types": {
                "type": "array",
                "items": {"type": "string", "enum": ["button", "link", "input", "form"]},
                "default": ["button", "link", "input"]
            }
        }
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_parse_dom', 1, 'extract_buttons', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''JSON.stringify(Array.from(document.querySelectorAll("button, input[type=button], input[type=submit]")).map(el => ({
             type: "button",
             text: el.textContent.trim(),
             id: el.id,
             class: el.className,
             selector: el.id ? "#" + el.id : "." + el.className.split(" ")[0]
         })))'',
         ''returnByValue'', 1
     ))'),
    ('browser_parse_dom', 2, 'extract_links', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''JSON.stringify(Array.from(document.querySelectorAll("a[href]")).map(el => ({
             type: "link",
             text: el.textContent.trim(),
             href: el.href,
             selector: el.id ? "#" + el.id : "a"
         })))'',
         ''returnByValue'', 1
     ))'),
    ('browser_parse_dom', 3, 'extract_inputs', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''JSON.stringify(Array.from(document.querySelectorAll("input:not([type=button]):not([type=submit]), textarea, select")).map(el => ({
             type: "input",
             name: el.name,
             id: el.id,
             inputType: el.type,
             placeholder: el.placeholder,
             value: el.value,
             selector: el.id ? "#" + el.id : (el.name ? `[name="${el.name}"]` : "input")
         })))'',
         ''returnByValue'', 1
     ))');

-- Tool 7: browser_extract_content
-- Extraire le contenu texte et structure de la page
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_extract_content',
    'Extract text content and structure from the page',
    '{
        "type": "object",
        "properties": {
            "include_metadata": {"type": "boolean", "default": true},
            "include_headings": {"type": "boolean", "default": true},
            "max_length": {"type": "integer", "default": 10000}
        }
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_extract_content', 1, 'get_title', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(''expression'', ''document.title'', ''returnByValue'', 1))'),
    ('browser_extract_content', 2, 'get_url', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(''expression'', ''window.location.href'', ''returnByValue'', 1))'),
    ('browser_extract_content', 3, 'get_text', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''document.body.innerText.substring(0, {{max_length}})'',
         ''returnByValue'', 1
     ))'),
    ('browser_extract_content', 4, 'get_headings', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''JSON.stringify(Array.from(document.querySelectorAll("h1, h2, h3")).map(h => ({
             level: h.tagName,
             text: h.textContent.trim()
         })))'',
         ''returnByValue'', 1
     ))');

-- Tool 8: browser_cookies_get
-- Récupérer les cookies
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_cookies_get',
    'Get all cookies from the current page',
    '{
        "type": "object",
        "properties": {
            "domain": {"type": "string", "description": "Optional domain filter"}
        }
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_cookies_get', 1, 'enable_network', 'sql',
     'SELECT cdp_call(''Network.enable'', json_object())'),
    ('browser_cookies_get', 2, 'get_cookies', 'sql',
     'SELECT cdp_call(''Network.getCookies'', json_object())');

-- Tool 9: browser_cookies_set
-- Définir un cookie
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_cookies_set',
    'Set a cookie in the browser',
    '{
        "type": "object",
        "properties": {
            "name": {"type": "string"},
            "value": {"type": "string"},
            "domain": {"type": "string"},
            "path": {"type": "string", "default": "/"}
        },
        "required": ["name", "value", "domain"]
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_cookies_set', 1, 'set_cookie', 'sql',
     'SELECT cdp_call(''Network.setCookie'', json_object(
         ''name'', ''{{name}}'',
         ''value'', ''{{value}}'',
         ''domain'', ''{{domain}}'',
         ''path'', ''{{path}}''
     ))');

-- Tool 10: browser_cookies_delete
-- Supprimer un cookie
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_cookies_delete',
    'Delete a cookie from the browser',
    '{
        "type": "object",
        "properties": {
            "name": {"type": "string"},
            "domain": {"type": "string"}
        },
        "required": ["name"]
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_cookies_delete', 1, 'delete_cookie', 'sql',
     'SELECT cdp_call(''Network.deleteCookies'', json_object(
         ''name'', ''{{name}}'',
         ''domain'', ''{{domain}}''
     ))');

-- Tool 11: browser_click
-- Cliquer sur un élément
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_click',
    'Click on an element using CSS selector',
    '{
        "type": "object",
        "properties": {
            "selector": {"type": "string", "description": "CSS selector of element to click"}
        },
        "required": ["selector"]
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_click', 1, 'click_element', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''document.querySelector("{{selector}}").click()'',
         ''returnByValue'', 0
     ))');

-- Tool 12: browser_type
-- Taper du texte dans un élément
INSERT OR REPLACE INTO tool_definitions
(name, description, input_schema, category, enabled, timeout_seconds, created_by)
VALUES (
    'browser_type',
    'Type text into an input element',
    '{
        "type": "object",
        "properties": {
            "selector": {"type": "string", "description": "CSS selector of input element"},
            "text": {"type": "string", "description": "Text to type"}
        },
        "required": ["selector", "text"]
    }',
    'browser',
    1,
    30,
    'system'
);

INSERT OR REPLACE INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('browser_type', 1, 'focus_element', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''document.querySelector("{{selector}}").focus()'',
         ''returnByValue'', 0
     ))'),
    ('browser_type', 2, 'set_value', 'sql',
     'SELECT cdp_call(''Runtime.evaluate'', json_object(
         ''expression'',
         ''document.querySelector("{{selector}}").value = "{{text}}"'',
         ''returnByValue'', 0
     ))');
