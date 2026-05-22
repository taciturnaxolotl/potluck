/**
 * Local-first conversation cache.
 *
 * Conversations and messages are mirrored into IndexedDB via Dexie. The
 * server is the source of truth — Dexie holds optimistic UI state and a
 * recent slice for offline / fast-paint. On reconnect we re-fetch and
 * upsert by id.
 */

import Dexie, { type Table } from 'dexie';

export interface DBConversation {
  id: string;
  title: string;
  updated_at: number;
  archived_at: number | null;
}

export interface DBMessage {
  id: string; // server id, or client_id while pending
  conversation_id: string;
  client_id: string | null;
  role: 'user' | 'assistant' | 'system' | 'tool';
  content: string;
  model: string | null;
  created_at: number;
  pending?: boolean; // optimistic; cleared on server upsert
}

class PotluckDB extends Dexie {
  conversations!: Table<DBConversation, string>;
  messages!: Table<DBMessage, string>;

  constructor() {
    super('potluck');
    this.version(1).stores({
      conversations: 'id, updated_at, archived_at',
      messages: 'id, conversation_id, [conversation_id+created_at], created_at, client_id'
    });
  }
}

export const db = new PotluckDB();

// Helpers — keep these tiny. Pages do the wiring in $effect blocks.
export async function upsertConversation(c: DBConversation) {
  await db.conversations.put(c);
}

export async function upsertMessages(ms: DBMessage[]) {
  await db.messages.bulkPut(ms);
}

export async function appendAssistantDelta(messageID: string, delta: string) {
  await db.transaction('rw', db.messages, async () => {
    const m = await db.messages.get(messageID);
    if (!m) return;
    m.content += delta;
    await db.messages.put(m);
  });
}
