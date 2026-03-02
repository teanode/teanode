import React, { useState, useEffect, useCallback, useRef } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Drawer from "@mui/material/Drawer";
import IconButton from "@mui/material/IconButton";
import List from "@mui/material/List";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemText from "@mui/material/ListItemText";
import Typography from "@mui/material/Typography";
import Divider from "@mui/material/Divider";
import AddRounded from "@mui/icons-material/AddRounded";
import ExpandMoreRounded from "@mui/icons-material/ExpandMoreRounded";
import { sendRpc, onEvent, ensureConnected, disconnect } from "./rpc";
import { ChatView } from "./ChatView";
import type {
  RpcEventFrame,
  ToolExecuteRequest,
  ToolExecuteResponse,
} from "../shared/types";

interface Agent {
  id: string;
  name?: string;
}

interface Conversation {
  id: string;
  summary?: string;
  agentId?: string;
}

interface BoundTab {
  tabId: number;
  url: string;
  title: string;
}

export function SidePanel() {
  const [connected, setConnected] = useState(false);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentId, setAgentId] = useState("");
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState("");
  const [error, setError] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selectionRestored, setSelectionRestored] = useState(false);

  // --- Tab binding state ---
  const [boundTab, setBoundTab] = useState<BoundTab | null>(null);
  const boundTabRef = useRef<BoundTab | null>(null);
  boundTabRef.current = boundTab;

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

  // Connect on mount + restore saved selection.
  useEffect(() => {
    (async () => {
      // Restore saved selection from storage.
      if (typeof chrome !== "undefined" && chrome.storage) {
        try {
          const stored = await chrome.storage.local.get([
            "sidepanelAgentId",
            "sidepanelConversationId",
          ]);
          if (stored.sidepanelAgentId) {
            setAgentId(stored.sidepanelAgentId);
          }
          if (stored.sidepanelConversationId) {
            setConversationId(stored.sidepanelConversationId);
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
          return (agent as { defaultConversationId?: string })
            ?.defaultConversationId || "";
        });
      })
      .catch(() => {});
  }, [connected, selectionRestored]);

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
  }, [connected, agentId, conversationId]);

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

  // Listen for tab URL changes and tab closure from background SW.
  useEffect(() => {
    if (typeof chrome === "undefined" || !chrome.runtime) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const onMessage = (message: any): undefined => {
      const bt = boundTabRef.current;
      if (!bt) return;
      if (
        message.type === "tab_url_changed" &&
        message.tabId === bt.tabId
      ) {
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

  // Listen for tab_attachment events from server.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "tab_attachment") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.agentId !== agentId || p.conversationId !== conversationId) return;
      if (p.action === "detached") {
        setBoundTab(null);
      }
    });
  }, [agentId, conversationId]);

  // Handle tab_tool_call events: forward to background SW using bound tab.
  useEffect(() => {
    if (!connected) return;
    return onEvent(async (frame: RpcEventFrame) => {
      if (frame.event !== "tab_tool_call") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.agentId !== agentId || p.conversationId !== conversationId) return;

      const bt = boundTabRef.current;
      const tabId = bt?.tabId;
      if (!tabId) {
        await sendRpc("tab.tool_result", {
          requestId: p.requestId,
          error: "no tab bound",
        }).catch(() => {});
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
        await sendRpc("tab.tool_result", {
          requestId: p.requestId,
          result: response.result || "",
          error: response.error || "",
        });
      } catch (err) {
        await sendRpc("tab.tool_result", {
          requestId: p.requestId,
          error: String(err),
        }).catch(() => {});
      }
    });
  }, [connected, agentId, conversationId]);

  const handleNewConversation = useCallback(() => {
    setConversationId("");
    setDrawerOpen(false);
  }, []);

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
        <ExpandMoreRounded sx={{ color: "text.secondary", fontSize: 20 }} />
      </Box>

      {/* Chat */}
      <ChatView agentId={agentId} conversationId={conversationId} />

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
                  primaryTypographyProps={{ variant: "body2", noWrap: true }}
                />
              </ListItemButton>
            ))}
          </List>
        </Box>
      </Drawer>
    </Box>
  );
}
