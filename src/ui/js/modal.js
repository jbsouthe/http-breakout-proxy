// Generic modal + specialized color rules manager UI.
// Expose: showPromptModal(), showRenameCaptureModal(), openColorRulesManager()

import { getColorRules, setColorRules, state } from './state.js';
import { saveRules, refreshRulesFromServer } from './rules.js';
import { renderList } from './list.js';
import { renderDetails, selectCapture } from './details.js';

export function showPromptModal(message, def=''){
    const res = window.showPromptModal ? window.showPromptModal(message, def) : Promise.resolve(prompt(message, def));
    return res;
}
export function showRenameCaptureModal(cur){
    const res = window.showRenameCaptureModal ? window.showRenameCaptureModal(cur) : Promise.resolve(prompt('Rename:', cur||''));
    return res;
}

// If you already have an HTML modal for color rules, wire it similarly to previous implementation.
// For brevity, we assume that code is already present. If you want me to port that here, say the word.
export async function openColorRulesManager() {
    const root   = document.getElementById('colorRulesModal');
    const dialog = root.querySelector('.modal-dialog');
    const table  = document.getElementById('crmTable').querySelector('tbody');
    const form   = document.getElementById('crmForm');
    const qEl    = document.getElementById('crmQuery');
    const cEl    = document.getElementById('crmColor');
    const nEl    = document.getElementById('crmNote');
    const eEl    = document.getElementById('crmEnabled');
    const pEl    = document.getElementById('crmPriority');
    const nameEl  = document.getElementById('crmName');
    const addBtn = document.getElementById('crmAdd');
    const saveBtn= document.getElementById('crmSave');
    const clrBtn = document.getElementById('crmClear');
    const status = document.getElementById('crmStatus');
    const colorDot  = document.getElementById('crmColorDot');
    const colorWell = document.getElementById('crmColorWell');

    if (!root) { console.error('ColorRules modal HTML missing'); return; }

    await refreshRulesFromServer();
    let rules = getColorRules();
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
        qEl.value = ''; cEl.value = ''; nEl.value = ''; eEl.checked = true; pEl.value = 0; nameEl.value = '';
        saveBtn.disabled = true;
        selIdx = -1;
        colorDot.style.background = 'transparent';
        if (colorWell) colorWell.value = '#000000';
        highlightRow();
    }

    function readEditor() {
        const pr = parseInt((pEl && pEl.value) ? pEl.value : '0', 10);
        return {
            query:   (qEl.value || '').trim(),
            name:    (nameEl.value || '').trim(),
            color:   (cEl.value || '').trim(),
            note:    (nEl.value || '').trim(),
            enabled: !!eEl.checked,
            priority: Number.isFinite(pr) ? pr : 0
        };
    }

    function writeEditor(rule) {
        qEl.value = rule.query || '';
        nameEl.value = rule.name || '';
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

            const tdName = document.createElement('td');
            tdName.textContent = r.name;

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
                saveRules(rules);
                renderTable();
                setStatus(rules[i].enabled ? 'Enabled' : 'Disabled');
                syncUIAfterChange();
            };

            btnDel.onclick = () => {
                // optional confirm; remove if you prefer one-click delete
                if (!confirm('Delete this rule? This cannot be undone.')) return;
                rules.splice(i, 1);
                saveRules(rules);
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
            tr.appendChild(tdName);
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
        if (state.selectedId) {
            const cur = (state.captures || []).find(x => x.id === state.selectedId);
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
        await saveRules(rules);
        // Re-fetch canonical, priority-sorted order from server
        await refreshRulesFromServer();
        rules = getColorRules();
        renderTable();
        clearEditor();
        setStatus('Added');
        syncUIAfterChange();
    };

    saveBtn.onclick = async () => {
        if (selIdx < 0 || selIdx >= rules.length) return;

        const r = readEditor();
        if (!r.query) { setStatus('Query is required'); qEl.focus(); return; }

        // Preserve the rule's ID
        const id = rules[selIdx].id || String(Date.now());
        rules[selIdx] = { ...rules[selIdx], ...r, id };

        // Persist full set; DO NOT reassign "rules" from the PUT response
        await saveRules(rules);

        // Re-fetch canonical (priority-sorted) list from server
        await refreshRulesFromServer();
        rules = getColorRules();

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

export function updateColorRuleNote(ruleId, newNote) {
    // 1) Read from the synchronous cache (never a Promise)
    const rules = getColorRules();
    if (!Array.isArray(rules) || rules.length === 0) return false;

    // 2) Locate target rule
    const idx = rules.findIndex(r => r && r.id === ruleId);
    if (idx < 0) return false;

    // 3) Update note (immutable write)
    const next = rules.slice();
    next[idx] = { ...next[idx], note: String(newNote || '').trim() };

    // 4) Persist asynchronously; do not block UI
    saveRules(next).catch(err => {
        console.error('[UI] saveRules(note) failed:', err);
    });

    // 5) Optimistically update in-memory cache for immediate UI coherence
    setColorRules(next);

    return true;
}

if (typeof window !== 'undefined') {
    window.openColorRulesManager = openColorRulesManager;
}