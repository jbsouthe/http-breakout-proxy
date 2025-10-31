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