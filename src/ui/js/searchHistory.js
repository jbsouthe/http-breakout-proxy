import { setSearchHistory, getSearchHistory, state, setFilterText } from './state.js';
import { fetchSearches, putSearches, postSearch } from './api.js';
import { renderList } from './list.js';

const SH_KEY = 'searchHistory.v1';

export async function initSearchHistory(){
    try { setSearchHistory(await fetchSearches()); }
    catch {
        try { setSearchHistory(JSON.parse(localStorage.getItem(SH_KEY) || '[]')); } catch { setSearchHistory([]); }
    }
}

export async function bindSearchHistoryUI() {
    const input = document.getElementById('filterInput');
    const saveBtn = document.getElementById('saveSearchBtn');
    const btn = document.getElementById('historyBtn');
    const menu = document.getElementById('historyMenu');

    function closeMenu(){ menu.style.display='none'; menu.setAttribute('aria-hidden','true'); }
    function openMenu(){
        renderHistoryMenu();
        const rect = btn.getBoundingClientRect();
        menu.style.left = rect.left+'px';
        menu.style.top = (rect.bottom + window.scrollY)+'px';
        menu.style.display='block';
        menu.setAttribute('aria-hidden','false');
    }
    btn.addEventListener('click', ()=> menu.style.display==='block' ? closeMenu() : openMenu());
    document.addEventListener('click', (e) => { if (!menu.contains(e.target) && e.target !== btn) closeMenu(); });

    saveBtn.addEventListener('click', async () => {
        const q = (input.value || '').trim();
        if (!q) return;
        const label = prompt('Optional name for this filter:', '');
        await postSearch(q, label || '', false);
        try { setSearchHistory(await fetchSearches()); } catch { /* ignore */ }
        renderHistoryMenu();
    });

    input.addEventListener('keydown', async (e) => {
        if (e.key === 'Enter') {
            const q = (input.value || '').trim();
            if (!q) return;
            await postSearch(q, '', false);
        }
    });

    function renderHistoryMenu() {
        const arr = getSearchHistory();
        menu.innerHTML = '';
        if (!arr.length) { menu.innerHTML = '<div class="small" style="padding:8px;color:var(--muted)">No history yet</div>'; return; }

        arr.forEach((it) => {
            const row = document.createElement('div');
            row.className = 'history-item';
            row.innerHTML = `
        <span class="pin ${it.pinned ? 'pinned':''}" title="${it.pinned?'Unpin':'Pin'}">★</span>
        <div class="history-query" title="${it.query}">${escapeHtml(it.label || it.query)}</div>
        <div class="small" style="color:var(--muted)">${new Date(it.last_used).toLocaleString()}</div>
        <span class="trash" title="Delete">🗑</span>
      `;
            row.addEventListener('click', async (ev) => {
                if (ev.target.closest('.pin') || ev.target.closest('.trash')) return;
                input.value = it.query;
                setFilterText(it.query);
                renderList();
                closeMenu();
                await postSearch(it.query, it.label || '', it.pinned);
            });
            row.querySelector('.pin').addEventListener('click', async (ev) => {
                ev.stopPropagation();
                const arr = getSearchHistory();
                const idx = arr.findIndex(s => s.id === it.id);
                if (idx >= 0) {
                    arr[idx].pinned = !arr[idx].pinned;
                    try { await putSearches(arr); } catch { localStorage.setItem(SH_KEY, JSON.stringify(arr)); }
                    try { setSearchHistory(await fetchSearches()); } catch { setSearchHistory(arr); }
                    renderHistoryMenu();
                }
            });
            row.querySelector('.trash').addEventListener('click', async (ev) => {
                ev.stopPropagation();
                try {
                    const r = await fetch('/api/searches/'+encodeURIComponent(it.id), { method:'DELETE' });
                    if (!r.ok && r.status !== 204) throw new Error('HTTP '+r.status);
                    setSearchHistory(await fetchSearches());
                } catch {
                    const arr = getSearchHistory().filter(s => s.id !== it.id);
                    localStorage.setItem(SH_KEY, JSON.stringify(arr));
                    setSearchHistory(arr);
                }
                renderHistoryMenu();
            });
            menu.appendChild(row);
        });
    }

    function escapeHtml(s){ return s==null ? '' : String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
}