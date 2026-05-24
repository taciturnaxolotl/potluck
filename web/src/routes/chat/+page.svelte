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

  // ── state ─────────────────────────────────────────────────────────────────

  let activeConvId = $state<string | null>(null);
  let input = $state('');
  let streaming = $state(false);
  let streamingMsgId = $state<string | null>(null);
  let activeStreamId = $state<string | null>(null);
  let activeAfterSeq = $state(0);
  let stopActiveConsumer: (() => void) | null = null;

  let models = $state<Model[]>([]);
  let selectedModel = $state('');
  let modelPickerOpen = $state(false);

  let messages = $state<DBMessage[]>([]);

  let msgsEl = $state<HTMLElement | null>(null);
  let inputEl = $state<HTMLTextAreaElement | null>(null);
  let pickerEl = $state<HTMLElement | null>(null);
  let atBottom = true;
  let errorMsg = $state<string | null>(null);

  // Tool call tracking during streaming — keyed by tool_call_id.
  // Persists past streaming=false so the expandable widget renders on the
  // finished assistant message.
  let toolCalls = $state<Map<string, { name: string; args: string; result: string; done: boolean }>>(new Map());
  let toolMsgId = $state<string | null>(null);
  let toolExpanded = $state(false);

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
  let elapsedInterval = 0;

  $effect(() => {
    if (streaming && streamFirstTokenMs > 0) {
      elapsedInterval = setInterval(() => {
        streamElapsedMs = Date.now() - streamFirstTokenMs;
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
    ).subscribe({ next: (r) => (messages = r as DBMessage[]), error: () => {} });
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

    // Resume in-flight stream after reload.
    if (resumeStreamId && resumeConvId && resumeAssistantId) {
      activeConvId = resumeConvId;
      streaming = true;
      activeStreamId = resumeStreamId;
      streamingMsgId = resumeAssistantId;
      activeAfterSeq = Number.isFinite(resumeAfterSeq) ? resumeAfterSeq : 0;
      streamStartMs = Date.now();
      streamFirstTokenMs = 0;
      attachStreamConsumer(resumeStreamId, resumeConvId, resumeAssistantId, Math.floor(Date.now() / 1000), resumeAfterSeq);
    }
  });

  onDestroy(() => {
    if (stopActiveConsumer) {
      stopActiveConsumer();
      stopActiveConsumer = null;
    }
  });

  // ── model selection ───────────────────────────────────────────────────────

  let freeModels = $derived(models.filter((m) => m.id.startsWith('free/')));
  let paidModels = $derived(models.filter((m) => !m.id.startsWith('free/')));

  function saveModel() {
    localStorage.setItem('chat:model', selectedModel);
  }

  function modelDisplay(id: string) {
    if (!id) return 'pick model';
    const m = models.find((x) => x.id === id);
    if (m) return m.label || id.replace(/^free\//, '');
    return id.replace(/^free\//, '');
  }

  function pickModel(id: string) {
    selectedModel = id;
    saveModel();
    modelPickerOpen = false;
    inputEl?.focus();
  }

  function onWindowClick(e: MouseEvent) {
    if (modelPickerOpen && pickerEl && !pickerEl.contains(e.target as Node)) {
      modelPickerOpen = false;
    }
  }

  // ── input ─────────────────────────────────────────────────────────────────

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
    if (e.key === 'Escape') {
      modelPickerOpen = false;
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

  function attachStreamConsumer(streamId: string, convId: string, assistantId: string, now: number, initialAfterSeq = 0) {
    if (stopActiveConsumer) {
      stopActiveConsumer();
      stopActiveConsumer = null;
    }

    stopActiveConsumer = consume(streamId, {
      onEvent: async (ev: StreamEvent) => {
        if (ev.seq > activeAfterSeq) {
          activeAfterSeq = ev.seq;
          localStorage.setItem('chat:active_stream_after_seq', String(activeAfterSeq));
        }

        if (ev.type === 'delta' && ev.content) {
          if (!streamFirstTokenMs) streamFirstTokenMs = Date.now();
          const msg = await db.messages.get(assistantId);
          const current = msg?.content ?? '';
          await db.messages.put({
            id: assistantId,
            conversation_id: convId,
            client_id: null,
            role: 'assistant',
            content: current + ev.content,
            model: selectedModel,
            created_at: now + 1,
            pending: true
          });
        }

        if (ev.type === 'tool_call') {
          if (!toolMsgId) toolMsgId = assistantId;
          const tcId = (ev as any).id as string || uuid();
          toolCalls.set(tcId, {
            name: ((ev as any).name as string) || 'tool',
            args: ((ev as any).arguments as string) || '',
            result: '',
            done: false
          });
          toolCalls = new Map(toolCalls);
        }

        if (ev.type === 'tool_result') {
          const tcId = ((ev as any).tool_call_id as string) || '';
          const existing = tcId ? toolCalls.get(tcId) : null;
          if (existing) {
            toolCalls.set(tcId, { ...existing, result: ((ev as any).content as string) || '', done: true });
          } else {
            for (const [id, tc] of toolCalls) {
              if (!tc.done) {
                toolCalls.set(id, { ...tc, result: ((ev as any).content as string) || '', done: true });
                break;
              }
            }
          }
          toolCalls = new Map(toolCalls);
        }

        if (ev.type === 'done' || ev.type === 'error') {
          const doneMs = Date.now();
          const completionTokens = ev.usage?.output_tokens;
          const ttft = streamFirstTokenMs ? streamFirstTokenMs - streamStartMs : undefined;
          const tps =
            completionTokens && streamFirstTokenMs
              ? completionTokens / ((doneMs - streamFirstTokenMs) / 1000)
              : undefined;

          await db.messages.where({ id: assistantId }).modify({
            pending: false,
            ttft,
            tps,
            tokens: completionTokens
          });
          await db.conversations.where({ id: convId }).modify({ updated_at: Math.floor(doneMs / 1000) });

          streaming = false;
          streamingMsgId = null;
          activeStreamId = null;
          activeAfterSeq = 0;
          clearActiveStreamStorage();

          if (stopActiveConsumer) {
            stopActiveConsumer();
            stopActiveConsumer = null;
          }

          if (ev.type === 'error') {
            errorMsg = ev.error?.message || (ev as any).message || 'stream error';
          }
        }
      },
      onClose: (reason) => {
        if (reason === 'aborted') return;
      }
    }, initialAfterSeq);
  }

  async function sendMessage() {
    const content = input.trim();
    if (!content || streaming || !selectedModel) return;

    input = '';
    if (inputEl) resize(inputEl);
    streaming = true;
    atBottom = true;
    errorMsg = null;
    toolCalls = new Map();
    toolMsgId = null;
    toolExpanded = false;
    streamStartMs = Date.now();
    streamFirstTokenMs = 0;
    streamElapsedMs = 0;

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

    let resolvedUserId = tmpUserId;
    let resolvedAssistantId = tmpAssistantId;
    let accContent = '';

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

          // Keep activeAfterSeq current so resume picks up from the right spot.
          if (typeof ev.seq === 'number' && ev.seq > activeAfterSeq) {
            activeAfterSeq = ev.seq;
            if (activeStreamId) {
              localStorage.setItem('chat:active_stream_after_seq', String(activeAfterSeq));
            }
          }

          switch (ev.type) {
            case 'start': {
              const serverConvId: string = ev.conversation_id;
              const serverUserId: string = ev.user_message_id;
              const serverAssistantId: string = ev.assistant_message_id;
              const serverStreamId: string | undefined = ev.stream_id;

              const outerConvId: string = convId!;

              // Resolve all ID swaps in a single transaction so the live query
              // sees exactly one consistent snapshot.
              await db.transaction('rw', db.conversations, db.messages, async () => {
                // New conversation ID swap.
                if (isNewConv && serverConvId && serverConvId !== outerConvId) {
                  const tmpConv = await db.conversations.get(outerConvId);
                  if (tmpConv) {
                    await db.conversations.delete(outerConvId);
                    await db.conversations.put({ ...tmpConv, id: serverConvId });
                    const affected = await db.messages
                      .where('conversation_id')
                      .equals(outerConvId)
                      .toArray();
                    await db.messages.bulkPut(affected.map((m) => ({ ...m, conversation_id: serverConvId })));
                    await db.messages.where('conversation_id').equals(outerConvId).delete();
                  }
                }

                // User message ID swap.
                if (serverUserId && serverUserId !== tmpUserId) {
                  const old = await db.messages.get(tmpUserId);
                  if (old) {
                    await db.messages.delete(tmpUserId);
                    await db.messages.put({ ...old, id: serverUserId, pending: false });
                    resolvedUserId = serverUserId;
                  }
                } else {
                  await db.messages.where({ id: resolvedUserId }).modify({ pending: false });
                }

                // Assistant message ID swap.
                if (serverAssistantId && serverAssistantId !== tmpAssistantId) {
                  const old = await db.messages.get(tmpAssistantId);
                  if (old) {
                    await db.messages.delete(tmpAssistantId);
                    await db.messages.put({ ...old, id: serverAssistantId });
                    resolvedAssistantId = serverAssistantId;
                    streamingMsgId = serverAssistantId;
                  }
                }
              });

              if (isNewConv && serverConvId && serverConvId !== outerConvId) {
                convId = serverConvId;
                activeConvId = serverConvId;
                goto(`/chat?c=${serverConvId}`, { replaceState: true, noScroll: true, keepFocus: true });
              }

              // Save stream metadata for resume-on-reload — don't hand off mid-stream.
              if (serverStreamId) {
                activeStreamId = serverStreamId;
                localStorage.setItem('chat:active_stream_id', serverStreamId);
                localStorage.setItem('chat:active_stream_conv_id', convId);
                localStorage.setItem('chat:active_stream_assistant_id', resolvedAssistantId);
                localStorage.setItem('chat:active_stream_after_seq', String(activeAfterSeq));
              }

              break;
            }

            case 'delta': {
              if (!streamFirstTokenMs) {
                streamFirstTokenMs = Date.now();
              }
              accContent += ev.content as string;
              await db.messages.put({
                id: resolvedAssistantId,
                conversation_id: convId,
                client_id: null,
                role: 'assistant',
                content: accContent,
                model: selectedModel,
                created_at: now + 1,
                pending: true
              });
              break;
            }

            case 'done': {
              const completionTokens: number | undefined = (ev.usage as any)?.completion_tokens;
              const ttft = streamFirstTokenMs ? streamFirstTokenMs - streamStartMs : undefined;
              const doneMs = Date.now();
              const tps =
                completionTokens && streamFirstTokenMs
                  ? completionTokens / ((doneMs - streamFirstTokenMs) / 1000)
                  : undefined;
              await db.messages.where({ id: resolvedAssistantId }).modify({
                pending: false,
                ttft,
                tps,
                tokens: completionTokens
              });
              await db.conversations.where({ id: convId }).modify({ updated_at: Math.floor(doneMs / 1000) });
              activeStreamId = null;
              activeAfterSeq = 0;
              clearActiveStreamStorage();
              break outer;
            }

            case 'error': {
              await db.messages.delete(resolvedAssistantId);
              errorMsg = ev.message as string;
              activeStreamId = null;
              activeAfterSeq = 0;
              clearActiveStreamStorage();
              break outer;
            }

            case 'tool_call': {
              if (!toolMsgId) toolMsgId = resolvedAssistantId;
              const tcId = (ev.id as string) || uuid();
              toolCalls.set(tcId, {
                name: (ev.name as string) || 'tool',
                args: (ev.arguments as string) || '',
                result: '',
                done: false
              });
              toolCalls = new Map(toolCalls);
              break;
            }

            case 'tool_result': {
              const tcId = (ev.tool_call_id as string) || '';
              const existing = tcId ? toolCalls.get(tcId) : null;
              if (existing) {
                toolCalls.set(tcId, { ...existing, result: (ev.content as string) || '', done: true });
              } else {
                for (const [id, tc] of toolCalls) {
                  if (!tc.done) {
                    toolCalls.set(id, { ...tc, result: (ev.content as string) || '', done: true });
                    break;
                  }
                }
                if (![...toolCalls.values()].some(tc => tc.done && tc.result)) {
                  toolCalls.set(tcId || uuid(), {
                    name: 'tool',
                    args: '',
                    result: (ev.content as string) || '',
                    done: true
                  });
                }
              }
              toolCalls = new Map(toolCalls);
              break;
            }
          }
        }
      }
    } catch (err) {
      await db.messages.delete(resolvedAssistantId);
      errorMsg = err instanceof Error ? err.message : 'Something went wrong';
    } finally {
      streaming = false;
      streamingMsgId = null;
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

<svelte:window onclick={onWindowClick} />

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
                {#if msg.id === toolMsgId && toolCalls.size > 0}
                  {@const entries = [...toolCalls.entries()]}
                  {@const running = entries.filter(([, tc]) => !tc.done)}
                  {@const done = entries.filter(([, tc]) => tc.done)}
                  <div class="tool-block">
                    <!-- Collapsed summary line -->
                    <button
                      class="tool-summary"
                      onclick={() => (toolExpanded = !toolExpanded)}
                      aria-expanded={toolExpanded}
                    >
                      <span class="tool-summary-left">
                        {#if running.length > 0}
                          <span class="tool-spinner"></span>
                          <span class="tool-summary-label">Running {running.map(([, tc]) => tc.name).join(', ')}…</span>
                        {:else}
                          <span class="tool-check">✓</span>
                          <span class="tool-summary-label">Ran {entries.map(([, tc]) => tc.name).join(', ')}</span>
                        {/if}
                      </span>
                      <svg class="tool-chevron" class:tool-chevron-open={toolExpanded} width="10" height="6" viewBox="0 0 10 6" fill="none">
                        <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/>
                      </svg>
                    </button>

                    <!-- Expanded detail -->
                    {#if toolExpanded}
                      <div class="tool-detail">
                        {#each entries as [id, tc] (id)}
                          <div class="tool-detail-item" class:tool-done={tc.done}>
                            <div class="tool-detail-head">
                              {#if tc.done}
                                <span class="tool-check">✓</span>
                              {:else}
                                <span class="tool-spinner"></span>
                              {/if}
                              <span class="tool-detail-name">{tc.name}</span>
                            </div>
                            {#if tc.args}
                              <details class="tool-nested">
                                <summary>arguments</summary>
                                <pre class="tool-pre">{tc.args}</pre>
                              </details>
                            {/if}
                            {#if tc.result}
                              <details class="tool-nested" open>
                                <summary>result</summary>
                                <pre class="tool-pre">{tc.result}</pre>
                              </details>
                            {/if}
                          </div>
                        {/each}
                      </div>
                    {/if}
                  </div>
                {/if}
                {#if msg.pending && msg.id === streamingMsgId}
                  {@html renderStreamingMarkdown(msg.content)}
                {:else if msg.content}
                  {@html renderMarkdown(msg.content)}
                {:else if msg.pending}
                {/if}
              </div>
              <div class="msg-meta">
                {#if msg.pending && msg.id === streamingMsgId}
                  <span class="spin-cursor">{spinChars}</span>
                {/if}
                {#if msg.model && typeof msg.model === 'string'}
                  <span class="meta-model">{msg.model.replace('free/', '')}</span>
                {/if}
                {#if msg.id === streamingMsgId && streamFirstTokenMs > 0}
                  <span class="meta-stat">{((streamFirstTokenMs - streamStartMs) / 1000).toFixed(2)}s ttft</span>
                  {#if streaming}
                    <span class="meta-stat">{(streamElapsedMs / 1000).toFixed(1)}s</span>
                  {/if}
                {:else if msg.ttft}
                  <span class="meta-stat">{(msg.ttft / 1000).toFixed(2)}s ttft</span>
                {/if}
                {#if msg.tps && msg.id !== streamingMsgId}
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
        <!-- Model picker -->
        <div class="model-picker" bind:this={pickerEl}>
          <button
            class="model-chip"
            onclick={() => (modelPickerOpen = !modelPickerOpen)}
            disabled={streaming}
            aria-haspopup="listbox"
            aria-expanded={modelPickerOpen}
          >
            {#if selectedModel?.startsWith('free/')}
              <span class="tier-dot free" aria-hidden="true"></span>
            {:else if selectedModel}
              <span class="tier-dot pool" aria-hidden="true"></span>
            {/if}
            <span class="chip-label">{modelDisplay(selectedModel)}</span>
            <svg class="chip-caret" width="8" height="5" viewBox="0 0 8 5" fill="none" aria-hidden="true">
              <path d="M1 1l3 3 3-3" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
          </button>

          {#if modelPickerOpen}
            <div class="model-menu" role="listbox">
              {#if freeModels.length > 0}
                <div class="model-group-hdr">free</div>
                {#each freeModels as m (m.id)}
                  <button
                    class="model-opt"
                    class:sel={selectedModel === m.id}
                    role="option"
                    aria-selected={selectedModel === m.id}
                    onclick={() => pickModel(m.id)}
                  >{m.label || m.id.replace('free/', '')}</button>
                {/each}
              {/if}
              {#if paidModels.length > 0}
                <div class="model-group-hdr">pool</div>
                {#each paidModels as m (m.id)}
                  <button
                    class="model-opt"
                    class:sel={selectedModel === m.id}
                    role="option"
                    aria-selected={selectedModel === m.id}
                    onclick={() => pickModel(m.id)}
                  >{m.label || m.id}</button>
                {/each}
              {/if}
              {#if models.length === 0}
                <span class="model-opt" style="opacity:0.5;cursor:default">loading…</span>
              {/if}
            </div>
          {/if}
        </div>

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
    height: 100dvh;
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

  /* ── tool calls (Claude-style collapsible) ──────────────────────────────── */
  .tool-block {
    margin-bottom: 0.65rem;
    border: 1px solid var(--accent);
    border-radius: 0.45rem;
    overflow: hidden;
    font-family: var(--font-mono);
    font-size: 0.7rem;
  }

  .tool-summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 0.45rem 0.65rem;
    background: color-mix(in srgb, var(--accent) 6%, transparent);
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    gap: 0.5rem;
  }
  .tool-summary:hover {
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }
  .tool-summary-left {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 0;
    overflow: hidden;
  }
  .tool-summary-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: left;
  }
  .tool-chevron {
    flex-shrink: 0;
    opacity: 0.5;
    transition: transform 0.15s;
    color: var(--text-muted);
  }
  .tool-chevron-open {
    transform: rotate(180deg);
  }

  .tool-detail {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.5rem 0.65rem 0.65rem;
    border-top: 1px solid color-mix(in srgb, var(--accent) 12%, transparent);
    background: color-mix(in srgb, var(--accent) 3%, transparent);
  }
  .tool-detail-item {
    color: var(--text-faint);
    font-size: 0.68rem;
  }
  .tool-detail-item.tool-done {
    color: var(--text-muted);
  }
  .tool-detail-head {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-weight: 500;
    margin-bottom: 0.15rem;
  }
  .tool-detail-name {
    text-transform: capitalize;
  }

  .tool-nested {
    margin-top: 0.2rem;
    margin-left: 1rem;
    font-size: 0.64rem;
  }
  .tool-nested > summary {
    cursor: pointer;
    color: var(--text-faint);
    opacity: 0.65;
    padding: 0.1rem 0;
    user-select: none;
  }
  .tool-nested > summary:hover {
    opacity: 0.9;
  }

  .tool-pre {
    margin: 0.2rem 0 0;
    padding: 0.4rem 0.55rem;
    background: light-dark(#f0eceb, #25282b);
    border-radius: 0.3rem;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 220px;
    overflow-y: auto;
    color: var(--text);
    font-size: 0.62rem;
    line-height: 1.45;
  }

  .tool-spinner {
    width: 0.65rem;
    height: 0.65rem;
    border: 1.5px solid var(--accent);
    border-top-color: transparent;
    border-radius: 50%;
    animation: tool-spin 0.6s linear infinite;
    flex-shrink: 0;
  }
  .tool-check {
    color: var(--accent);
    font-size: 0.7rem;
    flex-shrink: 0;
  }
  @keyframes tool-spin {
    to { transform: rotate(360deg); }
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

  /* ── model picker ───────────────────────────────────────────────────────── */
  .model-picker {
    position: relative;
  }

  .model-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    background: none;
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 0.18rem 0.5rem 0.18rem 0.4rem;
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: 0.7rem;
    color: var(--text-muted);
    transition: border-color 80ms, color 80ms;
    white-space: nowrap;
  }
  .model-chip:hover:not(:disabled) {
    border-color: color-mix(in srgb, var(--accent) 50%, transparent);
    color: var(--text);
  }
  .model-chip:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .tier-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .tier-dot.free  { background: #4ade80; }
  .tier-dot.pool  { background: var(--accent); }

  .chip-label {
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .chip-caret {
    opacity: 0.5;
    flex-shrink: 0;
  }

  .model-menu {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0.3rem;
    min-width: 190px;
    max-height: 260px;
    overflow-y: auto;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.12);
    z-index: 200;
  }

  .model-group-hdr {
    font-family: var(--font-sans);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0.4rem 0.5rem 0.2rem;
  }

  .model-opt {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    padding: 0.32rem 0.5rem;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text);
    cursor: pointer;
    transition: background 60ms;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .model-opt:hover { background: var(--bg-page); }
  .model-opt.sel {
    color: var(--accent);
    font-weight: 500;
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
    .msgs { padding: 1rem; }
    .msg { max-width: 92%; }
    .input-bar { padding: 0.5rem 0.75rem 0.85rem; }
  }
</style>
