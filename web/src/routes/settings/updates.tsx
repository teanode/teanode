import React, { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import { useAppContext } from "../../context";
import { useAlert } from "../../components/AlertProvider";
import ConfirmDialog from "../../components/ConfirmDialog";
import type {
  UpdateStatus,
  UpdateStatusResult,
  UpdateCheckResult,
  UpdateApplyResult,
} from "../../types";

function formatTimeAgo(
  t: (key: string, opts?: Record<string, unknown>) => string,
  dateString: string,
): string {
  const then = new Date(dateString).getTime();
  const now = Date.now();
  const diffMinutes = Math.floor((now - then) / 60_000);
  if (diffMinutes < 1) return t("updates.justNow");
  if (diffMinutes < 60) return t("updates.minutesAgo", { count: diffMinutes });
  const diffHours = Math.floor(diffMinutes / 60);
  if (diffHours < 24) return t("updates.hoursAgo", { count: diffHours });
  const diffDays = Math.floor(diffHours / 24);
  return t("updates.daysAgo", { count: diffDays });
}

export default function SettingsUpdatesPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();

  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(true);
  const [checking, setChecking] = useState(false);
  const [applying, setApplying] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const loadStatus = useCallback(async () => {
    if (!backend.connected || !backend.isAdmin) {
      setLoading(false);
      return;
    }
    try {
      const result = await backend.sendRpc<UpdateStatusResult>(
        "update.status",
        {},
      );
      setEnabled(result.enabled);
      setStatus(result.status ?? null);
      await backend.refreshUpdateStatus();
    } catch (error) {
      console.error("update.status:", error);
    } finally {
      setLoading(false);
    }
  }, [backend]);

  useEffect(() => {
    void loadStatus();
  }, [loadStatus]);

  const handleCheck = useCallback(async () => {
    setChecking(true);
    try {
      const result = await backend.sendRpc<UpdateCheckResult>(
        "update.check",
        {},
      );
      setStatus(result.status);
      await backend.refreshUpdateStatus();
    } catch (error) {
      console.error("update.check:", error);
      showAlert(t("updates.checkFailed"), "error");
    } finally {
      setChecking(false);
    }
  }, [backend, showAlert, t]);

  const handleApply = useCallback(async () => {
    setConfirmOpen(false);
    setApplying(true);
    try {
      await backend.sendRpc<UpdateApplyResult>("update.apply", {});
      await backend.refreshUpdateStatus();
      showAlert(t("updates.applySuccess"), "success");
    } catch (error) {
      console.error("update.apply:", error);
      const message =
        error instanceof Error ? error.message : t("updates.applyFailed");
      showAlert(message, "error");
    } finally {
      setApplying(false);
    }
  }, [backend, showAlert, t]);

  if (!backend.isAdmin) {
    return (
      <Box sx={{ flex: 1, display: "flex", alignItems: "center", p: 3 }}>
        <Typography variant="body2" color="text.secondary">
          {t("settings.usersAdminOnly")}
        </Typography>
      </Box>
    );
  }

  if (loading) {
    return (
      <Box
        sx={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <CircularProgress size={24} />
      </Box>
    );
  }

  if (!enabled) {
    return (
      <Box sx={{ flex: 1, overflowY: "auto" }}>
        <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
            {t("updates.title")}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t("updates.updaterDisabled")}
          </Typography>
        </Container>
      </Box>
    );
  }

  const policyLabel =
    status?.policy === "auto"
      ? t("updates.policyAuto")
      : status?.policy === "notify"
        ? t("updates.policyNotify")
        : t("updates.policyDisabled");

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
          {t("updates.title")}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t("updates.description")}
        </Typography>

        {status?.isContainer && (
          <Alert severity="info" sx={{ mb: 2 }}>
            {t("updates.containerWarning")}
          </Alert>
        )}

        {status?.error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {status.error}
          </Alert>
        )}

        <Paper variant="outlined" sx={{ p: 2, mb: 2 }}>
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
              gap: 2,
            }}
          >
            <Box>
              <Typography variant="caption" color="text.secondary">
                {t("updates.currentVersion")}
              </Typography>
              <Typography variant="body2" sx={{ fontWeight: 500 }}>
                {status?.currentVersion ?? "—"}
              </Typography>
            </Box>

            <Box>
              <Typography variant="caption" color="text.secondary">
                {t("updates.latestVersion")}
              </Typography>
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Typography variant="body2" sx={{ fontWeight: 500 }}>
                  {status?.latestVersion || "—"}
                </Typography>
                {status?.updateAvailable ? (
                  <Chip
                    label={t("updates.updateAvailable")}
                    color="warning"
                    size="small"
                    sx={{ height: 20, fontSize: "0.7rem" }}
                  />
                ) : status?.latestVersion ? (
                  <Chip
                    label={t("updates.upToDate")}
                    color="success"
                    size="small"
                    sx={{ height: 20, fontSize: "0.7rem" }}
                  />
                ) : null}
              </Box>
            </Box>

            <Box>
              <Typography variant="caption" color="text.secondary">
                {t("updates.policy")}
              </Typography>
              <Typography variant="body2" sx={{ fontWeight: 500 }}>
                {policyLabel}
              </Typography>
            </Box>

            <Box>
              <Typography variant="caption" color="text.secondary">
                {t("updates.lastChecked")}
              </Typography>
              <Typography variant="body2" sx={{ fontWeight: 500 }}>
                {status?.lastChecked
                  ? formatTimeAgo(t, status.lastChecked)
                  : t("updates.never")}
              </Typography>
            </Box>
          </Box>
        </Paper>

        {status?.updateAvailable && status.available && (
          <Paper variant="outlined" sx={{ p: 2, mb: 2 }}>
            <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
              {t("updates.updateAvailableVersion", {
                version: status.latestVersion,
              })}
            </Typography>
            {status.available.body && (
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ whiteSpace: "pre-wrap", fontSize: "0.8rem" }}
              >
                {status.available.body}
              </Typography>
            )}
          </Paper>
        )}

        <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
          <Button
            variant="outlined"
            size="small"
            disabled={checking}
            onClick={handleCheck}
            startIcon={checking ? <CircularProgress size={14} /> : undefined}
          >
            {checking ? t("updates.checking") : t("updates.checkNow")}
          </Button>

          {status?.updateAvailable && !status.isContainer && (
            <Button
              variant="contained"
              size="small"
              disabled={applying}
              onClick={() => setConfirmOpen(true)}
              startIcon={applying ? <CircularProgress size={14} /> : undefined}
            >
              {applying ? t("updates.applying") : t("updates.applyUpdate")}
            </Button>
          )}
        </Box>

        <ConfirmDialog
          open={confirmOpen}
          title={t("updates.applyConfirmTitle")}
          message={t("updates.applyConfirmMessage")}
          confirmLabel={t("updates.applyUpdate")}
          onConfirm={handleApply}
          onClose={() => setConfirmOpen(false)}
        />
      </Container>
    </Box>
  );
}
