/**
 * Client-side SSE consumer with resume.
 *
 * Subscribes to GET /api/streams/:id/events. On disconnect, reconnects with
 * `?after_seq=<lastSeq>` so the server replays missed chunks from DB before
 * attaching us back to the live bus.
 *
 * This is the supported path for streaming; do not adopt @ai-sdk/svelte's
 * `Chat` (it owns its own state machine and fights the Dexie-first model).
 */

export interface StreamEvent {
  seq: number;
  type: 'delta' | 'usage' | 'error' | 'done';
  content?: string;
  usage?: { input_tokens: number; output_tokens: number };
  error?: { code: string; message: string };
}

export interface StreamHandlers {
  onEvent(ev: StreamEvent): void;
  onClose?(reason: 'done' | 'error' | 'aborted'): void;
}

export function consume(streamID: string, handlers: StreamHandlers): () => void {
  let aborted = false;
  let lastSeq = 0;
  let controller = new AbortController();

  const loop = async () => {
    while (!aborted) {
      try {
        const res = await fetch(`/api/streams/${streamID}/events?after_seq=${lastSeq}`, {
          credentials: 'include',
          signal: controller.signal
        });
        if (!res.ok || !res.body) {
          handlers.onClose?.('error');
          return;
        }

        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        let buf = '';
        // SSE frames are separated by a blank line.
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += value;
          let idx: number;
          while ((idx = buf.indexOf('\n\n')) >= 0) {
            const frame = buf.slice(0, idx);
            buf = buf.slice(idx + 2);
            const dataLine = frame
              .split('\n')
              .find((l) => l.startsWith('data:'))
              ?.slice(5)
              .trim();
            if (!dataLine) continue;
            try {
              const ev = JSON.parse(dataLine) as StreamEvent;
              if (ev.seq > lastSeq) lastSeq = ev.seq;
              handlers.onEvent(ev);
              if (ev.type === 'done' || ev.type === 'error') {
                handlers.onClose?.(ev.type);
                return;
              }
            } catch {
              /* malformed frame — skip */
            }
          }
        }
        // EOF without done: server hung up. Reconnect with after_seq.
      } catch (_e) {
        if (aborted) return;
        // network error — back off briefly and retry
        await new Promise((r) => setTimeout(r, 500));
        controller = new AbortController();
      }
    }
  };

  loop();

  return () => {
    aborted = true;
    controller.abort();
    handlers.onClose?.('aborted');
  };
}
