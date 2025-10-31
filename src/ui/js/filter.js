import { findMatchingRule } from './rules.js';

export function toLowerSafe(s){ return (s == null ? '' : String(s)).toLowerCase(); }
export function headersToPairs(obj){
    const pairs=[];
    if (obj && typeof obj === 'object') {
        for (const k of Object.keys(obj)) {
            const vs = Array.isArray(obj[k]) ? obj[k] : [obj[k]];
            pairs.push([String(k), String(vs.join(', '))]);
        }
    }
    return pairs;
}

export function parseMaybeRegex(term){
    const m = term.match(/^\/(.*)\/(\w*)$/);
    if (m) {
        try { return { regex: new RegExp(m[1], m[2]) }; } catch{ /* fall back */}
    }
    return { text: term.toLowerCase() };
}

export function matches(hay, q, equals=false){
    if (hay == null) return false;
    const s = String(hay);
    if (q.regex) return q.regex.test(s);
    if (equals) return s.toLowerCase() === q.text;
    return s.toLowerCase().includes(q.text);
}

export function matchHeaderTerm(pairs, nameQuery, valueQuery) {
    for (const [k,v] of pairs) {
        const okName  = !nameQuery  || matches(k, nameQuery);
        const okValue = !valueQuery || matches(v, valueQuery);
        if (okName && okValue) return true;
    }
    return false;
}

export function parseHeaderSpec(spec) {
    if (!spec) return { nameQ:null, valueQ:null };
    const eq = spec.indexOf('=');
    if (eq === -1) return { nameQ: parseMaybeRegex(spec), valueQ:null };
    return { nameQ: parseMaybeRegex(spec.slice(0,eq)), valueQ: parseMaybeRegex(spec.slice(eq+1)) };
}

// Core predicate used by list and rules
export function captureMatchesQuery(c, queryLine, opts = {}) {
    const terms = (queryLine || '').trim().split(/\s+/).filter(Boolean);
    const ignoreRuleName = !!opts.ignoreRuleName;
    if (!terms.length) return false;

    const url     = c.url || '';
    const method  = c.method || '';
    const statusS = String(c.response_status ?? '');
    const host    = (() => { try { return new URL(c.url).host; } catch { return ''; } })();

    const reqBody  = c.request_body  || '';
    const respBody = c.response_body || '';

    const reqHdrPairs  = headersToPairs(c.request_headers);
    const respHdrPairs = headersToPairs(c.response_headers);

    let ruleName = '';
    if (!ignoreRuleName) {
        const r = findMatchingRule(c);
        ruleName = (r && typeof r.name === 'string') ? r.name.toLowerCase() : '';
    }

    return terms.every(term => {
        if (term.startsWith('color:') && !ignoreRuleName) {
            const q = parseMaybeRegex(term.slice(6)); return matches(ruleName, q, true);
        }
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