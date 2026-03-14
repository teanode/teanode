// WebSocket RPC frame types (mirrors Go internal/types/types.go)

export interface RequestFrame {
  type: "req";
  id: string;
  method: string;
  params?: unknown;
}

export interface ResponseFrame {
  type: "res";
  id: string;
  ok: boolean;
  payload?: unknown;
  error?: RPCError;
}

export interface EventFrame {
  type: "event";
  event: string;
  payload?: unknown;
}

export interface RPCError {
  code: number;
  message: string;
}

export interface Profile {
  name: string;
  description?: string;
  avatarMediaId?: string;
}

// Agent types

export interface AgentInfo {
  id: string;
  name?: string;
  avatarMediaId?: string;
  defaultConversationId?: string;
}

// RPC method payloads

export interface ConnectResult {
  version: string;
  capabilities: string[];
  defaultProviderModelName: string;
  agents: AgentInfo[];
  defaultAgentId: string;
  defaultConversationId?: string;
  isAdmin?: boolean;
  userId?: string;
}

export interface AgentsSetDefaultResult {
  defaultAgentId: string;
  defaultConversationId: string;
}

export interface ConversationsSetDefaultResult {
  defaultAgentId: string;
  defaultConversationId: string;
}

export interface ConversationSendParams {
  conversationId: string;
  message: string;
  providerModelName?: string;
  agentId?: string;
  attachments?: Attachment[];
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

export interface ActiveRunState {
  phase: "thinking" | "tool" | "streaming";
  toolName?: string;
}

export interface ConversationHistoryResult {
  conversationId: string;
  messages: Message[];
  activeRunId?: string;
  activeRunState?: ActiveRunState;
  hasMore?: boolean;
  totalCount?: number;
  oldestLoadedIndex?: number;
  providerName?: string;
  providerModelName?: string;
}

export interface ConversationAbortParams {
  runId?: string;
  conversationId?: string;
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
  providerName?: string;
  providerModelName?: string;
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
  role: "user" | "assistant" | "system" | "tool";
  content: string | null;
  timestamp?: number;
  stopReason?: string;
  usage?: Usage;
  providerModelName?: string;
  providerName?: string;
  toolCalls?: ToolCall[] | string;
  toolCallId?: string;
  toolName?: string;
}

// Conversation event payloads (server-pushed via WebSocket)

export type ConversationEventState =
  | "user_message"
  | "queued"
  | "delta"
  | "text_done"
  | "tool_call"
  | "tool_result"
  | "title"
  | "final"
  | "error"
  | "aborted";

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
  providerModelName?: string;
  stopReason?: string;
  originId?: string;
  contextWindow?: number;
  attachments?: Attachment[];
}

// Model types

export interface ModelInfo {
  providerName: string;
  id: string;
  context_length?: number;
}

export interface ModelsListResult {
  models: ModelInfo[];
  defaultProviderModelName: string;
}

// Job types

export interface Job {
  id: string;
  name: string;
  schedule: string;
  prompt: string;
  providerModelName?: string;
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
  prompt: string;
  providerModelName?: string;
  agentId?: string;
}

export interface JobUpdateParams {
  id: string;
  name?: string;
  schedule?: string;
  prompt?: string;
  providerModelName?: string;
  agentId?: string;
  enabled?: boolean;
}

export interface JobsListResult {
  jobs: Job[];
}

// Config schema types (JSON Schema with x-sections extension)

export interface JSONSchemaProperty {
  type?: string;
  title?: string;
  titleKey?: string;
  description?: string;
  descriptionKey?: string;
  default?: unknown;
  enum?: string[];
  format?: string;
  items?: JSONSchemaProperty;
  properties?: Record<string, JSONSchemaProperty>;
  additionalProperties?: JSONSchemaProperty;
  "x-placeholder"?: string;
  "x-placeholderKey"?: string;
  "x-widget"?: string;
  "x-suggest"?: string;
  "x-enumLabels"?: Record<string, string>;
  "x-enumLabelKeys"?: Record<string, string>;
  "x-titleKey"?: string;
  "x-descriptionKey"?: string;
}

export interface SchemaSection {
  id: string;
  title: string;
  titleKey?: string;
  description?: string;
  descriptionKey?: string;
  path?: string;
  properties?: string[];
  "x-titleKey"?: string;
  "x-descriptionKey"?: string;
}

export interface ConfigSchema {
  properties: Record<string, JSONSchemaProperty>;
  "x-sections": SchemaSection[];
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

export interface VoiceProvidersResult {
  transcribers: string[];
  streamingTranscribers: string[];
  synthesizers: string[];
  streamingSynthesizers: string[];
}

// Agent config types for the editor

export interface AgentConfig {
  id: string;
  name?: string;
  avatarMediaId?: string;
  providerModelName?: string;
  skills?: string[];
  tools?: string[];
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

// Auth types

export interface AuthStatusResult {
  passwordSet: boolean;
  authenticated: boolean;
  isAdmin?: boolean;
}

export interface AuthTokenInfo {
  id: string;
  token: string;
  createdAt?: string;
  lastUsedAt?: string;
  remoteAddress?: string;
  userAgent?: string;
}

export interface UserInfo {
  id: string;
  username: string;
  admin: boolean;
  hasPassword: boolean;
  name?: string;
  description?: string;
  avatarMediaId?: string;
}

export interface SessionInfo {
  id: string;
  createdAt: string;
  expiresAt: string;
  userAgent: string;
  remoteAddress: string;
  lastSeenAt: string;
}

export interface SessionsListResult {
  sessions: SessionInfo[];
  currentSessionId: string;
}

// Attachment types

export interface Attachment {
  mediaId: string;
  format: string;
  filename: string;
}

// Todo types

export interface Todo {
  id: string;
  projectId?: string;
  conversationId?: string;
  title?: string;
  description?: string;
  status?: string;
  priority?: string;
  tags?: string[];
  completedAt?: string;
  createdAt?: string;
  modifiedAt?: string;
}

export interface ConversationTodoBatchResult {
  index: number;
  op: string;
  success: boolean;
  todo?: Todo;
  todoId?: string;
  error?: string;
}

export interface ConversationTodosEvent {
  conversationId: string;
  userId: string;
  action: string;
  results?: ConversationTodoBatchResult[];
}

export interface ConversationTodosListResult {
  action: string;
  todos: Todo[];
  totalCount: number;
  openCount: number;
  doneCount: number;
}

// Pending question types (ask_user_question tool)

export interface PendingQuestion {
  id: string;
  conversationId: string;
  agentId: string;
  runId: string;
  question: string;
  choices: string[];
  allowOther?: boolean;
  otherLabel?: string;
  otherPlaceholder?: string;
}

export interface PendingQuestionsListResult {
  questions: PendingQuestion[];
}

export interface ConversationQuestionsEvent {
  action: string;
  conversationId?: string;
  agentId?: string;
  userId?: string;
  runId?: string;
  questionId: string;
  question?: string;
  choices?: string[];
  allowOther?: boolean;
  otherLabel?: string;
  otherPlaceholder?: string;
  answer?: string;
  other?: string;
}

// Pending approval types (tool approval system)

export interface PendingApproval {
  id: string;
  conversationId: string;
  agentId: string;
  userId: string;
  runId: string;
  toolCallId: string;
  toolName: string;
  arguments: string;
  policyReason: string;
  risk?: string;
}

export interface PendingApprovalsListResult {
  approvals: PendingApproval[];
}

export interface ConversationApprovalsEvent {
  action: string; // "requested" | "resolved"
  approvalId: string;
  conversationId?: string;
  agentId?: string;
  userId?: string;
  runId?: string;
  toolCallId?: string;
  toolName?: string;
  arguments?: string;
  policyReason?: string;
  risk?: string;
  verdict?: string;
  reason?: string;
}

// Display message types for the UI

export type DisplayMessageType =
  | "user"
  | "assistant"
  | "tool-invoke"
  | "tool-result"
  | "usage";

export interface DisplayMessage {
  id: string;
  type: DisplayMessageType;
  content: string;
  toolName?: string;
  toolCallId?: string; // links tool-invoke ↔ tool-result for stable ordering
  usage?: Usage;
  timestamp?: number; // ms since epoch
  runId?: string; // associates message with a runner for queuing
  attachments?: Attachment[];
}

// Tool policy types

export type ToolPolicyLevel =
  | "disabled"
  | "admin_approval"
  | "admin_only"
  | "anyone_approval"
  | "anyone";

export type ToolPolicyGroup = "*" | "read" | "write";

export interface ToolPolicyConfiguration {
  tool: string;
  group: ToolPolicyGroup;
  level: ToolPolicyLevel;
}

export interface ToolActionGroupEntry {
  group: ToolPolicyGroup;
  defaultPolicy: ToolPolicyLevel;
}

export interface ToolActionEntry {
  name: string;
  groups: ToolActionGroupEntry[];
  source: "builtin" | "skill";
  skill?: string;
}

export interface ToolPoliciesListResult {
  tools: ToolActionEntry[];
  policies: ToolPolicyConfiguration[];
}

// Memory types

export interface MemoryItem {
  id: string;
  title?: string;
  content?: string;
  tags?: string[];
  scope?: string;
  scopeId?: string;
  createdAt?: string;
  modifiedAt?: string;
  archivedAt?: string;
}
