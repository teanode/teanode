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
  const [streamText, setStreamText] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [toolActivity, setToolActivity] = useState<string | null>(null);

  const sessionKeyRef = useRef(sessionKey);
  sessionKeyRef.current = sessionKey;

  const currentRunIdRef = useRef<string | null>(null);
  const activeRunsRef = useRef<Map<string, string>>(new Map());
  const afterToolCallsRef = useRef(false);
  const streamTextRef = useRef('');
  const sessionsRef = useRef(sessions);
  sessionsRef.current = sessions;

  const sessionsRefreshRef = useRef(0);
  const historyLoadedRef = useRef(true);
  const pendingEventsRef = useRef<EventFrame[]>([]);

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

    // Title events: update session list immediately (no message mutation)
    if (chatEvent.state === 'title') {
      setSessions((prev) => {
        const updated = prev.map((session) =>
          session.key === chatEvent.sessionKey ? { ...session, title: chatEvent.title } : session
        );
        sessionsRef.current = updated;
        return updated;
      });
      return;
    }

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

    // Handle user messages from external sources (Discord, Telegram, cron)
    if (chatEvent.state === 'user_message') {
      if (chatEvent.sessionKey === sessionKeyRef.current) {
        currentRunIdRef.current = chatEvent.runId || null;
        if (chatEvent.runId && chatEvent.sessionKey) {
          activeRunsRef.current.set(chatEvent.sessionKey, chatEvent.runId);
        }
        setIsRunning(true);
        setStatus('thinking...');
        setMessages(prev => [
          ...prev,
          { id: nextMessageId(), type: 'user', content: chatEvent.text || '', timestamp: Date.now() },
          { id: nextMessageId(), type: 'assistant', content: '' },
        ]);
        streamTextRef.current = '';
        setStreamText('');
        setIsStreaming(true);
      }
      return;
    }

    // Auto-detect new runs on current session from broadcast events
    if (chatEvent.runId && chatEvent.sessionKey === sessionKeyRef.current && !currentRunIdRef.current) {
      if (chatEvent.state === 'delta' || chatEvent.state === 'tool_call') {
        currentRunIdRef.current = chatEvent.runId;
        activeRunsRef.current.set(chatEvent.sessionKey, chatEvent.runId);
        setIsRunning(true);
        setStatus('thinking...');
        setMessages(prev => [...prev, { id: nextMessageId(), type: 'assistant', content: '' }]);
      }
    }

    // Only update UI for the currently viewed session
    if (!sessionKeyRef.current || chatEvent.sessionKey !== sessionKeyRef.current) return;
    if (currentRunIdRef.current && chatEvent.runId !== currentRunIdRef.current) return;

    // Guard: skip final/error/aborted if we have no active run (avoids corrupting history)
    if (!currentRunIdRef.current && (chatEvent.state === 'final' || chatEvent.state === 'error' || chatEvent.state === 'aborted')) return;

    if (chatEvent.state === 'delta') {
      setToolActivity(null);
      if (afterToolCallsRef.current) {
        // New LLM round after tool calls — finalize old text and start fresh
        const prevText = streamTextRef.current;
        if (prevText) {
          setMessages((prev) => {
            // The last message should be the streaming assistant message — finalize it
            const updated = [...prev];
            if (updated.length > 0 && updated[updated.length - 1].type === 'assistant') {
              updated[updated.length - 1] = {
                ...updated[updated.length - 1],
                content: prevText,
              };
            }
            // Add new streaming message
            updated.push({ id: nextMessageId(), type: 'assistant', content: '' });
            return updated;
          });
        } else {
          // Empty old stream — just reset
          setMessages((prev) => {
            const updated = [...prev];
            // Remove empty streaming message and add a fresh one
            if (updated.length > 0 && updated[updated.length - 1].type === 'assistant' && !updated[updated.length - 1].content) {
              // Reuse it
            } else {
              updated.push({ id: nextMessageId(), type: 'assistant', content: '' });
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
        // Insert tool invoke before the last (streaming) message
        const updated = [...prev];
        const streamIdx = updated.length - 1;
        const toolMsg: DisplayMessage = {
          id: nextMessageId(),
          type: 'tool-invoke',
          content: chatEvent.arguments || '{}',
          toolName: chatEvent.toolName,
          timestamp: Date.now(),
        };
        updated.splice(streamIdx, 0, toolMsg);
        return updated;
      });
      setToolActivity(chatEvent.toolName || null);
      setStatus(`calling ${chatEvent.toolName}...`);
    } else if (chatEvent.state === 'tool_result') {
      setMessages((prev) => {
        const updated = [...prev];
        const streamIdx = updated.length - 1;
        const toolMsg: DisplayMessage = {
          id: nextMessageId(),
          type: 'tool-result',
          content: chatEvent.result || '',
          toolName: chatEvent.toolName,
          timestamp: Date.now(),
        };
        updated.splice(streamIdx, 0, toolMsg);
        return updated;
      });
      setToolActivity(null);
      setStatus('tool done, thinking...');
    } else if (chatEvent.state === 'final') {
      setToolActivity(null);
      const finalText = chatEvent.text || streamTextRef.current;
      const finalTs = Date.now();
      setMessages((prev) => {
        const updated = [...prev];
        // Update last assistant message with final text
        if (updated.length > 0 && updated[updated.length - 1].type === 'assistant') {
          if (finalText) {
            updated[updated.length - 1] = {
              ...updated[updated.length - 1],
              content: finalText,
              timestamp: finalTs,
            };
          } else {
            // Remove empty streaming element
            updated.pop();
          }
        }
        // Add usage
        const usageNumbers = getUsageNumbers(chatEvent.usage);
        if (usageNumbers) {
          updated.push({
            id: nextMessageId(),
            type: 'usage',
            content: `${usageNumbers.input} in / ${usageNumbers.output} out \u00b7 ${usageNumbers.total} tokens`,
            usage: chatEvent.usage,
            timestamp: finalTs,
          });
        }
        return updated;
      });
      finishRun();
    } else if (chatEvent.state === 'error') {
      setToolActivity(null);
      setMessages((prev) => {
        const updated = [...prev];
        if (updated.length > 0 && updated[updated.length - 1].type === 'assistant') {
          if (streamTextRef.current) {
            updated[updated.length - 1] = {
              ...updated[updated.length - 1],
              content: streamTextRef.current,
            };
          } else {
            updated[updated.length - 1] = {
              ...updated[updated.length - 1],
              content: `__error__:${chatEvent.error || 'Unknown error'}`,
            };
          }
        }
        return updated;
      });
      finishRun();
    } else if (chatEvent.state === 'aborted') {
      setToolActivity(null);
      setMessages((prev) => {
        const updated = [...prev];
        if (updated.length > 0 && updated[updated.length - 1].type === 'assistant') {
          if (streamTextRef.current) {
            updated[updated.length - 1] = {
              ...updated[updated.length - 1],
              content: streamTextRef.current,
            };
          } else {
            updated[updated.length - 1] = {
              ...updated[updated.length - 1],
              content: '__aborted__',
            };
          }
        }
        return updated;
      });
      finishRun();
    }
  }, []);

  function finishRun() {
    if (sessionKeyRef.current && currentRunIdRef.current) {
      activeRunsRef.current.delete(sessionKeyRef.current);
    }
    currentRunIdRef.current = null;
    streamTextRef.current = '';
    afterToolCallsRef.current = false;
    setStreamText('');
    setIsStreaming(false);
    setIsRunning(false);
    setStatus('connected');
    setToolActivity(null);
  }

  // sendRpc is defined below but we need it in handleConnect — use a ref
  const sendRpcRef = useRef<(<T = unknown>(method: string, params: unknown) => Promise<T>)>(null!);

  const handleConnect = useCallback((result: ConnectResult) => {
    if (result.defaultModel) {
      setDefaultModel(result.defaultModel);
    }
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
      sendRpcRef.current<ChatHistoryResult>('chat.history', { sessionKey: key })
        .then((res) => {
          if (sessionKeyRef.current !== key) return;
          const displayMessages = convertHistory(res.messages || []);
          if (res.activeRunId) {
            currentRunIdRef.current = res.activeRunId;
            activeRunsRef.current.set(key, res.activeRunId);
            setIsRunning(true);
            setStatus('thinking...');
            displayMessages.push({ id: nextMessageId(), type: 'assistant', content: '' });
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
    (key: string) => {
      // Detach current streaming state
      currentRunIdRef.current = null;
      streamTextRef.current = '';
      afterToolCallsRef.current = false;
      setStreamText('');
      setIsStreaming(false);
      setToolActivity(null);

      setSessionKey(key);
      sessionKeyRef.current = key;
      setMessages([]);

      setIsRunning(false);
      setStatus('connected');

      // Buffer events while history is loading
      historyLoadedRef.current = false;
      pendingEventsRef.current = [];

      sendRpc<ChatHistoryResult>('chat.history', { sessionKey: key })
        .then((res) => {
          if (sessionKeyRef.current !== key) return;
          const displayMessages = convertHistory(res.messages || []);

          // Use activeRunId from server response to detect active runs
          if (res.activeRunId) {
            currentRunIdRef.current = res.activeRunId;
            activeRunsRef.current.set(key, res.activeRunId);
            setIsRunning(true);
            setStatus('thinking...');
            displayMessages.push({ id: nextMessageId(), type: 'assistant', content: '' });
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
    (key: string) => {
      sendRpc('sessions.delete', { sessionKey: key })
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

  const renameSession = useCallback(
    (key: string, title: string) => {
      setSessions((prev) => {
        const updated = prev.map((session) => (session.key === key ? { ...session, title } : session));
        sessionsRef.current = updated;
        return updated;
      });
      sendRpc('sessions.rename', { sessionKey: key, title }).catch((error) =>
        console.error('sessions.rename:', error)
      );
    },
    [sendRpc]
  );

  const sendMessage = useCallback(
    (text: string, model?: string) => {
      if (!text.trim() || isRunning) return;

      // Add user message
      const now = Date.now();
      setMessages((prev) => [
        ...prev,
        { id: nextMessageId(), type: 'user', content: text, timestamp: now },
        { id: nextMessageId(), type: 'assistant', content: '', timestamp: now },
      ]);
      streamTextRef.current = '';
      setStreamText('');
      setIsStreaming(true);
      setIsRunning(true);
      setStatus('thinking...');

      const rpcParams: Record<string, string> = {
        sessionKey: sessionKeyRef.current || '',
        message: text,
      };
      if (model) rpcParams.model = model;

      sendRpc<ChatSendResult>('chat.send', rpcParams)
        .then((res) => {
          currentRunIdRef.current = res.runId;
          activeRunsRef.current.set(res.sessionKey, res.runId);
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
          // Remove the empty streaming element
          setMessages((prev) => {
            const updated = [...prev];
            if (updated.length > 0 && updated[updated.length - 1].type === 'assistant' && !updated[updated.length - 1].content) {
              updated.pop();
            }
            return updated;
          });
          setStatus(`error: ${(error as { message?: string }).message || error}`);
          setIsRunning(false);
          setIsStreaming(false);
        });
    },
    [isRunning, sendRpc]
  );

  const abortRun = useCallback(() => {
    if (!currentRunIdRef.current) return;
    sendRpc('chat.abort', { runId: currentRunIdRef.current }).catch(() => {});
  }, [sendRpc]);

  return {
    sessions,
    sessionKey,
    messages,
    isRunning,
    status,
    defaultModel,
    streamText,
    isStreaming,
    toolActivity,
    sendMessage,
    abortRun,
    switchSession,
    newSession,
    deleteSession,
    renameSession,
    loadSessions,
    sendRpc,
  };
}
