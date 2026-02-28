import React, { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import Checkbox from "@mui/material/Checkbox";
import Chip from "@mui/material/Chip";
import Collapse from "@mui/material/Collapse";
import Tooltip from "@mui/material/Tooltip";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import AddIcon from "@mui/icons-material/Add";

import type { Todo } from "../types";

interface TodoPanelProps {
  todos: Todo[];
  collapsed: boolean;
  onToggleCollapsed: (collapsed: boolean) => void;
  onAdd: (title: string, priority?: string) => void;
  onComplete: (todoId: string) => void;
  onReopen: (todoId: string) => void;
  onDelete: (todoId: string) => void;
}

const priorityColor: Record<string, "error" | "warning" | "default"> = {
  high: "error",
  medium: "warning",
  low: "default",
};

export default function TodoPanel({
  todos,
  collapsed,
  onToggleCollapsed,
  onAdd,
  onComplete,
  onReopen,
  onDelete,
}: TodoPanelProps) {
  const { t } = useTranslation();
  const [newTitle, setNewTitle] = useState("");

  const openTodos = todos.filter((todo) => todo.status === "open");
  const doneTodos = todos.filter((todo) => todo.status === "done");
  const openCount = openTodos.length;

  const handleAdd = useCallback(() => {
    const title = newTitle.trim();
    if (!title) return;
    onAdd(title);
    setNewTitle("");
  }, [newTitle, onAdd]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter") {
        e.preventDefault();
        handleAdd();
      }
    },
    [handleAdd],
  );

  if (todos.length === 0) return null;

  return (
    <Box
      sx={{
        borderTop: 1,
        borderColor: "divider",
        bgcolor: "background.paper",
      }}
    >
      {/* Header */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          px: 2,
          py: 0.5,
          cursor: "pointer",
          "&:hover": { bgcolor: "action.hover" },
        }}
        onClick={() => onToggleCollapsed(!collapsed)}
      >
        <Typography variant="subtitle2" sx={{ flex: 1 }}>
          {t("todos.title")}
          {openCount > 0 && (
            <Chip
              label={openCount}
              size="small"
              color="primary"
              sx={{ ml: 1, height: 20, fontSize: "0.7rem" }}
            />
          )}
        </Typography>
        <IconButton size="small">
          {collapsed ? <ExpandLessIcon /> : <ExpandMoreIcon />}
        </IconButton>
      </Box>

      <Collapse in={!collapsed}>
        <Box sx={{ px: 2, pb: 1 }}>
          {/* Add todo input */}
          <Box sx={{ display: "flex", alignItems: "center", mb: 1 }}>
            <TextField
              size="small"
              fullWidth
              placeholder={t("todos.addPlaceholder")}
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              onKeyDown={handleKeyDown}
              sx={{ mr: 0.5 }}
            />
            <IconButton
              size="small"
              onClick={handleAdd}
              disabled={!newTitle.trim()}
            >
              <AddIcon fontSize="small" />
            </IconButton>
          </Box>

          {/* Open todos */}
          {openTodos.map((todo) => (
            <TodoItem
              key={todo.id}
              todo={todo}
              onToggle={() => onComplete(todo.id)}
              onDelete={() => onDelete(todo.id)}
            />
          ))}

          {/* Done todos */}
          {doneTodos.map((todo) => (
            <TodoItem
              key={todo.id}
              todo={todo}
              onToggle={() => onReopen(todo.id)}
              onDelete={() => onDelete(todo.id)}
            />
          ))}
        </Box>
      </Collapse>
    </Box>
  );
}

function TodoItem({
  todo,
  onToggle,
  onDelete,
}: {
  todo: Todo;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const isDone = todo.status === "done";
  const priority = todo.priority || "medium";

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        py: 0.25,
        "&:hover .todo-delete": { opacity: 1 },
      }}
    >
      <Checkbox
        size="small"
        checked={isDone}
        onChange={onToggle}
        sx={{ p: 0.5 }}
      />
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
      <Tooltip title="Delete">
        <IconButton
          size="small"
          className="todo-delete"
          onClick={onDelete}
          sx={{ opacity: 0, transition: "opacity 0.2s" }}
        >
          <DeleteOutlineIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Tooltip>
    </Box>
  );
}
