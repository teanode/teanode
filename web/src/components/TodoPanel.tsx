import React from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import Chip from "@mui/material/Chip";
import Collapse from "@mui/material/Collapse";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import CheckCircleOutlineIcon from "@mui/icons-material/CheckCircleOutline";
import RadioButtonUncheckedIcon from "@mui/icons-material/RadioButtonUnchecked";

import type { Todo } from "../types";

interface TodoPanelProps {
  todos: Todo[];
  collapsed: boolean;
  onToggleCollapsed: (collapsed: boolean) => void;
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
}: TodoPanelProps) {
  const { t } = useTranslation();

  const byCreatedAt = (a: Todo, b: Todo) =>
    (a.createdAt || "").localeCompare(b.createdAt || "");
  const openTodos = todos
    .filter((todo) => todo.status === "open")
    .sort(byCreatedAt);
  const doneTodos = todos
    .filter((todo) => todo.status === "done")
    .sort(byCreatedAt);
  const openCount = openTodos.length;

  if (todos.length === 0) return null;

  return (
    <Box
      sx={{
        borderTop: 1,
        borderColor: "divider",
      }}
    >
      <Container maxWidth="md" disableGutters>
        {/* Header */}
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            px: 2,
            py: 0.5,
          }}
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
          <IconButton
            size="small"
            onClick={() => onToggleCollapsed(!collapsed)}
          >
            {collapsed ? <ExpandLessIcon /> : <ExpandMoreIcon />}
          </IconButton>
        </Box>

        <Collapse in={!collapsed}>
          <Box sx={{ px: 2, pb: 1, maxHeight: 240, overflowY: "auto" }}>
            {/* Open todos */}
            {openTodos.map((todo) => (
              <TodoItem key={todo.id} todo={todo} />
            ))}

            {/* Done todos */}
            {doneTodos.map((todo) => (
              <TodoItem key={todo.id} todo={todo} />
            ))}
          </Box>
        </Collapse>
      </Container>
    </Box>
  );
}

function TodoItem({ todo }: { todo: Todo }) {
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
            sx={{ fontSize: 20, color: "success.main", opacity: 0.6 }}
          />
        ) : (
          <RadioButtonUncheckedIcon
            sx={{ fontSize: 20, color: "text.secondary" }}
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
