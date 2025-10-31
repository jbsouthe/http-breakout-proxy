import { state, setColorRules, getColorRules } from './state.js';
import { fetchColorRules, putColorRules } from './api.js';
import { captureMatchesQuery } from './filter.js';

export async function refreshRulesFromServer() {
    try {
        const arr = await fetchColorRules();
        setColorRules(arr || []);
    } catch (e) {
        console.error('[rules] fetch failed:', e);
        setColorRules([]);
    }
}

export async function saveRules(next) {
    setColorRules(next || []);
    try { await putColorRules(getColorRules()); }
    catch (e) { console.error('[rules] save failed:', e); }
}

export function findMatchingRule(capture) {
    const rules = getColorRules();
    if (!Array.isArray(rules) || rules.length === 0) return null;
    for (const r of rules) {
        if (!r || !r.enabled) continue;
        const q = (r.query || '').trim();
        if (!q) continue;
        if (captureMatchesQuery(capture, q, { ignoreRuleName:true })) return r;
    }
    return null;
}

export function updateColorRuleNote(ruleId, newNote) {
    const next = getColorRules().map(r => r && r.id === ruleId ? ({ ...r, note: String(newNote||'').trim() }) : r);
    setColorRules(next);
    // fire-and-forget
    saveRules(next);
}