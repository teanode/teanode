import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Container from "@mui/material/Container";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import FormControl from "@mui/material/FormControl";
import FormControlLabel from "@mui/material/FormControlLabel";
import IconButton from "@mui/material/IconButton";
import InputLabel from "@mui/material/InputLabel";
import MenuItem from "@mui/material/MenuItem";
import Paper from "@mui/material/Paper";
import Radio from "@mui/material/Radio";
import RadioGroup from "@mui/material/RadioGroup";
import Select from "@mui/material/Select";
import Switch from "@mui/material/Switch";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import EditOutlinedIcon from "@mui/icons-material/EditOutlined";
import KeyOutlinedIcon from "@mui/icons-material/KeyOutlined";
import LinkIcon from "@mui/icons-material/Link";
import LinkOffIcon from "@mui/icons-material/LinkOff";
import RefreshIcon from "@mui/icons-material/Refresh";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAlert } from "../../components/AlertProvider";
import { useAppContext } from "../../context";
import type { MCPConnectionStatus, MCPServerListItem } from "../../types";
import {
  authorizeMcpConnection,
  createMcpConnection,
  deleteMcpConnection,
  listMcpServers,
  MCP_OAUTH_CALLBACK_PATH,
  parseOAuthCallback,
  serverAction,
} from "./connections.helpers";
import {
  emptyForm,
  formToServer,
  hasErrors,
  loadMcpServers,
  saveMcpServers,
  serverToForm,
  validateForm,
  type FormErrors,
  type McpAuthMode,
  type McpServerFormValues,
  type McpTransport,
  type RawMcpServer,
} from "./mcpServers.helpers";

dayjs.extend(relativeTime);

const TRANSPORTS: McpTransport[] = ["http", "stdio"];
const AUTH_MODES: McpAuthMode[] = ["none", "static", "user", "oauth"];

type ChipColor = "default" | "success" | "warning" | "error";
type RedirectChoice = "browser" | "node";

function statusColor(status?: MCPConnectionStatus): ChipColor {
  switch (status) {
    case "connected":
      return "success";
    case "pending":
      return "warning";
    case "error":
      return "error";
    default:
      return "default";
  }
}

/** Admin edit/add dialog state. originalName is null when adding. */
interface EditState {
  originalName: string | null;
  values: McpServerFormValues;
}

const normalizeName = (server: RawMcpServer) => (server.name ?? "").trim();

export default function SettingsMcpPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const isAdmin = backend.isAdmin;

  const [servers, setServers] = useState<MCPServerListItem[]>([]);
  const [loaded, setLoaded] = useState(false);
  // Raw server definitions (admin only, from config.get) used for editing.
  const [rawServers, setRawServers] = useState<RawMcpServer[]>([]);
  const [rawLoaded, setRawLoaded] = useState(false);

  // Admin management dialogs.
  const [edit, setEdit] = useState<EditState | null>(null);
  const [errors, setErrors] = useState<FormErrors>({});
  const [saving, setSaving] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<MCPServerListItem | null>(
    null,
  );

  // Per-user connection dialogs (available to every user).
  const [connectTarget, setConnectTarget] = useState<MCPServerListItem | null>(
    null,
  );
  const [credential, setCredential] = useState("");
  const [connectSaving, setConnectSaving] = useState(false);
  const [authorizeTarget, setAuthorizeTarget] =
    useState<MCPServerListItem | null>(null);
  const [redirectChoice, setRedirectChoice] =
    useState<RedirectChoice>("browser");
  const [disconnectTarget, setDisconnectTarget] =
    useState<MCPServerListItem | null>(null);

  const browserRedirectUri = window.location.origin + MCP_OAUTH_CALLBACK_PATH;
  const rawByName = useMemo(() => {
    const map: Record<string, RawMcpServer> = {};
    for (const server of rawServers) map[normalizeName(server)] = server;
    return map;
  }, [rawServers]);

  const load = useCallback(() => {
    listMcpServers(backend)
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
    // The raw definitions back the admin edit form; non-admins never load them.
    if (isAdmin) {
      loadMcpServers(backend)
        .then((items) => {
          setRawServers(items);
          setRawLoaded(true);
        })
        .catch((error: unknown) =>
          showAlert(
            error instanceof Error ? error.message : t("mcpServers.loadFailed"),
            "error",
          ),
        );
    }
  }, [backend, isAdmin, showAlert, t]);

  useEffect(() => {
    if (backend.connected) load();
  }, [backend.connected, load]);

  // OAuth callback landing: the backend redirects the browser back here with the
  // outcome in the query string. Surface it, then strip the markers.
  useEffect(() => {
    const outcome = parseOAuthCallback(window.location.search);
    if (!outcome) return;
    if (outcome.ok) {
      showAlert(
        outcome.server
          ? t("mcp.authorizeSucceeded", { server: outcome.server })
          : t("mcp.authorizeSucceededGeneric"),
      );
    } else {
      showAlert(
        outcome.error
          ? t("mcp.authorizeFailed", { error: outcome.error })
          : t("mcp.authorizeFailedGeneric"),
        "error",
      );
    }
    window.history.replaceState(null, "", window.location.pathname);
  }, [showAlert, t]);

  // Persist the full raw server array, then reload to reflect what was stored.
  const persistRaw = useCallback(
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
    setEdit({ originalName: null, values: emptyForm() });
  }, []);

  const openEdit = useCallback(
    (server: MCPServerListItem) => {
      setErrors({});
      const raw = rawByName[server.name] ?? { name: server.name };
      setEdit({ originalName: server.name, values: serverToForm(raw) });
    },
    [rawByName],
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
    const otherNames = rawServers
      .map(normalizeName)
      .filter(Boolean)
      .filter((name) => name !== edit.originalName);
    const validation = validateForm(edit.values, otherNames);
    if (hasErrors(validation)) {
      setErrors(validation);
      return;
    }
    const built = formToServer(edit.values);
    const next =
      edit.originalName == null
        ? [...rawServers, built]
        : rawServers.map((server) =>
            normalizeName(server) === edit.originalName ? built : server,
          );
    if (await persistRaw(next)) setEdit(null);
  }, [edit, persistRaw, rawServers]);

  const toggleEnabled = useCallback(
    (name: string) => {
      const next = rawServers.map((server) =>
        normalizeName(server) === name
          ? { ...server, enabled: server.enabled === false }
          : server,
      );
      persistRaw(next);
    },
    [persistRaw, rawServers],
  );

  const confirmDelete = useCallback(() => {
    if (!deleteTarget) return;
    const name = deleteTarget.name;
    setDeleteTarget(null);
    persistRaw(
      rawServers.filter((server) => normalizeName(server) !== name),
      t("mcpServers.deleted", { server: name }),
    );
  }, [deleteTarget, persistRaw, rawServers, t]);

  const submitConnect = useCallback(async () => {
    if (!connectTarget) return;
    const authorization = credential.trim();
    if (!authorization) return;
    setConnectSaving(true);
    try {
      await createMcpConnection(backend, connectTarget.name, authorization);
      showAlert(t("mcp.connected", { server: connectTarget.name }));
      setConnectTarget(null);
      setCredential("");
      load();
    } catch (error) {
      showAlert(
        error instanceof Error ? error.message : t("mcp.connectFailed"),
        "error",
      );
    } finally {
      setConnectSaving(false);
    }
  }, [backend, connectTarget, credential, load, showAlert, t]);

  const confirmAuthorize = useCallback(async () => {
    if (!authorizeTarget) return;
    const server = authorizeTarget;
    const redirectUri =
      redirectChoice === "browser" ? browserRedirectUri : undefined;
    setAuthorizeTarget(null);
    try {
      const authorizationUrl = await authorizeMcpConnection(
        backend,
        server.name,
        redirectUri,
      );
      window.location.href = authorizationUrl;
    } catch (error) {
      showAlert(
        error instanceof Error ? error.message : t("mcp.authorizeStartFailed"),
        "error",
      );
    }
  }, [
    authorizeTarget,
    redirectChoice,
    browserRedirectUri,
    backend,
    showAlert,
    t,
  ]);

  const confirmDisconnect = useCallback(async () => {
    if (!disconnectTarget?.connectionId) {
      setDisconnectTarget(null);
      return;
    }
    const server = disconnectTarget;
    setDisconnectTarget(null);
    try {
      await deleteMcpConnection(backend, server.connectionId as string);
      showAlert(t("mcp.disconnected", { server: server.name }));
      load();
    } catch (error) {
      showAlert(
        error instanceof Error ? error.message : t("mcp.disconnectFailed"),
        "error",
      );
    }
  }, [backend, disconnectTarget, load, showAlert, t]);

  const canManage = isAdmin && rawLoaded;

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box
          sx={{
            mb: 3,
            display: "flex",
            alignItems: "flex-start",
            justifyContent: "space-between",
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
          {canManage && (
            <Tooltip title={t("mcpServers.add")} arrow>
              <IconButton size="small" onClick={openAdd} sx={{ ml: 2 }}>
                <AddIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
        </Box>

        {loaded && servers.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            {t("mcpServers.empty")}
          </Typography>
        ) : (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
            {servers.map((server) => (
              <ServerRow
                key={server.name}
                server={server}
                canManage={canManage}
                busy={saving}
                onConnect={(value) => {
                  setConnectTarget(value);
                  setCredential("");
                }}
                onAuthorize={(value) => {
                  setRedirectChoice("browser");
                  setAuthorizeTarget(value);
                }}
                onDisconnect={setDisconnectTarget}
                onEdit={openEdit}
                onDelete={setDeleteTarget}
                onToggleEnabled={toggleEnabled}
              />
            ))}
          </Box>
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

      {/* Per-user connect dialog (user-token servers). */}
      <Dialog
        open={!!connectTarget}
        onClose={() => setConnectTarget(null)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle sx={{ fontSize: "0.875rem", fontWeight: 600 }}>
          {connectTarget
            ? t("mcp.connectTitle", { server: connectTarget.name })
            : ""}
        </DialogTitle>
        <DialogContent>
          <Typography variant="caption" color="text.secondary">
            {t("mcp.connectHelp")}
          </Typography>
          <TextField
            autoFocus
            fullWidth
            size="small"
            type="password"
            label={t("mcp.authorizationLabel")}
            placeholder={t("mcp.authorizationPlaceholder")}
            value={credential}
            onChange={(event) => setCredential(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submitConnect();
              }
            }}
            sx={{ mt: 1.5 }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConnectTarget(null)}>
            {t("common.cancel")}
          </Button>
          <Button
            variant="contained"
            disabled={!credential.trim() || connectSaving}
            onClick={submitConnect}
          >
            {connectSaving ? t("common.saving") : t("mcp.connect")}
          </Button>
        </DialogActions>
      </Dialog>

      {/* OAuth authorize dialog (oauth servers). */}
      <Dialog
        open={!!authorizeTarget}
        onClose={() => setAuthorizeTarget(null)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle sx={{ fontSize: "0.875rem", fontWeight: 600 }}>
          {authorizeTarget
            ? t("mcp.authorizeTitle", { server: authorizeTarget.name })
            : ""}
        </DialogTitle>
        <DialogContent>
          <Typography variant="caption" color="text.secondary">
            {t("mcp.redirectHelp")}
          </Typography>
          <FormControl sx={{ mt: 1, display: "block" }}>
            <RadioGroup
              value={redirectChoice}
              onChange={(event) =>
                setRedirectChoice(event.target.value as RedirectChoice)
              }
            >
              <FormControlLabel
                value="browser"
                control={<Radio size="small" />}
                label={
                  <Box>
                    <Typography variant="body2">
                      {t("mcp.redirectBrowser")}
                    </Typography>
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{ wordBreak: "break-all" }}
                    >
                      {browserRedirectUri}
                    </Typography>
                  </Box>
                }
              />
              <FormControlLabel
                value="node"
                control={<Radio size="small" />}
                label={
                  <Box>
                    <Typography variant="body2">
                      {t("mcp.redirectNode")}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {t("mcp.redirectNodeHelp")}
                    </Typography>
                  </Box>
                }
              />
            </RadioGroup>
          </FormControl>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAuthorizeTarget(null)}>
            {t("common.cancel")}
          </Button>
          <Button variant="contained" onClick={confirmAuthorize}>
            {t("mcp.authorize")}
          </Button>
        </DialogActions>
      </Dialog>

      <ConfirmDialog
        open={!!disconnectTarget}
        title={t("mcp.disconnectTitle")}
        message={
          disconnectTarget
            ? t("mcp.disconnectMessage", { server: disconnectTarget.name })
            : ""
        }
        confirmLabel={t("mcp.disconnect")}
        onConfirm={confirmDisconnect}
        onClose={() => setDisconnectTarget(null)}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        title={t("mcpServers.deleteTitle")}
        message={
          deleteTarget
            ? t("mcpServers.deleteMessage", { server: deleteTarget.name })
            : ""
        }
        confirmLabel={t("mcpServers.delete")}
        onConfirm={confirmDelete}
        onClose={() => setDeleteTarget(null)}
      />
    </Box>
  );
}

/** A single server row: metadata + chips on the left, actions on the right. */
function ServerRow({
  server,
  canManage,
  busy,
  onConnect,
  onAuthorize,
  onDisconnect,
  onEdit,
  onDelete,
  onToggleEnabled,
}: {
  server: MCPServerListItem;
  canManage: boolean;
  busy: boolean;
  onConnect: (server: MCPServerListItem) => void;
  onAuthorize: (server: MCPServerListItem) => void;
  onDisconnect: (server: MCPServerListItem) => void;
  onEdit: (server: MCPServerListItem) => void;
  onDelete: (server: MCPServerListItem) => void;
  onToggleEnabled: (name: string) => void;
}) {
  const { t } = useTranslation();
  const action = serverAction(server);
  const summary = server.transport === "stdio" ? server.command : server.url;
  const lastConnected = server.lastConnectedAt
    ? dayjs(server.lastConnectedAt)
    : null;

  return (
    <Paper variant="outlined" sx={{ p: 2 }}>
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: { xs: 1, sm: 2 },
        }}
      >
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              gap: 1,
              flexWrap: "wrap",
            }}
          >
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {server.name}
            </Typography>
            <Chip
              label={t(`mcpServers.transport.${server.transport}`)}
              size="small"
              variant="outlined"
            />
            {server.transport === "http" && (
              <Chip
                label={t(`mcp.authMode.${server.authMode}`)}
                size="small"
                variant="outlined"
              />
            )}
            {server.requiresConnection && (
              <Chip
                label={t(`mcp.status.${server.status ?? "disconnected"}`)}
                size="small"
                color={statusColor(server.status)}
              />
            )}
            {!server.enabled && (
              <Chip label={t("mcpServers.disabled")} size="small" />
            )}
          </Box>
          {summary && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                display: "block",
                wordBreak: "break-all",
                fontFamily:
                  server.transport === "stdio" ? "monospace" : undefined,
              }}
            >
              {summary}
            </Typography>
          )}
          {server.requiresConnection && !server.connectionId && (
            <Typography variant="caption" color="text.secondary">
              {t("mcp.noConnectionRequired")}
            </Typography>
          )}
          {lastConnected && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: "block" }}
            >
              {t("mcp.lastConnected")}{" "}
              <Tooltip
                title={lastConnected.format("YYYY-MM-DD HH:mm:ss")}
                arrow
              >
                <span>{lastConnected.fromNow()}</span>
              </Tooltip>
            </Typography>
          )}
          {server.lastError && (
            <Typography
              variant="caption"
              color="error.main"
              sx={{ display: "block" }}
            >
              {server.lastError}
            </Typography>
          )}
        </Box>

        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: { xs: 0.5, sm: 1 },
            flexShrink: 0,
          }}
        >
          {action === "connect" && (
            <Tooltip title={t("mcp.connect")}>
              <IconButton size="small" onClick={() => onConnect(server)}>
                <LinkIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
          {action === "authorize" && (
            <Tooltip title={t("mcp.authorize")}>
              <IconButton size="small" onClick={() => onAuthorize(server)}>
                <KeyOutlinedIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
          {action === "reauthorize" && (
            <Tooltip title={t("mcp.reauthorize")}>
              <IconButton size="small" onClick={() => onAuthorize(server)}>
                <RefreshIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
          {(action === "connected" || action === "reauthorize") && (
            <Tooltip title={t("mcp.disconnect")}>
              <IconButton
                size="small"
                color="error"
                onClick={() => onDisconnect(server)}
              >
                <LinkOffIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}

          {canManage && (
            <>
              <Tooltip
                title={
                  server.enabled
                    ? t("mcpServers.disable")
                    : t("mcpServers.enable")
                }
              >
                <Switch
                  size="small"
                  checked={server.enabled}
                  disabled={busy}
                  onChange={() => onToggleEnabled(server.name)}
                />
              </Tooltip>
              <Tooltip title={t("common.edit")}>
                <IconButton size="small" onClick={() => onEdit(server)}>
                  <EditOutlinedIcon fontSize="small" />
                </IconButton>
              </Tooltip>
              <Tooltip title={t("common.delete")}>
                <IconButton
                  size="small"
                  color="error"
                  onClick={() => onDelete(server)}
                >
                  <DeleteOutlineIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            </>
          )}
        </Box>
      </Box>
    </Paper>
  );
}

/** The admin add/edit form, rendered in a dialog. */
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
        {edit?.originalName == null
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
