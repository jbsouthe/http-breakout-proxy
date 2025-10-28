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

function bindUI() {
    const filterInput = document.getElementById('filterInput');
    if (filterInput) {
        filterInput.addEventListener('input', () => renderList());
    }
    // bind other buttons/handlers here...
}

async function loadInitial() {
    try {
        const r = await fetch(API_LIST);
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const arr = await r.json();
        window._captures = (arr || []).reverse(); // newest first
        renderList();
        if (window._captures.length) {
            selectCapture(window._captures[0].id);
        }
    } catch (e) {
        console.error('initial fetch failed:', e);
    }
}

function startSSE() {
    const es = new EventSource('/events');
    es.onmessage = (ev) => {
        try {
            const c = JSON.parse(ev.data);
            window._captures.unshift(c);
            if (window._captures.length > 2000) window._captures.pop();
            renderList();
        } catch (e) { console.error('SSE parse', e); }
    };
    es.onerror = () => console.warn('SSE error');
}

function renderList() {
    const list = document.getElementById('list');
    if (!list) return;
    list.innerHTML = '';
    (window._captures || []).slice(0, 1000).forEach(c => {
        const row = document.createElement('div');
        row.className = 'row';
        row.textContent = `${c.method} ${c.url} [${c.response_status ?? '-'}]`;
        row.onclick = () => selectCapture(c.id);
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

// --- REPLACE your selectCapture + renderDetails with these:

async function selectCapture(id) {
    console.log('[UI] selectCapture', id);
    try {
        const r = await fetch(`/api/captures/${id}`);
        if (!r.ok) {
            console.error('[UI] fetch capture failed', r.status, r.statusText);
            return;
        }
        const c = await r.json();
        console.log('[UI] capture', c);
        renderDetails(c);
    } catch (e) {
        console.error('[UI] selectCapture error', e);
    }
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

    // Assert DOM presence
    if (!title || !sub || !reqHdrEl || !respHdrEl || !reqBodyEl || !respBodyEl || !rawEl || !ovMethod || !ovURL || !ovStatus || !ovDuration || !ovTime) {
        console.error('[UI] Missing one or more detail elements',
            {title, sub, reqHdrEl, respHdrEl, reqBodyEl, respBodyEl, rawEl, ovMethod, ovURL, ovStatus, ovDuration, ovTime});
        return;
    }

    title.textContent = `${method || ''} ${url || ''}`;
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

// If you used <script defer>, either:
//  - call init() here:
init();

// Or, if you skipped defer, use:
// window.addEventListener('DOMContentLoaded', init);