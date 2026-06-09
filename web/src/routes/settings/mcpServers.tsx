import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Container from "@mui/material/Container";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import FormControl from "@mui/material/FormControl";
import IconButton from "@mui/material/IconButton";
import InputLabel from "@mui/material/InputLabel";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemText from "@mui/material/ListItemText";
import MenuItem from "@mui/material/MenuItem";
import Select from "@mui/material/Select";
import Switch from "@mui/material/Switch";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import EditOutlinedIcon from "@mui/icons-material/EditOutlined";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAlert } from "../../components/AlertProvider";
import { useAppContext } from "../../context";
import {
  emptyForm,
  formToServer,
  hasErrors,
  loadMcpServers,
  resolveAuthMode,
  resolveTransport,
  saveMcpServers,
  serverToForm,
  summarizeServer,
  validateForm,
  type FormErrors,
  type McpAuthMode,
  type McpServerFormValues,
  type McpTransport,
  type RawMcpServer,
} from "./mcpServers.helpers";

const TRANSPORTS: McpTransport[] = ["http", "stdio"];
const AUTH_MODES: McpAuthMode[] = ["none", "static", "user", "oauth"];

/** Dialog state: the form plus the index being edited (null = adding). */
interface EditState {
  index: number | null;
  values: McpServerFormValues;
}

export default function SettingsMcpServersPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const [servers, setServers] = useState<RawMcpServer[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [edit, setEdit] = useState<EditState | null>(null);
  const [errors, setErrors] = useState<FormErrors>({});
  const [saving, setSaving] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null);

  const load = useCallback(() => {
    loadMcpServers(backend)
      .then((items) => {
        setServers(items);
        setLoaded(true);
      })
      .catch((error: unknown) =>
        showAlert(
          error instanceof Error ? error.message : t("mcpServers.loadFailed"),
          "error",
        ),
      );
  }, [backend, showAlert, t]);

  useEffect(() => {
    if (backend.connected && backend.isAdmin) {
      load();
    }
  }, [backend.connected, backend.isAdmin, load]);

  // Persist a new servers array, then reload to reflect what was actually stored.
  const persist = useCallback(
    async (next: RawMcpServer[], successMessage?: string) => {
      setSaving(true);
      try {
        await saveMcpServers(backend, next);
        if (successMessage) showAlert(successMessage);
        load();
        return true;
      } catch (error) {
        showAlert(
          error instanceof Error ? error.message : t("mcpServers.saveFailed"),
          "error",
        );
        return false;
      } finally {
        setSaving(false);
      }
    },
    [backend, load, showAlert, t],
  );

  const openAdd = useCallback(() => {
    setErrors({});
    setEdit({ index: null, values: emptyForm() });
  }, []);

  const openEdit = useCallback(
    (index: number) => {
      setErrors({});
      setEdit({ index, values: serverToForm(servers[index]) });
    },
    [servers],
  );

  const setField = useCallback(
    <Key extends keyof McpServerFormValues>(
      key: Key,
      value: McpServerFormValues[Key],
    ) => {
      setEdit((current) =>
        current
          ? { ...current, values: { ...current.values, [key]: value } }
          : current,
      );
    },
    [],
  );

  const submit = useCallback(async () => {
    if (!edit) return;
    const otherNames = servers
      .filter((_, index) => index !== edit.index)
      .map((server) => (server.name ?? "").trim())
      .filter(Boolean);
    const validation = validateForm(edit.values, otherNames);
    if (hasErrors(validation)) {
      setErrors(validation);
      return;
    }
    const server = formToServer(edit.values);
    const next =
      edit.index == null
        ? [...servers, server]
        : servers.map((existing, index) =>
            index === edit.index ? server : existing,
          );
    if (await persist(next)) {
      setEdit(null);
    }
  }, [edit, persist, servers]);

  const toggleEnabled = useCallback(
    (index: number) => {
      const next = servers.map((server, position) =>
        position === index
          ? { ...server, enabled: server.enabled === false }
          : server,
      );
      persist(next);
    },
    [persist, servers],
  );

  const confirmDelete = useCallback(() => {
    if (deleteTarget == null) return;
    const removed = servers[deleteTarget];
    const next = servers.filter((_, index) => index !== deleteTarget);
    setDeleteTarget(null);
    persist(next, t("mcpServers.deleted", { server: removed?.name ?? "" }));
  }, [deleteTarget, persist, servers, t]);

  if (loaded && !backend.isAdmin) {
    return (
      <Box sx={{ flex: 1, p: 3 }}>
        <Typography variant="body2" color="text.secondary">
          {t("mcpServers.adminOnly")}
        </Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            mb: 3,
          }}
        >
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t("mcpServers.title")}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {t("mcpServers.description")}
            </Typography>
          </Box>
          <Button
            size="small"
            variant="contained"
            startIcon={<AddIcon />}
            onClick={openAdd}
          >
            {t("mcpServers.add")}
          </Button>
        </Box>

        {loaded && servers.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            {t("mcpServers.empty")}
          </Typography>
        ) : (
          <List disablePadding>
            {servers.map((server, index) => {
              const transport = resolveTransport(server);
              const authMode = resolveAuthMode(server);
              const enabled = server.enabled !== false;
              return (
                <ListItem
                  key={`${server.name ?? "server"}-${index}`}
                  disableGutters
                  alignItems="flex-start"
                  secondaryAction={
                    <Box sx={{ display: "flex", alignItems: "center" }}>
                      <Tooltip
                        title={
                          enabled
                            ? t("mcpServers.disable")
                            : t("mcpServers.enable")
                        }
                        arrow
                      >
                        <Switch
                          size="small"
                          checked={enabled}
                          disabled={saving}
                          onChange={() => toggleEnabled(index)}
                        />
                      </Tooltip>
                      <IconButton size="small" onClick={() => openEdit(index)}>
                        <EditOutlinedIcon fontSize="small" />
                      </IconButton>
                      <IconButton
                        size="small"
                        color="error"
                        onClick={() => setDeleteTarget(index)}
                      >
                        <DeleteOutlineIcon fontSize="small" />
                      </IconButton>
                    </Box>
                  }
                  sx={{ pr: 16 }}
                >
                  <ListItemText
                    primary={
                      <Box
                        sx={{ display: "flex", alignItems: "center", gap: 1 }}
                      >
                        <Typography variant="body2" sx={{ fontWeight: 600 }}>
                          {server.name || t("mcpServers.unnamed")}
                        </Typography>
                        <Chip
                          label={t(`mcpServers.transport.${transport}`)}
                          size="small"
                          variant="outlined"
                        />
                        {transport === "http" && (
                          <Chip
                            label={t(`mcp.authMode.${authMode}`)}
                            size="small"
                            variant="outlined"
                          />
                        )}
                        {!enabled && (
                          <Chip
                            label={t("mcpServers.disabled")}
                            size="small"
                            color="default"
                          />
                        )}
                      </Box>
                    }
                    secondary={
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{
                          display: "block",
                          wordBreak: "break-all",
                          fontFamily:
                            transport === "stdio" ? "monospace" : undefined,
                        }}
                      >
                        {summarizeServer(server)}
                      </Typography>
                    }
                  />
                </ListItem>
              );
            })}
          </List>
        )}
      </Container>

      <ServerDialog
        edit={edit}
        errors={errors}
        saving={saving}
        onField={setField}
        onClose={() => setEdit(null)}
        onSubmit={submit}
      />

      <ConfirmDialog
        open={deleteTarget != null}
        title={t("mcpServers.deleteTitle")}
        message={
          deleteTarget != null
            ? t("mcpServers.deleteMessage", {
                server: servers[deleteTarget]?.name ?? "",
              })
            : ""
        }
        confirmLabel={t("mcpServers.delete")}
        onConfirm={confirmDelete}
        onClose={() => setDeleteTarget(null)}
      />
    </Box>
  );
}

/** The add/edit form, rendered in a dialog. */
function ServerDialog({
  edit,
  errors,
  saving,
  onField,
  onClose,
  onSubmit,
}: {
  edit: EditState | null;
  errors: FormErrors;
  saving: boolean;
  onField: <Key extends keyof McpServerFormValues>(
    key: Key,
    value: McpServerFormValues[Key],
  ) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const { t } = useTranslation();
  const values = edit?.values;

  const nameError = useMemo(() => {
    if (errors.name === "required") return t("mcpServers.errors.nameRequired");
    if (errors.name === "duplicate")
      return t("mcpServers.errors.nameDuplicate");
    return undefined;
  }, [errors.name, t]);
  const urlError = useMemo(() => {
    if (errors.url === "required") return t("mcpServers.errors.urlRequired");
    if (errors.url === "invalid") return t("mcpServers.errors.urlInvalid");
    return undefined;
  }, [errors.url, t]);

  if (!values) return null;

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ fontSize: "0.9rem", fontWeight: 600 }}>
        {edit?.index == null
          ? t("mcpServers.addTitle")
          : t("mcpServers.editTitle", { server: values.name })}
      </DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 0.5 }}>
          <TextField
            autoFocus
            size="small"
            fullWidth
            label={t("mcpServers.fields.name")}
            value={values.name}
            error={!!nameError}
            helperText={nameError}
            onChange={(event) => onField("name", event.target.value)}
          />

          <FormControl size="small" fullWidth>
            <InputLabel>{t("mcpServers.fields.transport")}</InputLabel>
            <Select
              label={t("mcpServers.fields.transport")}
              value={values.transport}
              onChange={(event) =>
                onField("transport", event.target.value as McpTransport)
              }
            >
              {TRANSPORTS.map((transport) => (
                <MenuItem key={transport} value={transport}>
                  {t(`mcpServers.transport.${transport}`)}
                </MenuItem>
              ))}
            </Select>
          </FormControl>

          {values.transport === "http" ? (
            <>
              <TextField
                size="small"
                fullWidth
                label={t("mcpServers.fields.url")}
                placeholder="https://example.com/mcp"
                value={values.url}
                error={!!urlError}
                helperText={urlError}
                onChange={(event) => onField("url", event.target.value)}
              />
              <FormControl size="small" fullWidth>
                <InputLabel>{t("mcpServers.fields.authMode")}</InputLabel>
                <Select
                  label={t("mcpServers.fields.authMode")}
                  value={values.authMode}
                  onChange={(event) =>
                    onField("authMode", event.target.value as McpAuthMode)
                  }
                >
                  {AUTH_MODES.map((mode) => (
                    <MenuItem key={mode} value={mode}>
                      {t(`mcp.authMode.${mode}`)}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
              {values.authMode === "static" && (
                <TextField
                  size="small"
                  fullWidth
                  type="password"
                  label={t("mcpServers.fields.authorization")}
                  placeholder="Bearer ..."
                  value={values.authorization}
                  onChange={(event) =>
                    onField("authorization", event.target.value)
                  }
                />
              )}
              {values.authMode === "oauth" && (
                <>
                  <TextField
                    size="small"
                    fullWidth
                    label={t("mcpServers.fields.oauthClientId")}
                    value={values.oauthClientId}
                    onChange={(event) =>
                      onField("oauthClientId", event.target.value)
                    }
                  />
                  <TextField
                    size="small"
                    fullWidth
                    type="password"
                    label={t("mcpServers.fields.oauthClientSecret")}
                    value={values.oauthClientSecret}
                    onChange={(event) =>
                      onField("oauthClientSecret", event.target.value)
                    }
                  />
                  <TextField
                    size="small"
                    fullWidth
                    label={t("mcpServers.fields.oauthScopes")}
                    value={values.oauthScopes}
                    onChange={(event) =>
                      onField("oauthScopes", event.target.value)
                    }
                  />
                  <TextField
                    size="small"
                    fullWidth
                    label={t("mcpServers.fields.oauthAuthorizationUrl")}
                    value={values.oauthAuthorizationUrl}
                    onChange={(event) =>
                      onField("oauthAuthorizationUrl", event.target.value)
                    }
                  />
                  <TextField
                    size="small"
                    fullWidth
                    label={t("mcpServers.fields.oauthTokenUrl")}
                    value={values.oauthTokenUrl}
                    onChange={(event) =>
                      onField("oauthTokenUrl", event.target.value)
                    }
                  />
                </>
              )}
            </>
          ) : (
            <>
              <TextField
                size="small"
                fullWidth
                label={t("mcpServers.fields.command")}
                placeholder="npx"
                value={values.command}
                error={errors.command === "required"}
                helperText={
                  errors.command === "required"
                    ? t("mcpServers.errors.commandRequired")
                    : undefined
                }
                onChange={(event) => onField("command", event.target.value)}
              />
              <TextField
                size="small"
                fullWidth
                multiline
                minRows={2}
                label={t("mcpServers.fields.args")}
                helperText={t("mcpServers.fields.argsHelp")}
                value={values.args}
                onChange={(event) => onField("args", event.target.value)}
              />
              <TextField
                size="small"
                fullWidth
                multiline
                minRows={2}
                label={t("mcpServers.fields.env")}
                helperText={t("mcpServers.fields.envHelp")}
                value={values.env}
                onChange={(event) => onField("env", event.target.value)}
              />
              <TextField
                size="small"
                fullWidth
                label={t("mcpServers.fields.workingDir")}
                value={values.workingDir}
                onChange={(event) => onField("workingDir", event.target.value)}
              />
            </>
          )}

          <TextField
            size="small"
            fullWidth
            label={t("mcpServers.fields.timeoutSeconds")}
            value={values.timeoutSeconds}
            error={errors.timeoutSeconds === "invalid"}
            helperText={
              errors.timeoutSeconds === "invalid"
                ? t("mcpServers.errors.timeoutInvalid")
                : undefined
            }
            onChange={(event) => onField("timeoutSeconds", event.target.value)}
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t("common.cancel")}</Button>
        <Button variant="contained" disabled={saving} onClick={onSubmit}>
          {saving ? t("common.saving") : t("common.save")}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
