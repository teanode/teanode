import { useState, useCallback, useRef } from 'react';
import type {
  Session,
  DisplayMessage,
  ChatEvent,
  EventFrame,
  ConnectResult,
  ChatSendResult,
  ChatHistoryResult,
  SessionsListResult,
  ModelsListResult,
  AgentsConfigListResult,
  ModelInfo,
  AgentInfo,
  Message,
  ToolCall,
  Usage,
} from '../types';
import { useWebSocket } from './useWebSocket';

let messageIdCounter = 0;
function nextMessageId(): string {
  return `msg-${++messageIdCounter}`;
}

function extractContent(message: Message): string {
  if (!message.content) return '';
  if (typeof message.content === 'string') return message.content;
  try {
    return JSON.parse(message.content as string);
  } catch {
    return String(message.content);
  }
}

function parseToolCalls(raw: ToolCall[] | string | undefined): ToolCall[] {
  if (!raw) return [];
  if (typeof raw === 'string') {
    try {
      return JSON.parse(raw);
    } catch {
      return [];
    }
  }
  return raw;
}

function getUsageNumbers(usage: Usage | undefined): { input: number; output: number; total: number } | null {
  if (!usage) return null;
  const input = usage.input ?? usage.Input ?? 0;
  const output = usage.output ?? usage.Output ?? 0;
  const total = usage.total ?? usage.Total ?? (input + output);
  if (!total) return null;
  return { input, output, total };
}

/** Find the assistant message placeholder for a given runId. */
function findRunAssistantIndex(messages: DisplayMessage[], runId: string | null): number {
  if (!runId) return messages.length - 1;
  for (let index = messages.length - 1; index >= 0; index--) {
    if (messages[index].type === 'assistant' && messages[index].runId === runId) {
      return index;
    }
  }
  return messages.length - 1; // fallback
}

function convertHistory(msgs: Message[]): DisplayMessage[] {
  const displayMessages: DisplayMessage[] = [];
  for (const message of msgs) {
    const content = extractContent(message);
    const timestamp = message.timestamp;
    if (message.role === 'user') {
      displayMessages.push({ id: nextMessageId(), type: 'user', content, timestamp });
    } else if (message.role === 'assistant') {
      const toolCalls = parseToolCalls(message.toolCalls);
      if (toolCalls.length > 0) {
        if (content?.trim()) {
          displayMessages.push({ id: nextMessageId(), type: 'assistant', content, timestamp });
        }
        for (const toolCall of toolCalls) {
          const functionData = toolCall.function || (toolCall as unknown as { name: string; arguments: string });
          displayMessages.push({
            id: nextMessageId(),
            type: 'tool-invoke',
            content: functionData.arguments || '{}',
            toolName: functionData.name || 'tool',
            timestamp,
          });
        }
      } else if (content?.trim()) {
        displayMessages.push({ id: nextMessageId(), type: 'assistant', content, timestamp });
        const usageNumbers = getUsageNumbers(message.usage);
        if (usageNumbers) {
          displayMessages.push({
            id: nextMessageId(),
            type: 'usage',
            content: `${usageNumbers.input} in / ${usageNumbers.output} out \u00b7 ${usageNumbers.total} tokens`,
            usage: message.usage,
            timestamp,
          });
        }
      }
    } else if (message.role === 'tool') {
      displayMessages.push({
        id: nextMessageId(),
        type: 'tool-result',
        content,
        toolName: message.toolName || 'tool',
        timestamp,
      });
    }
  }
  return displayMessages;
}

export function useChat() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionKey, setSessionKey] = useState<string | null>(null);
  const [messages, setMessages] = useState<DisplayMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [status, setStatus] = useState('connecting...');
  const [defaultModel, setDefaultModel] = useState('');
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [streamText, setStreamText] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [toolActivity, setToolActivity] = useState<string | null>(null);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [currentAgentId, setCurrentAgentId] = useState<string>('');
  const [connected, setConnected] = useState(false);
  const currentAgentIdRef = useRef(currentAgentId);

  const sessionKeyRef = useRef(sessionKey);
  sessionKeyRef.current = sessionKey;
  currentAgentIdRef.current = currentAgentId;

  const currentRunIdRef = useRef<string | null>(null);
  const activeRunsRef = useRef<Map<string, string>>(new Map());
  const afterToolCallsRef = useRef(false);
  const streamTextRef = useRef('');
  const sessionsRef = useRef(sessions);
  sessionsRef.current = sessions;

  const sessionsRefreshRef = useRef(0);
  const historyLoadedRef = useRef(true);
  const pendingEventsRef = useRef<EventFrame[]>([]);
  const runQueueRef = useRef<string[]>([]); // ordered run IDs: [active, queued1, queued2, ...]

  function touchSession(key: string) {
    const now = Date.now();
    setSessions((previous) => {
      const updated = previous.map((session) =>
        session.key === key ? { ...session, lastActive: now } : session
      );
      sessionsRef.current = updated;
      return updated;
    });
  }

  function finishCurrentRun() {
    streamTextRef.current = '';
    afterToolCallsRef.current = false;
    setStreamText('');
    setIsStreaming(false);
    setToolActivity(null);

    // Remove finished run from queue
    if (currentRunIdRef.current) {
      const index = runQueueRef.current.indexOf(currentRunIdRef.current);
      if (index !== -1) runQueueRef.current.splice(index, 1);
    }

    // Promote next queued run or finish
    if (runQueueRef.current.length > 0) {
      currentRunIdRef.current = runQueueRef.current[0];
      setStatus('thinking...');
      // Keep isRunning = true
    } else {
      currentRunIdRef.current = null;
      if (sessionKeyRef.current) {
        activeRunsRef.current.delete(sessionKeyRef.current);
      }
      setIsRunning(false);
      setStatus('connected');
    }
  }

  const handleEvent = useCallback((frame: EventFrame) => {
    if (frame.event === 'sessions') {
      const now = Date.now();
      if (now - sessionsRefreshRef.current < 2000) return;
      sessionsRefreshRef.current = now;
      sendRpcRef.current<SessionsListResult>('sessions.list', {}).then((res) => {
        const list = res.sessions || [];
        setSessions(list);
        sessionsRef.current = list;
      }).catch((error: unknown) => console.error('sessions.list (event):', error));
      return;
    }

    if (frame.event !== 'chat') return;
    const chatEvent = frame.payload as ChatEvent;
    if (!chatEvent) return;

    // Clean up activeRuns for completed runs (no message mutation)
    if (chatEvent.state === 'final' || chatEvent.state === 'error' || chatEvent.state === 'aborted') {
      if (chatEvent.sessionKey && activeRunsRef.current.get(chatEvent.sessionKey) === chatEvent.runId) {
        activeRunsRef.current.delete(chatEvent.sessionKey);
      }
    }

    // Buffer events for the current session while history is loading
    if (!historyLoadedRef.current && chatEvent.sessionKey === sessionKeyRef.current) {
      pendingEventsRef.current.push(frame);
      return;
    }

    // Handle queued events early — no UI update needed, placeholder is already visible
    if (chatEvent.state === 'queued') {
      return;
    }

    // Handle user messages from external sources (Discord, Telegram, cron)
    if (chatEvent.state === 'user_message') {
      if (chatEvent.sessionKey) touchSession(chatEvent.sessionKey);
      if (chatEvent.sessionKey === sessionKeyRef.current) {
        currentRunIdRef.current = chatEvent.runId || null;
        if (chatEvent.runId && chatEvent.sessionKey) {
          activeRunsRef.current.set(chatEvent.sessionKey, chatEvent.runId);
        }
        if (chatEvent.runId) {
          runQueueRef.current.push(chatEvent.runId);
        }
        setIsRunning(true);
        setStatus('thinking...');
        const assistantMessageId = nextMessageId();
        setMessages(prev => [
          ...prev,
          { id: nextMessageId(), type: 'user', content: chatEvent.text || '', timestamp: Date.now() },
          { id: assistantMessageId, type: 'assistant', content: '', runId: chatEvent.runId || undefined },
        ]);
        streamTextRef.current = '';
        setStreamText('');
        // Don't set isStreaming — let the delta event set it
      }
      return;
    }

    // Auto-detect new runs on current session from broadcast events
    if (chatEvent.runId && chatEvent.sessionKey === sessionKeyRef.current && !currentRunIdRef.current) {
      if (chatEvent.state === 'delta' || chatEvent.state === 'tool_call') {
        currentRunIdRef.current = chatEvent.runId;
        activeRunsRef.current.set(chatEvent.sessionKey, chatEvent.runId);
        runQueueRef.current.push(chatEvent.runId);
        setIsRunning(true);
        setStatus('thinking...');
        setMessages(prev => [...prev, { id: nextMessageId(), type: 'assistant', content: '', runId: chatEvent.runId || undefined }]);
      }
    }

    // Only update UI for the currently viewed session
    if (!sessionKeyRef.current || chatEvent.sessionKey !== sessionKeyRef.current) return;
    if (currentRunIdRef.current && chatEvent.runId !== currentRunIdRef.current) return;

    // Guard: skip final/error/aborted if we have no active run (avoids corrupting history)
    if (!currentRunIdRef.current && (chatEvent.state === 'final' || chatEvent.state === 'error' || chatEvent.state === 'aborted')) return;

    const eventRunId = chatEvent.runId || null;

    if (chatEvent.state === 'delta') {
      setToolActivity(null);
      if (afterToolCallsRef.current) {
        // New LLM round after tool calls — finalize old text and start fresh
        const prevText = streamTextRef.current;
        if (prevText) {
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(updated, eventRunId);
            if (assistantIndex >= 0 && updated[assistantIndex].type === 'assistant') {
              updated[assistantIndex] = {
                ...updated[assistantIndex],
                content: prevText,
              };
            }
            // Add new streaming message after the finalized one
            const newAssistant: DisplayMessage = { id: nextMessageId(), type: 'assistant', content: '', runId: eventRunId || undefined };
            updated.splice(assistantIndex + 1, 0, newAssistant);
            return updated;
          });
        } else {
          // Empty old stream — just reset, reuse existing placeholder
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(updated, eventRunId);
            if (assistantIndex >= 0 && updated[assistantIndex].type === 'assistant' && !updated[assistantIndex].content) {
              // Reuse it
            } else {
              const newAssistant: DisplayMessage = { id: nextMessageId(), type: 'assistant', content: '', runId: eventRunId || undefined };
              updated.splice(assistantIndex + 1, 0, newAssistant);
            }
            return updated;
          });
        }
        streamTextRef.current = '';
        setStreamText('');
        afterToolCallsRef.current = false;
      }
      streamTextRef.current += chatEvent.text || '';
      setStreamText(streamTextRef.current);
      setIsStreaming(true);
    } else if (chatEvent.state === 'tool_call') {
      afterToolCallsRef.current = true;
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        const toolMsg: DisplayMessage = {
          id: nextMessageId(),
          type: 'tool-invoke',
          content: chatEvent.arguments || '{}',
          toolName: chatEvent.toolName,
          timestamp: Date.now(),
        };
        // Insert tool invoke before the run's assistant (streaming) message
        updated.splice(assistantIndex, 0, toolMsg);
        return updated;
      });
      setIsStreaming(false); // FIX: clear streaming so thinking spinner shows during tool execution
      setToolActivity(chatEvent.toolName || null);
      setStatus(`calling ${chatEvent.toolName}...`);
    } else if (chatEvent.state === 'tool_result') {
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        const toolMsg: DisplayMessage = {
          id: nextMessageId(),
          type: 'tool-result',
          content: chatEvent.result || '',
          toolName: chatEvent.toolName,
          timestamp: Date.now(),
        };
        // Insert tool result before the run's assistant (streaming) message
        updated.splice(assistantIndex, 0, toolMsg);
        return updated;
      });
      setToolActivity(null);
      setStatus('tool done, thinking...');
    } else if (chatEvent.state === 'final') {
      if (chatEvent.sessionKey) touchSession(chatEvent.sessionKey);
      setToolActivity(null);
      const finalText = chatEvent.text || streamTextRef.current;
      const finalTimestamp = Date.now();
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        if (assistantIndex >= 0 && updated[assistantIndex].type === 'assistant') {
          if (finalText) {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: finalText,
              timestamp: finalTimestamp,
            };
          } else {
            // Remove empty streaming element
            updated.splice(assistantIndex, 1);
          }
        }
        // Add usage
        const usageNumbers = getUsageNumbers(chatEvent.usage);
        if (usageNumbers) {
          // Insert usage after the assistant message (or at the position it was)
          const insertPosition = finalText ? assistantIndex + 1 : assistantIndex;
          updated.splice(insertPosition, 0, {
            id: nextMessageId(),
            type: 'usage',
            content: `${usageNumbers.input} in / ${usageNumbers.output} out \u00b7 ${usageNumbers.total} tokens`,
            usage: chatEvent.usage,
            timestamp: finalTimestamp,
          });
        }
        return updated;
      });
      finishCurrentRun();
    } else if (chatEvent.state === 'error') {
      setToolActivity(null);
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        if (assistantIndex >= 0 && updated[assistantIndex].type === 'assistant') {
          if (streamTextRef.current) {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: streamTextRef.current,
            };
          } else {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: `__error__:${chatEvent.error || 'Unknown error'}`,
            };
          }
        }
        return updated;
      });
      finishCurrentRun();
    } else if (chatEvent.state === 'aborted') {
      setToolActivity(null);
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        if (assistantIndex >= 0 && updated[assistantIndex].type === 'assistant') {
          if (streamTextRef.current) {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: streamTextRef.current,
            };
          } else {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: '__aborted__',
            };
          }
        }
        return updated;
      });
      finishCurrentRun();
    }
  }, []);

  // sendRpc is defined below but we need it in handleConnect — use a ref
  const sendRpcRef = useRef<(<T = unknown>(method: string, params: unknown) => Promise<T>)>(null!);

  const handleConnect = useCallback((result: ConnectResult) => {
    setConnected(true);
    if (result.defaultModel) {
      setDefaultModel(result.defaultModel);
    }
    if (result.agents) {
      setAgents(result.agents);
    }
    if (result.defaultAgentId && !currentAgentIdRef.current) {
      setCurrentAgentId(result.defaultAgentId);
      currentAgentIdRef.current = result.defaultAgentId;
    }
    // Fetch available models
    sendRpcRef.current<ModelsListResult>('models.list', {})
      .then((res) => {
        if (res.models) setModels(res.models);
      })
      .catch((error: unknown) => console.error('models.list:', error));

    // Load sessions on every (re)connect
    sendRpcRef.current<SessionsListResult>('sessions.list', {})
      .then((res) => {
        const list = res.sessions || [];
        setSessions(list);
        sessionsRef.current = list;
      })
      .catch((error: unknown) => console.error('sessions.list:', error));

    // Reload current session's history on (re)connect
    const key = sessionKeyRef.current;
    if (key) {
      historyLoadedRef.current = false;
      pendingEventsRef.current = [];
      sendRpcRef.current<ChatHistoryResult>('chat.history', { sessionKey: key, agentId: currentAgentIdRef.current || undefined })
        .then((res) => {
          if (sessionKeyRef.current !== key) return;
          const displayMessages = convertHistory(res.messages || []);
          if (res.activeRunId) {
            currentRunIdRef.current = res.activeRunId;
            activeRunsRef.current.set(key, res.activeRunId);
            runQueueRef.current = [res.activeRunId];
            setIsRunning(true);
            setStatus('thinking...');
            displayMessages.push({ id: nextMessageId(), type: 'assistant', content: '', runId: res.activeRunId });
          }
          setMessages(displayMessages);
          historyLoadedRef.current = true;
          // Replay buffered events (only if run is still active — otherwise history is complete)
          if (res.activeRunId && pendingEventsRef.current.length > 0) {
            for (const event of pendingEventsRef.current) {
              handleEvent(event);
            }
          }
          pendingEventsRef.current = [];
        })
        .catch((error: unknown) => console.error('chat.history reconnect:', error));
    }
  }, []);

  const { sendRpc } = useWebSocket({
    onEvent: handleEvent,
    onConnect: handleConnect,
    onStatusChange: setStatus,
  });

  sendRpcRef.current = sendRpc;

  // Load sessions (callable externally if needed)
  const loadSessions = useCallback(() => {
    sendRpc<SessionsListResult>('sessions.list', {})
      .then((res) => {
        const list = res.sessions || [];
        setSessions(list);
        sessionsRef.current = list;
      })
      .catch((error) => console.error('sessions.list:', error));
  }, [sendRpc]);

  const switchSession = useCallback(
    (key: string, agentId?: string) => {
      // Detach current streaming state
      currentRunIdRef.current = null;
      streamTextRef.current = '';
      afterToolCallsRef.current = false;
      runQueueRef.current = [];
      setStreamText('');
      setIsStreaming(false);
      setToolActivity(null);

      // Switch agent if a different one is specified.
      if (agentId && agentId !== currentAgentIdRef.current) {
        setCurrentAgentId(agentId);
        currentAgentIdRef.current = agentId;
      }

      const resolvedAgentId = agentId || currentAgentIdRef.current || undefined;

      setSessionKey(key);
      sessionKeyRef.current = key;
      setMessages([]);

      setIsRunning(false);
      setStatus('connected');

      // Buffer events while history is loading
      historyLoadedRef.current = false;
      pendingEventsRef.current = [];

      sendRpc<ChatHistoryResult>('chat.history', { sessionKey: key, agentId: resolvedAgentId })
        .then((res) => {
          if (sessionKeyRef.current !== key) return;
          const displayMessages = convertHistory(res.messages || []);

          // Use activeRunId from server response to detect active runs
          if (res.activeRunId) {
            currentRunIdRef.current = res.activeRunId;
            activeRunsRef.current.set(key, res.activeRunId);
            runQueueRef.current = [res.activeRunId];
            setIsRunning(true);
            setStatus('thinking...');
            displayMessages.push({ id: nextMessageId(), type: 'assistant', content: '', runId: res.activeRunId });
          }

          setMessages(displayMessages);
          historyLoadedRef.current = true;

          // Replay buffered events (only if run is still active — otherwise history is complete)
          if (res.activeRunId && pendingEventsRef.current.length > 0) {
            for (const event of pendingEventsRef.current) {
              handleEvent(event);
            }
          }
          pendingEventsRef.current = [];
        })
        .catch((error) => console.error('chat.history:', error));
    },
    [sendRpc, handleEvent]
  );

  const newSession = useCallback(() => {
    currentRunIdRef.current = null;
    streamTextRef.current = '';
    afterToolCallsRef.current = false;
    runQueueRef.current = [];
    setStreamText('');
    setIsStreaming(false);
    setIsRunning(false);
    setToolActivity(null);
    setSessionKey(null);
    sessionKeyRef.current = null;
    setMessages([]);
    setStatus('connected');
  }, []);

  const deleteSession = useCallback(
    (key: string, agentId?: string) => {
      sendRpc('sessions.delete', { sessionKey: key, agentId })
        .then(() => {
          setSessions((prev) => {
            const updated = prev.filter((session) => session.key !== key);
            sessionsRef.current = updated;
            return updated;
          });
          if (sessionKeyRef.current === key) {
            setSessionKey(null);
            sessionKeyRef.current = null;
            setMessages([]);
          }
        })
        .catch((error) => console.error('sessions.delete:', error));
    },
    [sendRpc]
  );

  const sendMessage = useCallback(
    (text: string, model?: string) => {
      if (!text.trim()) return;
      // Allow sending while running — backend queues per-session

      const now = Date.now();
      const assistantMessageId = nextMessageId();
      setMessages((prev) => [
        ...prev,
        { id: nextMessageId(), type: 'user', content: text, timestamp: now },
        { id: assistantMessageId, type: 'assistant', content: '', timestamp: now },
      ]);

      if (!isRunning) {
        // First message — set running state
        streamTextRef.current = '';
        setStreamText('');
        setIsRunning(true);
        setStatus('thinking...');
      }
      // Don't set isStreaming true — let the delta event set it
      setIsStreaming(false);

      const rpcParams: Record<string, string> = {
        sessionKey: sessionKeyRef.current || '',
        message: text,
      };
      if (model) rpcParams.model = model;
      if (currentAgentIdRef.current) rpcParams.agentId = currentAgentIdRef.current;

      sendRpc<ChatSendResult>('chat.send', rpcParams)
        .then((res) => {
          // Tag assistant placeholder with runId
          setMessages((prev) =>
            prev.map((message) =>
              message.id === assistantMessageId ? { ...message, runId: res.runId } : message
            )
          );
          runQueueRef.current.push(res.runId);
          if (!currentRunIdRef.current) {
            currentRunIdRef.current = res.runId;
          }
          activeRunsRef.current.set(res.sessionKey, res.runId);
          touchSession(res.sessionKey);
          if (!sessionKeyRef.current) {
            setSessionKey(res.sessionKey);
            sessionKeyRef.current = res.sessionKey;
            setSessions((prev) => {
              const exists = prev.some((session) => session.key === res.sessionKey);
              if (!exists) {
                const updated = [
                  { key: res.sessionKey, lastActive: Date.now() },
                  ...prev,
                ];
                sessionsRef.current = updated;
                return updated;
              }
              return prev;
            });
          }
        })
        .catch((error) => {
          // Remove both user message and empty assistant placeholder
          setMessages((prev) => {
            const updated = [...prev];
            // Remove empty assistant placeholder
            if (updated.length > 0 && updated[updated.length - 1].type === 'assistant'
                && updated[updated.length - 1].id === assistantMessageId) {
              updated.pop();
              // Also remove the user message we just added
              if (updated.length > 0 && updated[updated.length - 1].type === 'user') {
                updated.pop();
              }
            }
            return updated;
          });
          setStatus(`error: ${(error as { message?: string }).message || error}`);
          // Only clear running state if no other runs in queue
          if (runQueueRef.current.length === 0) {
            setIsRunning(false);
            setIsStreaming(false);
          }
        });
    },
    [isRunning, sendRpc]
  );

  const abortRun = useCallback(() => {
    if (!currentRunIdRef.current) return;
    sendRpc('chat.abort', { runId: currentRunIdRef.current }).catch(() => {});
  }, [sendRpc]);

  const refreshAgents = useCallback(() => {
    sendRpc<AgentsConfigListResult>('agents.config.list', {})
      .then((result) => {
        if (result.agents) setAgents(result.agents.map((agent) => ({ id: agent.id })));
      })
      .catch((error: unknown) => console.error('agents.config.list:', error));
  }, [sendRpc]);

  return {
    sessions,
    sessionKey,
    messages,
    isRunning,
    status,
    defaultModel,
    models,
    streamText,
    isStreaming,
    toolActivity,
    agents,
    currentAgentId,
    connected,
    currentRunId: currentRunIdRef.current,
    setCurrentAgentId,
    sendMessage,
    abortRun,
    switchSession,
    newSession,
    deleteSession,
    loadSessions,
    refreshAgents,
    sendRpc,
  };
}
