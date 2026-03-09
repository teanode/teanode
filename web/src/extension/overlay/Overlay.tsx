import React, { useState, useEffect, useCallback, useRef } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Drawer from "@mui/material/Drawer";
import FormControlLabel from "@mui/material/FormControlLabel";
import IconButton from "@mui/material/IconButton";
import List from "@mui/material/List";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemText from "@mui/material/ListItemText";
import Switch from "@mui/material/Switch";
import Typography from "@mui/material/Typography";
import Divider from "@mui/material/Divider";
import Alert from "@mui/material/Alert";
import AddRounded from "@mui/icons-material/AddRounded";
import BugReportRounded from "@mui/icons-material/BugReportRounded";
import ExpandMoreRounded from "@mui/icons-material/ExpandMoreRounded";
import Tooltip from "@mui/material/Tooltip";
import { keyframes } from "@mui/material/styles";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import {
  sendRpc,
  onEvent,
  ensureConnected,
  disconnect,
  onConnectionStateChange,
} from "./rpc";

dayjs.extend(relativeTime);
import { ChatView } from "./ChatView";
import { MSG } from "../shared/messages";
import type {
  CdpState,
  CdpStateChanged,
  RpcEventFrame,
  ToolExecuteRequest,
  ToolExecuteResponse,
} from "../shared/types";

interface Agent {
  id: string;
  name?: string;
  avatarMediaId?: string;
}

interface UserProfile {
  name: string;
  avatarMediaId: string;
}

interface Conversation {
  id: string;
  summary?: string;
  agentId?: string;
  lastActive?: number;
}

interface BoundTab {
  tabId: number;
  url: string;
  title: string;
}

const pulseAnimation = keyframes`
  0%, 100% { opacity: 0.4; }
  50% { opacity: 1; }
`;

export function Overlay() {
  const [connected, setConnected] = useState(false);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentId, setAgentId] = useState("");
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState("");
  const [error, setError] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selectionRestored, setSelectionRestored] = useState(false);

  // --- User profile for avatar display ---
  const [profile, setProfile] = useState<UserProfile>({
    name: "",
    avatarMediaId: "",
  });

  // --- Tab binding state ---
  const [boundTab, setBoundTabState] = useState<BoundTab | null>(null);
  const boundTabRef = useRef<BoundTab | null>(null);
  // Keep ref in sync with state for render, but also update ref eagerly
  // in setBoundTab() so event handlers see the value immediately.
  boundTabRef.current = boundTab;
  const setBoundTab = useCallback((tab: BoundTab | null) => {
    boundTabRef.current = tab;
    setBoundTabState(tab);
  }, []);

  // --- CDP relay state ---
  const [cdpState, setCdpState] = useState<CdpState>("detached");

  // --- Displacement warning ---
  const [displaced, setDisplaced] = useState(false);

  // --- WS reconnection tracking ---
  const [wsGeneration, setWsGeneration] = useState(0);

  // --- Display settings ---
  const [showToolCalls, setShowToolCalls] = useState(false);
  const [showTokenUsage, setShowTokenUsage] = useState(false);

  // Persist selection to chrome.storage.local.
  useEffect(() => {
    if (!selectionRestored) return;
    if (typeof chrome !== "undefined" && chrome.storage) {
      chrome.storage.local.set({
        sidepanelAgentId: agentId,
        sidepanelConversationId: conversationId,
      });
    }
  }, [agentId, conversationId, selectionRestored]);

  // Persist display settings to chrome.storage.local.
  useEffect(() => {
    if (!selectionRestored) return;
    if (typeof chrome !== "undefined" && chrome.storage) {
      chrome.storage.local.set({
        overlayShowToolCalls: showToolCalls,
        overlayShowTokenUsage: showTokenUsage,
      });
    }
  }, [showToolCalls, showTokenUsage, selectionRestored]);

  // Connect on mount + restore saved selection.
  useEffect(() => {
    (async () => {
      // Restore saved selection and display settings from storage.
      if (typeof chrome !== "undefined" && chrome.storage) {
        try {
          const stored = await chrome.storage.local.get([
            "sidepanelAgentId",
            "sidepanelConversationId",
            "overlayShowToolCalls",
            "overlayShowTokenUsage",
          ]);
          if (stored.sidepanelAgentId) {
            setAgentId(stored.sidepanelAgentId);
          }
          if (stored.sidepanelConversationId) {
            setConversationId(stored.sidepanelConversationId);
          }
          if (stored.overlayShowToolCalls === true) {
            setShowToolCalls(true);
          }
          if (stored.overlayShowTokenUsage === true) {
            setShowTokenUsage(true);
          }
        } catch {
          // ignore
        }
      }
      setSelectionRestored(true);

      try {
        await ensureConnected();
        setConnected(true);
        setError("");
      } catch (err) {
        setError(String(err));
      }
    })();
  }, []);

  // Track WS reconnections so auto-attach re-fires.
  useEffect(() => {
    return onConnectionStateChange((isConnected) => {
      if (isConnected) {
        setWsGeneration((g) => g + 1);
      }
    });
  }, []);

  // Load agents + apply defaults.
  useEffect(() => {
    if (!connected || !selectionRestored) return;
    sendRpc("agents.list")
      .then((payload) => {
        const data = payload as {
          agents?: (Agent & { defaultConversationId?: string })[];
          defaultAgentId?: string;
        };
        if (data.agents) setAgents(data.agents);
        // If no agent selected (nothing was persisted), use server default.
        setAgentId((prev) => {
          if (prev) return prev;
          return data.defaultAgentId || data.agents?.[0]?.id || "";
        });
        // If no conversation selected, use the default agent's defaultConversationId.
        setConversationId((prev) => {
          if (prev) return prev;
          const resolvedAgentId =
            agentId || data.defaultAgentId || data.agents?.[0]?.id || "";
          const agent = data.agents?.find((a) => a.id === resolvedAgentId);
          return (
            (agent as { defaultConversationId?: string })
              ?.defaultConversationId || ""
          );
        });
      })
      .catch(() => {});
  }, [connected, selectionRestored]);

  // Fetch user profile for avatar display.
  useEffect(() => {
    if (!connected) return;
    let mounted = true;
    sendRpc("profile.get", {})
      .then((payload) => {
        if (!mounted) return;
        const data = payload as {
          name?: string;
          avatarMediaId?: string;
        };
        setProfile({
          name: data.name || "",
          avatarMediaId: data.avatarMediaId || "",
        });
      })
      .catch(() => {});
    return () => {
      mounted = false;
    };
  }, [connected]);

  // Load conversations when agent changes.
  useEffect(() => {
    if (!connected || !agentId) return;
    sendRpc("conversations.list", { agentId })
      .then((payload) => {
        const data = payload as { conversations?: Conversation[] };
        if (data.conversations) {
          setConversations(data.conversations);
          // If no conversation selected, pick first available.
          setConversationId((prev) => {
            if (prev) return prev;
            return data.conversations?.[0]?.id || "";
          });
        }
      })
      .catch(() => {});
  }, [connected, agentId]);

  // Listen for conversation list changes.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event === "conversations") {
        sendRpc("conversations.list", { agentId })
          .then((payload) => {
            const data = payload as { conversations?: Conversation[] };
            if (data.conversations) setConversations(data.conversations);
          })
          .catch(() => {});
      }
    });
  }, [agentId]);

  // Refs for current agentId/conversationId (used in callbacks).
  const agentIdRef = useRef(agentId);
  agentIdRef.current = agentId;
  const conversationIdRef = useRef(conversationId);
  conversationIdRef.current = conversationId;

  // --- Auto-attach: bind the current tab when agent+conversation are ready ---
  // wsGeneration is included so we re-attach after a WS reconnection
  // (the server cleans up attachments when a connection drops).
  useEffect(() => {
    if (!connected || !agentId || !conversationId) return;
    if (typeof chrome === "undefined" || !chrome.tabs) return;

    let cancelled = false;

    (async () => {
      try {
        const [active] = await chrome.tabs.query({
          active: true,
          currentWindow: true,
        });
        if (cancelled || !active?.id || !active.url) return;

        await sendRpc("tab.attach", {
          agentId,
          conversationId,
          tabUrl: active.url,
          tabTitle: active.title || "",
          tabId: active.id,
        });

        if (!cancelled) {
          setBoundTab({
            tabId: active.id,
            url: active.url,
            title: active.title || "",
          });
          setDisplaced(false);
        }
      } catch {
        // ignore attach errors
      }
    })();

    return () => {
      cancelled = true;
      // Detach on cleanup (conversation change or unmount).
      sendRpc("tab.detach", { agentId, conversationId }).catch(() => {});
      setBoundTab(null);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, agentId, conversationId, wsGeneration]);

  // --- Query CDP state when bound tab is known ---
  useEffect(() => {
    if (!boundTab) {
      setCdpState("detached");
      return;
    }
    if (typeof chrome === "undefined" || !chrome.runtime) return;
    chrome.runtime
      .sendMessage({ type: MSG.CDP_STATE_QUERY, tabId: boundTab.tabId })
      .then((response: unknown) => {
        const message = response as CdpStateChanged | undefined;
        if (message?.state) setCdpState(message.state);
      })
      .catch(() => {});
  }, [boundTab?.tabId]);

  // --- Handle overlay close: detach tab and disconnect before iframe is torn down ---
  useEffect(() => {
    const handler = (e: MessageEvent) => {
      if (e.data?.type !== "tn:closing") return;
      const aid = agentIdRef.current;
      const cid = conversationIdRef.current;
      if (boundTabRef.current && aid && cid) {
        sendRpc("tab.detach", { agentId: aid, conversationId: cid })
          .catch(() => {})
          .finally(() => disconnect());
      } else {
        disconnect();
      }
    };
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, []);

  // Listen for tab URL changes, tab closure, and CDP state from background SW.
  useEffect(() => {
    if (typeof chrome === "undefined" || !chrome.runtime) return;
    const onMessage = (message: any): undefined => {
      // CDP state broadcasts (not filtered by boundTab — may arrive before binding).
      if (message.type === MSG.CDP_STATE) {
        const cdpMessage = message as CdpStateChanged;
        const bt = boundTabRef.current;
        if (bt && cdpMessage.tabId === bt.tabId) {
          setCdpState(cdpMessage.state);
        }
        return undefined;
      }

      const bt = boundTabRef.current;
      if (!bt) return;
      if (message.type === "tab_url_changed" && message.tabId === bt.tabId) {
        const updated: BoundTab = {
          tabId: message.tabId,
          url: message.url,
          title: message.title,
        };
        setBoundTab(updated);
        sendRpc("tab.attach", {
          agentId: agentIdRef.current,
          conversationId: conversationIdRef.current,
          tabUrl: message.url,
          tabTitle: message.title,
          tabId: message.tabId,
        }).catch(() => {});
      }
      if (message.type === "tab_closed" && message.tabId === bt.tabId) {
        sendRpc("tab.detach", {
          agentId: agentIdRef.current,
          conversationId: conversationIdRef.current,
        }).catch(() => {});
        setBoundTab(null);
      }
      return undefined;
    };
    chrome.runtime.onMessage.addListener(onMessage);
    return () => chrome.runtime.onMessage.removeListener(onMessage);
  }, []);

  // Listen for tabAttachment events from server.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "tabAttachment") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.agentId !== agentId || p.conversationId !== conversationId) return;
      if (p.action === "attached") {
        const bt = boundTabRef.current;
        // Another tab took over — we've been displaced.
        if (bt && typeof p.tabId === "number" && p.tabId !== bt.tabId) {
          setBoundTab(null);
          setDisplaced(true);
        }
      } else if (p.action === "detached") {
        const bt = boundTabRef.current;
        // If this detach targets our tab (or is a displacement broadcast), clear binding.
        if (
          bt &&
          (p.displaced || (typeof p.tabId === "number" && p.tabId === bt.tabId))
        ) {
          setBoundTab(null);
          setDisplaced(true);
        } else if (!bt) {
          // Already unbound, nothing to do.
        }
      }
    });
  }, [agentId, conversationId]);

  // Handle tabCommand events: forward to background SW using bound tab.
  useEffect(() => {
    if (!connected) return;
    return onEvent(async (frame: RpcEventFrame) => {
      if (frame.event !== "tabCommand") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.agentId !== agentId || p.conversationId !== conversationId) return;

      const bt = boundTabRef.current;
      const tabId = bt?.tabId;

      // If the command targets a specific tab that isn't ours, skip silently.
      if (typeof p.tabId === "number" && tabId && p.tabId !== tabId) return;

      if (!tabId) {
        // Don't respond with an error — another overlay instance on a
        // different tab may hold the binding and will respond instead.
        // If nobody responds, the server-side context timeout cancels
        // the pending call.
        return;
      }

      const request: ToolExecuteRequest = {
        type: "tool_execute",
        toolName: p.toolName as ToolExecuteRequest["toolName"],
        requestId: p.requestId as string,
        tabId,
        arguments: p.arguments as Record<string, unknown>,
      };

      try {
        const response = (await chrome.runtime.sendMessage(
          request,
        )) as ToolExecuteResponse;
        await sendRpc("tab.commandResult", {
          requestId: p.requestId,
          result: response.result || "",
          error: response.error || "",
        });
      } catch (err) {
        await sendRpc("tab.commandResult", {
          requestId: p.requestId,
          error: String(err),
        }).catch(() => {});
      }
    });
  }, [connected, agentId, conversationId]);

  const handleCdpToggle = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation(); // Prevent header click → drawer open.
      if (typeof chrome === "undefined" || !chrome.runtime) return;
      chrome.runtime
        .sendMessage({ type: MSG.CDP_TOGGLE, tabId: boundTab?.tabId })
        .catch(() => {});
    },
    [boundTab?.tabId],
  );

  const handleReattach = useCallback(async () => {
    if (typeof chrome === "undefined" || !chrome.tabs) return;
    try {
      const [active] = await chrome.tabs.query({
        active: true,
        currentWindow: true,
      });
      if (!active?.id || !active.url) return;
      await sendRpc("tab.attach", {
        agentId: agentIdRef.current,
        conversationId: conversationIdRef.current,
        tabUrl: active.url,
        tabTitle: active.title || "",
        tabId: active.id,
      });
      setBoundTab({
        tabId: active.id,
        url: active.url,
        title: active.title || "",
      });
      setDisplaced(false);
    } catch {
      // ignore
    }
  }, []);

  const handleNewConversation = useCallback(() => {
    setConversationId("");
    setDrawerOpen(false);
  }, []);

  // Called by ChatView when a new conversation is created via the first message.
  const handleConversationCreated = useCallback(
    (id: string) => {
      setConversationId(id);
      // Refresh conversation list so the new entry appears in the drawer.
      if (agentId) {
        sendRpc("conversations.list", { agentId })
          .then((payload) => {
            const data = payload as { conversations?: Conversation[] };
            if (data.conversations) setConversations(data.conversations);
          })
          .catch(() => {});
      }
    },
    [agentId],
  );

  const currentConversation = conversations.find(
    (c) => c.id === conversationId,
  );
  const currentAgent = agents.find((a) => a.id === agentId);
  const headerLabel =
    currentConversation?.summary ||
    (conversationId ? conversationId.slice(0, 12) : "New conversation");

  if (error) {
    return (
      <Box sx={{ p: 2, color: "error.main", fontSize: 13 }}>
        <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
          Connection Error
        </Typography>
        <Typography variant="body2">{error}</Typography>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ mt: 1.5, display: "block" }}
        >
          Check that TeaNode is running and the URL/token are configured in the
          extension options.
        </Typography>
        <Button
          size="small"
          variant="outlined"
          sx={{ mt: 1.5 }}
          onClick={() => {
            setError("");
            ensureConnected()
              .then(() => {
                setConnected(true);
                setError("");
              })
              .catch((err) => setError(String(err)));
          }}
        >
          Retry
        </Button>
      </Box>
    );
  }

  if (!connected) {
    return (
      <Box sx={{ p: 2, color: "text.secondary", fontSize: 13 }}>
        Connecting...
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", height: "100%" }}>
      {/* Header bar: conversation info + drawer toggle */}
      <Box
        sx={{
          px: 1.5,
          py: 1,
          borderBottom: 1,
          borderColor: "divider",
          display: "flex",
          alignItems: "center",
          gap: 1,
          cursor: "pointer",
          "&:hover": { bgcolor: "action.hover" },
        }}
        onClick={() => setDrawerOpen(true)}
      >
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: "block", lineHeight: 1.2 }}
          >
            {currentAgent?.name || agentId}
          </Typography>
          <Typography
            variant="body2"
            sx={{
              fontWeight: 600,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {headerLabel}
          </Typography>
        </Box>
        <Tooltip
          title={
            cdpState === "attached"
              ? "CDP attached (click to detach)"
              : cdpState === "connecting"
                ? "Connecting CDP..."
                : cdpState === "error"
                  ? "CDP error (click to retry)"
                  : "Attach CDP debugger"
          }
          arrow
        >
          <IconButton size="small" onClick={handleCdpToggle} sx={{ p: 0.5 }}>
            <BugReportRounded
              sx={{
                fontSize: 18,
                color:
                  cdpState === "attached"
                    ? "primary.main"
                    : cdpState === "error"
                      ? "error.main"
                      : cdpState === "connecting"
                        ? "warning.main"
                        : "text.disabled",
                opacity: cdpState === "detached" ? 0.4 : 1,
                ...(cdpState === "connecting" && {
                  animation: `${pulseAnimation} 1.2s ease-in-out infinite`,
                }),
              }}
            />
          </IconButton>
        </Tooltip>
        <ExpandMoreRounded sx={{ color: "text.secondary", fontSize: 20 }} />
      </Box>

      {/* Displacement warning */}
      {displaced && (
        <Alert
          severity="warning"
          variant="outlined"
          sx={{ mx: 1, mt: 0.5, py: 0, fontSize: 12 }}
          action={
            <Button size="small" onClick={handleReattach} sx={{ fontSize: 11 }}>
              Re-attach
            </Button>
          }
        >
          Tab control moved to another tab.
        </Alert>
      )}

      {/* Chat */}
      <ChatView
        agentId={agentId}
        conversationId={conversationId}
        onConversationCreated={handleConversationCreated}
        agentAvatarMediaId={currentAgent?.avatarMediaId}
        agentName={currentAgent?.name || agentId}
        userAvatarMediaId={profile.avatarMediaId || undefined}
        userName={profile.name || "You"}
        showToolCalls={showToolCalls}
        showTokenUsage={showTokenUsage}
      />

      {/* Bottom sheet drawer for choosing conversation */}
      <Drawer
        anchor="bottom"
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        PaperProps={{
          sx: { maxHeight: "80vh", borderRadius: "12px 12px 0 0" },
        }}
      >
        <Box sx={{ p: 2 }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
            Agent
          </Typography>
          <List dense disablePadding>
            {agents.map((a) => (
              <ListItemButton
                key={a.id}
                selected={a.id === agentId}
                onClick={() => {
                  if (a.id !== agentId) {
                    setAgentId(a.id);
                    setConversationId("");
                  }
                }}
                sx={{ borderRadius: 1, py: 0.5 }}
              >
                <ListItemText
                  primary={a.name || a.id}
                  primaryTypographyProps={{ variant: "body2" }}
                />
              </ListItemButton>
            ))}
          </List>

          <Divider sx={{ my: 1.5 }} />

          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              mb: 1,
            }}
          >
            <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
              Conversations
            </Typography>
            <IconButton size="small" onClick={handleNewConversation}>
              <AddRounded sx={{ fontSize: 18 }} />
            </IconButton>
          </Box>
          <List
            dense
            disablePadding
            sx={{ maxHeight: "50vh", overflowY: "auto" }}
          >
            {conversations.length === 0 && (
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ px: 2, py: 1, display: "block" }}
              >
                No conversations yet
              </Typography>
            )}
            {conversations.map((c) => (
              <ListItemButton
                key={c.id}
                selected={c.id === conversationId}
                onClick={() => {
                  setConversationId(c.id);
                  setDrawerOpen(false);
                }}
                sx={{ borderRadius: 1, py: 0.5 }}
              >
                <ListItemText
                  primary={c.summary || c.id.slice(0, 16)}
                  secondary={
                    c.lastActive ? dayjs(c.lastActive).fromNow() : undefined
                  }
                  primaryTypographyProps={{
                    variant: "body2",
                    noWrap: true,
                    title: c.summary || c.id,
                  }}
                  secondaryTypographyProps={{
                    variant: "caption",
                    fontSize: "10px",
                    title: c.lastActive
                      ? dayjs(c.lastActive).format("YYYY-MM-DD HH:mm:ss")
                      : undefined,
                    color: "text.disabled",
                  }}
                />
              </ListItemButton>
            ))}
          </List>

          <Divider sx={{ my: 1.5 }} />

          <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 0.5 }}>
            Display
          </Typography>
          <FormControlLabel
            control={
              <Switch
                size="small"
                checked={showToolCalls}
                onChange={(_, checked) => setShowToolCalls(checked)}
              />
            }
            label={<Typography variant="body2">Show tool calls</Typography>}
            sx={{ ml: 0, gap: 0.5 }}
          />
          <FormControlLabel
            control={
              <Switch
                size="small"
                checked={showTokenUsage}
                onChange={(_, checked) => setShowTokenUsage(checked)}
              />
            }
            label={<Typography variant="body2">Show token usage</Typography>}
            sx={{ ml: 0, gap: 0.5 }}
          />
        </Box>
      </Drawer>
    </Box>
  );
}
