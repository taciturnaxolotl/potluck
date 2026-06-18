/**
 * Client-side SSE consumer with resume.
 *
 * Subscribes to an SSE endpoint. On disconnect, reconnects with
 * `?after_seq=<lastSeq>` so the server replays missed chunks from DB before
 * attaching us back to the live bus.
 *
 * Pass a stream ID string to use the default stream endpoint
 * (`/api/streams/:id/events?after_seq=N`), or pass a URL builder function for
 * custom endpoints (e.g. the conversation bus at `/api/conversations/:id/events`).
 *
 * Set `reconnectAlways` for long-lived push channels that should always
 * reconnect, even if no events arrived on the current connection.
 *
 * This is the supported path for streaming; do not adopt @ai-sdk/svelte's
 * `Chat` (it owns its own state machine and fights the Dexie-first model).
 */

export interface StreamEvent {
  seq: number;
  type:
    | "delta"
    | "usage"
    | "error"
    | "done"
    | "tool_call"
    | "tool_result"
    | "start";
  content?: string;
  usage?: { input_tokens: number; output_tokens: number };
  error?: { code: string; message: string };
  [extra: string]: any;
}

export interface StreamHandlers {
  onEvent(ev: StreamEvent): void;
  onClose?(reason: "done" | "error" | "aborted"): void;
}

export function consume(
  streamID: string | ((afterSeq: number) => string),
  handlers: StreamHandlers,
  initialAfterSeq = 0,
  { reconnectAlways = false }: { reconnectAlways?: boolean } = {},
): () => void {
  let aborted = false;
  let lastSeq = initialAfterSeq;
  let controller = new AbortController();

  const buildUrl =
    typeof streamID === "function"
      ? streamID
      : (seq: number) =>
          `/api/streams/${streamID}/events?after_seq=${Math.max(0, seq)}`;

  const loop = async () => {
    while (!aborted) {
      try {
        const res = await fetch(buildUrl(lastSeq), {
          credentials: "include",
          signal: controller.signal,
        });
        if (!res.ok || !res.body) {
          handlers.onClose?.("error");
          return;
        }

        const reader = res.body
          .pipeThrough(new TextDecoderStream())
          .getReader();
        let buf = "";
        let gotEvent = false;
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += value;
          let idx: number;
          while ((idx = buf.indexOf("\n\n")) >= 0) {
            const frame = buf.slice(0, idx);
            buf = buf.slice(idx + 2);
            const dataLine = frame
              .split("\n")
              .find((l) => l.startsWith("data:"))
              ?.slice(5)
              .trim();
            if (!dataLine) continue;
            try {
              const ev = JSON.parse(dataLine) as StreamEvent;
              if (ev.seq > lastSeq) lastSeq = ev.seq;
              gotEvent = true;
              handlers.onEvent(ev);
              if (ev.type === "done" || ev.type === "error") {
                handlers.onClose?.(ev.type);
                return;
              }
            } catch {
              /* malformed frame — skip */
            }
          }
        }
        // EOF without done: if no events arrived and we're not in reconnect-always
        // mode, the stream is stale/dead — give up so the UI doesn't spin forever.
        if (!gotEvent && !reconnectAlways) {
          handlers.onClose?.("error");
          return;
        }
        // Reconnect with after_seq (or always, for push channels).
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
    handlers.onClose?.("aborted");
  };
}
