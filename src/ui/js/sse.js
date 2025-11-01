// sse.js
import { upsertCapture } from './state.js';
import { prependRowIfVisible } from './list.js';

let es = null;
export function startEventStream() {
    if (es) return es;             // singleton guard
    es = new EventSource('/events');
    es.onmessage = (ev) => {
        try { upsertCapture(JSON.parse(ev.data)); }
        catch (e) { console.error('SSE parse error', e); }
    };
    es.onerror = (e) => console.warn('SSE error', e);
    return es;
}
