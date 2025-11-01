// Centralized state + a minimal event bus

export const state = {
    captures: [],          // newest-first
    selectedId: null,
    filterText: '',
    COLOR_RULES: [],       // []ColorRule {id,name?,query,color,note?,enabled,priority?}
    SEARCH_HISTORY: [],    // [{id,query,label?,pinned?,count,last_used,created_at}]
};

export function setCaptures(arr)      { state.captures = Array.isArray(arr) ? arr : []; }
export function setSelectedId(id)     { state.selectedId = id ?? null; }
export function setFilterText(s)      { state.filterText = (s || '').trim().toLowerCase(); }
export function setColorRules(arr)    { state.COLOR_RULES = Array.isArray(arr) ? arr : []; }
export function getColorRules()       { return state.COLOR_RULES.slice(); }
export function setSearchHistory(arr) { state.SEARCH_HISTORY = Array.isArray(arr) ? arr : []; }
export function getSearchHistory()    { return state.SEARCH_HISTORY.slice(); }

// Tiny pub/sub for UI updates
const topics = new Map(); // topic -> Set<fn>
export function on(topic, fn){ if(!topics.has(topic)) topics.set(topic,new Set()); topics.get(topic).add(fn); return ()=>off(topic,fn); }
export function off(topic, fn){ const s=topics.get(topic); if(s) s.delete(fn); }
export function emit(topic, payload){ const s=topics.get(topic); if(!s) return; for(const fn of s) fn(payload); }

export function upsertCapture(capture) {
    const arr = state.captures;
    const i = arr.findIndex(x => x.id === capture.id);

    if (i >= 0) {
        arr[i] = capture;
    } else {
        arr.unshift(capture); // NEWEST FIRST
    }

    window.dispatchEvent(new CustomEvent('captures-updated', { detail: { id: capture.id } }));
}

export function getCaptures() { return state.captures; } // avoid stale copies