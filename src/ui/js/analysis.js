// analysis.js
//
// ---- Temporal latency chart ----
//
async function fetchTemporalBuckets() {
    const res = await fetch('/metrics/temporal');
    if (!res.ok) {
        throw new Error('Failed to fetch temporal metrics: ' + res.status);
    }
    return res.json();
}

function formatTimeLabel(ts) {
    const d = new Date(ts);
    return d.toLocaleTimeString();
}

export async function initTemporalChart() {
    const canvas = document.getElementById('temporalChart');
    if (!canvas) {
        return; // no chart on this page / tab
    }

    let data;
    try {
        data = await fetchTemporalBuckets();
    } catch (err) {
        console.error(err);
        return;
    }

    data.sort((a, b) => new Date(a.window_start) - new Date(b.window_start));

    const labels = data.map(b => formatTimeLabel(b.window_start));
    const meanLatency = data.map(b => b.mean_latency_ms);

    const ctx = canvas.getContext('2d');

    // Chart is provided globally by Chart.js (loaded via <script> in HTML)
    // eslint-disable-next-line no-undef
    new Chart(ctx, {
        type: 'line',
        data: {
            labels,
            datasets: [
                {
                    label: 'Mean latency (ms)',
                    data: meanLatency,
                    fill: false,
                    tension: 0.2,
                    pointRadius: 0,
                    borderWidth: 2
                }
            ]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            scales: {
                x: {
                    title: {
                        display: true,
                        text: 'Time'
                    }
                },
                y: {
                    title: {
                        display: true,
                        text: 'Latency (ms)'
                    },
                    beginAtZero: true
                }
            },
            plugins: {
                legend: {
                    display: true
                },
                tooltip: {
                    callbacks: {
                        label(ctx) {
                            const v = ctx.parsed.y;
                            return `Mean latency: ${v.toFixed(2)} ms`;
                        }
                    }
                }
            }
        }
    });
}

//
// ---- Retry / duplicate request table ----
//

async function fetchRetries(minCount = 2) {
    const res = await fetch(`/metrics/retries?min=${minCount}`);
    if (!res.ok) {
        throw new Error('Failed to fetch retry metrics: ' + res.status);
    }
    return res.json();
}

function formatTimestamp(ts) {
    if (!ts) return '';
    const d = new Date(ts);
    // Local date + time; tweak if you want UTC
    return d.toLocaleString();
}

function renderRetryTable(rows) {
    const table = document.getElementById('retryTable');
    if (!table) {
        return; // analysis tab might not have the table yet
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    // Clear old rows
    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 7;
        td.textContent = 'No recent retries detected.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    for (const row of rows) {
        const tr = document.createElement('tr');

        const clientCell = document.createElement('td');
        clientCell.textContent = row.client_ip || '';
        clientCell.title = row.user_agent || '';

        const methodCell = document.createElement('td');
        methodCell.textContent = row.method;

        const hostCell = document.createElement('td');
        hostCell.textContent = row.host;

        const pathCell = document.createElement('td');
        pathCell.textContent = row.path;

        const countCell = document.createElement('td');
        countCell.textContent = String(row.count);

        const statusCell = document.createElement('td');
        statusCell.textContent = `${row.last_status} (${row.last_outcome})`;

        const timeCell = document.createElement('td');
        timeCell.textContent = formatTimestamp(row.last_timestamp);

        tr.appendChild(clientCell);
        tr.appendChild(methodCell);
        tr.appendChild(hostCell);
        tr.appendChild(pathCell);
        tr.appendChild(countCell);
        tr.appendChild(statusCell);
        tr.appendChild(timeCell);

        tbody.appendChild(tr);
    }
}

async function initRetryTable() {
    const table = document.getElementById('retryTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        rows = await fetchRetries(2); // require at least one retry
    } catch (err) {
        console.error(err);
        return;
    }

    renderRetryTable(rows);
}

// ----- Per-route latency (backend: /metrics/latency/routes) -----

async function fetchRouteLatency(minCount = 10, limit = 100) {
    const params = new URLSearchParams();
    if (minCount > 0) params.set('min', String(minCount));
    if (limit > 0) params.set('limit', String(limit));

    const res = await fetch(`/metrics/latency/routes?${params.toString()}`);
    if (!res.ok) {
        throw new Error('Failed to fetch route latency metrics: ' + res.status);
    }
    return res.json();
}


function renderRouteLatencyTable(rows) {
    const table = document.getElementById('routeLatencyTable');
    if (!table) {
        return;
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    // Clear existing rows
    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 9;
        td.textContent = 'No routes with sufficient samples yet.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    for (const row of rows) {
        const tr = document.createElement('tr');

        const methodCell = document.createElement('td');
        methodCell.textContent = row.method;

        const hostCell = document.createElement('td');
        hostCell.textContent = row.host;

        const pathCell = document.createElement('td');
        pathCell.textContent = row.path;

        const countCell = document.createElement('td');
        countCell.textContent = String(row.count);

        const meanCell = document.createElement('td');
        meanCell.textContent = row.mean_ms.toFixed(2);

        const stddevCell = document.createElement('td');
        stddevCell.textContent = row.stddev_ms.toFixed(2);

        const minCell = document.createElement('td');
        minCell.textContent = row.min_ms.toFixed(2);

        const maxCell = document.createElement('td');
        maxCell.textContent = row.max_ms.toFixed(2);

        const lastCell = document.createElement('td');
        const last = row.last_updated ? new Date(row.last_updated) : null;
        lastCell.textContent = last ? last.toLocaleString() : '';

        tr.appendChild(methodCell);
        tr.appendChild(hostCell);
        tr.appendChild(pathCell);
        tr.appendChild(countCell);
        tr.appendChild(meanCell);
        tr.appendChild(stddevCell);
        tr.appendChild(minCell);
        tr.appendChild(maxCell);
        tr.appendChild(lastCell);

        tbody.appendChild(tr);
    }
}

async function initRouteLatencyTable() {
    const table = document.getElementById('routeLatencyTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        // Require at least 10 samples per route, return top 100 by mean_ms.
        rows = await fetchRouteLatency(10, 100);
    } catch (err) {
        console.error(err);
        return;
    }

    renderRouteLatencyTable(rows);
}

async function fetchClientErrors(minErrors = 3, limit = 100) {
    const params = new URLSearchParams();
    if (minErrors > 0) params.set('min', String(minErrors));
    if (limit > 0) params.set('limit', String(limit));

    const res = await fetch(`/metrics/errors/clients?${params.toString()}`);
    if (!res.ok) {
        throw new Error('Failed to fetch client error metrics: ' + res.status);
    }
    return res.json();
}

function renderClientErrorTable(rows) {
    const table = document.getElementById('clientErrorTable');
    if (!table) {
        return;
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    // Clear existing rows
    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 7;
        td.textContent = 'No clients with significant error streaks.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    for (const row of rows) {
        const tr = document.createElement('tr');

        const ipCell = document.createElement('td');
        ipCell.textContent = row.client_ip || '';

        const uaCell = document.createElement('td');
        // Keep cell concise, full UA in title
        uaCell.textContent = row.user_agent ? row.user_agent.slice(0, 40) + (row.user_agent.length > 40 ? '…' : '') : '';
        uaCell.title = row.user_agent || '';

        const c5xxCell = document.createElement('td');
        c5xxCell.textContent = String(row.consecutive_5xx);

        const c4xxCell = document.createElement('td');
        c4xxCell.textContent = String(row.consecutive_4xx);

        const cerrCell = document.createElement('td');
        cerrCell.textContent = String(row.consecutive_errors);

        const outcomeCell = document.createElement('td');
        outcomeCell.textContent = row.last_outcome || '';

        const lastCell = document.createElement('td');
        const last = row.last_updated ? new Date(row.last_updated) : null;
        lastCell.textContent = last ? last.toLocaleString() : '';

        tr.appendChild(ipCell);
        tr.appendChild(uaCell);
        tr.appendChild(c5xxCell);
        tr.appendChild(c4xxCell);
        tr.appendChild(cerrCell);
        tr.appendChild(outcomeCell);
        tr.appendChild(lastCell);

        tbody.appendChild(tr);
    }
}

async function initClientErrorTable() {
    const table = document.getElementById('clientErrorTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        rows = await fetchClientErrors(3, 100);
    } catch (err) {
        console.error(err);
        return;
    }

    renderClientErrorTable(rows);
}

async function fetchRouteSize(minCount = 10, limit = 100) {
    const params = new URLSearchParams();
    if (minCount > 0) params.set('min', String(minCount));
    if (limit > 0) params.set('limit', String(limit));

    const res = await fetch(`/metrics/size/routes?${params.toString()}`);
    if (!res.ok) {
        throw new Error('Failed to fetch route size metrics: ' + res.status);
    }
    return res.json();
}

function renderRouteSizeTable(rows) {
    const table = document.getElementById('routeSizeTable');
    if (!table) {
        return;
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 14;
        td.textContent = 'No routes with sufficient payload samples yet.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    for (const row of rows) {
        const tr = document.createElement('tr');

        const methodCell = document.createElement('td');
        methodCell.textContent = row.method;

        const hostCell = document.createElement('td');
        hostCell.textContent = row.host;

        const pathCell = document.createElement('td');
        pathCell.textContent = row.path;

        const reqCountCell = document.createElement('td');
        reqCountCell.textContent = String(row.req_count);

        const reqMeanCell = document.createElement('td');
        reqMeanCell.textContent = row.req_mean_bytes.toFixed(1);

        const reqStdCell = document.createElement('td');
        reqStdCell.textContent = row.req_std_bytes.toFixed(1);

        const reqMinCell = document.createElement('td');
        reqMinCell.textContent = String(row.req_min_bytes);

        const reqMaxCell = document.createElement('td');
        reqMaxCell.textContent = String(row.req_max_bytes);

        const resCountCell = document.createElement('td');
        resCountCell.textContent = String(row.res_count);

        const resMeanCell = document.createElement('td');
        resMeanCell.textContent = row.res_mean_bytes.toFixed(1);

        const resStdCell = document.createElement('td');
        resStdCell.textContent = row.res_std_bytes.toFixed(1);

        const resMinCell = document.createElement('td');
        resMinCell.textContent = String(row.res_min_bytes);

        const resMaxCell = document.createElement('td');
        resMaxCell.textContent = String(row.res_max_bytes);

        const lastCell = document.createElement('td');
        const last = row.last_updated ? new Date(row.last_updated) : null;
        lastCell.textContent = last ? last.toLocaleString() : '';

        tr.appendChild(methodCell);
        tr.appendChild(hostCell);
        tr.appendChild(pathCell);
        tr.appendChild(reqCountCell);
        tr.appendChild(reqMeanCell);
        tr.appendChild(reqStdCell);
        tr.appendChild(reqMinCell);
        tr.appendChild(reqMaxCell);
        tr.appendChild(resCountCell);
        tr.appendChild(resMeanCell);
        tr.appendChild(resStdCell);
        tr.appendChild(resMinCell);
        tr.appendChild(resMaxCell);
        tr.appendChild(lastCell);

        tbody.appendChild(tr);
    }
}

async function initRouteSizeTable() {
    const table = document.getElementById('routeSizeTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        rows = await fetchRouteSize(10, 100);
    } catch (err) {
        console.error(err);
        return;
    }

    renderRouteSizeTable(rows);
}

async function fetchEndpointAnomalies(limit = 100) {
    const params = new URLSearchParams();
    if (limit > 0) params.set('limit', String(limit));

    const res = await fetch(`/metrics/methods/anomalies?${params.toString()}`);
    if (!res.ok) {
        throw new Error('Failed to fetch endpoint anomaly metrics: ' + res.status);
    }
    return res.json();
}

function renderEndpointAnomalyTable(rows) {
    const table = document.getElementById('endpointAnomalyTable');
    if (!table) {
        return;
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 10;
        td.textContent = 'No anomalous endpoints detected yet.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    const formatTs = ts => {
        if (!ts) return '';
        const d = new Date(ts);
        return d.toLocaleString();
    };

    const formatStatusMap = statusMap => {
        if (!statusMap) return '';
        const entries = Object.entries(statusMap);
        if (!entries.length) return '';
        // sort by code
        entries.sort((a, b) => Number(a[0]) - Number(b[0]));
        return entries.map(([code, count]) => `${code}×${count}`).join(', ');
    };

    for (const row of rows) {
        const tr = document.createElement('tr');

        const methodCell = document.createElement('td');
        methodCell.textContent = row.method;

        const hostCell = document.createElement('td');
        hostCell.textContent = row.host;

        const pathCell = document.createElement('td');
        pathCell.textContent = row.path;

        const countCell = document.createElement('td');
        countCell.textContent = String(row.count);

        const nonStdCell = document.createElement('td');
        nonStdCell.textContent = row.non_standard_method ? 'yes' : '';

        const entropyCell = document.createElement('td');
        entropyCell.textContent = row.high_entropy_path ? 'yes' : '';

        const rareCell = document.createElement('td');
        rareCell.textContent = row.rare ? 'yes' : '';

        const statusCell = document.createElement('td');
        statusCell.textContent = formatStatusMap(row.status_count);

        const firstCell = document.createElement('td');
        firstCell.textContent = formatTs(row.first_seen);

        const lastCell = document.createElement('td');
        lastCell.textContent = formatTs(row.last_seen);

        tr.appendChild(methodCell);
        tr.appendChild(hostCell);
        tr.appendChild(pathCell);
        tr.appendChild(countCell);
        tr.appendChild(nonStdCell);
        tr.appendChild(entropyCell);
        tr.appendChild(rareCell);
        tr.appendChild(statusCell);
        tr.appendChild(firstCell);
        tr.appendChild(lastCell);

        tbody.appendChild(tr);
    }
}

async function initEndpointAnomalyTable() {
    const table = document.getElementById('endpointAnomalyTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        rows = await fetchEndpointAnomalies(100);
    } catch (err) {
        console.error(err);
        return;
    }

    renderEndpointAnomalyTable(rows);
}

async function fetchClientFingerprints(minChanges = 1, limit = 100) {
    const params = new URLSearchParams();
    if (minChanges >= 0) params.set('min_changes', String(minChanges));
    if (limit > 0) params.set('limit', String(limit));

    const res = await fetch(`/metrics/clients/fingerprints?${params.toString()}`);
    if (!res.ok) {
        throw new Error('Failed to fetch client fingerprint metrics: ' + res.status);
    }
    return res.json();
}

function renderClientFingerprintTable(rows) {
    const table = document.getElementById('clientFingerprintTable');
    if (!table) {
        return;
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 11;
        td.textContent = 'No clients with UA/TLS drift detected yet.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    const fmtTs = ts => {
        if (!ts) return '';
        const d = new Date(ts);
        return d.toLocaleString();
    };

    for (const row of rows) {
        const tr = document.createElement('tr');

        const ipCell = document.createElement('td');
        ipCell.textContent = row.client_ip || '';

        const hintCell = document.createElement('td');
        hintCell.textContent = row.client_hint || '';

        const obsCell = document.createElement('td');
        obsCell.textContent = String(row.observation_count);

        const uaCell = document.createElement('td');
        const ua = row.current_ua || '';
        uaCell.textContent = ua.length > 60 ? ua.slice(0, 57) + '…' : ua;
        uaCell.title = ua;

        const variantsCell = document.createElement('td');
        variantsCell.textContent = String(row.ua_variant_count);

        const uaChangeCell = document.createElement('td');
        uaChangeCell.textContent = String(row.ua_change_count);

        const tlsChangeCell = document.createElement('td');
        tlsChangeCell.textContent = String(row.tls_change_count);

        const uaDriftCell = document.createElement('td');
        uaDriftCell.textContent = row.has_ua_drift ? 'yes' : '';

        const tlsDriftCell = document.createElement('td');
        tlsDriftCell.textContent = row.has_tls_drift ? 'yes' : '';

        const firstCell = document.createElement('td');
        firstCell.textContent = fmtTs(row.first_seen);

        const lastCell = document.createElement('td');
        lastCell.textContent = fmtTs(row.last_seen);

        tr.appendChild(ipCell);
        tr.appendChild(hintCell);
        tr.appendChild(obsCell);
        tr.appendChild(uaCell);
        tr.appendChild(variantsCell);
        tr.appendChild(uaChangeCell);
        tr.appendChild(tlsChangeCell);
        tr.appendChild(uaDriftCell);
        tr.appendChild(tlsDriftCell);
        tr.appendChild(firstCell);
        tr.appendChild(lastCell);

        tbody.appendChild(tr);
    }
}

async function initClientFingerprintTable() {
    const table = document.getElementById('clientFingerprintTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        rows = await fetchClientFingerprints(1, 100);
    } catch (err) {
        console.error(err);
        return;
    }

    renderClientFingerprintTable(rows);
}

async function fetchAuthCookieStability(minRequests = 5, minChanges = 1, limit = 100) {
    const params = new URLSearchParams();
    if (minRequests >= 0) params.set('min_requests', String(minRequests));
    if (minChanges >= 0) params.set('min_changes', String(minChanges));
    if (limit > 0) params.set('limit', String(limit));

    const res = await fetch(`/metrics/authcookie/stability?${params.toString()}`);
    if (!res.ok) {
        throw new Error('Failed to fetch auth/cookie stability metrics: ' + res.status);
    }
    return res.json();
}

function renderAuthCookieTable(rows) {
    const table = document.getElementById('authCookieTable');
    if (!table) {
        return;
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 13;
        td.textContent = 'No significant auth/cookie instability detected yet.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    const fmtTs = ts => {
        if (!ts) return '';
        const d = new Date(ts);
        return d.toLocaleString();
    };

    for (const row of rows) {
        const tr = document.createElement('tr');

        const ipCell = document.createElement('td');
        ipCell.textContent = row.client_ip || '';

        const hostCell = document.createElement('td');
        hostCell.textContent = row.host || '';

        const uaCell = document.createElement('td');
        const ua = row.user_agent || '';
        uaCell.textContent = ua.length > 60 ? ua.slice(0, 57) + '…' : ua;
        uaCell.title = ua;

        const totalCell = document.createElement('td');
        totalCell.textContent = String(row.total_requests);

        const presentCell = document.createElement('td');
        presentCell.textContent = String(row.auth_present_count);

        const missingCell = document.createElement('td');
        missingCell.textContent = String(row.auth_missing_count);

        const authChangeCell = document.createElement('td');
        authChangeCell.textContent = String(row.auth_change_count);

        const cookieChangeCell = document.createElement('td');
        cookieChangeCell.textContent = String(row.cookie_pattern_change_count);

        const authFlapCell = document.createElement('td');
        authFlapCell.textContent = row.has_auth_flapping ? 'yes' : '';

        const cookieDriftCell = document.createElement('td');
        cookieDriftCell.textContent = row.has_cookie_drift ? 'yes' : '';

        const patternCell = document.createElement('td');
        patternCell.textContent = row.current_cookie_pattern || '';

        const firstCell = document.createElement('td');
        firstCell.textContent = fmtTs(row.first_seen);

        const lastCell = document.createElement('td');
        lastCell.textContent = fmtTs(row.last_seen);

        tr.appendChild(ipCell);
        tr.appendChild(hostCell);
        tr.appendChild(uaCell);
        tr.appendChild(totalCell);
        tr.appendChild(presentCell);
        tr.appendChild(missingCell);
        tr.appendChild(authChangeCell);
        tr.appendChild(cookieChangeCell);
        tr.appendChild(authFlapCell);
        tr.appendChild(cookieDriftCell);
        tr.appendChild(patternCell);
        tr.appendChild(firstCell);
        tr.appendChild(lastCell);

        tbody.appendChild(tr);
    }
}

async function initAuthCookieTable() {
    const table = document.getElementById('authCookieTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        rows = await fetchAuthCookieStability(5, 1, 100);
    } catch (err) {
        console.error(err);
        return;
    }

    renderAuthCookieTable(rows);
}

async function fetchResponseProfiles(minCount = 5, minChanges = 1, limit = 100) {
    const params = new URLSearchParams();
    if (minCount >= 0) params.set('min_count', String(minCount));
    if (minChanges >= 0) params.set('min_changes', String(minChanges));
    if (limit > 0) params.set('limit', String(limit));

    const res = await fetch(`/metrics/response/profile?${params.toString()}`);
    if (!res.ok) {
        throw new Error('Failed to fetch response profile metrics: ' + res.status);
    }
    return res.json();
}

function renderResponseProfileTable(rows) {
    const table = document.getElementById('responseProfileTable');
    if (!table) {
        return;
    }
    const tbody = table.querySelector('tbody');
    if (!tbody) {
        return;
    }

    while (tbody.firstChild) {
        tbody.removeChild(tbody.firstChild);
    }

    if (!rows || !rows.length) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 12;
        td.textContent = 'No routes with significant content-type drift or entropy mix yet.';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    const fmtTs = ts => {
        if (!ts) return '';
        const d = new Date(ts);
        return d.toLocaleString();
    };

    for (const row of rows) {
        const tr = document.createElement('tr');

        const methodCell = document.createElement('td');
        methodCell.textContent = row.method;

        const hostCell = document.createElement('td');
        hostCell.textContent = row.host;

        const pathCell = document.createElement('td');
        pathCell.textContent = row.path;

        const countCell = document.createElement('td');
        countCell.textContent = String(row.count);

        const ctCell = document.createElement('td');
        ctCell.textContent = row.primary_content_type || '';

        const ctChangeCell = document.createElement('td');
        ctChangeCell.textContent = String(row.content_type_change_count);

        const highCell = document.createElement('td');
        highCell.textContent = String(row.high_entropy_count);

        const lowCell = document.createElement('td');
        lowCell.textContent = String(row.low_entropy_count);

        const driftCell = document.createElement('td');
        driftCell.textContent = row.has_content_type_drift ? 'yes' : '';

        const mixCell = document.createElement('td');
        mixCell.textContent = row.has_entropy_mix ? 'yes' : '';

        const firstCell = document.createElement('td');
        firstCell.textContent = fmtTs(row.first_seen);

        const lastCell = document.createElement('td');
        lastCell.textContent = fmtTs(row.last_seen);

        tr.appendChild(methodCell);
        tr.appendChild(hostCell);
        tr.appendChild(pathCell);
        tr.appendChild(countCell);
        tr.appendChild(ctCell);
        tr.appendChild(ctChangeCell);
        tr.appendChild(highCell);
        tr.appendChild(lowCell);
        tr.appendChild(driftCell);
        tr.appendChild(mixCell);
        tr.appendChild(firstCell);
        tr.appendChild(lastCell);

        tbody.appendChild(tr);
    }
}

async function initResponseProfileTable() {
    const table = document.getElementById('responseProfileTable');
    if (!table) {
        return;
    }

    let rows;
    try {
        rows = await fetchResponseProfiles(5, 1, 100);
    } catch (err) {
        console.error(err);
        return;
    }

    renderResponseProfileTable(rows);
}



//
// ---- Public entrypoint the rest of the app calls ----
//

export async function initAnalysisUI() {
    // both are safe no-ops if elements are missing
    await initTemporalChart();
    await initRetryTable();
    await initRouteLatencyTable();
    await initClientErrorTable();
    await initRouteSizeTable();
    await initEndpointAnomalyTable();
    await initClientFingerprintTable();
    await initAuthCookieTable();
    await initResponseProfileTable();
}