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

function headerSkip(name){
    const n = String(name).toLowerCase();
    return ['host','content-length','accept-encoding','connection','proxy-connection','keep-alive','transfer-encoding','upgrade','content-encoding'].includes(n);
}

export function buildPythonFromCapture(c) {
    const method = (c.method || 'GET').toUpperCase();
    const url = c.url || '';
    const hdrs = c.request_headers || {};

    const headerLines = Object.keys(hdrs)
        .filter(k => !headerSkip(k))
        .map(k => {
            const v = Array.isArray(hdrs[k]) ? hdrs[k][0] : hdrs[k];
            return `    ${JSON.stringify(k)}: ${JSON.stringify(v)},`;
        }).join('\n');

    const rawBody = c.request_body || '';
    const isTruncated = /\b--truncated--\b/i.test(rawBody);

    let looksJson = false, parsed = null;
    try { parsed = JSON.parse(rawBody); looksJson = true; } catch{}

    const L=[];
    L.push('import requests','');
    L.push(`url = ${JSON.stringify(url)}`);
    L.push('headers = {', headerLines || '    # no headers', '}', '');

    if (!isTruncated && looksJson) {
        const pretty = JSON.stringify(parsed, null, 4)  // 4-space indentation for readability
            .replace(/"(\w+)":/g, '$1:')
            .replace(/: null/g, ': None')
            .replace(/: true/g, ': True')
            .replace(/: false/g, ': False');
        L.push(`payload = ${pretty}`);
        L.push('', `response = requests.${method.toLowerCase()}(url, headers=headers, json=payload)`);
    } else if (!isTruncated && rawBody) {
        L.push(`data = """${rawBody.replace(/"""/g,'\\"""')}"""`,'',`response = requests.${method.toLowerCase()}(url, headers=headers, data=data)`);
    } else {
        if (isTruncated) L.push('# NOTE: truncated body not included');
        L.push(`response = requests.${method.toLowerCase()}(url, headers=headers)`);
    }
    L.push('', 'print(response.status_code)', 'print(response.text)');
    return L.join('\n');
}