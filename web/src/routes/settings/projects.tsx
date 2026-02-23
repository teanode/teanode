import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import SaveOutlinedIcon from "@mui/icons-material/SaveOutlined";
import AddCircleOutlineIcon from "@mui/icons-material/AddCircleOutline";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAppContext } from "../../context";

interface ProjectEntry {
  id: string;
  name: string;
  description?: string;
  updatedAt?: string;
}

function formatUpdated(updatedAt?: string): string {
  if (!updatedAt) return "";
  const timestamp = Date.parse(updatedAt);
  if (Number.isNaN(timestamp)) return updatedAt;
  return new Date(timestamp).toLocaleString();
}

export default function SettingsProjectsPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { sendRpc, connected } = backend;
  const [projects, setProjects] = useState<ProjectEntry[]>([]);
  const [nameByProjectId, setNameByProjectId] = useState<
    Record<string, string>
  >({});
  const [busyProjectId, setBusyProjectId] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<ProjectEntry | null>(null);
  const [statusText, setStatusText] = useState("");
  const [newProjectName, setNewProjectName] = useState("");

  const loadProjects = useCallback(async () => {
    if (!connected) return;
    try {
      const result = await sendRpc<{ projects: ProjectEntry[] }>(
        "projects.list",
        {},
      );
      setProjects(result.projects || []);
    } catch (error) {
      console.error("projects.list:", error);
    }
  }, [connected, sendRpc]);

  useEffect(() => {
    void loadProjects();
  }, [loadProjects]);

  const sortedProjects = useMemo(() => {
    const updatedAtMs = (value?: string) => {
      if (!value) return 0;
      const timestamp = Date.parse(value);
      return Number.isNaN(timestamp) ? 0 : timestamp;
    };
    return [...projects].sort(
      (left, right) =>
        updatedAtMs(right.updatedAt) - updatedAtMs(left.updatedAt),
    );
  }, [projects]);

  const renameProject = useCallback(
    async (project: ProjectEntry) => {
      const nextName = (nameByProjectId[project.id] ?? project.name).trim();
      if (!nextName || nextName === project.name) return;
      setBusyProjectId(project.id);
      setStatusText("");
      try {
        await sendRpc("projects.rename", {
          projectId: project.id,
          name: nextName,
        });
        setStatusText(t("settings.projectRenamed", { name: nextName }));
        await loadProjects();
      } catch (error) {
        console.error("projects.rename:", error);
        setStatusText(
          t("settings.projectRenameFailed", { name: project.name }),
        );
      } finally {
        setBusyProjectId(null);
      }
    },
    [loadProjects, nameByProjectId, sendRpc, t],
  );

  const confirmDeleteProject = useCallback(async () => {
    if (!pendingDelete) return;
    setBusyProjectId(pendingDelete.id);
    setStatusText("");
    try {
      await sendRpc("projects.delete", { projectId: pendingDelete.id });
      setStatusText(t("settings.projectDeleted", { name: pendingDelete.name }));
      setPendingDelete(null);
      await loadProjects();
    } catch (error) {
      console.error("projects.delete:", error);
      setStatusText(
        t("settings.projectDeleteFailed", { name: pendingDelete.name }),
      );
    } finally {
      setBusyProjectId(null);
    }
  }, [loadProjects, pendingDelete, sendRpc, t]);

  const createProject = useCallback(async () => {
    const name = newProjectName.trim();
    if (!name) return;
    setBusyProjectId("__create__");
    setStatusText("");
    try {
      await sendRpc("projects.create", { name });
      setStatusText(t("settings.projectCreated", { name }));
      setNewProjectName("");
      await loadProjects();
    } catch (error) {
      console.error("projects.create:", error);
      setStatusText(t("settings.projectCreateFailed", { name }));
    } finally {
      setBusyProjectId(null);
    }
  }, [loadProjects, newProjectName, sendRpc, t]);

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Box sx={{ mb: 1 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t("settings.projects")}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {t("settings.projectsDescription")}
            </Typography>
          </Box>

          {!!statusText && (
            <Typography variant="caption" color="text.secondary">
              {statusText}
            </Typography>
          )}

          {sortedProjects.length === 0 && (
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Typography variant="body2" color="text.secondary">
                {t("settings.noProjects")}
              </Typography>
            </Paper>
          )}

          {sortedProjects.map((project) => {
            const currentName = nameByProjectId[project.id] ?? project.name;
            const dirty = currentName.trim() !== project.name;
            const disabled = busyProjectId !== null;

            return (
              <Paper key={project.id} variant="outlined" sx={{ p: 2 }}>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: 1.5,
                  }}
                >
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <TextField
                      variant="standard"
                      size="small"
                      value={currentName}
                      onChange={(event) =>
                        setNameByProjectId((previous) => ({
                          ...previous,
                          [project.id]: event.target.value,
                        }))
                      }
                      InputProps={{ disableUnderline: true }}
                      sx={{
                        width: "100%",
                        "& .MuiInputBase-input": {
                          fontSize: "0.95rem",
                          fontWeight: 600,
                          py: 0.25,
                        },
                      }}
                    />
                    {project.description && (
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{ display: "block", mt: 0.25 }}
                      >
                        {project.description}
                      </Typography>
                    )}
                    {project.updatedAt && (
                      <Typography
                        variant="caption"
                        color="text.disabled"
                        sx={{ display: "block", mt: 0.25 }}
                      >
                        {t("settings.projectUpdated", {
                          updated: formatUpdated(project.updatedAt),
                        })}
                      </Typography>
                    )}
                  </Box>

                  <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                    <Tooltip title={t("common.save")}>
                      <span>
                        <IconButton
                          size="small"
                          color={dirty ? "primary" : "default"}
                          disabled={!dirty || disabled}
                          onClick={() => void renameProject(project)}
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
                          disabled={disabled}
                          onClick={() => setPendingDelete(project)}
                        >
                          <DeleteOutlineIcon fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                  </Box>
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
                gap: 1.5,
              }}
            >
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <TextField
                  variant="standard"
                  size="small"
                  value={newProjectName}
                  onChange={(event) => setNewProjectName(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      void createProject();
                    }
                  }}
                  placeholder={t("settings.newProjectPlaceholder")}
                  InputProps={{ disableUnderline: true }}
                  sx={{
                    width: "100%",
                    "& .MuiInputBase-input": {
                      fontSize: "0.95rem",
                      fontWeight: 600,
                      py: 0.25,
                    },
                  }}
                />
              </Box>

              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Tooltip title={t("common.create")}>
                  <span>
                    <IconButton
                      size="small"
                      color="primary"
                      disabled={
                        !newProjectName.trim() || busyProjectId !== null
                      }
                      onClick={() => void createProject()}
                    >
                      <AddCircleOutlineIcon fontSize="small" />
                    </IconButton>
                  </span>
                </Tooltip>
              </Box>
            </Box>
          </Paper>
        </Box>
      </Container>

      <ConfirmDialog
        open={!!pendingDelete}
        title={t("settings.deleteProject")}
        message={
          pendingDelete
            ? t("settings.projectDeleteConfirm", { name: pendingDelete.name })
            : ""
        }
        confirmLabel={t("common.delete")}
        onConfirm={() => void confirmDeleteProject()}
        onClose={() => setPendingDelete(null)}
      />
    </Box>
  );
}
