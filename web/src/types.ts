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
  activeConversationId?: string;
}

// RPC method payloads

export interface ConnectResult {
  version: string;
  capabilities: string[];
  defaultModel: string;
  agents: AgentInfo[];
  defaultAgentId: string;
  activeAgentId?: string;
  activeConversationId?: string;
}

export interface AgentsSetActiveResult {
  activeAgentId: string;
  activeConversationId: string;
}

export interface ConversationsSetActiveResult {
  activeAgentId: string;
  activeConversationId: string;
}

export interface ConversationSendParams {
  conversationId: string;
  message: string;
  model?: string;
  agentId?: string;
}

export interface ConversationSendResult {
  runId: string;
  conversationId: string;
}

export interface ConversationHistoryParams {
  conversationId: string;
  agentId?: string;
  limit?: number;
  beforeIndex?: number;
}

export interface ConversationHistoryResult {
  conversationId: string;
  messages: Message[];
  activeRunId?: string;
  hasMore?: boolean;
  totalCount?: number;
  oldestLoadedIndex?: number;
}

export interface ConversationAbortParams {
  runId: string;
}

export interface ConversationsListResult {
  conversations: Conversation[];
}

export interface ConversationsDeleteParams {
  conversationId: string;
  agentId?: string;
}

// Domain types

export interface Conversation {
  id: string;
  title?: string;
  summary?: string;
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

// Conversation event payloads (server-pushed via WebSocket)

export type ConversationEventState =
  | 'user_message'
  | 'queued'
  | 'delta'
  | 'tool_call'
  | 'tool_result'
  | 'title'
  | 'final'
  | 'error'
  | 'aborted';

export interface ConversationEvent {
  state: ConversationEventState;
  runId?: string;
  conversationId?: string;
  text?: string;
  toolName?: string;
  arguments?: string;
  result?: string;
  title?: string;
  error?: string;
  usage?: Usage;
  model?: string;
  stopReason?: string;
  originId?: string;
  contextWindow?: number;
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

// Job types

export interface Job {
  id: string;
  name: string;
  schedule: string;
  message: string;
  model?: string;
  agentId?: string;
  enabled: boolean;
  conversationId: string;
  runAt?: number;
  oneShot?: boolean;
  lastRun?: number;
  lastStatus?: string;
  lastError?: string;
  createdAt: number;
}

export interface JobCreateParams {
  name: string;
  schedule: string;
  message: string;
  model?: string;
  agentId?: string;
}

export interface JobUpdateParams {
  id: string;
  name?: string;
  schedule?: string;
  message?: string;
  model?: string;
  agentId?: string;
  enabled?: boolean;
}

export interface JobsListResult {
  jobs: Job[];
}

// Config schema types (JSON Schema with x-sections extension)

export interface JsonSchemaProperty {
  type?: string;
  title?: string;
  description?: string;
  default?: unknown;
  enum?: string[];
  format?: string;
  items?: JsonSchemaProperty;
  properties?: Record<string, JsonSchemaProperty>;
  additionalProperties?: JsonSchemaProperty;
  'x-placeholder'?: string;
  'x-widget'?: string;
  'x-suggest'?: string;
  'x-enumLabels'?: Record<string, string>;
}

export interface SchemaSection {
  id: string;
  title: string;
  description?: string;
  path?: string;
  properties?: string[];
}

export interface ConfigSchema {
  properties: Record<string, JsonSchemaProperty>;
  'x-sections': SchemaSection[];
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

export interface AgentConfig {
  id: string;
  name?: string;
  model?: string;
  systemPrompt?: string;
  skills?: string[];
  tools?: string[];
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
  suggestions?: Record<string, string[]>;
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
  runId?: string;     // associates message with a run for queuing
}
