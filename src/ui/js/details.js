import { state, setSelectedId } from './state.js';
import { fetchCapture, deleteCapture, renameCapture } from './api.js';
import { findMatchingRule, updateColorRuleNote } from './rules.js';
import { buildCurlFromCapture, buildPythonFromCapture } from './exports.js';
import { renderTimingGanttForCapture } from './timings.js';
import { renderList, updateRowSelectionHighlight } from './list.js';

const DETAILS_SELECTOR = 'details'; // <-- ensure this matches your HTML

function getDetailsEl() {
    const el = document.getElementById(DETAILS_SELECTOR);
    if (!el) {
        console.warn(`[details] container not found for ${DETAILS_SELECTOR}`);
    }
    return el;
}


export function blankDetails() {
    renderDetails({
        id: null, method:'', url:'', response_status:'', duration_ms:'', time:'',
        request_headers:{}, response_headers:{}, request_body:'', response_body:''
    });
}

export async function selectCapture(id) {
    setSelectedId(id);
    const c = await fetchCapture(id);
    if (!c) { setSelectedId(null); blankDetails(); renderList(); return; }
    renderDetails(c);
    updateRowSelectionHighlight();
}

function pretty(s) { if (!s) return ''; try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return String(s); } }
function escapeHtml(s){ return s==null ? '' : String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
function detectLanguage(hdrs){
    const ct = (hdrs['Content-Type'] || hdrs['content-type'] || [])[0] || '';
    if (ct.includes('application/json')) return 'json';
    if (ct.includes('xml') || ct.includes('html')) return 'xml';
    return 'plaintext';
}
function renderCode(preEl, body, language) {
    const formatted = pretty(body);
    preEl.innerHTML = `<code class="language-${language}"></code>`;
    const codeEl = preEl.querySelector('code');
    codeEl.textContent = formatted;
    if (window.hljs) window.hljs.highlightElement(codeEl);
}

export function renderDetails(c) {
    const el = getDetailsEl();
    if (!el) return;
    const title = document.getElementById('titleLarge');
    const sub   = document.getElementById('subMeta');
    const reqHdrEl = document.getElementById('ov-req-headers');
    const respHdrEl= document.getElementById('ov-resp-headers');
    const ovReqBodyEl = document.getElementById('ov-req-body');
    const ovRespBodyEl= document.getElementById('ov-resp-body');
    const reqBodyEl = document.getElementById('req-body');
    const respBodyEl= document.getElementById('resp-body');
    const rawEl = document.getElementById('rawJson');
    const ovMethod = document.getElementById('ov-method');
    const ovURL = document.getElementById('ov-url');
    const ovStatus = document.getElementById('ov-status');
    const ovDuration = document.getElementById('ov-duration');
    const ovTime = document.getElementById('ov-time');

    const displayName = (c.name && c.name.trim()) ? c.name : `${c.method || ''} ${c.url || ''}`;
    if (title) title.textContent = displayName;
    if (sub)   sub.textContent = `Status: ${c.response_status ?? '-'} • Duration: ${c.duration_ms ?? '-'}ms • Captured: ${c.time ? new Date(c.time).toLocaleString() : '-'}`;

    renderHeaders(reqHdrEl, c.request_headers);
    renderHeaders(respHdrEl, c.response_headers);

    renderCode(ovReqBodyEl,  c.request_body, detectLanguage(c.request_headers));
    renderCode(ovRespBodyEl, c.response_body, detectLanguage(c.response_headers));
    renderCode(reqBodyEl,    c.request_body, detectLanguage(c.request_headers));
    renderCode(respBodyEl,   c.response_body, detectLanguage(c.response_headers));

    if (rawEl) rawEl.textContent = JSON.stringify(c, null, 2);
    if (ovMethod)   ovMethod.textContent = c.method || '';
    if (ovURL)      ovURL.textContent = c.url || '';
    if (ovStatus)   ovStatus.textContent = String(c.response_status || '');
    if (ovDuration) ovDuration.textContent = (c.duration_ms ?? '-') + ' ms';
    if (ovTime)     ovTime.textContent = c.time ? new Date(c.time).toLocaleString() : '-';

    // Hooks
    const deleteBtn = document.getElementById('deleteBtn');
    if (deleteBtn) deleteBtn.onclick = () => onDelete(c.id);

    const renameBtn = document.getElementById('renameBtn');
    if (renameBtn) renameBtn.onclick = () => onRename(c);

    const downloadBtn = document.getElementById('downloadBtn');
    if (downloadBtn) downloadBtn.onclick = () => downloadResponseBody(c);

    const copyCurlBtn = document.getElementById('copyCurlBtn');
    if (copyCurlBtn) copyCurlBtn.onclick = async () => {
        try { await navigator.clipboard.writeText(buildCurlFromCapture(c)); copyCurlBtn.textContent='Copied!'; setTimeout(()=>copyCurlBtn.textContent='Copy cURL',900);} catch(e){ alert('Failed to copy cURL');}
    };

    const copyPythonBtn = document.getElementById('copyPythonBtn');
    if (copyPythonBtn) copyPythonBtn.onclick = async () => {
        try { await navigator.clipboard.writeText(buildPythonFromCapture(c)); copyPythonBtn.textContent='Copied!'; setTimeout(()=>copyPythonBtn.textContent='Copy Python',900);} catch(e){ alert('Failed to copy Python');}
    };

    // Color rule UI bits
    const badge = document.getElementById('colorRuleBadge');
    if (badge) {
        const rule = findMatchingRule(c);
        if (rule) {
            badge.style.display = '';
            badge.textContent = 'Tagged';
            badge.style.background = rule.color || '#eee';
        } else {
            badge.style.display = 'none';
            badge.textContent = '';
            badge.style.background = '';
        }
    }

    // Editable note
    const wrap  = document.getElementById('colorRuleNoteWrap');
    const input = document.getElementById('colorRuleNoteInput');
    const dot   = document.getElementById('colorRuleColorDot');
    const saved = document.getElementById('colorRuleSaveHint');
    if (wrap && input && dot && saved) {
        const rule = findMatchingRule(c);
        if (!rule) {
            wrap.style.display = 'none';
            input.value = '';
            dot.style.background = '#eee';
            saved.style.display = 'none';
        } else {
            wrap.style.display = 'flex';
            input.value = rule.note || '';
            dot.style.background = rule.color || '#eee';
            saved.style.display = 'none';
            let t=null;
            input.oninput = () => {
                if (t) clearTimeout(t);
                t=setTimeout(()=>{ updateColorRuleNote(rule.id, input.value); saved.style.display=''; setTimeout(()=>saved.style.display='none',700); }, 250);
            };
            input.onchange = () => { updateColorRuleNote(rule.id, input.value); saved.style.display=''; setTimeout(()=>saved.style.display='none',700); };
        }
    }

    // Timing chart + force scroll to top
    renderTimingGanttForCapture(c);

    const detailsPanel = document.querySelector('.details');
    if (c.grpc) {
        const grpcSec = renderGRPCSection(c.grpc);
        detailsPanel.appendChild(grpcSec);
    }
    if (detailsPanel) detailsPanel.scrollTo({ top: 0, behavior: 'instant' });
}

function renderHeaders(container, headers) {
    if (!container) return;
    if (!headers || typeof headers !== 'object') { container.innerHTML = '<div class="small">—</div>'; return; }
    const rows = Object.keys(headers).map(k =>
        `<div style="display:flex;gap:8px;margin-bottom:6px">
      <div style="width:160px;color:var(--muted)">${escapeHtml(k)}</div>
      <div style="flex:1"><pre style="margin:0;white-space:pre-wrap;font-family:var(--mono);font-size:13px;background:transparent;border:0;padding:0">${escapeHtml(String(headers[k]))}</pre></div>
     </div>`
    ).join('');
    container.innerHTML = rows || '<div class="small">—</div>';
}

async function onDelete(id) {
    if (!id) return;
    if (!confirm(`Delete capture #${id}?`)) return;
    try {
        const ok = await deleteCapture(id);
        // update local mirror if present
        const idx = state.captures.findIndex(x => x.id === id);
        if (idx >= 0) state.captures.splice(idx, 1);
        if (state.selectedId === id) {
            if (state.captures.length === 0) { setSelectedId(null); blankDetails(); }
            else {
                const next = state.captures[Math.min(idx, state.captures.length-1)];
                setSelectedId(next.id);
                renderList();
                selectCapture(next.id);
                return;
            }
        }
        renderList();
    } catch (e) {
        alert('Delete failed: ' + e);
    }
}

async function onRename(c) {
    const current = (c.name && c.name.trim()) ? c.name : `${c.method} ${c.url} [${c.response_status ?? '-'}]`;
    const next = window.showRenameCaptureModal ? await window.showRenameCaptureModal(current) : prompt('New name:', current);
    if (next == null) return;
    const trimmed = next.trim();
    if (!trimmed) return;
    try {
        const updated = await renameCapture(c.id, trimmed);
        const idx = state.captures.findIndex(x => x.id === updated.id);
        if (idx >= 0) state.captures[idx] = updated;
        renderDetails(updated);
        renderList();
        updateRowSelectionHighlight();
    } catch (e) {
        alert('Rename failed: '+e);
    }
}

function downloadResponseBody(c) {
    if (!c || !c.response_body) { alert('No response body'); return; }
    let contentType = '';
    try { contentType = (c.response_headers['Content-Type'] || c.response_headers['content-type'] || [])[0] || '';} catch {}
    let ext = 'bin';
    if (contentType.includes('json')) ext = 'json';
    else if (contentType.includes('html')) ext = 'html';
    else if (contentType.includes('xml')) ext = 'xml';
    else if (contentType.includes('text')) ext = 'txt';
    else if (contentType.includes('jpeg')) ext = 'jpg';
    else if (contentType.includes('png')) ext = 'png';
    else if (contentType.includes('pdf')) ext = 'pdf';
    const blob = new Blob([c.response_body], { type: contentType || 'application/octet-stream' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `response_${c.id||'capture'}.${ext}`;
    document.body.appendChild(a); a.click(); document.body.removeChild(a); URL.revokeObjectURL(url);
}

function decodeB64ToUtf8(b64) {
    try {
        const bytes = Uint8Array.from(atob(b64), c => c.charCodeAt(0));
        return new TextDecoder('utf-8', { fatal: false }).decode(bytes);
    } catch {
        return null;
    }
}

function renderGRPCSection(grpc) {
    const wrap = document.createElement('div');
    wrap.className = 'content';

    const h = document.createElement('div');
    h.innerHTML = `<div class="titleLarge">gRPC · <code>${escapeHtml(grpc.service_method || '')}</code></div>
    <div class="subMeta">encoding=${escapeHtml(grpc.encoding||'identity')}
      ${grpc.trailer_status ? ` · status=${grpc.trailer_status}` : ''}
      ${grpc.trailer_message ? ` · message=${escapeHtml(grpc.trailer_message)}` : ''}</div>`;
    wrap.appendChild(h);

    const mkFrameList = (title, frames) => {
        const sec = document.createElement('div');
        sec.style.marginTop = '10px';
        const t = document.createElement('div');
        t.className = 'h-title';
        t.textContent = `${title} (${frames.length})`;
        sec.appendChild(t);

        frames.forEach((f, idx) => {
            const box = document.createElement('div');
            box.className = 'boxed-text';
            const txt = decodeB64ToUtf8(f.base64);
            let body = txt;
            // Heuristic: pretty-print if JSON
            if (txt && txt.trim().startsWith('{') || txt && txt.trim().startsWith('[')) {
                try { body = JSON.stringify(JSON.parse(txt), null, 2); } catch {}
            }
            if (!body) body = `[${f.size} bytes binary]`;
            box.textContent = body;

            const meta = document.createElement('div');
            meta.className = 'subMeta';
            meta.style.marginTop = '4px';
            meta.textContent = `frame ${idx+1} · size=${f.size}${f.compressed ? ' · compressed' : ''}`;

            sec.appendChild(box);
            sec.appendChild(meta);
        });
        return sec;
    };

    if (grpc.req_frames && grpc.req_frames.length) {
        wrap.appendChild(mkFrameList('Request frames', grpc.req_frames));
    }
    if (grpc.resp_frames && grpc.resp_frames.length) {
        wrap.appendChild(mkFrameList('Response frames', grpc.resp_frames));
    }
    return wrap;
}