function shellQuote(s){ if (s == null) return "''"; const str=String(s); if (str==='') return "''"; return `'${str.replace(/'/g, `'\\''`)}'`; }
function shouldSkipHeader(name){
    const n = String(name).toLowerCase();
    return ['host','content-length','accept-encoding','connection','proxy-connection','keep-alive','transfer-encoding','upgrade','content-encoding'].includes(n);
}
function looksTruncated(s){ return /\b--truncated--\b/i.test(String(s||'')); }
function isPrintableAscii(s){ if (s == null) return true; for (let i=0;i<s.length;i++){ const c=s.charCodeAt(i); if(!(c===9||c===10||c===13||(c>=32&&c<=126))) return false; } return true; }

function formatCurlParts(parts){
    const out=[]; let current='';
    for (let i=0;i<parts.length;i++){
        const token = parts[i];
        if (i===0){ out.push(token); continue; }
        const prev = parts[i-1];
        if (prev === '-X' || prev === '-H' || prev.startsWith('--data')) current += ' ' + token;
        else if (token === '-H' || token.startsWith('--data') || token === '-X'){ if (current) out.push(current.trim()); current = token; }
        else current += ' ' + token;
    }
    if (current) out.push(current.trim());
    return out.map((s,i)=> i===0 ? s : ' \\\n  ' + s).join('');
}

export function buildCurlFromCapture(c) {
    const parts = ['curl', '-i', '-sS'];
    const method = (c.method || 'GET').toUpperCase();
    const url = c.url || '';
    if (method !== 'GET') parts.push('-X', method);

    const hdrs = c.request_headers || {};
    Object.keys(hdrs).forEach(k => {
        if (shouldSkipHeader(k)) return;
        const values = Array.isArray(hdrs[k]) ? hdrs[k] : [hdrs[k]];
        values.forEach(v => parts.push('-H', shellQuote(`${k}: ${String(v).replace(/[\r\n]+/g,' ')}`)));
    });

    const body = c.request_body || '';
    const hasBody = body && !looksTruncated(body) && !['GET','HEAD'].includes(method);
    if (hasBody) {
        const printable = isPrintableAscii(body);
        const seemsJSON = /^\s*[\[{]/.test(body) ||
            ((hdrs['Content-Type']||hdrs['content-type']||[])[0]||'').includes('application/json');
        if (printable && seemsJSON) parts.push('--data-raw', shellQuote(body));
        else parts.push('--data-binary', shellQuote(body));
    }
    parts.push(shellQuote(url));
    return formatCurlParts(parts);
}