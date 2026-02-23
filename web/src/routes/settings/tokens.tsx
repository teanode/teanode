import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Container from "@mui/material/Container";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import Paper from "@mui/material/Paper";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAppContext } from "../../context";
import type { AuthTokenInfo } from "../../types";

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
      await backend.sendRpc("auth.tokens.delete", { tokenId: pendingDelete.id });
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

  const formatDate = useCallback((value?: string) => {
    if (!value) return "-";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "-";
    return date.toLocaleString();
  }, []);

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Box sx={{ mb: 1 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t("auth.tokensTitle")}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {t("auth.tokensDescription")}
            </Typography>
          </Box>

          <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
            <Button variant="contained" size="small" onClick={createToken}>
              {t("auth.createToken")}
            </Button>
          </Box>

          {sortedTokens.map((token) => {
            const visible = !!visibleByTokenId[token.id];
            return (
              <Paper key={token.id} variant="outlined" sx={{ p: 2 }}>
                <Box sx={{ display: "flex", flexDirection: "column", gap: 1.25 }}>
                  <TextField
                    label={t("auth.tokensTitle")}
                    type={visible ? "text" : "password"}
                    size="small"
                    value={token.token}
                    fullWidth
                    slotProps={{
                      input: {
                        readOnly: true,
                        sx: { fontFamily: "monospace", fontSize: "13px" },
                        endAdornment: (
                          <InputAdornment position="end">
                            <IconButton
                              size="small"
                              onClick={() => copyToken(token)}
                              edge="end"
                            >
                              <ContentCopyIcon
                                fontSize="small"
                                sx={{
                                  color:
                                    copiedTokenId === token.id
                                      ? "primary.main"
                                      : undefined,
                                }}
                              />
                            </IconButton>
                            <IconButton
                              size="small"
                              edge="end"
                              onClick={() =>
                                setVisibleByTokenId((previous) => ({
                                  ...previous,
                                  [token.id]: !previous[token.id],
                                }))
                              }
                            >
                              {visible ? (
                                <VisibilityOffIcon fontSize="small" />
                              ) : (
                                <VisibilityIcon fontSize="small" />
                              )}
                            </IconButton>
                          </InputAdornment>
                        ),
                      },
                    }}
                  />
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      gap: 1,
                    }}
                  >
                    <Box sx={{ minWidth: 0 }}>
                      <Typography variant="caption" color="text.secondary">
                        {t("auth.tokenCreatedAt")}: {formatDate(token.createdAt)}
                      </Typography>
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{ display: "block" }}
                      >
                        {t("auth.tokenLastUsedAt")}: {formatDate(token.lastUsedAt)}
                      </Typography>
                    </Box>
                    <Tooltip title={t("common.delete")}>
                      <IconButton
                        size="small"
                        onClick={() => setPendingDelete(token)}
                      >
                        <DeleteOutlineIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Box>
                </Box>
              </Paper>
            );
          })}

          {sortedTokens.length === 0 && (
            <Typography variant="caption" color="text.secondary">
              {t("auth.noTokens")}
            </Typography>
          )}

          {error && (
            <Typography variant="caption" color="error">
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
