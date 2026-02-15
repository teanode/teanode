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

// Agent types

export interface AgentInfo {
  id: string;
  name?: string;
}

// RPC method payloads

export interface ConnectResult {
  version: string;
  capabilities: string[];
  defaultModel: string;
  agents: AgentInfo[];
  defaultAgentId: string;
}

export interface ChatSendParams {
  sessionKey: string;
  message: string;
  model?: string;
  agentId?: string;
}

export interface ChatSendResult {
  runId: string;
  sessionKey: string;
}

export interface ChatHistoryParams {
  sessionKey: string;
  agentId?: string;
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
  agentId?: string;
}

// Domain types

export interface Session {
  key: string;
  title?: string;
  lastActive?: number;
  agentId?: string;
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

// Model types

export interface ModelInfo {
  provider: string;
  id: string;
  context_length?: number;
}

export interface ModelsListResult {
  models: ModelInfo[];
  defaultModel: string;
}

// Cron job types

export interface CronJob {
  id: string;
  name: string;
  schedule: string;
  message: string;
  model?: string;
  agentId?: string;
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
  agentId?: string;
}

export interface CronJobUpdateParams {
  id: string;
  name?: string;
  schedule?: string;
  message?: string;
  model?: string;
  agentId?: string;
  enabled?: boolean;
}

export interface CronsListResult {
  jobs: CronJob[];
}

// Config schema types

export interface SchemaFieldOption {
  value: string;
  label: string;
}

export interface SchemaFieldDef {
  key: string;
  label: string;
  type: 'string' | 'number' | 'boolean' | 'select' | 'password' | 'textarea' | 'stringArray' | 'providers';
  description?: string;
  placeholder?: string;
  default?: unknown;
  options?: SchemaFieldOption[];
  sensitive?: boolean;
}

export interface SchemaSection {
  id: string;
  label: string;
  description?: string;
  fields: SchemaFieldDef[];
}

export interface ConfigSchema {
  sections: SchemaSection[];
}

export interface ConfigSchemaResult {
  schema: ConfigSchema;
}

export interface ConfigGetResult {
  config: Record<string, unknown>;
}

export interface ConfigUpdateParams {
  config: Record<string, unknown>;
}

// Agent config types for the editor

export interface AgentFilterConfig {
  allow?: string[];
  deny?: string[];
}

export interface AgentConfig {
  id: string;
  name?: string;
  model?: string;
  systemPrompt?: string;
  skills?: AgentFilterConfig;
  tools?: AgentFilterConfig;
  canMessage?: string[];
  maxToolRounds?: number;
  compressionThreshold?: number;
  minKeepMessages?: number;
  maxToolResultChars?: number;
  maxWorkspaceFileChars?: number;
}

// Agent config RPC types

export interface AgentsConfigListResult {
  agents: AgentConfig[];
}

export interface AgentConfigSaveParams {
  agent: AgentConfig;
}

export interface AgentConfigDeleteParams {
  id: string;
}

export interface AgentConfigSchemaResult {
  schema: ConfigSchema;
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
