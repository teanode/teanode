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
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogContentText from "@mui/material/DialogContentText";
import DialogTitle from "@mui/material/DialogTitle";
import AddRounded from "@mui/icons-material/AddRounded";
import ExpandMoreRounded from "@mui/icons-material/ExpandMoreRounded";
import LinkRounded from "@mui/icons-material/LinkRounded";
import LinkOffRounded from "@mui/icons-material/LinkOffRounded";
import WarningAmberRounded from "@mui/icons-material/WarningAmberRounded";
import { sendRpc, onEvent, ensureConnected } from "./rpc";
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
  const [agentId, setAgentId] = useState("default-agent");
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState("");
  const [error, setError] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);

  // --- Tab binding state (C6) ---
  const [boundTab, setBoundTab] = useState<BoundTab | null>(null);
  const [activeTabDiffers, setActiveTabDiffers] = useState(false);
  const [activeTabTitle, setActiveTabTitle] = useState("");
  const [tabBusy, setTabBusy] = useState(false);
  const [tabError, setTabError] = useState("");
  const [rebindConfirmOpen, setRebindConfirmOpen] = useState(false);
  const boundTabRef = useRef<BoundTab | null>(null);
  boundTabRef.current = boundTab;

  // Connect on mount.
  useEffect(() => {
    ensureConnected()
      .then(() => {
        setConnected(true);
        setError("");
      })
      .catch((err) => setError(String(err)));
  }, []);

  // Load agents.
  useEffect(() => {
    if (!connected) return;
    sendRpc("agents.list")
      .then((payload) => {
        const data = payload as { agents?: Agent[] };
        if (data.agents) setAgents(data.agents);
      })
      .catch(() => {});
  }, [connected]);

  // Load conversations when agent changes.
  useEffect(() => {
    if (!connected || !agentId) return;
    sendRpc("conversations.list", { agentId })
      .then((payload) => {
        const data = payload as { conversations?: Conversation[] };
        if (data.conversations) {
          setConversations(data.conversations);
          if (!conversationId && data.conversations.length > 0) {
            setConversationId(data.conversations[0].id);
          }
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

  // --- Tab binding: track active tab changes (C6) ---
  useEffect(() => {
    if (typeof chrome === "undefined" || !chrome.tabs) return;

    const checkActiveTab = async () => {
      const bt = boundTabRef.current;
      if (!bt) {
        setActiveTabDiffers(false);
        return;
      }
      try {
        const [active] = await chrome.tabs.query({
          active: true,
          currentWindow: true,
        });
        if (active?.id && active.id !== bt.tabId) {
          setActiveTabDiffers(true);
          setActiveTabTitle(active.title || active.url || "");
        } else {
          setActiveTabDiffers(false);
        }
      } catch {
        // ignore
      }
    };

    const onActivated = () => {
      checkActiveTab();
    };
    chrome.tabs.onActivated.addListener(onActivated);
    const onFocusChanged = () => {
      checkActiveTab();
    };
    chrome.windows.onFocusChanged.addListener(onFocusChanged);

    return () => {
      chrome.tabs.onActivated.removeListener(onActivated);
      chrome.windows.onFocusChanged.removeListener(onFocusChanged);
    };
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
          agentId,
          conversationId,
          tabUrl: message.url,
          tabTitle: message.title,
          tabId: message.tabId,
        }).catch(() => {});
      }
      if (message.type === "tab_closed" && message.tabId === bt.tabId) {
        sendRpc("tab.detach", { agentId, conversationId }).catch(() => {});
        setBoundTab(null);
        setActiveTabDiffers(false);
      }
      return undefined;
    };
    chrome.runtime.onMessage.addListener(onMessage);
    return () => chrome.runtime.onMessage.removeListener(onMessage);
  }, [agentId, conversationId]);

  // Listen for tab_attachment events from server.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "tab_attachment") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.agentId !== agentId || p.conversationId !== conversationId) return;
      if (p.action === "detached") {
        setBoundTab(null);
        setActiveTabDiffers(false);
      }
    });
  }, [agentId, conversationId]);

  // Reset tab binding when conversation changes.
  useEffect(() => {
    setBoundTab(null);
    setActiveTabDiffers(false);
    setTabError("");
  }, [conversationId]);

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

  // --- Tab binding actions ---
  const handleAttachTab = useCallback(async () => {
    if (!conversationId) return;
    setTabBusy(true);
    setTabError("");
    try {
      const [active] = await chrome.tabs.query({
        active: true,
        currentWindow: true,
      });
      if (!active?.id || !active.url) {
        setTabError("No active tab");
        return;
      }
      await sendRpc("tab.attach", {
        agentId,
        conversationId,
        tabUrl: active.url,
        tabTitle: active.title || "",
        tabId: active.id,
      });
      setBoundTab({
        tabId: active.id,
        url: active.url,
        title: active.title || "",
      });
      setActiveTabDiffers(false);
    } catch (err) {
      setTabError(String(err));
    } finally {
      setTabBusy(false);
    }
  }, [agentId, conversationId]);

  const handleDetachTab = useCallback(async () => {
    setTabBusy(true);
    setTabError("");
    try {
      await sendRpc("tab.detach", { agentId, conversationId });
      setBoundTab(null);
      setActiveTabDiffers(false);
    } catch (err) {
      setTabError(String(err));
    } finally {
      setTabBusy(false);
    }
  }, [agentId, conversationId]);

  const handleRebindConfirm = useCallback(async () => {
    setRebindConfirmOpen(false);
    try {
      await sendRpc("tab.detach", { agentId, conversationId }).catch(() => {});
    } catch {
      // ignore
    }
    await handleAttachTab();
  }, [agentId, conversationId, handleAttachTab]);

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

      {/* Tab attachment indicator */}
      <Box
        sx={{
          px: 1.5,
          py: 0.75,
          borderBottom: 1,
          borderColor: "divider",
          display: "flex",
          alignItems: "center",
          gap: 1,
        }}
      >
        <Box
          sx={{
            width: 8,
            height: 8,
            borderRadius: "50%",
            bgcolor: boundTab ? "success.main" : "action.disabled",
            flexShrink: 0,
          }}
        />
        {boundTab ? (
          <Typography
            variant="caption"
            sx={{
              flex: 1,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {boundTab.title || boundTab.url}
          </Typography>
        ) : (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ flex: 1 }}
          >
            No tab bound
          </Typography>
        )}
        <IconButton
          size="small"
          onClick={boundTab ? handleDetachTab : handleAttachTab}
          disabled={tabBusy || !conversationId}
          sx={{ width: 28, height: 28 }}
        >
          {boundTab ? (
            <LinkOffRounded sx={{ fontSize: 16 }} />
          ) : (
            <LinkRounded sx={{ fontSize: 16 }} />
          )}
        </IconButton>
      </Box>

      {/* Rebind banner (C6): shown when active tab differs from bound tab */}
      {boundTab && activeTabDiffers && (
        <Box
          sx={{
            px: 1.5,
            py: 0.75,
            bgcolor: "warning.main",
            color: "warning.contrastText",
            display: "flex",
            alignItems: "center",
            gap: 1,
          }}
        >
          <WarningAmberRounded sx={{ fontSize: 14 }} />
          <Typography
            variant="caption"
            sx={{
              flex: 1,
              fontWeight: 500,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            Bound to: {boundTab.title || boundTab.url}
          </Typography>
          <Button
            size="small"
            variant="outlined"
            onClick={() => setRebindConfirmOpen(true)}
            sx={{
              fontSize: 10,
              py: 0,
              px: 1,
              minWidth: 0,
              color: "inherit",
              borderColor: "inherit",
              whiteSpace: "nowrap",
            }}
          >
            Bind to current tab
          </Button>
        </Box>
      )}

      {tabError && (
        <Box sx={{ px: 1.5, py: 0.5 }}>
          <Typography variant="caption" color="error.main">
            {tabError}
          </Typography>
        </Box>
      )}

      {/* Chat */}
      <ChatView agentId={agentId} conversationId={conversationId} />

      {/* Bottom sheet drawer for choosing conversation (B3) */}
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

      {/* Rebind confirmation dialog (C6) */}
      <Dialog
        open={rebindConfirmOpen}
        onClose={() => setRebindConfirmOpen(false)}
      >
        <DialogTitle sx={{ fontSize: 14 }}>Rebind tab?</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ fontSize: 13 }}>
            Switch from &ldquo;{boundTab?.title || "current tab"}&rdquo; to the
            active tab &ldquo;{activeTabTitle || "unknown"}&rdquo;?
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button size="small" onClick={() => setRebindConfirmOpen(false)}>
            Cancel
          </Button>
          <Button
            size="small"
            variant="contained"
            onClick={handleRebindConfirm}
          >
            Rebind
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
