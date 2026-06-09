import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import CloseRounded from "@mui/icons-material/CloseRounded";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import FormControlLabel from "@mui/material/FormControlLabel";
import Checkbox from "@mui/material/Checkbox";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import { renderMarkdown } from "../markdown";
import type {
  Surface,
  SurfaceComponent,
  SurfaceButton,
  FormField,
  SurfaceActionPayload,
} from "../types";

interface SurfaceRendererProps {
  surface: Surface;
  onAction: (action: SurfaceActionPayload) => void;
  disabled?: boolean;
  /** When provided, renders a close button in the surface header. */
  onClose?: () => void;
}

const STATUS_COLOR: Record<
  string,
  "success" | "warning" | "error" | "info" | "default"
> = {
  success: "success",
  warning: "warning",
  error: "error",
  info: "info",
  neutral: "default",
};

function badgeColor(status?: string) {
  if (!status) return "default";
  return STATUS_COLOR[status] ?? "default";
}

/** Renders a single schema-driven surface and its component tree. */
export default function SurfaceRenderer({
  surface,
  onAction,
  disabled = false,
  onClose,
}: SurfaceRendererProps) {
  const { t } = useTranslation();
  return (
    <Box
      sx={{
        bgcolor: "surface2",
        border: 1,
        borderColor: "divider",
        borderRadius: 1.5,
        p: 1.5,
      }}
    >
      {(surface.title || onClose) && (
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 1,
            mb: 1,
          }}
        >
          <Typography
            variant="subtitle2"
            sx={{ fontWeight: 600, flex: 1, minWidth: 0, overflowWrap: "anywhere" }}
          >
            {surface.title}
          </Typography>
          {onClose && (
            <IconButton
              size="small"
              onClick={onClose}
              aria-label={t("surface.close")}
              title={t("surface.close")}
              sx={{ flexShrink: 0, m: -0.5 }}
            >
              <CloseRounded fontSize="small" />
            </IconButton>
          )}
        </Box>
      )}
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1.25 }}>
        {surface.components.map((component, index) => (
          <ComponentRenderer
            key={index}
            component={component}
            surfaceId={surface.surfaceId}
            onAction={onAction}
            disabled={disabled}
          />
        ))}
      </Box>
    </Box>
  );
}

interface ComponentRendererProps {
  component: SurfaceComponent;
  surfaceId: string;
  onAction: (action: SurfaceActionPayload) => void;
  disabled: boolean;
}

function ComponentRenderer({
  component,
  surfaceId,
  onAction,
  disabled,
}: ComponentRendererProps) {
  switch (component.type) {
    case "Section":
      return (
        <Box>
          {component.title && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                fontWeight: 600,
                textTransform: "uppercase",
                letterSpacing: 0.5,
              }}
            >
              {component.title}
            </Typography>
          )}
          <Box
            sx={{
              display: "flex",
              flexDirection: "column",
              gap: 1.25,
              mt: component.title ? 0.5 : 0,
            }}
          >
            {(component.children ?? []).map((child, index) => (
              <ComponentRenderer
                key={index}
                component={child}
                surfaceId={surfaceId}
                onAction={onAction}
                disabled={disabled}
              />
            ))}
          </Box>
        </Box>
      );

    case "Markdown":
      return (
        <Box
          className="markdown-content"
          sx={{ fontSize: "0.9rem", minWidth: 0, "& p": { my: 0.5 } }}
          dangerouslySetInnerHTML={{
            __html: renderMarkdown(component.text ?? ""),
          }}
        />
      );

    case "CodeBlock":
      return (
        <Box
          component="pre"
          sx={{
            fontSize: "0.8rem",
            fontFamily: "monospace",
            bgcolor: "surface1",
            p: 1,
            borderRadius: 0.5,
            overflow: "auto",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            m: 0,
          }}
        >
          {component.text ?? ""}
        </Box>
      );

    case "KeyValueList":
      return (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
          {(component.items ?? []).map((item, index) => (
            <Box key={index} sx={{ display: "flex", gap: 1, minWidth: 0 }}>
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ minWidth: 120, flexShrink: 0, fontWeight: 500 }}
              >
                {item.key}
              </Typography>
              <Typography
                variant="body2"
                sx={{ minWidth: 0, overflowWrap: "anywhere" }}
              >
                {item.value}
              </Typography>
            </Box>
          ))}
        </Box>
      );

    case "Table":
      return (
        <Box sx={{ overflowX: "auto" }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                {(component.columns ?? []).map((column, index) => (
                  <TableCell key={index} sx={{ fontWeight: 600 }}>
                    {column}
                  </TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {(component.rows ?? []).map((row, rowIndex) => (
                <TableRow key={rowIndex}>
                  {row.map((cell, cellIndex) => (
                    <TableCell
                      key={cellIndex}
                      sx={{ overflowWrap: "anywhere", wordBreak: "break-word" }}
                    >
                      {cell}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      );

    case "StatusBadge":
      return (
        <Box sx={{ minWidth: 0 }}>
          <Chip
            label={component.label ?? component.status ?? ""}
            size="small"
            color={badgeColor(component.status)}
            variant="outlined"
            sx={{
              maxWidth: "100%",
              height: "auto",
              "& .MuiChip-label": {
                whiteSpace: "normal",
                overflowWrap: "anywhere",
                py: 0.25,
              },
            }}
          />
        </Box>
      );

    case "ButtonRow":
      return (
        <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
          {(component.buttons ?? []).map((button, index) => (
            <SurfaceActionButton
              key={index}
              button={button}
              disabled={disabled}
              onClick={() =>
                onAction({
                  surfaceId,
                  actionId: button.actionId,
                  value: button.value,
                })
              }
            />
          ))}
        </Box>
      );

    case "Form":
      return (
        <SurfaceForm
          component={component}
          surfaceId={surfaceId}
          onAction={onAction}
          disabled={disabled}
        />
      );

    case "Timeline":
      return (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
          {(component.events ?? []).map((event, index) => (
            <Box
              key={index}
              sx={{
                borderLeft: 2,
                borderColor: "primary.main",
                pl: 1.5,
                py: 0.25,
              }}
            >
              <Box sx={{ display: "flex", gap: 1, alignItems: "baseline" }}>
                <Typography variant="body2" sx={{ fontWeight: 600 }}>
                  {event.title}
                </Typography>
                {event.timestamp && (
                  <Typography variant="caption" color="text.secondary">
                    {event.timestamp}
                  </Typography>
                )}
                {event.status && (
                  <Chip
                    label={event.status}
                    size="small"
                    color={badgeColor(event.status)}
                    variant="outlined"
                    sx={{ height: 18, fontSize: "0.65rem" }}
                  />
                )}
              </Box>
              {event.description && (
                <Typography variant="body2" color="text.secondary">
                  {event.description}
                </Typography>
              )}
            </Box>
          ))}
        </Box>
      );

    default:
      return null;
  }
}

function SurfaceActionButton({
  button,
  onClick,
  disabled,
}: {
  button: SurfaceButton;
  onClick: () => void;
  disabled: boolean;
}) {
  const isDanger = button.style === "danger";
  const isPrimary = button.style === "primary" || !button.style;
  return (
    <Button
      size="small"
      variant={isPrimary ? "contained" : "outlined"}
      color={isDanger ? "error" : "primary"}
      disabled={disabled}
      onClick={onClick}
      sx={{ textTransform: "none" }}
    >
      {button.label}
    </Button>
  );
}

function SurfaceForm({
  component,
  surfaceId,
  onAction,
  disabled,
}: {
  component: SurfaceComponent;
  surfaceId: string;
  onAction: (action: SurfaceActionPayload) => void;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const fields = component.fields ?? [];
  const [values, setValues] = useState<Record<string, string>>(() => {
    const initial: Record<string, string> = {};
    for (const field of fields) {
      initial[field.name] = field.defaultValue ?? "";
    }
    return initial;
  });
  const [submitted, setSubmitted] = useState(false);

  function setValue(name: string, value: string) {
    setValues((previous) => ({ ...previous, [name]: value }));
  }

  function handleSubmit() {
    if (disabled || submitted) return;
    setSubmitted(true);
    // onAction may be async (the surfaces.action round-trip); re-enable the
    // form if it rejects so the user can retry instead of being stuck.
    Promise.resolve(
      onAction({
        surfaceId,
        actionId: component.submitActionId || "submit",
        formData: values,
      }),
    ).catch(() => setSubmitted(false));
  }

  const missingRequired = fields.some(
    (field) =>
      field.required &&
      field.type !== "Checkbox" &&
      !(values[field.name] ?? "").trim(),
  );

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.25 }}>
      {fields.map((field) => (
        <FormFieldRenderer
          key={field.name}
          field={field}
          value={values[field.name] ?? ""}
          onChange={(value) => setValue(field.name, value)}
          disabled={disabled || submitted}
        />
      ))}
      <Box>
        <Button
          size="small"
          variant="contained"
          disabled={disabled || submitted || missingRequired}
          onClick={handleSubmit}
          sx={{ textTransform: "none" }}
        >
          {component.submitLabel || t("surface.submit")}
        </Button>
      </Box>
    </Box>
  );
}

function FormFieldRenderer({
  field,
  value,
  onChange,
  disabled,
}: {
  field: FormField;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
}) {
  switch (field.type) {
    case "Textarea":
      return (
        <TextField
          label={field.label || field.name}
          placeholder={field.placeholder}
          value={value}
          required={field.required}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          size="small"
          fullWidth
          multiline
          minRows={3}
        />
      );

    case "Select":
      return (
        <TextField
          select
          label={field.label || field.name}
          value={value}
          required={field.required}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          size="small"
          fullWidth
        >
          {(field.options ?? []).map((option) => (
            <MenuItem key={option} value={option}>
              {option}
            </MenuItem>
          ))}
        </TextField>
      );

    case "Checkbox":
      return (
        <FormControlLabel
          control={
            <Checkbox
              checked={value === "true"}
              disabled={disabled}
              onChange={(event) =>
                onChange(event.target.checked ? "true" : "false")
              }
            />
          }
          label={field.label || field.name}
        />
      );

    case "TextInput":
    default:
      return (
        <TextField
          label={field.label || field.name}
          placeholder={field.placeholder}
          value={value}
          required={field.required}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          size="small"
          fullWidth
        />
      );
  }
}
