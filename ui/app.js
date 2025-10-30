let captures = [];   // newest-first
let selectedId = null;
let filterText = '';



function init() {
    bindUI();
    loadInitial();
    startSSE();
    setupTabs();
}

function setupTabs() {
    const tabs = document.querySelectorAll('.tab');
    tabs.forEach(tab => {
        tab.addEventListener('click', () => {
            const name = tab.getAttribute('data-tab');

            // Deactivate all tabs
            tabs.forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.tab-pane').forEach(p => {
                p.style.display = 'none';
            });

            // Activate this one
            tab.classList.add('active');
            const activePane = document.getElementById(`tab-${name}`);
            if (activePane) activePane.style.display = 'block';
        });
    });

    // Default: activate Overview
    if (tabs.length) tabs[0].click();
}

async function bindUI() {
    function debounce(fn, ms=120){ let t; return (...a)=>{ clearTimeout(t); t=setTimeout(()=>fn(...a), ms); }; }

    const filterInput = document.getElementById('filterInput');
    if (filterInput) {
        filterInput.addEventListener('input', debounce((e) => {
            filterText = (e.target.value || '').trim().toLowerCase();
            renderList();
        }, 120));
    }

    const clearBtn = document.getElementById('clearBtn');
    if (clearBtn) {
        clearBtn.addEventListener('click', clearAllCaptures);
    }

    const pauseBtn = document.getElementById('pauseBtn');
    if (pauseBtn) {
        // initialize label from server state
        const paused = await getPauseState();
        updatePauseButtonUI(paused);

        pauseBtn.addEventListener('click', async () => {
            const current = await getPauseState(); // read latest (in case multiple tabs)
            const next = !current;
            const applied = await setPauseState(next);
            if (applied === null) return; // error already logged
            updatePauseButtonUI(applied);
        });
    }

    const colorRulesBtn = document.getElementById('colorRulesBtn');
    if (colorRulesBtn) {
        colorRulesBtn.addEventListener('click', openColorRulesManager);
    }

}

async function loadInitial() {
    try {
        const r = await fetch('/api/data');
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const data = await r.json(); // { captures: [], color_rules: [] }
        captures = (data.captures || []).reverse();
        await refreshRulesFromServer();
        console.debug('[CRM] rules type at open:', Object.prototype.toString.call(COLOR_RULES));
        renderList();
        if (captures.length) selectCapture(captures[0].id);
    } catch (e) {
        console.error('initial fetch failed:', e);
        // Fallback: old servers without /api/data
        const r2 = await fetch('/api/captures');
        if (r2.ok) {
            const arr = await r2.json();
            captures = (arr || []).reverse();
            renderList();
            if (captures.length) selectCapture(captures[0].id);
        }
    }
}

// Clears all visible fields via renderDetails()
function blankDetails() {
    const emptyCapture = {
        id: null,
        method: '',
        url: '',
        response_status: '',
        duration_ms: '',
        time: '',
        request_headers: {},
        response_headers: {},
        request_body: '',
        response_body: ''
    };
    renderDetails(emptyCapture);
}

function startSSE() {
    const es = new EventSource('/events');
    es.onmessage = function(e){
        try {
            const c = JSON.parse(e.data);

            if (c.notes === 'paused') {
                updatePauseButtonUI(true);
                return;
            }
            if (c.notes === 'resumed') {
                updatePauseButtonUI(false);
                return;
            }

            if (c.notes === 'cleared') {
                captures = [];
                selectedId = null;
                blankDetails();
                renderList();
                return;
            }

            if (c.deleted) {
                // remove from local list
                const idx = (captures || []).findIndex(x => x.id === c.id);
                if (idx >= 0) captures.splice(idx, 1);

                // if currently selected, clear or select next
                if (selectedId === c.id) {
                    selectedId = null;
                    // optionally auto-select the newest remaining
                    if (captures.length) {
                        selectCapture(captures[0].id);
                    } else {
                        // clear detail panes
                        blankDetails();
                        document.getElementById('titleLarge').textContent = 'No capture selected';
                        document.getElementById('subMeta').textContent = '';
                        document.getElementById('req-body').textContent = '';
                        document.getElementById('resp-body').textContent = '';
                        document.getElementById('rawJson').textContent = '';
                    }
                }
                // re-render list to reflect removal
                renderList();
                return;
            }

            const existingIdx = captures.findIndex(x => x.id === c.id);
            if (existingIdx >= 0) {
                captures[existingIdx] = { ...captures[existingIdx], ...c };
                renderList();
                if (selectedId === c.id) renderDetails(captures[existingIdx]);
                return;
            }
            // normal new-capture event
            captures = captures || [];
            captures.unshift(c);
            if (captures.length > 2000) captures.pop();
            renderList();
            if (!selectedId) selectCapture(c.id);

        } catch (err) {
            console.error(err);
        }
    };
    es.onerror = () => console.warn('SSE error');
}

function renderList() {
    const list = document.getElementById('list');
    if (!list) return;
    list.innerHTML = '';

    const query = (filterText || '').trim();
    const filtered = !query ? captures : captures.filter(c => captureMatchesQuery(c, query));

    filtered.slice(0, 1000).forEach(c => {
        const row = document.createElement('div');
        row.className = 'row' + (c.id === selectedId ? ' selected' : '');
        row.setAttribute('data-id', String(c.id));
        row.setAttribute('role', 'option');
        row.setAttribute('aria-selected', c.id === selectedId ? 'true' : 'false');

        // Build DOM: swatch + text
        const sw = document.createElement('span');
        sw.className = 'swatch';

        const text = document.createElement('span');
        const display = c.name && c.name.trim()
            ? c.name
            : `${c.method} ${c.url} [${c.response_status ?? '-'}]`;
        text.textContent = display;

        // If matching rule, colorize swatch
        const rule = findMatchingRule(c);
        if (rule) { sw.style.background = rule.color || 'transparent'; }

        row.appendChild(sw);
        row.appendChild(text);

        row.onclick = () => selectCapture(c.id);
        row.tabIndex = 0;
        row.addEventListener('keydown', e => { if (e.key === 'Enter') selectCapture(c.id); });

        list.appendChild(row);
    });
}


// IDs this code expects in your HTML:
//   #titleLarge, #subMeta
//   #ov-req-headers, #ov-resp-headers
//   #req-body, #resp-body, #rawJson
// If your HTML uses different IDs, rename them here or in the HTML.

function pretty(s) {
    if (!s) return '';
    try { return JSON.stringify(JSON.parse(s), null, 2); }
    catch { return String(s); }
}

function renderHeaders(container, headers) {
    if (!container) { console.warn('[UI] Missing header container'); return; }
    if (!headers || typeof headers !== 'object') { container.innerHTML = '<div class="small">—</div>'; return; }
    const rows = Object.keys(headers).map(k =>
        `<div style="display:flex;gap:8px;margin-bottom:6px">
      <div style="width:160px;color:var(--muted)">${escapeHtml(k)}</div>
      <div style="flex:1"><pre style="margin:0;white-space:pre-wrap;font-family:var(--mono);font-size:13px;background:transparent;border:0;padding:0">${escapeHtml(String(headers[k]))}</pre></div>
     </div>`
    ).join('');
    container.innerHTML = rows || '<div class="small">—</div>';
}

function escapeHtml(s){ return s==null ? '' : String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

async function selectCapture(id) {
    selectedId = id;
    try {
        const r = await fetch(`/api/captures/${id}`);
        if (r.status === 404) {
            // stale selection after clear or deletion
            selectedId = null;
            blankDetails();
            renderList();
            return;
        }
        if (!r.ok) {
            console.error('fetch capture failed', r.status, r.statusText);
            return;
        }
        const c = await r.json();
        renderDetails(c);
        updateRowSelectionHighlight();
    } catch (e) {
        console.error('selectCapture error', e);
    }
}

function updateRowSelectionHighlight() {
    const rows = document.querySelectorAll('#list .row');
    rows.forEach(el => {
        const id = Number(el.getAttribute('data-id'));
        const selected = id === selectedId;
        el.classList.toggle('selected', selected);
        el.setAttribute('aria-selected', selected ? 'true' : 'false');
    });
}

const STATUS_TEXT = {
    200: "OK",
    201: "Created",
    202: "Accepted",
    204: "No Content",
    301: "Moved Permanently",
    302: "Found",
    400: "Bad Request",
    401: "Unauthorized",
    403: "Forbidden",
    404: "Not Found",
    500: "Internal Server Error",
    502: "Bad Gateway",
    503: "Service Unavailable",
    // add more as needed
};

function getStatusText(code) {
    return STATUS_TEXT[code] || "";
}

async function renderDetails(c) {
    // Map snake_case fields from Go JSON:
    const method = c.method;
    const url = c.url;
    const status = c.response_status;
    const dur = c.duration_ms;
    const t = c.time;
    const reqHeaders = c.request_headers;
    const respHeaders = c.response_headers;
    const reqBody = c.request_body;     // NOTICE: snake_case
    const respBody = c.response_body;   // NOTICE: snake_case

    const title = document.getElementById('titleLarge');
    const sub = document.getElementById('subMeta');
    const reqHdrEl = document.getElementById('ov-req-headers');
    const respHdrEl = document.getElementById('ov-resp-headers');
    const ovReqBodyEl = document.getElementById('ov-req-body');
    const ovRespBodyEl = document.getElementById('ov-resp-body');
    const reqBodyEl = document.getElementById('req-body');
    const respBodyEl = document.getElementById('resp-body');
    const rawEl = document.getElementById('rawJson');
    const ovMethod = document.getElementById('ov-method');
    const ovURL = document.getElementById('ov-url');
    const ovStatus = document.getElementById('ov-status');
    const ovDuration = document.getElementById('ov-duration');
    const ovTime = document.getElementById('ov-time');
    const deleteBtn = document.getElementById('deleteBtn');
    const downloadBtn = document.getElementById('downloadBtn');
    if (downloadBtn) {
        downloadBtn.onclick = () => downloadResponseBody(c);
    }
    const renameBtn = document.getElementById('renameBtn');
    if (renameBtn) {
        renameBtn.onclick = async () => {
            const current = (c.name && c.name.trim())
                ? c.name
                : `${c.method} ${c.url} [${c.response_status ?? '-'}]`;
            const next = await showPromptModal('New name for this capture:', current);
            if (next == null) return; // cancelled
            const trimmed = next.trim();

            try {
                const r = await fetch(`/api/captures/${c.id}`, {
                    method: 'PATCH',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ name: trimmed })
                });
                if (!r.ok) { alert(`Rename failed: ${r.status} ${r.statusText}`); return; }
                const updated = await r.json();

                // update local state
                const idx = captures.findIndex(x => x.id === updated.id);
                if (idx >= 0) captures[idx] = updated;

                renderDetails(updated);
                renderList();
                updateRowSelectionHighlight();
            } catch (e) {
                console.error('rename error', e);
                alert('Rename error: ' + e);
            }
        };
    }

    const copyCurlBtn = document.getElementById('copyCurlBtn');
    if (copyCurlBtn) {
        copyCurlBtn.textContent = 'Copy cURL';
        copyCurlBtn.title = 'Copy a curl command for this request';
        copyCurlBtn.onclick = async () => {
            try {
                const curl = buildCurlFromCapture(c);
                await navigator.clipboard.writeText(curl);
                // optional: brief visual feedback
                copyCurlBtn.textContent = 'Copied!';
                setTimeout(() => { copyCurlBtn.textContent = 'Copy cURL'; }, 900);
            } catch (e) {
                console.error('clipboard error', e);
                alert('Failed to copy to clipboard. See console for details.');
            }
        };
    }

    const copyPythonBtn = document.getElementById('copyPythonBtn');
    if (copyPythonBtn) {
        copyPythonBtn.onclick = async () => {
            try {
                const py = buildPythonFromCapture(c);
                await navigator.clipboard.writeText(py);
                copyPythonBtn.textContent = 'Copied!';
                setTimeout(() => { copyPythonBtn.textContent = 'Copy Python'; }, 900);
            } catch (e) {
                console.error('clipboard error', e);
                alert('Failed to copy Python snippet.');
            }
        };
    }

    // Color rule badge (note)
    const badge = document.getElementById('colorRuleBadge');
    if (badge) {
        const rule = findMatchingRule(c);
        if (rule) {
            badge.style.display = '';
            badge.textContent = 'Tagged';
            badge.style.background = rule.color || '#eee';
            // pick readable text color if you like; for now, keep default
        } else {
            badge.style.display = 'none';
            badge.textContent = '';
            badge.style.background = '';
        }
    }

    // Assert DOM presence
    if (!title || !sub || !reqHdrEl || !respHdrEl || !reqBodyEl || !respBodyEl || !rawEl || !ovMethod || !ovURL || !ovStatus || !ovDuration || !ovTime) {
        console.error('[UI] Missing one or more detail elements',
            {title, sub, reqHdrEl, respHdrEl, reqBodyEl, respBodyEl, rawEl, ovMethod, ovURL, ovStatus, ovDuration, ovTime});
        return;
    }

    const displayName = (c.name && c.name.trim())
        ? c.name
        : `${method || ''} ${url || ''}`;
    title.textContent = displayName;
    sub.textContent = `Status: ${status ?? '-'} • Duration: ${dur ?? '-'}ms • Captured: ${t ? new Date(t).toLocaleString() : '-'}`;

    renderHeaders(reqHdrEl, reqHeaders);
    renderHeaders(respHdrEl, respHeaders);

    renderCode(ovReqBodyEl, reqBody, detectLanguage(c.request_headers));
    renderCode(ovRespBodyEl, respBody, detectLanguage(c.response_headers));
    renderCode(reqBodyEl, reqBody, detectLanguage(c.request_headers));
    renderCode(respBodyEl, respBody, detectLanguage(c.response_headers));

    rawEl.textContent = JSON.stringify(c, null, 2);
    ovMethod.textContent = method
    ovURL.textContent = url;
    ovStatus.textContent = status + " " + getStatusText(status);
    ovDuration.textContent = (dur ?? '-') + ' ms';
    ovTime.textContent = t ? new Date(t).toLocaleString() : '-';
    if (deleteBtn) {
        deleteBtn.onclick = () => deleteCapture(c.id, { confirmFirst: true });
    }

    // Editable color-note for matching rule
    (function applyEditableColorNote(capture) {
        const wrap   = document.getElementById('colorRuleNoteWrap');
        const input  = document.getElementById('colorRuleNoteInput');
        const dot    = document.getElementById('colorRuleColorDot');
        const saved  = document.getElementById('colorRuleSaveHint');

        if (!wrap || !input || !dot || !saved) return;

        const rule = findMatchingRule(capture);
        if (!rule) {
            wrap.style.display = 'none';
            input.value = '';
            dot.style.background = '#eee';
            saved.style.display = 'none';
            return;
        }

        // Show and populate
        wrap.style.display = 'flex';
        input.value = rule.note || '';
        dot.style.background = rule.color || '#eee';
        saved.style.display = 'none';

        // Debounced persistence (avoid stacking multiple listeners by resetting)
        input.oninput = null;
        input.onchange = null;

        let t = null;
        input.oninput = () => {
            if (t) clearTimeout(t);
            const v = input.value;
            t = setTimeout(() => {
                if (updateColorRuleNote(rule.id, v)) {
                    saved.style.display = '';
                    setTimeout(() => (saved.style.display = 'none'), 700);
                    // Refresh list row text if it uses the rule note anywhere (optional)
                    renderList();
                }
            }, 250);
        };

        input.onchange = () => {
            // final sync on blur/enter
            const v = input.value;
            if (updateColorRuleNote(rule.id, v)) {
                saved.style.display = '';
                setTimeout(() => (saved.style.display = 'none'), 700);
                renderList();
            }
        };
    })(c);

    // Force scroll-to-top of the details panel
    const detailsPanel = document.querySelector('.details');
    if (detailsPanel) detailsPanel.scrollTo({ top: 0, behavior: 'instant' });
}

function renderCode(preEl, body, language) {
    const formatted = pretty(body);
    preEl.innerHTML = `<code class="language-${language}"></code>`;
    const codeEl = preEl.querySelector('code');
    codeEl.textContent = formatted;
    hljs.highlightElement(codeEl);
}

function detectLanguage(headers) {
    const ct = (headers['Content-Type'] || headers['content-type'] || [])[0] || '';
    if (ct.includes('application/json')) return 'json';
    if (ct.includes('xml') || ct.includes('html')) return 'xml';
    return 'plaintext';
}

async function deleteCapture(id, { confirmFirst = true } = {}) {
    if (!id) return;
    if (confirmFirst && !window.confirm(`Delete capture #${id}? This cannot be undone.`)) return;

    try {
        const res = await fetch(`/api/captures/${id}`, { method: 'DELETE' });
        if (res.status === 204) {
            const idx = captures.findIndex(x => x.id === id);
            if (idx >= 0) {
                captures.splice(idx, 1);
                if (selectedId === id) {
                    // choose neighbor or clear
                    if (captures.length === 0) {
                        selectedId = null;
                        blankDetails();
                        renderList();
                    } else {
                        selectNeighborAfterRemoval(idx);
                    }
                } else {
                    renderList();
                }
            } else {
                // not found locally; just re-render
                renderList();
            }
            return;
        } else if (res.status === 404) {
            // Already gone (maybe via another client); refresh list
            const idx = (captures || []).findIndex(x => x.id === id);
            if (idx >= 0) { captures.splice(idx, 1); renderList(); }
        } else {
            console.error('Delete failed:', res.status, res.statusText);
            alert(`Delete failed: ${res.status} ${res.statusText}`);
        }
    } catch (e) {
        console.error('Delete error:', e);
        alert(`Delete error: ${e}`);
    }
}

function selectNeighborAfterRemoval(idxRemoved) {
    if (!Array.isArray(captures) || captures.length === 0) {
        selectedId = null;
        blankDetails();
        renderList();
        return;
    }
    // After splice, index `idxRemoved` now points to the *next* item (if any).
    if (idxRemoved < captures.length) {
        const nextId = captures[idxRemoved].id;
        selectedId = nextId;
        renderList();
        selectCapture(nextId);
        return;
    }
    // Otherwise select the last (previous neighbor)
    const prevId = captures[captures.length - 1].id;
    selectedId = prevId;
    renderList();
    selectCapture(prevId);
}

async function clearAllCaptures() {
    if (!confirm('Clear ALL captures? This cannot be undone.')) return;
    try {
        const res = await fetch('/api/captures', { method: 'DELETE' });
        if (res.status === 204) {
            captures = [];
            selectedId = null;
            blankDetails();   // <- blank the right panel
            renderList();     // <- remove selection highlight
        } else {
            alert(`Clear failed: ${res.status} ${res.statusText}`);
        }
    } catch (e) {
        console.error('clear error', e);
        alert(`Clear error: ${e}`);
    }
}

// --- Filtering helpers -------------------------------------------------------
function toLowerSafe(s){ return (s == null ? '' : String(s)).toLowerCase(); }

function headersToPairs(obj) {
    // obj: { "Header-Name": ["v1","v2"], ... }
    const pairs = [];
    if (obj && typeof obj === 'object') {
        for (const k of Object.keys(obj)) {
            const vs = Array.isArray(obj[k]) ? obj[k] : [obj[k]];
            pairs.push([String(k), String(vs.join(', '))]); // keep original case for regex
        }
    }
    return pairs; // array of [name, values]
}

// /pattern/flags  ->  { regex: /pattern/flags }
// other           ->  { text: 'lowercased text' }
function parseMaybeRegex(term) {
    const m = term.match(/^\/(.*)\/(\w*)$/);
    if (m) {
        try { return { regex: new RegExp(m[1], m[2]) }; }
        catch { /* fall through to text */ }
    }
    return { text: term.toLowerCase() };
}

function matches(hay, q, equals = false) {
    if (hay == null) return false;
    const s = String(hay);
    if (q.regex) return q.regex.test(s);
    if (equals) return s.toLowerCase() === q.text;
    return s.toLowerCase().includes(q.text);
}

// nameQuery/valueQuery can be regex or text-query objects from parseMaybeRegex()
function matchHeaderTerm(pairs, nameQuery, valueQuery) {
    for (const [k, v] of pairs) {
        const okName  = !nameQuery  || matches(k, nameQuery);
        const okValue = !valueQuery || matches(v, valueQuery);
        if (okName && okValue) return true;
    }
    return false;
}

// Parse header spec with optional "=value", returning {nameQ, valueQ}
// Supports regex on either side:   header:/^x-/i=/trace/i
function parseHeaderSpec(spec) {
    if (!spec) return { nameQ: null, valueQ: null };
    const eq = spec.indexOf('=');
    if (eq === -1) {
        return { nameQ: parseMaybeRegex(spec), valueQ: null };
    }
    const name  = spec.slice(0, eq);
    const value = spec.slice(eq + 1);
    return { nameQ: parseMaybeRegex(name), valueQ: parseMaybeRegex(value) };
}

async function getPauseState() {
    try {
        const r = await fetch('/api/pause');
        if (!r.ok) throw new Error('HTTP ' + r.status);
        const j = await r.json();
        return !!j.paused;
    } catch (e) {
        console.error('[UI] getPauseState error', e);
        return false;
    }
}

async function setPauseState(nextPaused) {
    try {
        const r = await fetch('/api/pause', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paused: !!nextPaused }),
        });
        if (!r.ok) throw new Error('HTTP ' + r.status);
        const j = await r.json();
        return !!j.paused;
    } catch (e) {
        console.error('[UI] setPauseState error', e);
        return null;
    }
}

function updatePauseButtonUI(paused) {
    const btn = document.getElementById('pauseBtn');
    if (!btn) return;
    btn.textContent = paused ? 'Resume' : 'Pause';
    btn.title = paused ? 'Resume capture' : 'Pause capture';
    // optional styling:
    // btn.classList.toggle('but-danger', paused);
}

function downloadResponseBody(c) {
    if (!c || !c.response_body) {
        alert('No response body to download.');
        return;
    }

    let dataStr = c.response_body;
    let contentType = '';

    // Extract from headers if available
    try {
        contentType =
            (c.response_headers['Content-Type'] ||
                c.response_headers['content-type'] ||
                [])[0] || '';
    } catch { /* ignore */ }

    // Infer a reasonable file extension
    let ext = 'bin';
    if (contentType.includes('json')) ext = 'json';
    else if (contentType.includes('html')) ext = 'html';
    else if (contentType.includes('xml')) ext = 'xml';
    else if (contentType.includes('text')) ext = 'txt';
    else if (contentType.includes('jpeg')) ext = 'jpg';
    else if (contentType.includes('png')) ext = 'png';
    else if (contentType.includes('pdf')) ext = 'pdf';

    // Construct blob for download
    const blob = new Blob([dataStr], { type: contentType || 'application/octet-stream' });
    const url = URL.createObjectURL(blob);
    const filename = `response_${c.id || 'capture'}.${ext}`;

    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

function shellQuote(s) {
    // conservative POSIX single-quote escaping
    if (s == null) return "''";
    const str = String(s);
    if (str === '') return "''";
    return `'${str.replace(/'/g, `'\\''`)}'`;
}

function shouldSkipHeader(name) {
    // hop-by-hop or auto-generated — do not replay
    const n = String(name).toLowerCase();
    return n === 'host'
        || n === 'content-length'
        || n === 'accept-encoding'
        || n === 'connection'
        || n === 'proxy-connection'
        || n === 'keep-alive'
        || n === 'transfer-encoding'
        || n === 'upgrade';
}

function buildCurlFromCapture(c) {
    const parts = ['curl', '-i', '-sS']; // show headers, fail loud
    const method = (c.method || 'GET').toUpperCase();

    if (method !== 'GET') parts.push('-X', shellQuote(method));

    // headers
    const hdrs = c.request_headers || {};
    Object.keys(hdrs).forEach((k) => {
        if (shouldSkipHeader(k)) return;
        const values = Array.isArray(hdrs[k]) ? hdrs[k] : [hdrs[k]];
        values.forEach(v => {
            parts.push('-H', shellQuote(`${k}: ${v}`));
        });
    });

    // body (only if present and method typically allows a body)
    const body = c.request_body || '';
    const hasBody = body.trim().length > 0 && !/^\s*--truncated--\s*$/i.test(body);
    if (hasBody && !['GET','HEAD'].includes(method)) {
        // Use --data-binary to preserve bytes; server-side capture is text so this is best-effort
        parts.push('--data-binary', shellQuote(body));
    }

    // URL last
    parts.push(shellQuote(c.url || ''));

    // pretty multiline render
    let out = '';
    for (let i = 0; i < parts.length; i++) {
        out += (i === 0 ? '' : ' \\\n  ') + parts[i];
    }
    return out;
}

function buildPythonFromCapture(c) {
    const method = (c.method || 'GET').toUpperCase();
    const url = c.url || '';
    const headers = c.request_headers || {};
    const body = c.request_body || '';

    // format headers
    const headerLines = Object.keys(headers)
        .filter(k => !shouldSkipHeader(k))
        .map(k => {
            const v = Array.isArray(headers[k]) ? headers[k][0] : headers[k];
            return `    ${JSON.stringify(k)}: ${JSON.stringify(v)},`;
        })
        .join('\n');

    const lines = [];
    lines.push('import requests');
    lines.push('');
    lines.push(`url = ${JSON.stringify(url)}`);
    if (headerLines) lines.push('headers = {\n' + headerLines + '\n}');
    if (body.trim()) lines.push(`data = ${JSON.stringify(body)}`);
    lines.push('');
    let req = `response = requests.${method.toLowerCase()}(url`;
    if (headerLines) req += ', headers=headers';
    if (body.trim()) req += ', data=data';
    req += ')';
    lines.push(req);
    lines.push('');
    lines.push('print(response.status_code)');
    lines.push('print(response.text)');
    return lines.join('\n');
}

// ---- Color rule model --------------------------
/*
Rule schema:
{
  id: string,
  query: string,   // uses same filter syntax as list filter (supports regex, headers, bodies, etc.)
  color: string,   // CSS color (e.g., '#e91e63' or 'hsl(210,80%,60%)')
  note: string,    // short label shown in details
  enabled: boolean
}
*/
// Synchronous cache (array) — never a Promise
let COLOR_RULES = [];

// Optional: setter if you mutate client-side then persist
function setColorRules(next) {
    COLOR_RULES = Array.isArray(next) ? next : [];
}
async function loadColorRules()  { COLOR_RULES = await fetchColorRules(); return COLOR_RULES; }
function getColorRulesSync()     { return COLOR_RULES.slice(); }
async function saveColorRules(nextRules) {
    // Normalize to array and update cache first
    COLOR_RULES = Array.isArray(nextRules) ? nextRules.slice() : [];
    // Persist; ignore response payload (usually {updated: N})
    await fetch('/api/rules', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(COLOR_RULES),
    });
}async function fetchColorRules() {
    const r = await fetch('/api/rules');
    if (!r.ok) throw new Error('HTTP '+r.status);
    return await r.json(); // []ColorRule
}
async function putColorRules(all) {
    const r = await fetch('/api/rules', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(all || []),
    });
    if (!r.ok) throw new Error('HTTP '+r.status);
    return await r.json();
}
// One-shot refresh from server; call during bootstrap and after edits
async function refreshRulesFromServer() {
    try {
        const r = await fetch('/api/rules');
        if (!r.ok) throw new Error('HTTP '+r.status);
        COLOR_RULES = await r.json();         // <-- array
        renderList();                         // repaint swatches
        if (selectedId) {
            const cur = (captures || []).find(x => x.id === selectedId);
            if (cur) renderDetails(cur);        // refresh badge/editor
        }
    } catch (e) {
        console.error('[UI] refreshRulesFromServer failed:', e);
        COLOR_RULES = []; // fail-safe
    }
}

// Evaluate rules in order; return the *first* matching enabled rule or null
function findMatchingRule(capture) {
    const rules = getColorRulesSync();
    if (!Array.isArray(rules) || rules.length === 0) return null;

    for (const r of rules) {
        if (!r || !r.enabled) continue;
        const q = (r.query || '').trim();
        if (!q) continue;
        if (captureMatchesQuery(capture, q)) return r;
    }
    return null;
}
// Decide if a capture matches a full query line (space-separated terms; AND semantics)
function captureMatchesQuery(c, queryLine) {
    const terms = (queryLine || '').trim().split(/\s+/).filter(Boolean);
    if (!terms.length) return false;

    // Local shorthands (as in renderList)
    const url     = c.url || '';
    const method  = c.method || '';
    const statusS = String(c.response_status ?? '');
    const host    = (() => { try { return new URL(c.url).host; } catch { return ''; } })();

    const reqBody  = c.request_body  || '';
    const respBody = c.response_body || '';

    const reqHdrPairs  = headersToPairs(c.request_headers);
    const respHdrPairs = headersToPairs(c.response_headers);

    return terms.every(term => {
        if (term.startsWith('method:')) {
            const q = parseMaybeRegex(term.slice(7)); return matches(method, q, true);
        }
        if (term.startsWith('status:')) {
            const spec = term.slice(7).toLowerCase();
            if (/^[1-5]$/.test(spec)) return statusS.startsWith(spec);
            const q = parseMaybeRegex(spec); return matches(statusS, q);
        }
        if (term.startsWith('host:')) {
            const q = parseMaybeRegex(term.slice(5)); return matches(host, q, true);
        }
        if (term.startsWith('url:')) {
            const q = parseMaybeRegex(term.slice(4)); return matches(url, q, true);
        }
        if (term.startsWith('body:')) {
            const q = parseMaybeRegex(term.slice(5));
            return matches(reqBody, q) || matches(respBody, q);
        }
        if (term.startsWith('req.body:')) {
            const q = parseMaybeRegex(term.slice(9)); return matches(reqBody, q);
        }
        if (term.startsWith('resp.body:')) {
            const q = parseMaybeRegex(term.slice(10)); return matches(respBody, q);
        }
        if (term.startsWith('header:')) {
            const { nameQ, valueQ } = parseHeaderSpec(term.slice(7));
            return matchHeaderTerm(reqHdrPairs, nameQ, valueQ) || matchHeaderTerm(respHdrPairs, nameQ, valueQ);
        }
        if (term.startsWith('req.header:')) {
            const { nameQ, valueQ } = parseHeaderSpec(term.slice(11));
            return matchHeaderTerm(reqHdrPairs, nameQ, valueQ);
        }
        if (term.startsWith('resp.header:')) {
            const { nameQ, valueQ } = parseHeaderSpec(term.slice(12));
            return matchHeaderTerm(respHdrPairs, nameQ, valueQ);
        }

        // default term: search everywhere
        const q = parseMaybeRegex(term);
        if (matches(url, q) || matches(method, q) || matches(statusS, q) || matches(host, q)) return true;
        if (matches(reqBody, q) || matches(respBody, q)) return true;
        if (matchHeaderTerm(reqHdrPairs, q, null)) return true;
        if (matchHeaderTerm(reqHdrPairs, null, q)) return true;
        if (matchHeaderTerm(respHdrPairs, q, null)) return true;
        if (matchHeaderTerm(respHdrPairs, null, q)) return true;

        return false;
    });
}

// === Color Rules Manager (rich modal) =======================================
async function openColorRulesManager() {
    const root   = document.getElementById('colorRulesModal');
    const dialog = root.querySelector('.modal-dialog');
    const table  = document.getElementById('crmTable').querySelector('tbody');
    const form   = document.getElementById('crmForm');
    const qEl    = document.getElementById('crmQuery');
    const cEl    = document.getElementById('crmColor');
    const nEl    = document.getElementById('crmNote');
    const eEl    = document.getElementById('crmEnabled');
    const pEl    = document.getElementById('crmPriority');
    const addBtn = document.getElementById('crmAdd');
    const saveBtn= document.getElementById('crmSave');
    const clrBtn = document.getElementById('crmClear');
    const status = document.getElementById('crmStatus');
    const colorDot  = document.getElementById('crmColorDot');
    const colorWell = document.getElementById('crmColorWell');

    if (!root) { console.error('ColorRules modal HTML missing'); return; }

    await refreshRulesFromServer();
    let rules = getColorRulesSync();
    if (!Array.isArray(rules)) { rules = []; }
    let selIdx = -1; // selected row index; -1 = none

    // Normalize arbitrary CSS color strings to computed rgb(a) form, or null if invalid.
    function normalizeCssColor(str) {
        if (!str) return null;
        const el = document.createElement('span');
        el.style.color = '#000';
        // try apply
        el.style.color = String(str);
        // If browser rejects it, style.color remains empty or unchanged; compute robustly:
        document.body.appendChild(el);
        const computed = getComputedStyle(el).color; // "rgb(r, g, b)" or "rgba(r,g,b,a)"
        document.body.removeChild(el);
        // Reject if computed is empty or default
        if (!computed || computed === 'rgb(0, 0, 0)' && !/^#?0+$|^black$/i.test(String(str).trim())) {
            // Heuristic: if input not explicitly black and computed is black, it might be invalid; still accept known tokens.
            // You can relax this if you want black to be accepted without ambiguity:
            if (!/^(#0+|black)$/i.test(String(str).trim())) return null;
        }
        return computed;
    }

    // Convert computed "rgb(...)" to 6-digit hex for the color well.
    function rgbToHex(rgb) {
        const m = rgb.match(/rgba?\s*\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/i);
        if (!m) return null;
        const toHex = (n) => ('0' + Math.max(0, Math.min(255, parseInt(n, 10))).toString(16)).slice(-2);
        return '#' + toHex(m[1]) + toHex(m[2]) + toHex(m[3]);
    }

    // Update the preview dot (and optionally color well) from the text field.
    function updateColorPreviewFromText() {
        const raw = cEl.value.trim();
        const norm = normalizeCssColor(raw);
        if (norm) {
            colorDot.style.background = norm;
            const hex = rgbToHex(norm);
            if (hex && colorWell) colorWell.value = hex;
            cEl.classList.remove('input-error');
        } else {
            colorDot.style.background = 'transparent';
            cEl.classList.add('input-error');
        }
    }

    // Update the text field from the color well (always hex) and preview.
    function updateFromWell() {
        if (!colorWell) return;
        const v = colorWell.value; // e.g., "#e91e63"
        if (!v) return;
        cEl.value = v;
        colorDot.style.background = v;
    }

    function setStatus(msg) {
        status.textContent = msg || '';
        if (msg) setTimeout(() => { if (status.textContent === msg) status.textContent = ''; }, 1500);
    }

    function clearEditor() {
        form.reset();
        qEl.value = ''; cEl.value = ''; nEl.value = ''; eEl.checked = true; pEl.value = 0;
        saveBtn.disabled = true;
        selIdx = -1;
        colorDot.style.background = 'transparent';
        if (colorWell) colorWell.value = '#000000';
        highlightRow();
    }

    function readEditor() {
        const pr = parseInt((pEl && pEl.value) ? pEl.value : '0', 10);
        return {
            query: (qEl.value || '').trim(),
            color: (cEl.value || '').trim(),
            note:  (nEl.value || '').trim(),
            priority: Number.isFinite(pr) ? pr : 0,
            enabled: !!eEl.checked
        };
    }

    function writeEditor(rule) {
        qEl.value = rule.query || '';
        cEl.value = rule.color || '';
        nEl.value = rule.note  || '';
        eEl.checked = !!rule.enabled;
        if (pEl) pEl.value = String(Number.isFinite(rule.priority) ? rule.priority : 0);
        // refresh preview + well
        updateColorPreviewFromText();
    }

    function highlightRow() {
        const rows = table.querySelectorAll('tr');
        rows.forEach((tr, i) => tr.classList.toggle('selected', i === selIdx));
    }

    function renderTable() {
        table.innerHTML = '';
        rules.forEach((r, i) => {
            const tr = document.createElement('tr');

            const tdState = document.createElement('td');
            tdState.className = 'state';
            tdState.textContent = r.enabled ? '✅' : '⛔';

            const tdPriority = document.createElement('td');
            tdPriority.textContent = Number.isFinite(r.priority) ? String(r.priority) : '0';

            const tdColor = document.createElement('td');
            const dot = document.createElement('span');
            dot.className = 'swatch-dot';
            dot.style.background = r.color || 'transparent';
            tdColor.appendChild(dot);

            const tdQuery = document.createElement('td');
            tdQuery.textContent = r.query || '';

            const tdNote = document.createElement('td');
            tdNote.textContent = r.note || '';

            const tdAct = document.createElement('td');
            tdAct.className = 'row-actions';

            const btnEdit = document.createElement('button');
            btnEdit.className = 'btn btn-muted';
            btnEdit.type = 'button';
            btnEdit.textContent = 'Edit';

            const btnToggle = document.createElement('button');
            btnToggle.className = 'btn btn-muted';
            btnToggle.type = 'button';
            btnToggle.textContent = r.enabled ? 'Disable' : 'Enable';

            const btnDel = document.createElement('button');
            btnDel.className = 'btn';
            btnDel.type = 'button';
            btnDel.textContent = 'Delete';
            btnDel.title = 'Delete this rule';

            btnEdit.onclick = () => {
                selIdx = i;
                writeEditor(rules[i]);
                saveBtn.disabled = false;
                highlightRow();
            };

            btnToggle.onclick = () => {
                rules[i].enabled = !rules[i].enabled;
                saveColorRules(rules);
                renderTable();
                setStatus(rules[i].enabled ? 'Enabled' : 'Disabled');
                syncUIAfterChange();
            };

            btnDel.onclick = () => {
                // optional confirm; remove if you prefer one-click delete
                if (!confirm('Delete this rule? This cannot be undone.')) return;
                rules.splice(i, 1);
                saveColorRules(rules);
                renderTable();
                // if the deleted row was selected, clear the editor
                if (selIdx === i) clearEditor();
                // keep selection in bounds
                if (selIdx > i) selIdx -= 1;
                highlightRow();
                setStatus('Deleted');
                syncUIAfterChange();
            };

            tdAct.appendChild(btnEdit);
            tdAct.appendChild(btnToggle);
            tdAct.appendChild(btnDel);

            tr.onclick = (ev) => {
                // avoid row click when clicking action buttons
                if (ev.target instanceof HTMLElement && ev.target.closest('.row-actions')) return;
                selIdx = i;
                writeEditor(rules[i]);
                saveBtn.disabled = false;
                highlightRow();
            };

            tr.appendChild(tdState);
            tr.appendChild(tdPriority);
            tr.appendChild(tdColor);
            tr.appendChild(tdQuery);
            tr.appendChild(tdNote);
            tr.appendChild(tdAct);
            table.appendChild(tr);
        });
        highlightRow();
    }

    function syncUIAfterChange() {
        // Repaint list swatches
        renderList();
        // Refresh details panel if a capture is selected
        if (selectedId) {
            const cur = (captures || []).find(x => x.id === selectedId);
            if (cur) renderDetails(cur);
        }
    }

    // Wire editor actions
    form.onsubmit = async (e) => {
        e.preventDefault();
        if (!Array.isArray(rules)) rules = [];
        const r = readEditor();
        if (!r.query) {
            setStatus('Query is required');
            qEl.focus();
            return;
        }
        const newRule = { id: String(Date.now()), ...r };
        rules.push(newRule);
        await saveColorRules(rules);
        // Re-fetch canonical, priority-sorted order from server
        await refreshRulesFromServer();
        rules = getColorRulesSync();
        renderTable();
        clearEditor();
        setStatus('Added');
        syncUIAfterChange();
    };

    saveBtn.onclick = async () => {
        if (selIdx < 0 || selIdx >= rules.length) return;
        const r = readEditor();
        if (!r.query) {
            setStatus('Query is required');
            qEl.focus();
            return;
        }
        rules[selIdx] = { ...rules[selIdx], ...r };
        await saveColorRules(rules);
        // Re-fetch canonical, priority-sorted order from server
        await refreshRulesFromServer();
        rules = getColorRulesSync();
        renderTable();
        saveBtn.disabled = true;
        setStatus('Saved');
        syncUIAfterChange();
    };

    clrBtn.onclick = clearEditor;

    // Keyboard shortcuts
    root.onkeydown = (e) => {
        if (e.key === 'Escape') { close(); }
    };

    // Open + focus mgmt
    function open() {
        root.setAttribute('aria-hidden', 'false');
        renderTable();
        clearEditor();
        qEl.focus();
    }
    function close() {
        root.setAttribute('aria-hidden', 'true');
        // detach simple click-to-close on backdrop/x
        root.removeEventListener('click', clickClose);
    }
    function clickClose(e){
        const t = e.target;
        if (t && t.dataset && t.dataset.close) close();
    }

    root.addEventListener('click', clickClose);
    // Live preview while typing
    cEl.addEventListener('input', updateColorPreviewFromText);

    // Sync when the color well changes
    if (colorWell) {
        colorWell.addEventListener('input', updateFromWell);
        colorWell.addEventListener('change', updateFromWell);
    }
    open();
}

function updateColorRuleNote(ruleId, newNote) {
    const rules = loadColorRules();
    const idx = rules.findIndex(r => r && r.id === ruleId);
    if (idx < 0) return false;
    rules[idx] = { ...rules[idx], note: String(newNote || '').trim() };
    saveColorRules(rules);
    return true;
}

// ---- Modal subsystem --------------------------------------------------------
(function initModalSubsystem(){
    const root   = document.getElementById('appModal');
    if (!root) return; // modal HTML not present
    const dialog = root.querySelector('.modal-dialog');
    const label  = document.getElementById('appModalLabel');
    const title  = document.getElementById('appModalTitle');
    const input  = document.getElementById('appModalInput');
    const okBtn  = document.getElementById('appModalOK');
    const cxBtn  = document.getElementById('appModalCancel');

    let resolveFn = null;
    let previousActive = null;

    function openModal({titleText, labelText, defaultValue, okText='OK', cancelText='Cancel'}) {
        return new Promise((resolve) => {
            resolveFn = resolve;
            previousActive = document.activeElement;

            title.textContent = titleText || 'Input';
            label.textContent = labelText || '';
            input.value = defaultValue ?? '';
            okBtn.textContent = okText;
            cxBtn.textContent = cancelText;

            root.setAttribute('aria-hidden', 'false');

            // Focus management
            setTimeout(() => {
                input.focus();
                input.select && input.select();
            }, 0);

            // Trap focus inside dialog
            function trap(e){
                if (e.key !== 'Tab') return;
                const focusables = dialog.querySelectorAll('button, [href], input, textarea, select, [tabindex]:not([tabindex="-1"])');
                if (!focusables.length) return;
                const first = focusables[0], last = focusables[focusables.length - 1];
                if (e.shiftKey && document.activeElement === first) { last.focus(); e.preventDefault(); }
                else if (!e.shiftKey && document.activeElement === last) { first.focus(); e.preventDefault(); }
            }
            root.addEventListener('keydown', trap);

            // Key handlers
            function keyHandler(e){
                if (e.key === 'Escape') { e.preventDefault(); close(null); return; }
                if (e.key === 'Enter')  { e.preventDefault(); close(input.value); return; }
            }
            root.addEventListener('keydown', keyHandler);

            // Click handlers
            root.addEventListener('click', clickClose);
            okBtn.onclick = () => close(input.value);
            cxBtn.onclick = () => close(null);

            function clickClose(e){
                const t = e.target;
                if (t && t.dataset && t.dataset.close) { close(null); }
            }

            function close(result){
                // cleanup listeners
                root.removeEventListener('keydown', keyHandler);
                root.removeEventListener('keydown', trap);
                root.removeEventListener('click', clickClose);
                okBtn.onclick = cxBtn.onclick = null;

                root.setAttribute('aria-hidden', 'true');
                resolveFn && resolveFn(result);
                resolveFn = null;

                // restore focus
                previousActive && previousActive.focus && previousActive.focus();
            }
        });
    }

    // Expose a simple prompt-like function
    window.showPromptModal = function(message, defaultValue=''){
        return openModal({
            titleText: 'Input',
            labelText: message || '',
            defaultValue
        });
    };

    // Optional more specific UX variants
    window.showRenameCaptureModal = function(currentName){
        return openModal({
            titleText: 'Rename Capture',
            labelText: 'Enter a new name for this capture:',
            defaultValue: currentName || '',
            okText: 'Rename'
        });
    };

})();

// If you used <script defer>, either:
//  - call init() here:
init();

// Or, if you skipped defer, use:
// window.addEventListener('DOMContentLoaded', init);