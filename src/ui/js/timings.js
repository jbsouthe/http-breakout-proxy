const PHASE_COLORS = { dns:'#9e9e9e', tcp:'#f4a261', tls:'#2a9d8f', ttfb:'#e9c46a', resp:'#4caf50' };
const fmtMs = v => (v || 0) + ' ms';

export function renderTimingGanttForCapture(capture) {
    const hostEl = document.getElementById('timingGantt');
    const legendEl = document.getElementById('timingLegend');
    const statsEl = document.getElementById('timingStats');
    if (!hostEl || !legendEl || !statsEl) return;

    const dns  = Number(capture.dns_ms || 0);
    const tcp  = Number(capture.connect_ms || 0);
    const tls  = Number(capture.tls_ms || 0);
    const ttfb = Number(capture.ttfb_ms || 0);
    const resp = Number(capture.resp_read_ms || 0);
    let total  = Number(capture.total_ms || 0) || Number(capture.duration_ms || 0);
    const sumParts = dns + tcp + tls + ttfb + resp;
    if (!total) total = sumParts || 1;
    const roundedTotal = Math.ceil(total / 1000) * 1000;

    const phases = [
        {k:'dns',  label:'DNS',      ms:dns,  color:PHASE_COLORS.dns},
        {k:'tcp',  label:'TCP',      ms:tcp,  color:PHASE_COLORS.tcp},
        {k:'tls',  label:'TLS',      ms:tls,  color:PHASE_COLORS.tls},
        {k:'ttfb', label:'TTFB',     ms:ttfb, color:PHASE_COLORS.ttfb},
        {k:'resp', label:'Response', ms:resp, color:PHASE_COLORS.resp},
    ];

    legendEl.innerHTML = phases.filter(p=>p.ms>0).map(p => `
    <span class="legend-item"><span class="legend-swatch" style="background:${p.color}"></span>${p.label} (${fmtMs(p.ms)})</span>
  `).join('') || '<span class="small muted">No phase timing available.</span>';

    const W = hostEl.clientWidth || 600, H = hostEl.clientHeight || 120, PAD = 18, BAR_Y=40, BAR_H=24;
    const scale = (W - PAD*2) / (roundedTotal || 1);

    const gridLines = [];
    const ticks = Math.min(roundedTotal / 1000, 10);
    for (let i=0;i<=ticks;i++){
        const ms = (i / ticks) * roundedTotal;
        const x = PAD + (ms / roundedTotal) * (W - PAD * 2);
        gridLines.push({x, label: ms + ' ms'});
    }

    let offset=0; const rects=[];
    for(const p of phases){
        if (p.ms <= 0) continue;
        const x = PAD + offset * scale;
        const w = Math.max(1, p.ms * scale);
        rects.push({x, y:BAR_Y, w, h:BAR_H, color:p.color, title:`${p.label}: ${p.ms} ms`});
        offset += p.ms;
    }

    const svg=[];
    svg.push(`<svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" xmlns="http://www.w3.org/2000/svg">`);
    svg.push(`<rect x="0" y="0" width="${W}" height="${H}" fill="transparent"/>`);
    gridLines.forEach((g,i)=> {
        const isZero = (i === 0);
        svg.push(`<line x1="${g.x}" y1="20" x2="${g.x}" y2="${H-10}" stroke="${isZero ? '#bbb':'#ddd'}" stroke-width="${isZero?1.2:1}"/>`);
        svg.push(`<text x="${g.x+4}" y="18" font-size="11" fill="var(--muted)">${g.label}</text>`);
    });
    rects.forEach(r => {
        svg.push(`<rect x="${r.x}" y="${r.y}" width="${r.w}" height="${r.h}" rx="6" ry="6" fill="${r.color}"><title>${r.title}</title></rect>`);
    });
    svg.push(`<rect x="${PAD}" y="${BAR_Y+BAR_H+10}" width="${Math.max(1, total * scale)}" height="6" rx="3" ry="3" fill="#ccc"><title>Total: ${total} ms</title></rect>`);
    svg.push(`<text x="${PAD}" y="${BAR_Y+BAR_H+26}" font-size="11" fill="var(--muted)">Total: ${total} ms</text>`);
    svg.push(`</svg>`);
    hostEl.innerHTML = svg.join('');

    const h2 = !!capture.h2, reused = !!capture.reused_conn, addr = capture.server_addr || '';
    statsEl.textContent = `DNS ${dns} • TCP ${tcp} • TLS ${tls} • TTFB ${ttfb} • RESP ${resp} • TOTAL ${total} ms` +
        (addr ? `  •  ${addr}` : '') + (h2 ? '  •  h2' : '') + (reused ? '  •  reused' : '');
}