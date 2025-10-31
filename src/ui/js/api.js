import { state, setCaptures, emit } from './state.js';

export async function fetchInitialData() {
    // Prefer /api/data if available
    try {
        const r = await fetch('/api/data');
        if (!r.ok) throw new Error('HTTP '+r.status);
        const data = await r.json(); // {captures, color_rules}
        setCaptures((data.captures || []).reverse());
        emit('captures:loaded');
        return data;
    } catch {
        // Fallback to legacy /api/captures
        const r2 = await fetch('/api/captures');
        if (r2.ok) {
            const arr = await r2.json();
            setCaptures((arr || []).reverse());
            emit('captures:loaded');
            return { captures: arr || [], color_rules: [] };
        }
        throw new Error('initial fetch failed');
    }
}

export function startSSE() {
    const es = new EventSource('/events');
    es.onmessage = (e) => {
        try {
            const c = JSON.parse(e.data);

            if (c.notes === 'paused') { emit('proxy:paused'); return; }
            if (c.notes === 'resumed') { emit('proxy:resumed'); return; }
            if (c.notes === 'cleared') { setCaptures([]); emit('captures:cleared'); return; }
            if (c.deleted) { emit('captures:deleted', c.id); return; }

            emit('captures:new-or-update', c);
        } catch (err) {
            console.error('SSE parse error', err);
        }
    };
    es.onerror = () => console.warn('SSE error');
}

export async function getPauseState(){
    const r = await fetch('/api/pause');
    if (!r.ok) throw new Error('HTTP '+r.status);
    const j = await r.json();
    return !!j.paused;
}
export async function setPauseState(nextPaused){
    const r = await fetch('/api/pause', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({paused:!!nextPaused}) });
    if (!r.ok) throw new Error('HTTP '+r.status);
    const j = await r.json();
    return !!j.paused;
}

export async function fetchCapture(id) {
    const r = await fetch(`/api/captures/${id}`);
    if (r.status === 404) return null;
    if (!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
}
export async function deleteCapture(id) {
    const r = await fetch(`/api/captures/${id}`, { method:'DELETE' });
    if (r.status === 204) return true;
    if (r.status === 404) return false;
    throw new Error('HTTP '+r.status);
}
export async function clearCaptures() {
    const r = await fetch('/api/captures', { method:'DELETE' });
    if (r.status !== 204) throw new Error('HTTP '+r.status);
}

export async function renameCapture(id, name) {
    const r = await fetch(`/api/captures/${id}`, {
        method:'PATCH', headers:{'Content-Type':'application/json'}, body: JSON.stringify({name})
    });
    if (!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
}

// Rules API
export async function fetchColorRules() {
    const r = await fetch('/api/rules');
    if (!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
}
export async function putColorRules(all) {
    const r = await fetch('/api/rules', {
        method:'PUT', headers:{'Content-Type':'application/json'}, body: JSON.stringify(all || [])
    });
    if (!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
}

// Search history API (server + fallback in searchHistory.js)
export async function fetchSearches() {
    const r = await fetch('/api/searches');
    if (!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
}
export async function putSearches(arr) {
    const r = await fetch('/api/searches', {
        method:'PUT', headers:{'Content-Type':'application/json'}, body: JSON.stringify(arr || [])
    });
    if (!r.ok) throw new Error('HTTP '+r.status);
}
export async function postSearch(query, label, pinned) {
    const r = await fetch('/api/searches', {
        method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({query, label:label||'', pinned:!!pinned})
    });
    if (!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
}