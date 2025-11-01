import { state, setSelectedId, emit } from './state.js';
import { captureMatchesQuery } from './filter.js';
import { findMatchingRule } from './rules.js';
import { selectCapture } from './details.js';

function buildRow(c) {
    const row = document.createElement('div');
    row.className = 'row' + (c.id === state.selectedId ? ' selected' : '');
    row.setAttribute('data-id', String(c.id));
    row.setAttribute('role', 'option');
    row.setAttribute('aria-selected', c.id === state.selectedId ? 'true' : 'false');

    const sw = document.createElement('span');
    sw.className = 'swatch';

    const text = document.createElement('span');
    const display = c.name && c.name.trim() ? c.name : `${c.method} ${c.url} [${c.response_status ?? '-'}]`;
    text.textContent = display;

    const rule = findMatchingRule(c);
    if (rule) sw.style.background = rule.color || 'transparent';

    row.appendChild(sw);
    row.appendChild(text);

    row.onclick = () => selectCapture(c.id);
    row.tabIndex = 0;
    row.addEventListener('keydown', e => {
        if (e.key === 'Enter') selectCapture(c.id);
    });

    if (c.grpc) {
        const badge = document.createElement('span');
        badge.className = 'badge';
        badge.textContent = 'gRPC';
        row.appendChild(badge);
    }
    return row;
}

export function renderList() {
    const list = document.getElementById('list');
    if (!list) return;
    list.innerHTML = '';

    const query = (state.filterText || '').trim();
    const filtered = !query ? state.captures : state.captures.filter(c => captureMatchesQuery(c, query));

    filtered.slice(0, 1000).forEach(c => {
        list.appendChild(buildRow(c));
    });
}

export function updateRowSelectionHighlight() {
    const rows = document.querySelectorAll('#list .row');
    rows.forEach(el => {
        const id = Number(el.getAttribute('data-id'));
        const selected = id === state.selectedId;
        el.classList.toggle('selected', selected);
        el.setAttribute('aria-selected', selected ? 'true' : 'false');
    });
}

export function prependRowIfVisible(capture) {
    const listEl = document.getElementById('list');
    if (!listEl) return false;
    // Only prepend if current filter would keep it visible
    if (state.filter && !captureMatchesQuery(capture, state.filter)) return false;

    const row = buildRow(capture);
    listEl.prepend(row);
    return true;
}