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

//
// ---- Public entrypoint the rest of the app calls ----
//

export async function initAnalysisUI() {
    // both are safe no-ops if elements are missing
    await initTemporalChart();
    await initRetryTable();
}