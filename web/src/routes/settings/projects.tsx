import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Collapse from "@mui/material/Collapse";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import SaveOutlinedIcon from "@mui/icons-material/SaveOutlined";
import AddCircleOutlineIcon from "@mui/icons-material/AddCircleOutline";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import CheckCircleOutlineIcon from "@mui/icons-material/CheckCircleOutline";
import RadioButtonUncheckedIcon from "@mui/icons-material/RadioButtonUnchecked";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAlert } from "../../components/AlertProvider";
import { useAppContext } from "../../context";
import type { Todo } from "../../types";

interface ProjectEntry {
  id: string;
  name: string;
  description?: string;
  updatedAt?: string;
}

interface TodoSummary {
  projectId: string;
  openCount: number;
  doneCount: number;
}

function formatUpdated(updatedAt?: string): string {
  if (!updatedAt) return "";
  const timestamp = Date.parse(updatedAt);
  if (Number.isNaN(timestamp)) return updatedAt;
  return new Date(timestamp).toLocaleString();
}

const priorityColor: Record<string, "error" | "warning" | "default"> = {
  high: "error",
  medium: "warning",
  low: "default",
};

export default function SettingsProjectsPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const { sendRpc, connected } = backend;
  const [projects, setProjects] = useState<ProjectEntry[]>([]);
  const [nameByProjectId, setNameByProjectId] = useState<
    Record<string, string>
  >({});
  const [busyProjectId, setBusyProjectId] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<ProjectEntry | null>(null);
  const [newProjectName, setNewProjectName] = useState("");

  // Todo summary and expanded todo state.
  const [summaryByProjectId, setSummaryByProjectId] = useState<
    Record<string, { openCount: number; doneCount: number }>
  >({});
  const [expandedProjectId, setExpandedProjectId] = useState<string | null>(
    null,
  );
  const [todosByProjectId, setTodosByProjectId] = useState<
    Record<string, Todo[]>
  >({});
  const todoCacheRef = useRef<Set<string>>(new Set());

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

  const loadTodoSummaries = useCallback(async () => {
    if (!connected) return;
    try {
      const result = await sendRpc<{ summaries: TodoSummary[] }>(
        "projects.todos.summary",
        {},
      );
      const next: Record<string, { openCount: number; doneCount: number }> = {};
      for (const summary of result.summaries || []) {
        next[summary.projectId] = {
          openCount: summary.openCount,
          doneCount: summary.doneCount,
        };
      }
      setSummaryByProjectId(next);
    } catch (error) {
      console.error("projects.todos.summary:", error);
    }
  }, [connected, sendRpc]);

  useEffect(() => {
    void loadProjects();
    void loadTodoSummaries();
  }, [loadProjects, loadTodoSummaries]);

  const loadTodosForProject = useCallback(
    async (projectId: string) => {
      if (todoCacheRef.current.has(projectId)) return;
      try {
        const result = await sendRpc<{ todos: Todo[] }>("projects.todos.list", {
          projectId,
        });
        setTodosByProjectId((previous) => ({
          ...previous,
          [projectId]: result.todos || [],
        }));
        todoCacheRef.current.add(projectId);
      } catch (error) {
        console.error("projects.todos.list:", error);
      }
    },
    [sendRpc],
  );

  const toggleExpanded = useCallback(
    (projectId: string) => {
      setExpandedProjectId((previous) => {
        if (previous === projectId) return null;
        void loadTodosForProject(projectId);
        return projectId;
      });
    },
    [loadTodosForProject],
  );

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
      try {
        await sendRpc("projects.rename", {
          projectId: project.id,
          name: nextName,
        });
        showAlert(t("settings.projectRenamed", { name: nextName }));
        await loadProjects();
      } catch (error) {
        console.error("projects.rename:", error);
        showAlert(
          t("settings.projectRenameFailed", { name: project.name }),
          "error",
        );
      } finally {
        setBusyProjectId(null);
      }
    },
    [loadProjects, nameByProjectId, sendRpc, showAlert, t],
  );

  const confirmDeleteProject = useCallback(async () => {
    if (!pendingDelete) return;
    setBusyProjectId(pendingDelete.id);
    try {
      await sendRpc("projects.delete", { projectId: pendingDelete.id });
      showAlert(t("settings.projectDeleted", { name: pendingDelete.name }));
      setPendingDelete(null);
      await loadProjects();
    } catch (error) {
      console.error("projects.delete:", error);
      showAlert(
        t("settings.projectDeleteFailed", { name: pendingDelete.name }),
        "error",
      );
    } finally {
      setBusyProjectId(null);
    }
  }, [loadProjects, pendingDelete, sendRpc, showAlert, t]);

  const createProject = useCallback(async () => {
    const name = newProjectName.trim();
    if (!name) return;
    setBusyProjectId("__create__");
    try {
      await sendRpc("projects.create", { name });
      showAlert(t("settings.projectCreated", { name }));
      setNewProjectName("");
      await loadProjects();
      void loadTodoSummaries();
    } catch (error) {
      console.error("projects.create:", error);
      showAlert(t("settings.projectCreateFailed", { name }), "error");
    } finally {
      setBusyProjectId(null);
    }
  }, [loadProjects, loadTodoSummaries, newProjectName, sendRpc, showAlert, t]);

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
            const summary = summaryByProjectId[project.id];
            const totalTodos = summary
              ? summary.openCount + summary.doneCount
              : 0;
            const isExpanded = expandedProjectId === project.id;
            const todos = todosByProjectId[project.id];

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
                    <Box
                      sx={{
                        display: "flex",
                        alignItems: "center",
                        gap: 1,
                        mt: 0.25,
                        flexWrap: "wrap",
                      }}
                    >
                      {project.description && (
                        <Typography variant="caption" color="text.secondary">
                          {project.description}
                        </Typography>
                      )}
                      {summary && totalTodos > 0 && (
                        <Box
                          sx={{
                            display: "flex",
                            alignItems: "center",
                            gap: 0.5,
                          }}
                        >
                          <Chip
                            label={t("todos.openCount", {
                              count: summary.openCount,
                            })}
                            size="small"
                            variant="outlined"
                            sx={{ height: 20, fontSize: "0.7rem" }}
                          />
                          <Chip
                            label={t("todos.doneCount", {
                              count: summary.doneCount,
                            })}
                            size="small"
                            variant="outlined"
                            sx={{ height: 20, fontSize: "0.7rem" }}
                          />
                        </Box>
                      )}
                    </Box>
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

                  <Box
                    sx={{ display: "flex", alignItems: "flex-start", gap: 1 }}
                  >
                    {totalTodos > 0 && (
                      <Tooltip title={t("todos.title")}>
                        <span>
                          <IconButton
                            size="small"
                            onClick={() => toggleExpanded(project.id)}
                          >
                            {isExpanded ? (
                              <ExpandLessIcon fontSize="small" />
                            ) : (
                              <ExpandMoreIcon fontSize="small" />
                            )}
                          </IconButton>
                        </span>
                      </Tooltip>
                    )}
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

                <Collapse in={isExpanded}>
                  <Box sx={{ mt: 1, pl: 0.5 }}>
                    {todos && todos.length > 0 ? (
                      todos.map((todo) => (
                        <ProjectTodoItem key={todo.id} todo={todo} />
                      ))
                    ) : (
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{ pl: 0.5 }}
                      >
                        {t("todos.noProjectTodos")}
                      </Typography>
                    )}
                  </Box>
                </Collapse>
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

function ProjectTodoItem({ todo }: { todo: Todo }) {
  const isDone = todo.status === "done";
  const priority = todo.priority || "medium";

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        py: 0.25,
      }}
    >
      <Box sx={{ p: 0.5, display: "flex", alignItems: "center" }}>
        {isDone ? (
          <CheckCircleOutlineIcon
            sx={{ fontSize: 18, color: "success.main", opacity: 0.6 }}
          />
        ) : (
          <RadioButtonUncheckedIcon
            sx={{ fontSize: 18, color: "text.secondary" }}
          />
        )}
      </Box>
      <Typography
        variant="body2"
        sx={{
          flex: 1,
          textDecoration: isDone ? "line-through" : "none",
          opacity: isDone ? 0.6 : 1,
          ml: 0.5,
        }}
      >
        {todo.title}
      </Typography>
      {priority !== "medium" && (
        <Chip
          label={priority}
          size="small"
          color={priorityColor[priority] || "default"}
          variant="outlined"
          sx={{ height: 18, fontSize: "0.65rem", mr: 0.5 }}
        />
      )}
    </Box>
  );
}
