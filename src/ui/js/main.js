import { state, setFilterText, setCaptures, setSelectedId } from './state.js';
import { fetchInitialData, startSSE, getPauseState, setPauseState, clearCaptures } from './api.js';
import { refreshRulesFromServer } from './rules.js';
import { renderList } from './list.js';
import { selectCapture, blankDetails, renderDetails } from './details.js';
import { bindSearchHistoryUI, initSearchHistory } from './searchHistory.js';
import { openColorRulesManager } from './modal.js';
import { startEventStream } from './sse.js';


function debounce(fn, ms=120){ let t; return (...a)=>{ clearTimeout(t); t=setTimeout(()=>fn(...a), ms); }; }

function setupTabs() {
    const tabs = document.querySelectorAll('.tab');
    tabs.forEach(tab => {
        tab.addEventListener('click', () => {
            const name = tab.getAttribute('data-tab');
            tabs.forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.tab-pane').forEach(p => p.style.display='none');
            tab.classList.add('active');
            const pane = document.getElementById(`tab-${name}`);
            if (pane) pane.style.display='block';
        });
    });
    if (tabs.length) tabs[0].click();
}

function updatePauseButtonUI(paused) {
    const btn = document.getElementById('pauseBtn');
    if (!btn) return;
    btn.textContent = paused ? 'Resume' : 'Pause';
    btn.title = paused ? 'Resume capture' : 'Pause capture';
}

async function bindUI() {
    const filterInput = document.getElementById('filterInput');
    if (filterInput) {
        filterInput.addEventListener('input', debounce((e) => {
            setFilterText(e.target.value || '');
            renderList();
        }, 120));
    }

    const clearBtn = document.getElementById('clearBtn');
    if (clearBtn) clearBtn.addEventListener('click', async () => {
        if (!confirm('Clear ALL captures? This cannot be undone.')) return;
        await clearCaptures();
        setCaptures([]);
        setSelectedId(null);
        blankDetails();
        renderList();
    });

    const pauseBtn = document.getElementById('pauseBtn');
    if (pauseBtn) {
        const paused = await getPauseState();
        updatePauseButtonUI(paused);
        pauseBtn.addEventListener('click', async () => {
            const current = await getPauseState();
            const next = !current;
            const applied = await setPauseState(next);
            updatePauseButtonUI(!!applied);
        });
    }

    const colorRulesBtn = document.getElementById('colorRulesBtn');
    if (colorRulesBtn) {
        colorRulesBtn.addEventListener('click', () => {
            openColorRulesManager();
        });
    }

    startEventStream();
    window.addEventListener('captures-updated', (e) => {
        renderList();
        if (state.selectedId === e.detail.id) {
            const c = state.captures.find(x => x.id === state.selectedId);
            if (c) renderDetails(c);
        }
    });
}

async function loadInitial() {
    await fetchInitialData();
    await refreshRulesFromServer();
    await initSearchHistory();
    renderList();
    if (state.captures.length) selectCapture(state.captures[0].id);
}

async function init() {
    setupTabs();
    await bindUI();
    await bindSearchHistoryUI();
    await loadInitial();
    startSSE();
}

init();