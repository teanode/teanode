import React, { useEffect, useMemo, useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import HistoryIcon from "@mui/icons-material/History";
import ToggleOnIcon from "@mui/icons-material/ToggleOn";
import ToggleOffIcon from "@mui/icons-material/ToggleOff";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import SaveOutlinedIcon from "@mui/icons-material/SaveOutlined";
import AddCircleOutlineIcon from "@mui/icons-material/AddCircleOutline";
import ReceiptLongIcon from "@mui/icons-material/ReceiptLong";
import RefreshIcon from "@mui/icons-material/Refresh";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAlert } from "../../components/AlertProvider";
import JobForm, { type JobFormHandle } from "../../components/JobForm";
import { useAppContext } from "../../context";
import type {
  Job,
  JobCreateParams,
  JobRun,
  JobUpdateParams,
} from "../../types";

function sortJobs(jobs: Job[]): Job[] {
  return [...jobs].sort((a, b) => {
    const aTs = a.lastRun || a.createdAt || 0;
    const bTs = b.lastRun || b.createdAt || 0;
    return bTs - aTs;
  });
}

function formatJobSchedule(
  job: Job,
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  if (job.oneShot) {
    if (job.runAt)
      return t("jobs.oneShotAt", {
        time: new Date(job.runAt).toLocaleString(),
      });
    return t("jobs.oneShot");
  }
  return t("jobs.recurringSchedule", { schedule: job.schedule });
}

function formatRunTime(timestamp?: number): string {
  if (!timestamp) return "—";
  return new Date(timestamp).toLocaleString();
}

function formatRunDuration(run: JobRun): string {
  if (typeof run.durationMilliseconds === "number") {
    if (run.durationMilliseconds < 1000) {
      return `${run.durationMilliseconds}ms`;
    }
    return `${(run.durationMilliseconds / 1000).toFixed(1)}s`;
  }
  if (run.startedAt && run.completedAt) {
    const durationMilliseconds = Math.max(0, run.completedAt - run.startedAt);
    if (durationMilliseconds < 1000) return `${durationMilliseconds}ms`;
    return `${(durationMilliseconds / 1000).toFixed(1)}s`;
  }
  return "—";
}

function runStatusColor(
  status?: JobRun["status"],
): "default" | "success" | "error" | "warning" {
  if (status === "success") return "success";
  if (status === "error") return "error";
  if (status === "running") return "warning";
  return "default";
}

function runTriggerLabel(
  trigger: JobRun["trigger"],
  t: (key: string) => string,
): string {
  if (trigger === "manual") return t("jobs.triggerManual");
  if (trigger === "scheduled") return t("jobs.triggerScheduled");
  if (trigger === "webhook") return t("jobs.triggerWebhook");
  return "—";
}

export default function SettingsJobsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const [pendingDelete, setPendingDelete] = useState<Job | null>(null);
  const [createFormKey, setCreateFormKey] = useState(0);
  const [oneShotMessageByJob, setOneShotMessageByJob] = useState<
    Record<string, string>
  >({});
  const [oneShotModelByJob, setOneShotModelByJob] = useState<
    Record<string, string>
  >({});
  const [oneShotAgentByJob, setOneShotAgentByJob] = useState<
    Record<string, string>
  >({});
  const [periodicDirtyByJob, setPeriodicDirtyByJob] = useState<
    Record<string, boolean>
  >({});
  const [expandedRunHistoryByJob, setExpandedRunHistoryByJob] = useState<
    Record<string, boolean>
  >({});
  const [runsByJob, setRunsByJob] = useState<Record<string, JobRun[]>>({});
  const [runsLoadingByJob, setRunsLoadingByJob] = useState<
    Record<string, boolean>
  >({});
  const [createCanSave, setCreateCanSave] = useState(false);
  const periodicFormRefs = React.useRef<Record<string, JobFormHandle | null>>(
    {},
  );
  const createFormRef = React.useRef<JobFormHandle | null>(null);

  useEffect(() => {
    if (backend.connected) backend.loadJobs();
  }, [backend.connected, backend.loadJobs]);

  const jobs = useMemo(() => sortJobs(backend.jobs), [backend.jobs]);
  const fallbackAgentId =
    backend.agents.length > 0 ? backend.agents[0].id : "main";

  const viewConversation = useCallback(
    (job: Job) => {
      const agentId = job.agentId || fallbackAgentId;
      const conversationId =
        job.conversationId ||
        backend.agents.find((candidate) => candidate.id === agentId)
          ?.defaultConversationId;
      if (conversationId) {
        navigate({
          to: "/conversations/$agentId/$conversationId",
          params: { agentId, conversationId },
        });
      } else {
        navigate({ to: "/conversations/$agentId", params: { agentId } });
      }
    },
    [backend.agents, fallbackAgentId, navigate],
  );

  const updateJobFromForm = useCallback(
    (
      job: Job,
      data: {
        name: string;
        schedule: string;
        prompt: string;
        model: string;
        agentId: string;
      },
    ) => {
      const params: JobUpdateParams = { id: job.id };
      if (data.name !== job.name) params.name = data.name;
      if (data.schedule !== job.schedule) params.schedule = data.schedule;
      if (data.prompt !== job.prompt) params.prompt = data.prompt;
      if (data.model !== (job.providerModelName || ""))
        params.providerModelName = data.model;
      if (data.agentId !== (job.agentId || "")) params.agentId = data.agentId;
      backend
        .updateJob(params)
        .then(() => showAlert(t("jobs.jobSaved")))
        .catch((err: unknown) =>
          showAlert(
            err instanceof Error ? err.message : t("jobs.jobSaveFailed"),
            "error",
          ),
        );
    },
    [backend.updateJob, showAlert, t],
  );

  const createJobFromForm = useCallback(
    (data: {
      name: string;
      schedule: string;
      prompt: string;
      model: string;
      agentId: string;
    }) => {
      const params: JobCreateParams = {
        name: data.name,
        schedule: data.schedule,
        prompt: data.prompt,
      };
      if (data.model) params.providerModelName = data.model;
      if (data.agentId) params.agentId = data.agentId;
      backend
        .createJob(params)
        .then(() => {
          showAlert(t("jobs.jobCreated"));
          setCreateFormKey((previous) => previous + 1);
          setCreateCanSave(false);
        })
        .catch((err: unknown) =>
          showAlert(
            err instanceof Error ? err.message : t("jobs.jobCreateFailed"),
            "error",
          ),
        );
    },
    [backend.createJob, showAlert, t],
  );

  const loadRunsForJob = useCallback(
    (jobId: string) => {
      setRunsLoadingByJob((previous) => ({ ...previous, [jobId]: true }));
      return backend
        .loadJobRuns(jobId)
        .then((runs) => {
          setRunsByJob((previous) => ({ ...previous, [jobId]: runs }));
        })
        .catch((err: unknown) =>
          showAlert(
            err instanceof Error ? err.message : t("jobs.jobRunsLoadFailed"),
            "error",
          ),
        )
        .finally(() =>
          setRunsLoadingByJob((previous) => ({ ...previous, [jobId]: false })),
        );
    },
    [backend, showAlert, t],
  );

  const toggleRunHistory = useCallback(
    (jobId: string) => {
      setExpandedRunHistoryByJob((previous) => {
        const nextExpanded = !previous[jobId];
        if (nextExpanded && runsByJob[jobId] === undefined) {
          void loadRunsForJob(jobId);
        }
        return { ...previous, [jobId]: nextExpanded };
      });
    },
    [loadRunsForJob, runsByJob],
  );

  const saveOneShotJob = useCallback(
    (job: Job) => {
      const message = (oneShotMessageByJob[job.id] ?? job.prompt).trim();
      const model = (
        oneShotModelByJob[job.id] ??
        (job.providerModelName || "")
      ).trim();
      const agentId = (oneShotAgentByJob[job.id] ?? (job.agentId || "")).trim();
      if (!message) return;

      const params: JobUpdateParams = { id: job.id };
      if (message !== job.prompt) params.prompt = message;
      if (model !== (job.providerModelName || ""))
        params.providerModelName = model;
      if (agentId !== (job.agentId || "")) params.agentId = agentId;
      if (Object.keys(params).length === 1) return;

      backend
        .updateJob(params)
        .then(() => showAlert(t("jobs.jobSaved")))
        .catch((err: unknown) =>
          showAlert(
            err instanceof Error ? err.message : t("jobs.jobSaveFailed"),
            "error",
          ),
        );
    },
    [
      backend.updateJob,
      oneShotMessageByJob,
      oneShotModelByJob,
      oneShotAgentByJob,
      showAlert,
      t,
    ],
  );

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Box sx={{ mb: 1 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t("settings.jobs")}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {t("settings.jobsDescription")}
            </Typography>
          </Box>
          {jobs.map((job) => {
            const currentMessage = oneShotMessageByJob[job.id] ?? job.prompt;
            const currentModel =
              oneShotModelByJob[job.id] ?? (job.providerModelName || "");
            const currentAgentId =
              oneShotAgentByJob[job.id] ?? (job.agentId || "");
            const oneShotDirty =
              job.oneShot &&
              (currentMessage.trim() !== job.prompt ||
                currentModel.trim() !== (job.providerModelName || "") ||
                currentAgentId.trim() !== (job.agentId || ""));
            const periodicDirty = !job.oneShot && !!periodicDirtyByJob[job.id];
            const showSaveActive = job.oneShot ? oneShotDirty : periodicDirty;
            const saveDisabled = job.oneShot
              ? !currentMessage.trim() || !oneShotDirty
              : !periodicDirty;
            const runHistoryExpanded = !!expandedRunHistoryByJob[job.id];
            const jobRuns = runsByJob[job.id] || [];
            const runsLoading = !!runsLoadingByJob[job.id];

            return (
              <Paper key={job.id} variant="outlined" sx={{ p: 2 }}>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: 1.5,
                    mb: 1.5,
                    py: 0.25,
                  }}
                >
                  <Box sx={{ minWidth: 0 }}>
                    <Typography
                      variant="subtitle2"
                      sx={{
                        fontWeight: 600,
                        minWidth: 0,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        color: job.enabled ? "text.primary" : "text.disabled",
                        textDecoration: job.enabled ? "none" : "line-through",
                      }}
                    >
                      {job.oneShot
                        ? t("jobs.onceLabel")
                        : t("jobs.recurringLabel")}
                      : {job.name}
                    </Typography>
                    <Typography
                      variant="caption"
                      color={job.enabled ? "text.secondary" : "text.disabled"}
                    >
                      {formatJobSchedule(job, t)}
                    </Typography>
                  </Box>
                  <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                    <Tooltip title={t("jobs.runNow")}>
                      <IconButton
                        size="small"
                        onClick={() =>
                          backend
                            .triggerJob(job.id)
                            .then(() => {
                              showAlert(t("jobs.jobTriggered"));
                              viewConversation(job);
                            })
                            .catch((err: unknown) =>
                              showAlert(
                                err instanceof Error
                                  ? err.message
                                  : t("jobs.jobTriggerFailed"),
                                "error",
                              ),
                            )
                        }
                      >
                        <PlayArrowIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={t("jobs.viewHistory")}>
                      <IconButton
                        size="small"
                        onClick={() => viewConversation(job)}
                      >
                        <HistoryIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                    <Tooltip
                      title={
                        runHistoryExpanded
                          ? t("jobs.hideRunHistory")
                          : t("jobs.runHistoryTooltip")
                      }
                    >
                      <IconButton
                        size="small"
                        color={runHistoryExpanded ? "primary" : "default"}
                        onClick={() => toggleRunHistory(job.id)}
                      >
                        <ReceiptLongIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                    <Tooltip
                      title={
                        job.enabled ? t("jobs.disableJob") : t("jobs.enableJob")
                      }
                    >
                      <IconButton
                        size="small"
                        onClick={() =>
                          backend
                            .updateJob({ id: job.id, enabled: !job.enabled })
                            .then(() =>
                              showAlert(
                                t(
                                  job.enabled
                                    ? "jobs.jobDisabled"
                                    : "jobs.jobEnabled",
                                ),
                              ),
                            )
                            .catch((err: unknown) =>
                              showAlert(
                                err instanceof Error
                                  ? err.message
                                  : t("jobs.jobToggleFailed"),
                                "error",
                              ),
                            )
                        }
                      >
                        {job.enabled ? (
                          <ToggleOnIcon fontSize="small" />
                        ) : (
                          <ToggleOffIcon fontSize="small" />
                        )}
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={t("common.save")}>
                      <span>
                        <IconButton
                          size="small"
                          color={showSaveActive ? "primary" : "default"}
                          onClick={() => {
                            if (job.oneShot) {
                              saveOneShotJob(job);
                            } else {
                              periodicFormRefs.current[job.id]?.save();
                            }
                          }}
                          disabled={saveDisabled}
                        >
                          <SaveOutlinedIcon fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                    <Tooltip title={t("common.delete")}>
                      <IconButton
                        size="small"
                        color="error"
                        onClick={() => setPendingDelete(job)}
                      >
                        <DeleteOutlineIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Box>
                </Box>
                {job.oneShot ? (
                  <Box
                    sx={{
                      display: "flex",
                      flexDirection: "column",
                      gap: 0.75,
                      pt: 0.5,
                    }}
                  >
                    <TextField
                      label={t("jobs.message")}
                      size="small"
                      fullWidth
                      multiline
                      minRows={2}
                      value={currentMessage}
                      onChange={(event) => {
                        const nextValue = event.target.value;
                        setOneShotMessageByJob((previous) => ({
                          ...previous,
                          [job.id]: nextValue,
                        }));
                      }}
                    />
                    {backend.models.length > 0 ? (
                      <TextField
                        select
                        label={t("jobs.modelOptional")}
                        size="small"
                        fullWidth
                        value={currentModel}
                        onChange={(event) =>
                          setOneShotModelByJob((previous) => ({
                            ...previous,
                            [job.id]: event.target.value,
                          }))
                        }
                      >
                        <MenuItem value="">{t("common.default")}</MenuItem>
                        {backend.models.map((modelInfo) => {
                          const qualified = `${modelInfo.providerName}:${modelInfo.id}`;
                          return (
                            <MenuItem key={qualified} value={qualified}>
                              {qualified}
                            </MenuItem>
                          );
                        })}
                      </TextField>
                    ) : (
                      <TextField
                        label={t("jobs.modelOptional")}
                        size="small"
                        fullWidth
                        value={currentModel}
                        onChange={(event) =>
                          setOneShotModelByJob((previous) => ({
                            ...previous,
                            [job.id]: event.target.value,
                          }))
                        }
                        placeholder={t("jobs.modelPlaceholder")}
                      />
                    )}
                    {backend.agents.length > 1 && (
                      <TextField
                        select
                        label={t("jobs.agentOptional")}
                        size="small"
                        fullWidth
                        value={currentAgentId}
                        onChange={(event) =>
                          setOneShotAgentByJob((previous) => ({
                            ...previous,
                            [job.id]: event.target.value,
                          }))
                        }
                      >
                        <MenuItem value="">{t("jobs.defaultAgent")}</MenuItem>
                        {backend.agents.map((agent) => (
                          <MenuItem key={agent.id} value={agent.id}>
                            {agent.name || agent.id}
                          </MenuItem>
                        ))}
                      </TextField>
                    )}
                  </Box>
                ) : (
                  <JobForm
                    ref={(instance) => {
                      periodicFormRefs.current[job.id] = instance;
                    }}
                    flat
                    showActions={false}
                    initial={job}
                    models={backend.models}
                    agents={backend.agents}
                    onSave={(data) => updateJobFromForm(job, data)}
                    onDirtyChange={(dirty) => {
                      setPeriodicDirtyByJob((previous) => {
                        if (previous[job.id] === dirty) return previous;
                        return { ...previous, [job.id]: dirty };
                      });
                    }}
                  />
                )}
                {runHistoryExpanded && (
                  <Box
                    sx={{
                      mt: 1.5,
                      pt: 1.5,
                      borderTop: 1,
                      borderColor: "divider",
                      display: "flex",
                      flexDirection: "column",
                      gap: 1,
                    }}
                  >
                    <Box
                      sx={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        gap: 1,
                      }}
                    >
                      <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
                        {t("jobs.runHistory")}
                      </Typography>
                      <Tooltip title={t("jobs.refreshRunHistory")}>
                        <span>
                          <IconButton
                            size="small"
                            disabled={runsLoading}
                            onClick={() => loadRunsForJob(job.id)}
                          >
                            <RefreshIcon fontSize="small" />
                          </IconButton>
                        </span>
                      </Tooltip>
                    </Box>
                    {runsLoading && jobRuns.length === 0 ? (
                      <Box
                        sx={{
                          display: "flex",
                          alignItems: "center",
                          gap: 1,
                          color: "text.secondary",
                        }}
                      >
                        <CircularProgress size={16} />
                        <Typography variant="caption">
                          {t("common.loading")}
                        </Typography>
                      </Box>
                    ) : jobRuns.length === 0 ? (
                      <Typography variant="body2" color="text.secondary">
                        {t("jobs.noJobRuns")}
                      </Typography>
                    ) : (
                      <Box
                        sx={{
                          display: "flex",
                          flexDirection: "column",
                          gap: 1,
                        }}
                      >
                        {jobRuns.map((run) => (
                          <Box
                            key={run.id}
                            sx={{
                              p: 1,
                              border: 1,
                              borderColor: "divider",
                              borderRadius: 1,
                              display: "flex",
                              flexDirection: "column",
                              gap: 0.5,
                            }}
                          >
                            <Box
                              sx={{
                                display: "flex",
                                alignItems: "center",
                                justifyContent: "space-between",
                                gap: 1,
                                flexWrap: "wrap",
                              }}
                            >
                              <Box
                                sx={{
                                  display: "flex",
                                  alignItems: "center",
                                  gap: 0.75,
                                  flexWrap: "wrap",
                                }}
                              >
                                <Chip
                                  label={run.status || "unknown"}
                                  size="small"
                                  color={runStatusColor(run.status)}
                                  sx={{ height: 22 }}
                                />
                                <Chip
                                  label={runTriggerLabel(run.trigger, t)}
                                  size="small"
                                  variant="outlined"
                                  sx={{ height: 22 }}
                                />
                              </Box>
                              <Typography
                                variant="caption"
                                color="text.secondary"
                              >
                                {formatRunTime(run.startedAt)}
                              </Typography>
                            </Box>
                            <Typography
                              variant="caption"
                              color="text.secondary"
                            >
                              {t("jobs.runDuration")}: {formatRunDuration(run)}
                              {run.runId
                                ? ` · ${t("jobs.runId")}: ${run.runId}`
                                : ""}
                            </Typography>
                            {(run.requestMethod ||
                              run.requestPath ||
                              run.remoteAddress) && (
                              <Typography
                                variant="caption"
                                color="text.secondary"
                              >
                                {t("jobs.runRequest")}:{" "}
                                {[
                                  run.requestMethod,
                                  run.requestPath,
                                  run.remoteAddress,
                                ]
                                  .filter(Boolean)
                                  .join(" ")}
                              </Typography>
                            )}
                            {run.error && (
                              <Typography variant="caption" color="error.main">
                                {t("jobs.runError")}: {run.error}
                              </Typography>
                            )}
                          </Box>
                        ))}
                      </Box>
                    )}
                  </Box>
                )}
              </Paper>
            );
          })}

          <Paper variant="outlined" sx={{ p: 2 }}>
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                mb: 1,
              }}
            >
              <Box>
                <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
                  {t("jobs.newJob")}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {t("jobs.createRecurringHint")}
                </Typography>
              </Box>
              <Tooltip title={t("common.create")}>
                <span>
                  <IconButton
                    size="small"
                    color={createCanSave ? "primary" : "default"}
                    disabled={!createCanSave}
                    onClick={() => createFormRef.current?.save()}
                  >
                    <AddCircleOutlineIcon fontSize="small" />
                  </IconButton>
                </span>
              </Tooltip>
            </Box>
            <JobForm
              key={createFormKey}
              ref={createFormRef}
              flat
              showActions={false}
              models={backend.models}
              agents={backend.agents}
              onSave={createJobFromForm}
              onCanSaveChange={setCreateCanSave}
            />
          </Paper>
        </Box>
      </Container>

      <ConfirmDialog
        open={!!pendingDelete}
        title={t("common.delete")}
        message={t("jobs.deleteConfirm", { name: pendingDelete?.name })}
        confirmLabel={t("common.delete")}
        onConfirm={() => {
          if (!pendingDelete) return;
          backend
            .deleteJob(pendingDelete.id)
            .then(() => {
              showAlert(t("jobs.jobDeleted"));
              setPendingDelete(null);
            })
            .catch((err: unknown) =>
              showAlert(
                err instanceof Error ? err.message : t("jobs.jobDeleteFailed"),
                "error",
              ),
            );
        }}
        onClose={() => setPendingDelete(null)}
      />
    </Box>
  );
}
