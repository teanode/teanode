// WebSocket RPC frame types (mirrors Go internal/types/types.go)

export interface RequestFrame {
  type: 'req';
  id: string;
  method: string;
  params?: unknown;
}

export interface ResponseFrame {
  type: 'res';
  id: string;
  ok: boolean;
  payload?: unknown;
  error?: RPCError;
}

export interface EventFrame {
  type: 'event';
  event: string;
  payload?: unknown;
}

export interface RPCError {
  code: number;
  message: string;
}

// RPC method payloads

export interface ConnectResult {
  version: string;
  capabilities: string[];
  defaultModel: string;
}

export interface ChatSendParams {
  sessionKey: string;
  message: string;
  model?: string;
}

export interface ChatSendResult {
  runId: string;
  sessionKey: string;
}

export interface ChatHistoryParams {
  sessionKey: string;
}

export interface ChatHistoryResult {
  sessionKey: string;
  messages: Message[];
  activeRunId?: string;
}

export interface ChatAbortParams {
  runId: string;
}

export interface SessionsListResult {
  sessions: Session[];
}

export interface SessionsDeleteParams {
  sessionKey: string;
}

export interface SessionsRenameParams {
  sessionKey: string;
  title: string;
}

// Domain types

export interface Session {
  key: string;
  title?: string;
  lastActive?: number;
}

export interface Usage {
  input?: number;
  Input?: number;
  output?: number;
  Output?: number;
  total?: number;
  Total?: number;
}

export interface ToolCall {
  id?: string;
  function: {
    name: string;
    arguments: string;
  };
}

export interface Message {
  role: 'user' | 'assistant' | 'system' | 'tool';
  content: string | null;
  timestamp?: number;
  stopReason?: string;
  usage?: Usage;
  model?: string;
  provider?: string;
  toolCalls?: ToolCall[] | string;
  toolCallId?: string;
  toolName?: string;
}

// Chat event payloads (server-pushed via WebSocket)

export type ChatEventState =
  | 'user_message'
  | 'delta'
  | 'tool_call'
  | 'tool_result'
  | 'title'
  | 'final'
  | 'error'
  | 'aborted';

export interface ChatEvent {
  state: ChatEventState;
  runId?: string;
  sessionKey?: string;
  text?: string;
  toolName?: string;
  arguments?: string;
  result?: string;
  title?: string;
  error?: string;
  usage?: Usage;
  model?: string;
  stopReason?: string;
}

// Cron job types

export interface CronJob {
  id: string;
  name: string;
  schedule: string;
  message: string;
  model?: string;
  enabled: boolean;
  sessionKey: string;
  lastRun?: number;
  lastStatus?: string;
  lastError?: string;
  createdAt: number;
}

export interface CronJobCreateParams {
  name: string;
  schedule: string;
  message: string;
  model?: string;
}

export interface CronJobUpdateParams {
  id: string;
  name?: string;
  schedule?: string;
  message?: string;
  model?: string;
  enabled?: boolean;
}

export interface CronsListResult {
  jobs: CronJob[];
}

// Display message types for the UI

export type DisplayMessageType =
  | 'user'
  | 'assistant'
  | 'tool-invoke'
  | 'tool-result'
  | 'usage';

export interface DisplayMessage {
  id: string;
  type: DisplayMessageType;
  content: string;
  toolName?: string;
  usage?: Usage;
  timestamp?: number; // ms since epoch
}
