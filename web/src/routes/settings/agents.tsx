import React, {
  useEffect,
  useState,
  useCallback,
  useMemo,
  useRef,
} from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Avatar from "@mui/material/Avatar";
import IconButton from "@mui/material/IconButton";
import CircularProgress from "@mui/material/CircularProgress";
import Tooltip from "@mui/material/Tooltip";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import ChatBubbleOutlineIcon from "@mui/icons-material/ChatBubbleOutline";
import SaveOutlinedIcon from "@mui/icons-material/SaveOutlined";
import StarOutlineIcon from "@mui/icons-material/StarOutline";
import StarIcon from "@mui/icons-material/Star";
import ConfirmDialog from "../../components/ConfirmDialog";
import AgentEditor, {
  type AgentEditorHandle,
} from "../../components/AgentEditor";
import { useAppContext } from "../../context";
import { useAgents } from "../../hooks/useAgents";
import type { AgentConfig } from "../../types";

async function uploadAgentAvatar(agentId: string, file: File): Promise<void> {
  const formData = new FormData();
  formData.append("file", file);
  const response = await fetch(
    `/api/v1/agents/${encodeURIComponent(agentId)}/avatar`,
    {
      method: "POST",
      body: formData,
    },
  );
  if (!response.ok) throw new Error(await response.text());
}

async function removeAgentAvatar(agentId: string): Promise<void> {
  const response = await fetch(
    `/api/v1/agents/${encodeURIComponent(agentId)}/avatar`,
    {
      method: "DELETE",
    },
  );
  if (!response.ok) throw new Error(await response.text());
}

export default function SettingsAgentsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { backend } = useAppContext();
  const agentsHook = useAgents(backend.sendRpc);
  const [newAgentName, setNewAgentName] = useState("");
  const [avatarBusyAgentId, setAvatarBusyAgentId] = useState<string | null>(
    null,
  );
  const [pendingDelete, setPendingDelete] = useState<{
    id: string;
    name: string;
  } | null>(null);
  const [dirtyByAgent, setDirtyByAgent] = useState<Record<string, boolean>>({});
  const [nameByAgent, setNameByAgent] = useState<Record<string, string>>({});
  const editorRefs = useRef<Record<string, AgentEditorHandle | null>>({});
  const avatarInputRefs = useRef<Record<string, HTMLInputElement | null>>({});

  useEffect(() => {
    if (backend.connected) {
      agentsHook.loadAgents();
      agentsHook.loadSchema();
    }
  }, [backend.connected, agentsHook.loadAgents, agentsHook.loadSchema]);

  const saveAgent = useCallback(
    (agent: AgentConfig) =>
      agentsHook.saveAgent(agent).then(() => backend.refreshAgents()),
    [agentsHook.saveAgent, backend.refreshAgents],
  );

  const sortedAgents = useMemo(() => {
    const list = [...agentsHook.agents];
    const defaultAgentId = backend.serverDefaultAgentId;
    list.sort((a, b) => {
      if (defaultAgentId) {
        if (a.id === defaultAgentId && b.id !== defaultAgentId) return -1;
        if (b.id === defaultAgentId && a.id !== defaultAgentId) return 1;
      }
      return (a.name || a.id).localeCompare(b.name || b.id);
    });
    return list;
  }, [agentsHook.agents, backend.serverDefaultAgentId]);

  const addAgent = useCallback(async () => {
    const name = newAgentName.trim();
    if (!name) return;
    const base = name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
    const existing = new Set(agentsHook.agents.map((agent) => agent.id));
    const stem = base || "agent";
    let id = stem;
    let suffix = 2;
    while (existing.has(id)) {
      id = `${stem}-${suffix}`;
      suffix += 1;
    }
    await agentsHook.saveAgent({ id, name: name || undefined });
    backend.refreshAgents();
    setNewAgentName("");
  }, [newAgentName, agentsHook, backend]);

  const confirmDeleteAgent = useCallback(async () => {
    if (!pendingDelete) return;
    await agentsHook.deleteAgent(pendingDelete.id);
    backend.refreshAgents();
    setPendingDelete(null);
  }, [pendingDelete, agentsHook, backend]);

  const handleAvatarInput = useCallback(
    async (agentId: string, event: React.ChangeEvent<HTMLInputElement>) => {
      const file = event.target.files?.[0];
      if (!file) return;
      setAvatarBusyAgentId(agentId);
      try {
        await uploadAgentAvatar(agentId, file);
        agentsHook.loadAgents();
        backend.refreshAgents();
      } catch (error) {
        console.error("avatar upload failed:", error);
      } finally {
        setAvatarBusyAgentId(null);
        event.target.value = "";
      }
    },
    [agentsHook, backend],
  );

  const handleAvatarDelete = useCallback(
    async (agentId: string) => {
      setAvatarBusyAgentId(agentId);
      try {
        await removeAgentAvatar(agentId);
        agentsHook.loadAgents();
        backend.refreshAgents();
      } catch (error) {
        console.error("avatar delete failed:", error);
      } finally {
        setAvatarBusyAgentId(null);
      }
    },
    [agentsHook, backend],
  );

  const handleAgentDirtyChange = useCallback(
    (agentId: string, dirty: boolean) => {
      setDirtyByAgent((previous) => {
        if (previous[agentId] === dirty) return previous;
        return { ...previous, [agentId]: dirty };
      });
    },
    [],
  );

  const saveAgentFromHeader = useCallback((agentId: string) => {
    editorRefs.current[agentId]?.save();
  }, []);

  const handleAgentNameChange = useCallback((agentId: string, name: string) => {
    setNameByAgent((previous) => ({ ...previous, [agentId]: name }));
    editorRefs.current[agentId]?.setField(
      "name",
      name.trim() ? name : undefined,
    );
  }, []);

  const handleAvatarClick = useCallback(
    (agent: AgentConfig) => {
      if (avatarBusyAgentId === agent.id) return;
      if (agent.avatarMediaId) {
        void handleAvatarDelete(agent.id);
        return;
      }
      avatarInputRefs.current[agent.id]?.click();
    },
    [avatarBusyAgentId, handleAvatarDelete],
  );

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Box sx={{ mb: 1 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t("settings.agents")}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {t("settings.agentsDescription")}
            </Typography>
          </Box>
          {sortedAgents.map((agent) => {
            const avatarBusy = avatarBusyAgentId === agent.id;
            const initial = (agent.name || agent.id)
              .trim()
              .charAt(0)
              .toUpperCase();
            return (
              <Paper key={agent.id} variant="outlined" sx={{ p: 2 }}>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: { xs: 1, sm: 2 },
                  }}
                >
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "center",
                      gap: 1.5,
                      flex: 1,
                      minWidth: 0,
                    }}
                  >
                    <IconButton
                      onClick={() => handleAvatarClick(agent)}
                      sx={{ p: 0, borderRadius: "50%" }}
                      disabled={avatarBusy}
                    >
                      <Box sx={{ position: "relative" }}>
                        <Avatar
                          src={
                            agent.avatarMediaId
                              ? `/api/v1/media/${agent.avatarMediaId}`
                              : undefined
                          }
                          sx={{ width: 48, height: 48 }}
                        >
                          {avatarBusy ? (
                            <CircularProgress size={18} />
                          ) : (
                            initial
                          )}
                        </Avatar>
                        {!avatarBusy && agent.avatarMediaId && (
                          <Box
                            sx={{
                              position: "absolute",
                              right: -2,
                              bottom: -2,
                              width: 18,
                              height: 18,
                              borderRadius: "50%",
                              bgcolor: "error.main",
                              color: "common.white",
                              display: "flex",
                              alignItems: "center",
                              justifyContent: "center",
                              border: 1,
                              borderColor: "background.paper",
                            }}
                          >
                            <DeleteOutlineIcon sx={{ fontSize: 12 }} />
                          </Box>
                        )}
                      </Box>
                    </IconButton>
                    <input
                      hidden
                      type="file"
                      accept="image/*"
                      ref={(input) => {
                        avatarInputRefs.current[agent.id] = input;
                      }}
                      onChange={(event) => handleAvatarInput(agent.id, event)}
                    />
                    <Box sx={{ flex: 1, minWidth: 0 }}>
                      <TextField
                        variant="standard"
                        size="small"
                        value={nameByAgent[agent.id] ?? agent.name ?? ""}
                        placeholder={agent.id}
                        onChange={(event) =>
                          handleAgentNameChange(agent.id, event.target.value)
                        }
                        InputProps={{ disableUnderline: true }}
                        sx={{
                          minWidth: { xs: 0, sm: 180 },
                          width: "100%",
                          maxWidth: "100%",
                          "& .MuiInputBase-input": {
                            fontSize: "0.95rem",
                            fontWeight: 600,
                            py: 0.25,
                          },
                        }}
                      />
                    </Box>
                  </Box>
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "center",
                      gap: { xs: 0.5, sm: 1 },
                      flexShrink: 0,
                    }}
                  >
                    {agent.id === backend.serverDefaultAgentId ? (
                      <Tooltip title={t("common.default")}>
                        <span>
                          <IconButton size="small" disabled>
                            <StarIcon fontSize="small" />
                          </IconButton>
                        </span>
                      </Tooltip>
                    ) : (
                      <Tooltip title={t("agent.makeDefault")}>
                        <IconButton
                          size="small"
                          onClick={() => backend.setDefaultAgent(agent.id)}
                        >
                          <StarOutlineIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    )}
                    <Tooltip title={t("agent.chat")}>
                      <IconButton
                        size="small"
                        onClick={() =>
                          navigate({
                            to: "/conversations/$agentId",
                            params: { agentId: agent.id },
                          })
                        }
                      >
                        <ChatBubbleOutlineIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={t("common.save")}>
                      <span>
                        <IconButton
                          size="small"
                          color={dirtyByAgent[agent.id] ? "primary" : "default"}
                          disabled={!dirtyByAgent[agent.id]}
                          onClick={() => saveAgentFromHeader(agent.id)}
                        >
                          <SaveOutlinedIcon fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                    <Tooltip title={t("common.delete")}>
                      <span>
                        <IconButton
                          size="small"
                          color="error"
                          onClick={() =>
                            setPendingDelete({
                              id: agent.id,
                              name: agent.name || agent.id,
                            })
                          }
                          disabled={agent.id === backend.serverDefaultAgentId}
                        >
                          <DeleteOutlineIcon fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                  </Box>
                </Box>
                <Box sx={{ mt: 1.5 }}>
                  <AgentEditor
                    ref={(instance) => {
                      editorRefs.current[agent.id] = instance;
                    }}
                    agent={agent}
                    models={backend.models}
                    schema={agentsHook.schema}
                    suggestions={agentsHook.suggestions}
                    onSave={saveAgent}
                    showIdentityHeader={false}
                    flat
                    showSaveControls={false}
                    onDirtyChange={(dirty) =>
                      handleAgentDirtyChange(agent.id, dirty)
                    }
                    hiddenDotPaths={["name"]}
                  />
                </Box>
              </Paper>
            );
          })}

          <Paper variant="outlined" sx={{ p: 2 }}>
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 2,
              }}
            >
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 1.5,
                  flex: 1,
                  minWidth: 0,
                }}
              >
                <Avatar sx={{ width: 48, height: 48 }}>
                  {(newAgentName.trim().charAt(0) || "A").toUpperCase()}
                </Avatar>
                <TextField
                  variant="standard"
                  size="small"
                  value={newAgentName}
                  placeholder={t("agent.name")}
                  onChange={(event) => setNewAgentName(event.target.value)}
                  InputProps={{ disableUnderline: true }}
                  sx={{
                    minWidth: { xs: 0, sm: 200 },
                    flex: 1,
                    "& .MuiInputBase-input": {
                      fontSize: "0.95rem",
                      fontWeight: 600,
                      py: 0.25,
                    },
                  }}
                />
              </Box>
              <Button
                variant="contained"
                size="small"
                onClick={addAgent}
                disabled={!newAgentName.trim()}
              >
                {t("common.create")}
              </Button>
            </Box>
          </Paper>
        </Box>
      </Container>

      <ConfirmDialog
        open={!!pendingDelete}
        title={t("agent.deleteAgent")}
        message={t("agent.deleteConfirm", { name: pendingDelete?.name })}
        confirmLabel={t("common.delete")}
        onConfirm={confirmDeleteAgent}
        onClose={() => setPendingDelete(null)}
      />
    </Box>
  );
}
