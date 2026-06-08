import React, { useCallback, useEffect, useState } from "react";
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
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemText from "@mui/material/ListItemText";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAlert } from "../../components/AlertProvider";
import { useAppContext } from "../../context";
import type {
  MCPConnectionStatus,
  MCPServerAuthMode,
  MCPServerListItem,
} from "../../types";
import {
  authorizeMcpConnection,
  createMcpConnection,
  deleteMcpConnection,
  listMcpServers,
  parseOAuthCallback,
  serverAction,
} from "./connections.helpers";

dayjs.extend(relativeTime);

type ChipColor =
  | "default"
  | "success"
  | "warning"
  | "error"
  | "info"
  | "primary";

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

function authModeLabel(
  t: (key: string) => string,
  authMode: MCPServerAuthMode,
): string {
  return t(`mcp.authMode.${authMode}`);
}

function ServerRow({
  server,
  onConnect,
  onAuthorize,
  onDisconnect,
}: {
  server: MCPServerListItem;
  onConnect: (server: MCPServerListItem) => void;
  onAuthorize: (server: MCPServerListItem) => void;
  onDisconnect: (server: MCPServerListItem) => void;
}) {
  const { t } = useTranslation();
  const action = serverAction(server);
  const lastConnected = server.lastConnectedAt
    ? dayjs(server.lastConnectedAt)
    : null;

  const actionButton = (() => {
    switch (action) {
      case "connect":
        return (
          <Button
            size="small"
            variant="outlined"
            onClick={() => onConnect(server)}
          >
            {t("mcp.connect")}
          </Button>
        );
      case "authorize":
        return (
          <Button
            size="small"
            variant="outlined"
            onClick={() => onAuthorize(server)}
          >
            {t("mcp.authorize")}
          </Button>
        );
      case "reauthorize":
        return (
          <Box sx={{ display: "flex", gap: 0.5 }}>
            <Button
              size="small"
              variant="outlined"
              onClick={() => onAuthorize(server)}
            >
              {t("mcp.reauthorize")}
            </Button>
            <Button
              size="small"
              color="error"
              onClick={() => onDisconnect(server)}
            >
              {t("mcp.disconnect")}
            </Button>
          </Box>
        );
      case "connected":
        return (
          <Button
            size="small"
            color="error"
            onClick={() => onDisconnect(server)}
          >
            {t("mcp.disconnect")}
          </Button>
        );
      default:
        return null;
    }
  })();

  return (
    <ListItem
      disableGutters
      alignItems="flex-start"
      secondaryAction={actionButton}
      sx={{ pr: 12 }}
    >
      <ListItemText
        primary={
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {server.name}
            </Typography>
            <Chip
              label={authModeLabel(t, server.authMode)}
              size="small"
              variant="outlined"
            />
            {server.requiresConnection && (
              <Chip
                label={t(`mcp.status.${server.status ?? "disconnected"}`)}
                size="small"
                color={statusColor(server.status)}
              />
            )}
            {!server.enabled && (
              <Chip label={t("mcp.disabled")} size="small" variant="outlined" />
            )}
          </Box>
        }
        secondary={
          <>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: "block", wordBreak: "break-all" }}
            >
              {server.url}
            </Typography>
            {!server.requiresConnection && (
              <Typography variant="caption" color="text.secondary">
                {t("mcp.noConnectionRequired")}
              </Typography>
            )}
            {lastConnected && (
              <Typography variant="caption" color="text.secondary">
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
          </>
        }
      />
    </ListItem>
  );
}

export default function SettingsConnectionsPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const [servers, setServers] = useState<MCPServerListItem[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [connectTarget, setConnectTarget] = useState<MCPServerListItem | null>(
    null,
  );
  const [credential, setCredential] = useState("");
  const [saving, setSaving] = useState(false);
  const [disconnectTarget, setDisconnectTarget] =
    useState<MCPServerListItem | null>(null);

  const loadServers = useCallback(() => {
    listMcpServers(backend)
      .then((items) => {
        setServers(items);
        setLoaded(true);
      })
      .catch((err: unknown) =>
        showAlert(
          err instanceof Error ? err.message : t("mcp.loadFailed"),
          "error",
        ),
      );
  }, [backend, showAlert, t]);

  useEffect(() => {
    if (backend.connected) {
      loadServers();
    }
  }, [backend.connected, loadServers]);

  // Land the OAuth callback: the backend redirects the browser back here with
  // the outcome in the query string. Surface it, then strip the markers so a
  // refresh does not replay the alert.
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

  const openConnect = useCallback((server: MCPServerListItem) => {
    setConnectTarget(server);
    setCredential("");
  }, []);

  const submitConnect = useCallback(async () => {
    if (!connectTarget) return;
    const authorization = credential.trim();
    if (!authorization) return;
    setSaving(true);
    try {
      await createMcpConnection(backend, connectTarget.name, authorization);
      showAlert(t("mcp.connected", { server: connectTarget.name }));
      setConnectTarget(null);
      setCredential("");
      loadServers();
    } catch (err) {
      showAlert(
        err instanceof Error ? err.message : t("mcp.connectFailed"),
        "error",
      );
    } finally {
      setSaving(false);
    }
  }, [backend, connectTarget, credential, loadServers, showAlert, t]);

  const startAuthorize = useCallback(
    async (server: MCPServerListItem) => {
      try {
        const authorizationUrl = await authorizeMcpConnection(
          backend,
          server.name,
        );
        // Full-page navigation so the provider can redirect back to the
        // callback with the user's session cookie attached.
        window.location.href = authorizationUrl;
      } catch (err) {
        showAlert(
          err instanceof Error ? err.message : t("mcp.authorizeStartFailed"),
          "error",
        );
      }
    },
    [backend, showAlert, t],
  );

  const confirmDisconnect = useCallback(async () => {
    if (!disconnectTarget?.connectionId) {
      setDisconnectTarget(null);
      return;
    }
    try {
      await deleteMcpConnection(backend, disconnectTarget.connectionId);
      showAlert(t("mcp.disconnected", { server: disconnectTarget.name }));
      setDisconnectTarget(null);
      loadServers();
    } catch (err) {
      showAlert(
        err instanceof Error ? err.message : t("mcp.disconnectFailed"),
        "error",
      );
    }
  }, [backend, disconnectTarget, loadServers, showAlert, t]);

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ mb: 3 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
            {t("mcp.title")}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {t("mcp.description")}
          </Typography>
        </Box>

        {loaded && servers.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            {t("mcp.noServers")}
          </Typography>
        ) : (
          <List disablePadding>
            {servers.map((server) => (
              <ServerRow
                key={server.name}
                server={server}
                onConnect={openConnect}
                onAuthorize={startAuthorize}
                onDisconnect={setDisconnectTarget}
              />
            ))}
          </List>
        )}
      </Container>

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
            disabled={!credential.trim() || saving}
            onClick={submitConnect}
          >
            {saving ? t("common.saving") : t("mcp.connect")}
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
    </Box>
  );
}
