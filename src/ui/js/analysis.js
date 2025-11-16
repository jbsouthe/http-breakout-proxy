// analysis.js

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
