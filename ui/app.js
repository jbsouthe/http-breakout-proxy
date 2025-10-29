let captures = [];   // newest-first
let selectedId = null;
let filterText = '';

const API_LIST = '/api/captures';
const API_GET  = (id) => `/api/captures/${id}`;


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

}

async function loadInitial() {
    try {
        const r = await fetch(API_LIST);
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const arr = await r.json();
        captures = (arr || []).reverse(); // newest first
        renderList();
        if (captures.length) {
            selectCapture(captures[0].id);
        }
    } catch (e) {
        console.error('initial fetch failed:', e);
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
    const terms = query.split(/\s+/).filter(Boolean);

    const filtered = !query ? captures : captures.filter(c => {
        // Canonical fields (keep originals for regex; add lowercased fast paths when needed)
        const url     = c.url || '';
        const method  = c.method || '';
        const statusS = String(c.response_status ?? '');
        const host    = (() => { try { return new URL(c.url).host; } catch { return ''; } })();

        const reqBody  = c.request_body  || '';
        const respBody = c.response_body || '';

        const reqHdrPairs  = headersToPairs(c.request_headers);
        const respHdrPairs = headersToPairs(c.response_headers);

        return terms.every(term => {
            // keyed operators (method:, status:, host:, url:)
            if (term.startsWith('method:')) {
                const q = parseMaybeRegex(term.slice(7));
                return matches(method, q);
            }
            if (term.startsWith('status:')) {
                const spec = term.slice(7).toLowerCase();
                // class (e.g., "4" → any 4xx) vs. exact (e.g., "404")
                if (/^[1-5]$/.test(spec)) return statusS.startsWith(spec);
                const q = parseMaybeRegex(spec);
                return matches(statusS, q);
            }
            if (term.startsWith('host:')) {
                const q = parseMaybeRegex(term.slice(5));
                return matches(host, q);
            }
            if (term.startsWith('url:')) {
                const q = parseMaybeRegex(term.slice(4));
                return matches(url, q);
            }

            // bodies (req/resp or combined)
            if (term.startsWith('body:')) {
                const q = parseMaybeRegex(term.slice(5));
                return matches(reqBody, q) || matches(respBody, q);
            }
            if (term.startsWith('req.body:')) {
                const q = parseMaybeRegex(term.slice(9));
                return matches(reqBody, q);
            }
            if (term.startsWith('resp.body:')) {
                const q = parseMaybeRegex(term.slice(10));
                return matches(respBody, q);
            }

            // headers (req/resp or combined). Support name=value with regex on each side.
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

            // DEFAULT TERM: search *everywhere* (broad, but efficient)
            const q = parseMaybeRegex(term);
            // Cheap scalars first
            if (matches(url, q) || matches(method, q) || matches(statusS, q) || matches(host, q)) return true;
            if (matches(reqBody, q) || matches(respBody, q)) return true;
            // Headers: names and values
            if (matchHeaderTerm(reqHdrPairs, q, null)) return true;
            if (matchHeaderTerm(reqHdrPairs, null, q)) return true;
            if (matchHeaderTerm(respHdrPairs, q, null)) return true;
            if (matchHeaderTerm(respHdrPairs, null, q)) return true;

            return false;
        });
    });

    filtered.slice(0, 1000).forEach(c => {
        const row = document.createElement('div');
        row.className = 'row' + (c.id === selectedId ? ' selected' : '');
        row.setAttribute('data-id', String(c.id));
        row.setAttribute('role', 'option');            // a11y
        row.setAttribute('aria-selected', c.id === selectedId ? 'true' : 'false');
        const display = c.name && c.name.trim()
            ? c.name
            : `${c.method} ${c.url} [${c.response_status ?? '-'}]`;
        row.textContent = display;
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

function renderDetails(c) {
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
            const next = window.prompt('New name for this capture:', current);
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

function matches(hay, q) {
    if (hay == null) return false;
    const s = String(hay);
    if (q.regex) return q.regex.test(s);
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

// If you used <script defer>, either:
//  - call init() here:
init();

// Or, if you skipped defer, use:
// window.addEventListener('DOMContentLoaded', init);