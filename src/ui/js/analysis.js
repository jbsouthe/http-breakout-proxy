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
}