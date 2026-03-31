import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import Container from "@mui/material/Container";
import Chip from "@mui/material/Chip";
import GppMaybeRounded from "@mui/icons-material/GppMaybeRounded";
import type { PendingApproval } from "../types";

interface ApprovalPanelProps {
  approvals: PendingApproval[];
  onResolve: (
    verdicts: { approvalId: string; verdict: string; reason?: string }[],
  ) => Promise<void> | void;
  /** Externally disable all actions (e.g. when the backend is disconnected). */
  disabled?: boolean;
}

/** Try to pretty-print tool arguments; fall back to raw string. */
function formatArguments(raw: string): string {
  try {
    const parsed = JSON.parse(raw);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return raw;
  }
}

export default function ApprovalPanel({
  approvals,
  onResolve,
  disabled: externalDisabled = false,
}: ApprovalPanelProps) {
  const { t } = useTranslation();
  const [submitted, setSubmitted] = useState(false);
  const disabled = submitted || externalDisabled;

  // Reset submitted state when the approvals list changes.
  React.useEffect(() => {
    setSubmitted(false);
  }, [approvals.length]);

  if (approvals.length === 0) return null;

  function safeResolve(
    verdicts: { approvalId: string; verdict: string; reason?: string }[],
  ) {
    setSubmitted(true);
    Promise.resolve(onResolve(verdicts)).catch(() => {
      // Re-enable buttons so the user can retry.
      setSubmitted(false);
    });
  }

  function handleApprove(approval: PendingApproval) {
    if (disabled) return;
    safeResolve([{ approvalId: approval.id, verdict: "approved" }]);
  }

  function handleReject(approval: PendingApproval) {
    if (disabled) return;
    safeResolve([{ approvalId: approval.id, verdict: "rejected" }]);
  }

  function handleApproveAll() {
    if (disabled) return;
    safeResolve(
      approvals.map((a) => ({ approvalId: a.id, verdict: "approved" })),
    );
  }

  return (
    <Container maxWidth="md" sx={{ py: 1.5 }}>
      <Box
        sx={{
          bgcolor: "surface2",
          borderRadius: 1.5,
          border: 1,
          borderColor: "warning.main",
          px: 2,
          py: 1.5,
        }}
      >
        {/* Header */}
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 0.75,
            mb: 1,
          }}
        >
          <GppMaybeRounded sx={{ fontSize: 16, color: "warning.main" }} />
          <Typography
            variant="caption"
            color="warning.main"
            sx={{ fontWeight: 600 }}
          >
            {t("tool.approvalRequired")}
          </Typography>
          {approvals.length > 1 && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ ml: "auto" }}
            >
              {approvals.length} {t("tool.approvalsPending")}
            </Typography>
          )}
        </Box>

        {/* Approval cards */}
        {approvals.map((approval) => (
          <Box
            key={approval.id}
            sx={{
              mb: 1.5,
              p: 1.5,
              bgcolor: "background.default",
              borderRadius: 1,
              border: 1,
              borderColor: "divider",
            }}
          >
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 1,
                mb: 0.5,
              }}
            >
              <Typography variant="body2" sx={{ fontWeight: 600 }}>
                {approval.toolName}
              </Typography>
              {approval.risk && (
                <Chip
                  label={approval.risk}
                  size="small"
                  color={approval.risk === "high" ? "error" : "warning"}
                  variant="outlined"
                  sx={{ height: 20, fontSize: "0.7rem" }}
                />
              )}
            </Box>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {approval.policyReason}
            </Typography>
            {approval.arguments && approval.arguments !== "{}" && (
              <Box
                component="pre"
                sx={{
                  fontSize: "0.75rem",
                  bgcolor: "surface1",
                  p: 1,
                  borderRadius: 0.5,
                  overflow: "auto",
                  maxHeight: 120,
                  mb: 1,
                  whiteSpace: "pre-wrap",
                  wordBreak: "break-word",
                }}
              >
                {formatArguments(approval.arguments)}
              </Box>
            )}
            <Box sx={{ display: "flex", gap: 1 }}>
              <Button
                variant="contained"
                size="small"
                color="success"
                disabled={disabled}
                onClick={() => handleApprove(approval)}
                sx={{ textTransform: "none" }}
              >
                {t("tool.approve")}
              </Button>
              <Button
                variant="outlined"
                size="small"
                color="error"
                disabled={disabled}
                onClick={() => handleReject(approval)}
                sx={{ textTransform: "none" }}
              >
                {t("tool.reject")}
              </Button>
            </Box>
          </Box>
        ))}

        {/* Approve all (when multiple) */}
        {approvals.length > 1 && (
          <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
            <Button
              variant="contained"
              size="small"
              color="success"
              disabled={disabled}
              onClick={handleApproveAll}
              sx={{ textTransform: "none" }}
            >
              {t("tool.approveAll")}
            </Button>
          </Box>
        )}
      </Box>
    </Container>
  );
}
