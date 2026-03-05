import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import IconButton from "@mui/material/IconButton";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemText from "@mui/material/ListItemText";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAppContext } from "../../context";
import type { AuthTokenInfo } from "../../types";

dayjs.extend(relativeTime);

function shortenUserAgent(userAgent?: string): string {
  if (!userAgent) return "";
  if (userAgent.length > 60) return userAgent.slice(0, 57) + "...";
  return userAgent;
}

function TokenItem({
  token,
  copiedTokenId,
  visible,
  onCopy,
  onToggleVisible,
  onDelete,
}: {
  token: AuthTokenInfo;
  copiedTokenId: string | null;
  visible: boolean;
  onCopy: () => void;
  onToggleVisible: () => void;
  onDelete: () => void;
}) {
  const maskedToken = visible
    ? token.token
    : token.token.slice(0, 6) + "··········" + token.token.slice(-4);
  const lastUsed = token.lastUsedAt ? dayjs(token.lastUsedAt) : null;
  const ua = shortenUserAgent(token.userAgent);

  return (
    <ListItem
      disableGutters
      secondaryAction={
        <Box sx={{ display: "flex", gap: 0 }}>
          <IconButton size="small" onClick={onCopy}>
            <ContentCopyIcon
              fontSize="small"
              sx={{
                color:
                  copiedTokenId === token.id ? "primary.main" : undefined,
              }}
            />
          </IconButton>
          <IconButton size="small" onClick={onToggleVisible}>
            {visible ? (
              <VisibilityOffIcon fontSize="small" />
            ) : (
              <VisibilityIcon fontSize="small" />
            )}
          </IconButton>
          <IconButton
            size="small"
            edge="end"
            color="error"
            onClick={onDelete}
          >
            <DeleteOutlineIcon fontSize="small" />
          </IconButton>
        </Box>
      }
    >
      <ListItemText
        primary={
          <Typography
            variant="body2"
            sx={{ fontFamily: "monospace", fontSize: "13px" }}
          >
            {maskedToken}
          </Typography>
        }
        secondary={
          <>
            {token.remoteAddress || "-"}
            {ua ? ` · ${ua}` : ""}
            {lastUsed ? (
              <>
                {" · "}
                <Tooltip
                  title={lastUsed.format("YYYY-MM-DD HH:mm:ss")}
                  arrow
                >
                  <span>{lastUsed.fromNow()}</span>
                </Tooltip>
              </>
            ) : null}
          </>
        }
      />
    </ListItem>
  );
}

export default function SettingsTokensPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const [tokens, setTokens] = useState<AuthTokenInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [copiedTokenId, setCopiedTokenId] = useState<string | null>(null);
  const [visibleByTokenId, setVisibleByTokenId] = useState<
    Record<string, boolean>
  >({});
  const [pendingDelete, setPendingDelete] = useState<AuthTokenInfo | null>(
    null,
  );

  const loadTokens = useCallback(() => {
    backend
      .sendRpc<{ tokens: AuthTokenInfo[] }>("auth.tokens.list", {})
      .then((result) => setTokens(result.tokens || []))
      .catch((err: unknown) =>
        setError(err instanceof Error ? err.message : "Failed to load tokens"),
      );
  }, [backend]);

  useEffect(() => {
    if (backend.connected) {
      loadTokens();
    }
  }, [backend.connected, loadTokens]);

  const sortedTokens = useMemo(() => {
    const items = [...tokens];
    items.sort((left, right) => {
      const leftTime = left.createdAt ? new Date(left.createdAt).getTime() : 0;
      const rightTime = right.createdAt
        ? new Date(right.createdAt).getTime()
        : 0;
      return rightTime - leftTime;
    });
    return items;
  }, [tokens]);

  const createToken = useCallback(async () => {
    setError(null);
    try {
      await backend.sendRpc("auth.tokens.create", {});
      loadTokens();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create token");
    }
  }, [backend, loadTokens]);

  const deleteToken = useCallback(async () => {
    if (!pendingDelete) return;
    setError(null);
    try {
      await backend.sendRpc("auth.tokens.delete", {
        tokenId: pendingDelete.id,
      });
      setPendingDelete(null);
      loadTokens();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete token");
    }
  }, [backend, loadTokens, pendingDelete]);

  const copyToken = useCallback((token: AuthTokenInfo) => {
    navigator.clipboard.writeText(token.token).then(() => {
      setCopiedTokenId(token.id);
      setTimeout(() => {
        setCopiedTokenId((current) => (current === token.id ? null : current));
      }, 1500);
    });
  }, []);

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box>
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
                {t("auth.tokensTitle")}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {t("auth.tokensDescription")}
              </Typography>
            </Box>
            <Tooltip title={t("auth.createToken")} arrow>
              <IconButton size="small" onClick={createToken} sx={{ ml: 2 }}>
                <AddIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Box>

          {sortedTokens.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              {t("auth.noTokens")}
            </Typography>
          ) : (
            <List disablePadding>
              {sortedTokens.map((token) => (
                <TokenItem
                  key={token.id}
                  token={token}
                  copiedTokenId={copiedTokenId}
                  visible={!!visibleByTokenId[token.id]}
                  onCopy={() => copyToken(token)}
                  onToggleVisible={() =>
                    setVisibleByTokenId((previous) => ({
                      ...previous,
                      [token.id]: !previous[token.id],
                    }))
                  }
                  onDelete={() => setPendingDelete(token)}
                />
              ))}
            </List>
          )}

          {error && (
            <Typography variant="caption" color="error" sx={{ mt: 1 }}>
              {error}
            </Typography>
          )}
        </Box>
      </Container>

      <ConfirmDialog
        open={!!pendingDelete}
        title={t("auth.deleteTokenTitle")}
        message={t("auth.deleteTokenMessage")}
        confirmLabel={t("common.delete")}
        onConfirm={deleteToken}
        onClose={() => setPendingDelete(null)}
      />
    </Box>
  );
}
