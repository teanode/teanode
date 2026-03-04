import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import type {
  Job,
  JobCreateParams,
  JobUpdateParams,
  ModelInfo,
  AgentInfo,
} from "../types";
import JobForm from "./JobForm";
import ConfirmDialog from "./ConfirmDialog";

function relativeTime(
  ms: number,
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  const diff = Date.now() - ms;
  if (diff < 60_000) return t("jobs.justNow");
  if (diff < 3_600_000)
    return t("jobs.minutesAgo", { count: Math.floor(diff / 60_000) });
  if (diff < 86_400_000)
    return t("jobs.hoursAgo", { count: Math.floor(diff / 3_600_000) });
  return t("jobs.daysAgo", { count: Math.floor(diff / 86_400_000) });
}

interface JobAreaProps {
  job: Job | null;
  creating: boolean;
  models: ModelInfo[];
  agents: AgentInfo[];
  onLoad: () => void;
  onCreate: (params: JobCreateParams) => Promise<Job>;
  onUpdate: (params: JobUpdateParams) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
  onTrigger: (id: string) => Promise<void>;
  onCancelCreate: () => void;
  onViewAgentConversation: (agentId: string, conversationId?: string) => void;
}

export default function JobArea({
  job,
  creating,
  models,
  agents,
  onLoad,
  onCreate,
  onDelete,
  onUpdate,
  onTrigger,
  onCancelCreate,
  onViewAgentConversation,
}: JobAreaProps) {
  const { t } = useTranslation();
  const [deleteConfirm, setDeleteConfirm] = useState(false);

  useEffect(() => {
    onLoad();
  }, [onLoad]);

  const fallbackAgentId = agents.length > 0 ? agents[0].id : "main";

  if (creating) {
    return (
      <Box sx={{ flex: 1, overflowY: "auto" }}>
        <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
          <JobForm
            models={models}
            agents={agents}
            onSave={(data) => {
              const params: JobCreateParams = {
                name: data.name,
                schedule: data.schedule,
                prompt: data.prompt,
              };
              if (data.model) params.providerModelName = data.model;
              if (data.agentId) params.agentId = data.agentId;
              onCreate(params).catch(() => {});
            }}
            onCancel={onCancelCreate}
          />
        </Container>
      </Box>
    );
  }

  if (!job) {
    return (
      <Box
        sx={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Typography variant="body2" color="text.secondary">
          {t("jobs.selectOrCreate")}
        </Typography>
      </Box>
    );
  }

  const effectiveAgentId = job.agentId || fallbackAgentId;

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          <JobForm
            key={job.id}
            initial={job}
            models={models}
            agents={agents}
            onSave={(data) => {
              const params: JobUpdateParams = { id: job.id };
              if (data.name !== job.name) params.name = data.name;
              if (data.schedule !== job.schedule)
                params.schedule = data.schedule;
              if (data.prompt !== job.prompt) params.prompt = data.prompt;
              if (data.model !== (job.providerModelName || ""))
                params.providerModelName = data.model;
              if (data.agentId !== (job.agentId || ""))
                params.agentId = data.agentId;
              onUpdate(params).catch(() => {});
            }}
          />

          {/* Actions */}
          <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            {!job.oneShot && (
              <Typography
                variant="body2"
                color="primary"
                sx={{
                  cursor: "pointer",
                  "&:hover": { textDecoration: "underline" },
                }}
                onClick={() =>
                  onTrigger(job.id).then(() =>
                    onViewAgentConversation(
                      effectiveAgentId,
                      job.conversationId || undefined,
                    ),
                  )
                }
              >
                {t("jobs.runNow")}
              </Typography>
            )}
            <Typography
              variant="body2"
              color="primary"
              sx={{
                cursor: "pointer",
                "&:hover": { textDecoration: "underline" },
              }}
              onClick={() =>
                onViewAgentConversation(
                  effectiveAgentId,
                  job.conversationId || undefined,
                )
              }
            >
              {t("jobs.viewHistory")}
            </Typography>
            {!job.oneShot && (
              <Typography
                variant="body2"
                color="primary"
                sx={{
                  cursor: "pointer",
                  "&:hover": { textDecoration: "underline" },
                }}
                onClick={() => onUpdate({ id: job.id, enabled: !job.enabled })}
              >
                {job.enabled ? t("jobs.disableJob") : t("jobs.enableJob")}
              </Typography>
            )}
            <Typography
              variant="body2"
              color="primary"
              sx={{
                cursor: "pointer",
                "&:hover": { textDecoration: "underline" },
              }}
              onClick={() => setDeleteConfirm(true)}
            >
              {t("common.delete")}
            </Typography>
          </Box>

          <ConfirmDialog
            open={deleteConfirm}
            title={t("common.delete")}
            message={t("jobs.deleteConfirm", { name: job.name })}
            confirmLabel={t("common.delete")}
            onConfirm={() => onDelete(job.id)}
            onClose={() => setDeleteConfirm(false)}
          />

          {/* Last run info */}
          <Box sx={{ borderTop: 1, borderColor: "divider", pt: 1.5 }}>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: "block", mb: 1 }}
            >
              {t("jobs.lastRun")}
            </Typography>
            {job.lastRun ? (
              <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
                <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                  <Typography variant="body2">
                    {relativeTime(job.lastRun, t)}
                  </Typography>
                  <Chip
                    label={job.lastStatus}
                    size="small"
                    color={job.lastStatus === "success" ? "success" : "error"}
                    sx={{ height: 20, fontSize: "10px" }}
                  />
                </Box>
                {job.lastError && (
                  <Typography variant="caption" color="error.main">
                    {job.lastError}
                  </Typography>
                )}
              </Box>
            ) : (
              <Typography variant="body2" color="text.secondary">
                {t("jobs.neverRun")}
              </Typography>
            )}
          </Box>
        </Box>
      </Container>
    </Box>
  );
}
