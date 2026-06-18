<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { liveQuery } from 'dexie';
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { v4 as uuid } from 'uuid';
  import { db, type DBMessage } from '$lib/db';
  import { listConversations, listMessages, listModels, type Model } from '$lib/api';
  import { renderMarkdown, renderStreamingMarkdown } from '$lib/markdown';
  import { consume, type StreamEvent } from '$lib/stream';
  import ModelPicker from './ModelPicker.svelte';
  import ProcessBlock, { type ProcessStep } from './ProcessBlock.svelte';

  // ── state ─────────────────────────────────────────────────────────────────

  let activeConvId = $state<string | null>(null);
  let input = $state('');
  let streaming = $state(false);
  let streamingMsgId = $state<string | null>(null);
  let activeStreamId = $state<string | null>(null);
  let activeAfterSeq = $state(0);
  let stopActiveConsumer: (() => void) | null = null;
  let stopConvWatcher: (() => void) | null = null;

  let models = $state<Model[]>([]);
  let selectedModel = $state('');


  let messages = $state<DBMessage[]>([]);

  let msgsEl = $state<HTMLElement | null>(null);
  let inputEl = $state<HTMLTextAreaElement | null>(null);

  let atBottom = true;
  let errorMsg = $state<string | null>(null);

  // Ordered process steps (reasoning + tool calls) for the active stream.
  // Persists past streaming=false so ProcessBlock stays rendered.
  let processSteps = $state<ProcessStep[]>([]);
  let processMsgId = $state<string | null>(null);


  // ── spin cursor (crush-style cycling glyphs) ──────────────────────────────

  const GLYPHS = '0123456789abcdefABCDEF~!@#$£€%^&*()+=_';
  const SPIN_LEN = 14;
  let spinChars = $state('.');
  let spinInterval = 0;

  $effect(() => {
    if (streaming) {
      spinChars = Array.from({ length: SPIN_LEN }, () =>
        GLYPHS[Math.floor(Math.random() * GLYPHS.length)]
      ).join('');
      spinInterval = setInterval(() => {
        spinChars = Array.from({ length: SPIN_LEN }, () =>
          GLYPHS[Math.floor(Math.random() * GLYPHS.length)]
        ).join('');
      }, 50) as unknown as number;
    } else {
      clearInterval(spinInterval);
      spinChars = '.';
    }
    return () => clearInterval(spinInterval);
  });

  // ── streaming stats ───────────────────────────────────────────────────────
  let streamStartMs = $state(0);
  let streamFirstTokenMs = $state(0);  // 0 = not yet arrived
  let streamElapsedMs = $state(0);     // ticking while streaming
  let streamLiveTokens = $state(0);    // rough token count from delta chars
  let streamToolPausedMs = $state(0);  // accumulated time waiting for tool results
  let elapsedInterval = 0;

  let streamLiveTps = $derived.by(() => {
    if (!streamFirstTokenMs) return 0;
    const postTtftMs = streamElapsedMs - (streamFirstTokenMs - streamStartMs) - streamToolPausedMs;
    if (postTtftMs <= 0) return 0;
    return Math.round(streamLiveTokens / (postTtftMs / 1000));
  });

  $effect(() => {
    if (streaming) {
      elapsedInterval = setInterval(() => {
        streamElapsedMs = Date.now() - streamStartMs;
      }, 100) as unknown as number;
    } else {
      clearInterval(elapsedInterval);
    }
    return () => clearInterval(elapsedInterval);
  });

  // ── reactive IDB message query ────────────────────────────────────────────

  $effect(() => {
    const id = activeConvId;
    if (!id) {
      messages = [];
      return;
    }
    const sub = liveQuery(() =>
      db.messages.where('conversation_id').equals(id).sortBy('created_at')
    ).subscribe({
      next: (r) => {
        // During streaming, suppress empty results: the tmpConvId→serverConvId
        // migration fires the old subscription with [] before Svelte can
        // unsubscribe it, causing a brief flash with no messages.
        if (r.length > 0 || !streaming) messages = r as DBMessage[];
      },
      error: () => {}
    });
    return () => sub.unsubscribe();
  });

  // ── URL-reactive conversation selection ───────────────────────────────────

  let urlConvId = $derived(page.url.searchParams.get('c'));

  $effect(() => {
    const id = urlConvId;
    if (id === activeConvId) return;
    activeConvId = id;
    atBottom = true;
    if (!id) return;
    db.messages
      .where('conversation_id')
      .equals(id)
      .count()
      .then((n) => syncMessages(id));
  });

  // ── auto-scroll ───────────────────────────────────────────────────────────

  $effect(() => {
    void messages;
    if (atBottom && msgsEl) {
      msgsEl.scrollTop = msgsEl.scrollHeight;
    }
  });

  function onMsgsScroll() {
    if (!msgsEl) return;
    atBottom = msgsEl.scrollTop + msgsEl.clientHeight >= msgsEl.scrollHeight - 40;
  }

  // ── server sync ───────────────────────────────────────────────────────────

  async function syncConversations() {
    try {
      const rows = await listConversations();
      await db.conversations.bulkPut(
        rows.map((c) => ({ id: c.id, title: c.title, updated_at: c.updated_at, archived_at: c.archived_at }))
      );
    } catch {
      /* silent */
    }
  }

  async function syncMessages(id: string) {
    try {
      const rows = await listMessages(id);
      await db.messages.bulkPut(
        rows.map((m) => ({
          id: m.id,
          conversation_id: m.conversation_id,
          client_id: m.client_id,
          role: m.role as DBMessage['role'],
          content: m.content,
          model: m.model,
          created_at: m.created_at
        }))
      );
    } catch {
      /* silent */
    }
  }

  // ── lifecycle ─────────────────────────────────────────────────────────────

  onMount(async () => {
    const saved = localStorage.getItem('chat:model');
    const resumeStreamId = localStorage.getItem('chat:active_stream_id');
    const resumeConvId = localStorage.getItem('chat:active_stream_conv_id');
    const resumeAssistantId = localStorage.getItem('chat:active_stream_assistant_id');
    const resumeAfterSeq = Number(localStorage.getItem('chat:active_stream_after_seq') || '0');
    if (saved) selectedModel = saved;

    try {
      const result = await listModels();
      models = result.models;
      if (!selectedModel && models.length > 0) {
        selectedModel = models[0].id;
      }
    } catch {
      /* silent */
    }

    // Sync conversations so the layout sidebar has data.
    syncConversations();

    // Resume in-flight stream after reload — only if the URL actually matches
    // the stale conv (i.e. this is a true reload, not a navigation to /chat).
    // Mismatched stale data means the stream finished or was abandoned; clear it.
    const urlConvIdAtMount = page.url.searchParams.get('c');
    if (resumeStreamId && resumeConvId && resumeAssistantId) {
      if (urlConvIdAtMount !== resumeConvId) {
        clearActiveStreamStorage();
      } else {
        activeConvId = resumeConvId;
        streaming = true;
        activeStreamId = resumeStreamId;
        streamingMsgId = resumeAssistantId;
        activeAfterSeq = Number.isFinite(resumeAfterSeq) ? resumeAfterSeq : 0;
        streamStartMs = Date.now();
        streamFirstTokenMs = 0;
        attachStreamConsumer(resumeStreamId, resumeConvId, resumeAssistantId, Math.floor(Date.now() / 1000), resumeAfterSeq);
      }
    }
  });

  onDestroy(() => {
    if (stopActiveConsumer) {
      stopActiveConsumer();
      stopActiveConsumer = null;
    }
    if (stopConvWatcher) {
      stopConvWatcher();
      stopConvWatcher = null;
    }
  });

  // ── cross-browser streaming sync ──────────────────────────────────────────
  // When another tab or browser sends a message on the same conversation,
  // the backend publishes a "start" event to the conversation-level bus.
  // We subscribe here and attach a stream consumer so the spinner starts
  // immediately without a reload.

  $effect(() => {
    const id = activeConvId;
    if (!id) {
      stopConvWatcher?.();
      stopConvWatcher = null;
      return;
    }
    stopConvWatcher?.();
    stopConvWatcher = watchConversation(id);
    return () => {
      stopConvWatcher?.();
      stopConvWatcher = null;
    };
  });

  function watchConversation(convId: string): () => void {
    return consume(
      () => `/api/conversations/${convId}/events`,
      { onEvent: (ev) => { handleConvEvent(ev, convId); } },
      0,
      { reconnectAlways: true }
    );
  }

  async function handleConvEvent(ev: StreamEvent, convId: string) {
    if (ev.type === 'title_updated' && typeof ev.title === 'string' && ev.title) {
      await db.conversations.update(convId, { title: ev.title });
      return;
    }
    if (ev.type !== 'start') return;
    if (!ev.stream_id || !ev.assistant_message_id) return;
    // Ignore if we're the one already consuming this stream.
    if (streaming && activeStreamId === ev.stream_id) return;
    if (streaming) return;

    const sid: string = ev.stream_id;
    const aid: string = ev.assistant_message_id;
    const model: string = ev.model || selectedModel || '';

    streaming = true;
    streamingMsgId = aid;
    activeStreamId = sid;
    streamStartMs = Date.now();
    streamFirstTokenMs = 0;
    streamElapsedMs = 0;
    streamLiveTokens = 0;
    processSteps = [];
    processMsgId = null;

    // Seed the placeholder message in Dexie so the spinner appears immediately.
    const now = Math.floor(Date.now() / 1000);
    await db.messages.put({
      id: aid,
      conversation_id: convId,
      client_id: null,
      role: 'assistant',
      content: '',
      model,
      created_at: now,
      pending: true
    });

    attachStreamConsumer(sid, convId, aid, now, 0);
  }

  // ── model selection ───────────────────────────────────────────────────────



  // ── input ─────────────────────────────────────────────────────────────────

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  }

  function resize(el: HTMLTextAreaElement) {
    requestAnimationFrame(() => {
      el.style.height = 'auto';
      el.style.height = Math.min(el.scrollHeight, 200) + 'px';
    });
  }

  $effect(() => {
    void input;
    if (inputEl) resize(inputEl);
  });

  // ── send / stream ─────────────────────────────────────────────────────────

  function clearActiveStreamStorage() {
    localStorage.removeItem('chat:active_stream_id');
    localStorage.removeItem('chat:active_stream_conv_id');
    localStorage.removeItem('chat:active_stream_assistant_id');
    localStorage.removeItem('chat:active_stream_after_seq');
  }

  // Unified stream event handler used by both sendMessage (after handoff)
  // and resume/cross-tab flows via attachStreamConsumer.
  function makeStreamHandlers(convId: string, assistantId: string, now: number) {
    let accContent = '';
    let toolPauseStartMs = 0;

    return {
      onEvent: async (ev: StreamEvent) => {
        if (ev.seq > activeAfterSeq) {
          activeAfterSeq = ev.seq;
          localStorage.setItem('chat:active_stream_after_seq', String(activeAfterSeq));
        }

        if (ev.type === 'delta' && ev.content) {
          if (!streamFirstTokenMs) streamFirstTokenMs = Date.now();
          streamLiveTokens += Math.ceil(ev.content.length / 4);
          if (!accContent) {
            // Resume path: seed accContent from DB so replayed deltas append
            // to existing content rather than overwriting it.
            const existing = await db.messages.get(assistantId);
            accContent = existing?.content ?? '';
          }
          accContent += ev.content;
          await db.messages.put({
            id: assistantId,
            conversation_id: convId,
            client_id: null,
            role: 'assistant',
            content: accContent,
            model: selectedModel,
            created_at: now + 1,
            pending: true
          });
        }

        if (ev.type === 'reasoning' && ev.content) {
          if (!streamFirstTokenMs) streamFirstTokenMs = Date.now();
          streamLiveTokens += Math.ceil((ev.content as string).length / 4);
          if (!processMsgId) processMsgId = assistantId;
          const last = processSteps.at(-1);
          if (last?.type === 'reasoning') {
            last.content += ev.content as string;
            processSteps = [...processSteps];
          } else {
            processSteps = [...processSteps, { type: 'reasoning', content: ev.content as string }];
          }
        }

        if (ev.type === 'tool_call') {
          if (!processMsgId) processMsgId = assistantId;
          const hadPending = processSteps.some((s) => s.type === 'tool' && !s.done);
          if (!hadPending) toolPauseStartMs = Date.now();
          processSteps = [...processSteps, {
            type: 'tool',
            id: ((ev as any).id as string) || uuid(),
            name: ((ev as any).name as string) || 'tool',
            args: ((ev as any).arguments as string) || '',
            result: '',
            done: false
          }];
        }

        if (ev.type === 'tool_result') {
          const tcId = ((ev as any).tool_call_id as string) || '';
          const result = ((ev as any).content as string) || '';
          const idx = tcId
            ? processSteps.findIndex((s) => s.type === 'tool' && s.id === tcId)
            : processSteps.findLastIndex((s) => s.type === 'tool' && !s.done);
          if (idx >= 0) {
            const step = processSteps[idx] as Extract<ProcessStep, { type: 'tool' }>;
            processSteps = [
              ...processSteps.slice(0, idx),
              { ...step, result, done: true },
              ...processSteps.slice(idx + 1)
            ];
          }
          const stillPending = processSteps.some((s) => s.type === 'tool' && !s.done);
          if (!stillPending && toolPauseStartMs > 0) {
            streamToolPausedMs += Date.now() - toolPauseStartMs;
            toolPauseStartMs = 0;
          }
        }

        if (ev.type === 'done' || ev.type === 'error') {
          const doneMs = Date.now();
          const completionTokens = (ev.usage as any)?.completion_tokens ?? ev.usage?.output_tokens;
          const ttft = streamFirstTokenMs ? streamFirstTokenMs - streamStartMs : undefined;
          const tps =
            completionTokens && streamFirstTokenMs
              ? completionTokens / ((doneMs - streamFirstTokenMs - streamToolPausedMs) / 1000)
              : undefined;

          if (ev.type === 'done') {
            await db.messages.where({ id: assistantId }).modify({
              pending: false,
              ttft,
              tps,
              tokens: completionTokens
            });
            await db.conversations.where({ id: convId }).modify({ updated_at: Math.floor(doneMs / 1000) });
          } else {
            await db.messages.delete(assistantId);
            errorMsg = ev.error?.message || (ev as any).message || 'stream error';
          }
          // UI state teardown: onClose (attach path) or finally (inline path).
        }
      },
      onClose: (reason: string) => {
        if (reason === 'aborted') return;
        streaming = false;
        streamingMsgId = null;
        activeStreamId = null;
        activeAfterSeq = 0;
        clearActiveStreamStorage();
        // consume() has already terminated when onClose fires — just null the ref.
        stopActiveConsumer = null;
      }
    };
  }

  function attachStreamConsumer(streamId: string, convId: string, assistantId: string, now: number, initialAfterSeq = 0) {
    if (stopActiveConsumer) {
      stopActiveConsumer();
      stopActiveConsumer = null;
    }

    stopActiveConsumer = consume(streamId, makeStreamHandlers(convId, assistantId, now), initialAfterSeq);
  }

  async function sendMessage() {
    const content = input.trim();
    if (!content || streaming || !selectedModel) return;

    input = '';
    if (inputEl) resize(inputEl);
    streaming = true;
    atBottom = true;
    errorMsg = null;
    processSteps = [];
    processMsgId = null;
    streamToolPausedMs = 0;

    streamStartMs = Date.now();
    streamFirstTokenMs = 0;
    streamElapsedMs = 0;
    streamLiveTokens = 0;

    const clientId = uuid();
    const now = Math.floor(Date.now() / 1000);

    let convId = activeConvId;
    let isNewConv = false;
    if (!convId) {
      convId = uuid();
      isNewConv = true;
      await db.conversations.put({
        id: convId,
        title: content.slice(0, 60),
        updated_at: now,
        archived_at: null
      });
      activeConvId = convId;
      goto(`/chat?c=${convId}`, { replaceState: true, noScroll: true, keepFocus: true });
    }

    const tmpUserId = uuid();
    await db.messages.put({
      id: tmpUserId,
      conversation_id: convId,
      client_id: clientId,
      role: 'user',
      content,
      model: null,
      created_at: now,
      pending: true
    });

    const tmpAssistantId = uuid();
    streamingMsgId = tmpAssistantId;
    await db.messages.put({
      id: tmpAssistantId,
      conversation_id: convId,
      client_id: null,
      role: 'assistant',
      content: '',
      model: selectedModel,
      created_at: now + 1,
      pending: true
    });

    const msgHistory = await db.messages
      .where('conversation_id')
      .equals(convId)
      .sortBy('created_at');

    const apiMessages = msgHistory
      .filter((m) => (m.role === 'user' || m.role === 'assistant') && (!m.pending || m.id === tmpUserId))
      .map((m) => ({
        role: m.role,
        content: m.content,
        ...(m.id === tmpUserId ? { client_id: clientId } : {})
      }));

    try {
      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          ...(isNewConv ? {} : { conversation_id: convId }),
          ...(isNewConv ? { title: content.slice(0, 60) } : {}),
          model: selectedModel,
          messages: apiMessages
        })
      });

      if (!res.ok || !res.body) {
        const j = await res.json().catch(() => ({}));
        throw new Error((j as any)?.error?.message || `HTTP ${res.status}`);
      }

      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
      let buf = '';
      let resolvedHandlers: ReturnType<typeof makeStreamHandlers> | null = null;

      outer: for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += value;

        let idx: number;
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);

          const data = frame
            .split('\n')
            .find((l) => l.startsWith('data:'))
            ?.slice(5)
            .trim();
          if (!data) continue;

          let ev: Record<string, any>;
          try {
            ev = JSON.parse(data);
          } catch {
            continue;
          }

          if (typeof ev.seq === 'number' && ev.seq > activeAfterSeq) {
            activeAfterSeq = ev.seq;
            localStorage.setItem('chat:active_stream_after_seq', String(activeAfterSeq));
          }

          if (ev.type === 'start') {
            const serverConvId: string = ev.conversation_id;
            const serverUserId: string = ev.user_message_id;
            const serverAssistantId: string = ev.assistant_message_id || tmpAssistantId;
            const serverStreamId: string | undefined = ev.stream_id;
            const localConvId: string = convId!;

            await db.transaction('rw', db.conversations, db.messages, async () => {
              if (isNewConv && serverConvId && serverConvId !== localConvId) {
                const tmpConv = await db.conversations.get(localConvId);
                if (tmpConv) {
                  await db.conversations.delete(localConvId);
                  await db.conversations.put({ ...tmpConv, id: serverConvId });
                  const affected = await db.messages
                    .where('conversation_id')
                    .equals(localConvId)
                    .toArray();
                  await db.messages.bulkPut(affected.map((m) => ({ ...m, conversation_id: serverConvId })));
                  await db.messages.where('conversation_id').equals(localConvId).delete();
                }
              }

              if (serverUserId && serverUserId !== tmpUserId) {
                const old = await db.messages.get(tmpUserId);
                if (old) {
                  await db.messages.delete(tmpUserId);
                  await db.messages.put({ ...old, id: serverUserId, pending: false });
                }
              } else {
                await db.messages.where({ id: tmpUserId }).modify({ pending: false });
              }

              if (serverAssistantId && serverAssistantId !== tmpAssistantId) {
                const old = await db.messages.get(tmpAssistantId);
                if (old) {
                  await db.messages.delete(tmpAssistantId);
                  await db.messages.put({ ...old, id: serverAssistantId });
                  streamingMsgId = serverAssistantId;
                }
              }
            });

            if (isNewConv && serverConvId && serverConvId !== localConvId) {
              convId = serverConvId;
              activeConvId = serverConvId;
              goto(`/chat?c=${serverConvId}`, { replaceState: true, noScroll: true, keepFocus: true });
            }

            if (serverStreamId) {
              activeStreamId = serverStreamId;
              localStorage.setItem('chat:active_stream_id', serverStreamId);
              localStorage.setItem('chat:active_stream_conv_id', convId);
              localStorage.setItem('chat:active_stream_assistant_id', streamingMsgId || tmpAssistantId);
              localStorage.setItem('chat:active_stream_after_seq', String(activeAfterSeq));
            }

            resolvedHandlers = makeStreamHandlers(convId, streamingMsgId || tmpAssistantId, now);
            continue;
          }

          if (ev.type === 'error' && !resolvedHandlers) {
            await db.messages.delete(tmpAssistantId);
            errorMsg = ev.message as string;
            return;
          }

          if (resolvedHandlers) {
            await resolvedHandlers.onEvent(ev as StreamEvent);
            if (ev.type === 'done' || ev.type === 'error') break outer;
          }
        }
      }
    } catch (err) {
      await db.messages.delete(streamingMsgId || tmpAssistantId);
      errorMsg = err instanceof Error ? err.message : 'Something went wrong';
    } finally {
      streaming = false;
      streamingMsgId = null;
      activeStreamId = null;
      activeAfterSeq = 0;
      clearActiveStreamStorage();
    }
  }

  // ── time formatting ───────────────────────────────────────────────────────

  function relTime(unix: number): string {
    const s = Math.floor(Date.now() / 1000) - unix;
    if (s < 60) return 'now';
    if (s < 3600) return `${Math.floor(s / 60)}m`;
    if (s < 86400) return `${Math.floor(s / 3600)}h`;
    if (s < 604800) return `${Math.floor(s / 86400)}d`;
    return new Date(unix * 1000).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }
</script>

<svelte:head>
  <title>chat · potluck</title>
</svelte:head>

<div class="chat">
  <!-- Messages -->
  <div
    class="msgs"
    bind:this={msgsEl}
    onscroll={onMsgsScroll}
  >
    {#if !activeConvId}
      <div class="welcome">
        <p class="welcome-title">What do you want to make today?</p>
        <p class="welcome-sub">Pick a model and start typing.</p>
      </div>
    {:else if messages.length === 0}
      <div class="welcome">
        <p class="welcome-sub">Send a message to start.</p>
      </div>
    {:else}
      {#each messages as msg (msg.id)}
        {#if msg.role === 'user'}
          <div class="msg user">
            <div class="user-bubble">{msg.content}</div>
          </div>
        {:else if msg.role === 'assistant'}
          <div class="msg assistant">
            <div class="assistant-body">
              <div class="assistant-bubble" class:pending={msg.pending}>
                {#if msg.id === processMsgId && processSteps.length > 0}
                  <ProcessBlock steps={processSteps} streaming={msg.id === streamingMsgId} />
                {/if}
                {#if msg.id === streamingMsgId}
                  {@html renderStreamingMarkdown(msg.content)}
                {:else if msg.content}
                  {@html renderMarkdown(msg.content)}
                {:else if msg.pending}
                  <!-- pending: spinner shown below -->
                {/if}
              </div>
              <div class="msg-meta">
                {#if msg.id === streamingMsgId}
                  <span class="spin-cursor">{spinChars}</span>
                {/if}
                {#if (typeof msg.model === 'string' && msg.model) || msg.id === streamingMsgId}
                  <span class="meta-model">{(typeof msg.model === 'string' && msg.model ? msg.model : selectedModel).replace('free/', '')}</span>
                {/if}
                {#if msg.id === streamingMsgId}
                  {#if streamFirstTokenMs > 0}
                    <span class="meta-stat">{((streamFirstTokenMs - streamStartMs) / 1000).toFixed(2)}s ttft</span>
                  {/if}
                  {#if streaming}
                    <span class="meta-stat">{(streamElapsedMs / 1000).toFixed(1)}s</span>
                  {/if}
                {:else if msg.ttft}
                  <span class="meta-stat">{(msg.ttft / 1000).toFixed(2)}s ttft</span>
                {/if}
                {#if msg.id === streamingMsgId}
                  <span class="meta-stat">{streamLiveTps > 0 ? streamLiveTps + ' tok/s' : '??? tok/s'}</span>
                {:else if msg.tps}
                  <span class="meta-stat">{msg.tps.toFixed(0)} tok/s</span>
                {/if}
                {#if msg.tokens && msg.id !== streamingMsgId}
                  <span class="meta-stat">{msg.tokens} tok</span>
                {/if}
              </div>
            </div>
          </div>
        {/if}
      {/each}
      {#if streaming && streamingMsgId && !messages.some((m) => m.id === streamingMsgId)}
        <div class="msg assistant">
          <div class="assistant-body">
            <div class="assistant-bubble pending"></div>
            <div class="msg-meta">
              <span class="spin-cursor">{spinChars}</span>
              <span class="meta-model">{selectedModel.replace('free/', '')}</span>
              <span class="meta-stat">{(streamElapsedMs / 1000).toFixed(1)}s</span>
            </div>
          </div>
        </div>
      {/if}
    {/if}
  </div>

  <!-- Error warning -->
  {#if errorMsg}
    <div class="error-banner" role="alert">
      <span>{errorMsg}</span>
      <button class="dismiss-btn" onclick={() => (errorMsg = null)} aria-label="Dismiss">✕</button>
    </div>
  {/if}

  <!-- Input -->
  <div class="input-bar">
    <div class="input-wrap">
      <textarea
        class="input"
        bind:value={input}
        bind:this={inputEl}
        onkeydown={handleKeydown}
        oninput={(e) => resize(e.currentTarget as HTMLTextAreaElement)}
        placeholder={streaming ? 'Thinking…' : 'Message…'}
        disabled={streaming}
        rows={1}
      ></textarea>
      <div class="input-footer">
        <ModelPicker
          {models}
          selectedModel={selectedModel}
          disabled={streaming}
          onPick={(id) => {
            selectedModel = id;
            localStorage.setItem('chat:model', id);
            inputEl?.focus();
          }}
        />

        <button
          class="send-btn"
          onclick={sendMessage}
          disabled={streaming || !input.trim() || !selectedModel}
          aria-label="Send"
        >
          {#if streaming}
            <span class="spinner"></span>
          {:else}
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M8 13V3M3 8l5-5 5 5" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
          {/if}
        </button>
      </div>
    </div>
  </div>
</div>

<style>
  .chat {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    background: var(--bg-page);
  }

  /* ── messages ───────────────────────────────────────────────────────────── */
  .msgs {
    flex: 1;
    overflow-y: auto;
    padding: 2rem 2.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .welcome {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.35rem;
    text-align: center;
    opacity: 0.55;
  }
  .welcome-title {
    font-family: var(--font-serif);
    font-variation-settings: var(--fraunces-text);
    font-size: 1.05rem;
    color: var(--text);
    margin: 0;
  }
  .welcome-sub {
    font-size: 0.82rem;
    color: var(--text-muted);
    margin: 0;
  }

  .msg {
    display: flex;
    width: 100%;
  }
  .msg.user {
    justify-content: flex-end;
  }
  .msg.assistant {
    justify-content: flex-start;
  }

  /* User bubble — soft warm pill, not the big accent block */
  .user-bubble {
    max-width: min(68%, 44rem);
    background: color-mix(in srgb, var(--accent) 9%, var(--bg-surface));
    border: 1px solid color-mix(in srgb, var(--accent) 18%, var(--border));
    color: var(--text);
    border-radius: 14px;
    border-bottom-right-radius: 3px;
    padding: 0.55rem 0.9rem;
    font-size: 0.9rem;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
  }

  /* Assistant — full-width prose with left accent rule */
  .assistant-body {
    max-width: min(82%, 56rem);
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  .assistant-bubble {
    color: var(--text);
    font-size: 0.9rem;
    line-height: 1.65;
    overflow-wrap: anywhere;
    word-break: break-word;
    min-width: 0;
  }
  .assistant-bubble.pending {
    opacity: 0.8;
  }

  .spin-cursor {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    color: var(--accent);
    letter-spacing: 0.05em;
  }
  .spin-cursor::after {
    content: '·';
    margin-left: 0.5rem;
    opacity: 0.5;
    color: var(--text-faint);
  }



  .msg-meta {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .meta-model {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    color: var(--text-faint);
    letter-spacing: 0.02em;
  }
  .meta-stat {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    color: var(--text-faint);
  }
  .meta-stat::before {
    content: '·';
    margin-right: 0.5rem;
    opacity: 0.5;
  }

  /* Markdown inside assistant bubbles — semantic elements from markdown-it */
  .assistant-bubble :global(p) {
    margin: 0.35em 0;
  }
  .assistant-bubble :global(p:first-child) {
    margin-top: 0;
  }
  .assistant-bubble :global(p:last-child) {
    margin-bottom: 0;
  }
  .assistant-bubble :global(h1),
  .assistant-bubble :global(h2),
  .assistant-bubble :global(h3),
  .assistant-bubble :global(h4),
  .assistant-bubble :global(h5),
  .assistant-bubble :global(h6) {
    margin: 0.6em 0 0.25em;
    font-weight: 600;
    line-height: 1.3;
  }
  .assistant-bubble :global(h1) { font-size: 1.15em; }
  .assistant-bubble :global(h2) { font-size: 1.05em; }
  .assistant-bubble :global(h3) { font-size: 1em; }
  .assistant-bubble :global(h4),
  .assistant-bubble :global(h5),
  .assistant-bubble :global(h6) { font-size: 0.95em; }
  .assistant-bubble :global(pre.md-code) {
    display: block;
    background: var(--bg-code);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.65rem 0.85rem;
    font-family: var(--font-mono);
    font-size: 0.8rem;
    overflow-x: auto;
    margin: 0.5em 0;
    white-space: pre;
    max-width: 100%;
  }
  .assistant-bubble :global(pre.md-code code) {
    font-family: var(--font-mono);
    font-size: inherit;
    background: none;
    border: none;
    padding: 0;
    border-radius: 0;
  }
  .assistant-bubble :global(code) {
    font-family: var(--font-mono);
    font-size: 0.82em;
    background: var(--bg-code);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 4px;
  }
  .assistant-bubble :global(ul),
  .assistant-bubble :global(ol) {
    margin: 0.3em 0;
    padding-left: 1.4em;
  }
  .assistant-bubble :global(li) {
    margin-bottom: 0.15em;
  }
  .assistant-bubble :global(li > p) {
    margin: 0.1em 0;
  }
  .assistant-bubble :global(blockquote) {
    margin: 0.5em 0;
    padding: 0.3em 0.8em;
    border-left: 3px solid var(--accent);
    color: var(--text-muted);
  }
  .assistant-bubble :global(blockquote p) {
    margin: 0.15em 0;
  }
  .assistant-bubble :global(hr) {
    border: none;
    border-top: 1px solid var(--border);
    margin: 0.8em 0;
  }
  .assistant-bubble :global(table) {
    border-collapse: collapse;
    margin: 0.5em 0;
    font-size: 0.85em;
    width: 100%;
  }
  .assistant-bubble :global(th),
  .assistant-bubble :global(td) {
    border: 1px solid var(--border);
    padding: 0.3em 0.6em;
    text-align: left;
  }
  .assistant-bubble :global(th) {
    font-weight: 600;
    background: var(--bg-code);
  }
  .assistant-bubble :global(a) {
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .assistant-bubble :global(a:hover) {
    opacity: 0.85;
  }
  .assistant-bubble :global(strong) { font-weight: 600; }
  .assistant-bubble :global(em) { font-style: italic; }
  .assistant-bubble :global(del) {
    opacity: 0.5;
    text-decoration: line-through;
  }
  .assistant-bubble :global(img) {
    max-width: 100%;
    border-radius: var(--radius-sm);
  }

  /* ── error banner ───────────────────────────────────────────────────────── */
  .error-banner {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.45rem 1.5rem;
    background: color-mix(in srgb, #f97316 10%, var(--bg-page));
    border-top: 1px solid color-mix(in srgb, #f97316 25%, transparent);
    color: #c2410c;
    font-size: 0.8rem;
  }
  .error-banner span { flex: 1; }
  .dismiss-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: inherit;
    opacity: 0.65;
    font-size: 0.72rem;
    padding: 2px 4px;
    line-height: 1;
  }
  .dismiss-btn:hover { opacity: 1; }

  /* ── input area ─────────────────────────────────────────────────────────── */
  .input-bar {
    display: flex;
    flex-direction: column;
    flex-shrink: 0;
    padding: 0.75rem 1.5rem 1.25rem;
    background: var(--bg-page);
  }

  .input-wrap {
    display: flex;
    flex-direction: column;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 0.65rem 0.65rem 0.45rem 0.9rem;
    transition: border-color 100ms ease;
  }
  .input-wrap:focus-within {
    border-color: color-mix(in srgb, var(--accent) 60%, transparent);
  }

  .input {
    width: 100%;
    box-sizing: border-box;
    background: none;
    border: none;
    outline: none;
    resize: none;
    font-family: var(--font-sans);
    font-size: 0.9rem;
    color: var(--text);
    line-height: 1.5;
    height: 1.35rem;
    max-height: 200px;
    overflow-y: auto;
    padding: 0;
  }
  .input::placeholder { color: var(--text-faint); }
  .input:disabled { opacity: 0.5; }

  .input-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 0.4rem;
  }



  /* ── send button ────────────────────────────────────────────────────────── */
  .send-btn {
    flex-shrink: 0;
    width: 30px;
    height: 30px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--accent);
    color: var(--text-on-accent);
    border: none;
    border-radius: 7px;
    cursor: pointer;
    transition: filter 80ms ease, opacity 80ms ease;
  }
  .send-btn:hover:not(:disabled) { filter: brightness(1.1); }
  .send-btn:disabled { opacity: 0.35; cursor: not-allowed; }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
  .spinner {
    width: 12px;
    height: 12px;
    border: 1.5px solid rgba(255, 217, 218, 0.3);
    border-top-color: var(--text-on-accent);
    border-radius: 50%;
    animation: spin 700ms linear infinite;
  }

  /* ── responsive ─────────────────────────────────────────────────────────── */
  @media (max-width: 720px) {
    .msgs { padding: 0.85rem 1rem; gap: 0.75rem; }
    .user-bubble { max-width: min(88%, 44rem); }
    .assistant-body { max-width: 100%; }
    .input-bar { padding: 0.5rem 0.75rem 0.85rem; }
    /* Prevent iOS Safari from zooming on textarea focus */
    .input { font-size: 1rem; }
    .meta-stat { display: none; }
    .meta-stat:first-of-type { display: inline; }
  }
</style>
