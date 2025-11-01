// sse.js
import { upsertCapture } from './state.js';

export function startEventStream() {
    const es = new EventSource('/events');

    es.onopen = () => console.log('[SSE] open');
    es.onmessage = ev => {
        try {
            const cap = JSON.parse(ev.data);
            upsertCapture(cap); // mutate state and emit a UI event
        } catch (e) {
            console.error('[SSE] parse error', e, ev.data);
        }
    };
    es.onerror = err => console.warn('[SSE] error', err);
}