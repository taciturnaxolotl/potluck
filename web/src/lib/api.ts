/**
 * Tiny `/api` client. Stays mechanical: all data flow is client-first via
 * Dexie + this client. Don't add caching here.
 */

export type APIError = {
  code: string;
  message: string;
};

export class HTTPError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body ? { 'content-type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'include'
  });
  if (!res.ok) {
    let code = 'http_error',
      message = res.statusText;
    try {
      const j = (await res.json()) as { error?: APIError };
      if (j.error) {
        code = j.error.code;
        message = j.error.message;
      }
    } catch {
      /* ignore */
    }
    throw new HTTPError(res.status, code, message);
  }
  // Empty body? Caller should pick a void return.
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  del: <T>(path: string) => request<T>('DELETE', path)
};

// Typed helpers ------------------------------------------------------------

export type User = {
  id: string;
  email: string;
  display_name: string;
  slack_id: { String: string; Valid: boolean } | null;
  created_at: number;
  last_seen_at: number | null;
};

export type Conversation = {
  id: string;
  user_id: string;
  title: string;
  created_at: number;
  updated_at: number;
  archived_at: number | null;
};

export type Message = {
  id: string;
  conversation_id: string;
  client_id: string | null;
  role: 'user' | 'assistant' | 'system' | 'tool';
  content: string;
  model: string | null;
  created_at: number;
};

export const me = () => api.get<User>('/api/me');
export const updateMe = (display_name: string) => api.patch<User>('/api/me', { display_name });
export const balance = () =>
  api.get<{ balance_micros: number; balance_usd: string }>('/api/balance');
export const listConversations = () => api.get<Conversation[]>('/api/conversations');
export const createConversation = (title: string) =>
  api.post<Conversation>('/api/conversations', { title });
export const listMessages = (id: string) => api.get<Message[]>(`/api/conversations/${id}/messages`);

export type PoolStats = {
  balance_micros: number;
  balance_usd: string;
  spent_today_micros: number;
  spent_today_usd: string;
  contributors: number;
  users: number;
  active_keys: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  as_of: number;
};

export const poolStats = () => api.get<PoolStats>('/api/stats');

export type AllocationUser = {
  user_id: string;
  display_name: string;
  email: string;
  key_count: number;
  shared_contribution_micros: number;
  private_reservation_micros: number;
  shared_allowance_today_micros: number;
  shared_spent_today_micros: number;
  private_spent_today_micros: number;
  shared_remaining_today_micros: number;
  share_fraction: number;
};

export type Allocations = {
  pool: {
    total_shared_micros: number;
    spent_today_shared_micros: number;
    remaining_pool_today_micros: number;
    active_key_count: number;
    active_team_count: number;
  };
  users: AllocationUser[];
  last_recompute: {
    at: number;
    by_user_id: string;
    by_display_name: string;
  } | null;
};

export const getAllocations = () => api.get<Allocations>('/api/allocations');

export type APIKey = {
  id: string;
  name: string;
  word: string;
  last4: string;
  masked: string;
  spent_micros: number;
  created_at: number;
  last_used_at: number;
  revoked_at: number;
  revoked: boolean;
};

export const listKeys = () => api.get<APIKey[]>('/api/keys');
export const createKey = (name: string) =>
  api.post<APIKey & { plaintext: string }>('/api/keys', { name });
export const revokeKey = (id: string) => api.del<void>(`/api/keys/${id}`);

export type Session = {
  id: string;
  created_at: number;
  last_used_at: number;
  expires_at: number;
  current: boolean;
  ip: string;
  user_agent: string;
  location: string;
};

export type Model = {
  id: string;
  label: string;
  description: string;
  context_window: number;
  max_output_tokens: number;
  input_per_mil: number;
  output_per_mil: number | null;
  license: string;
  tier: string;
  thinking: boolean;
  image_input: boolean;
  structured_outputs: boolean;
  stats: {
    request_count: number;
    total_input_tokens: number;
    total_output_tokens: number;
    avg_tps: number | null;
  } | null;
};

export type UsageDay = {
  day: number;
  amount_micros: number;
  input_tokens: number;
  output_tokens: number;
};

export type UsageByModel = {
  day: number;
  model: string;
  amount_micros: number;
  input_tokens: number;
  output_tokens: number;
};

export type UsageData = {
  daily: UsageDay[];
  by_model: UsageByModel[];
};

export const getUsage = () => api.get<UsageData>('/api/usage');

export const listModels = () =>
  api.get<{ models: Model[]; refreshed_at: number }>('/api/models');

export const listSessions = () => api.get<Session[]>('/api/sessions');
export const revokeSession = (id: string) => api.del<void>(`/api/sessions/${id}`);

export type PoolKey = {
  id: string;
  user_id: string;
  label: string;
  active: boolean;
  max_micros: number;
  shared_micros: number;
  private_micros: number;
  today_micros: number;
  total_micros: number;
  request_count: number;
  pioneer_health: number; // 0=unknown 1=healthy 2=unauthorized
  pioneer_team_id: string;
  pioneer_payment_plan: string;
  pioneer_credit_limit_micros: number;
  pioneer_remaining_micros: number;
  pending_validation: boolean;
  created_at: number;
  last_used_at: number;
  last_billing_sync_at: number;
  owner_name: string;
  owner_email: string;
  mine: boolean;
};

export const listPoolKeys = () => api.get<PoolKey[]>('/api/pool-keys');
export const addPoolKey = (label: string, apiKey: string, maxMicros?: number, sharedMicros?: number) =>
  api.post<PoolKey & { pending_validation?: boolean; pending_reason?: string }>('/api/pool-keys', {
    label,
    api_key: apiKey,
    ...(maxMicros != null ? { daily_limit_micros: maxMicros } : {})
  });
export const setPoolKeyActive = (id: string, active: boolean) =>
  api.patch<void>(`/api/pool-keys/${id}/active`, { active });
export const updatePoolKeyLabel = (id: string, label: string) =>
  api.patch<void>(`/api/pool-keys/${id}/label`, { label });
export const updatePoolKeyLimits = (id: string, maxMicros: number, sharedMicros: number) =>
  api.patch<void>(`/api/pool-keys/${id}/limits`, { max_micros: maxMicros, shared_micros: sharedMicros });
export const syncPoolKey = (id: string) =>
  api.post<{ today_micros: number }>(`/api/pool-keys/${id}/sync`);
export const deletePoolKey = (id: string) => api.del<void>(`/api/pool-keys/${id}`);
export const recomputeAllocations = () =>
  api.post<Allocations>('/api/allocations/recompute');

export async function logout(): Promise<void> {
  await fetch('/auth/logout', { method: 'POST', credentials: 'include' });
}
