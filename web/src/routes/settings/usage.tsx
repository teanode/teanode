import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Collapse from "@mui/material/Collapse";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import CircularProgress from "@mui/material/CircularProgress";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import InputAdornment from "@mui/material/InputAdornment";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import Paper from "@mui/material/Paper";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import IconButton from "@mui/material/IconButton";
import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import FilterListIcon from "@mui/icons-material/FilterList";
import SettingsIcon from "@mui/icons-material/Settings";
import { useTheme } from "@mui/material/styles";
import ReactECharts from "echarts-for-react";
import dayjs from "dayjs";
import { useAppContext } from "../../context";
import type { UserInfo } from "../../types";

type IntervalType = "hour" | "day" | "week" | "month" | "year";
type MetricType =
  | "totalTokens"
  | "completionTokens"
  | "promptTokens"
  | "cacheCreationTokens"
  | "cacheReadTokens"
  | "requestCount"
  | "estimatedCost";

const METRIC_KEYS: MetricType[] = [
  "totalTokens",
  "completionTokens",
  "promptTokens",
  "cacheCreationTokens",
  "cacheReadTokens",
  "requestCount",
  "estimatedCost",
];

const METRIC_I18N: Record<MetricType, string> = {
  totalTokens: "usage.totalTokens",
  completionTokens: "usage.completionTokens",
  promptTokens: "usage.promptTokens",
  cacheCreationTokens: "usage.cacheCreation",
  cacheReadTokens: "usage.cacheRead",
  requestCount: "usage.requests",
  estimatedCost: "usage.estimatedCost",
};

// ── Pricing config (persisted in localStorage) ────────────────────────

interface PricingConfig {
  inputPer1M: number;
  cachedInputPer1M: number;
  outputPer1M: number;
}

const DEFAULT_PRICING: PricingConfig = {
  inputPer1M: 2.5,
  cachedInputPer1M: 0.25,
  outputPer1M: 15.0,
};

const PRICING_STORAGE_KEY = "teanode-usage-pricing";

function loadPricing(): PricingConfig {
  try {
    const raw = localStorage.getItem(PRICING_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      return {
        inputPer1M:
          typeof parsed.inputPer1M === "number"
            ? parsed.inputPer1M
            : DEFAULT_PRICING.inputPer1M,
        cachedInputPer1M:
          typeof parsed.cachedInputPer1M === "number"
            ? parsed.cachedInputPer1M
            : DEFAULT_PRICING.cachedInputPer1M,
        outputPer1M:
          typeof parsed.outputPer1M === "number"
            ? parsed.outputPer1M
            : DEFAULT_PRICING.outputPer1M,
      };
    }
  } catch {
    // ignore
  }
  return { ...DEFAULT_PRICING };
}

function savePricing(config: PricingConfig) {
  localStorage.setItem(PRICING_STORAGE_KEY, JSON.stringify(config));
}

function estimateCost(entry: UsageEntry, pricing: PricingConfig): number {
  const prompt = entry.promptTokens || 0;
  const cached =
    (entry.cacheCreationTokens || 0) + (entry.cacheReadTokens || 0);
  const completion = entry.completionTokens || 0;
  // prompt tokens includes non-cached input
  const nonCachedInput = Math.max(0, prompt - cached);
  return (
    (nonCachedInput / 1_000_000) * pricing.inputPer1M +
    (cached / 1_000_000) * pricing.cachedInputPer1M +
    (completion / 1_000_000) * pricing.outputPer1M
  );
}

function formatCost(value: number): string {
  if (value >= 1) return `$${value.toFixed(2)}`;
  if (value >= 0.01) return `$${value.toFixed(3)}`;
  if (value > 0) return `$${value.toFixed(4)}`;
  return "$0.00";
}

interface UsageEntry {
  userId?: string;
  providerName?: string;
  modelName?: string;
  intervalType?: string;
  startedAt?: string;
  promptTokens?: number;
  completionTokens?: number;
  cacheCreationTokens?: number;
  cacheReadTokens?: number;
  totalTokens?: number;
  requestCount?: number;
}

interface UsageListResult {
  entries: UsageEntry[];
}

// ── Interval window helpers ───────────────────────────────────────────
// The backend returns startedAt in server-local time with a tz offset.
// When server and browser share the same timezone, dayjs parses the offset
// and converts to browser-local automatically.  We normalise to a naive
// local-time string so bucket keys match the generated time slots.

const NAIVE_FMT = "YYYY-MM-DDTHH:mm:ss";

/** Parse a server timestamp (with tz offset) and return a naive local-time key. */
function toLocalKey(dateStr: string): string {
  return dayjs(dateStr).format(NAIVE_FMT);
}

function defaultRange(interval: IntervalType): {
  start: dayjs.Dayjs;
  end: dayjs.Dayjs;
} {
  const now = dayjs();
  switch (interval) {
    case "hour":
      return {
        start: now.subtract(24, "hour").startOf("hour"),
        end: now.endOf("hour"),
      };
    case "day":
      return {
        start: now.subtract(30, "day").startOf("day"),
        end: now.endOf("day"),
      };
    case "week":
      return {
        start: now.subtract(12, "week").startOf("week"),
        end: now.endOf("week"),
      };
    case "month":
      return {
        start: now.subtract(12, "month").startOf("month"),
        end: now.endOf("month"),
      };
    case "year":
      return {
        start: now.subtract(5, "year").startOf("year"),
        end: now.endOf("year"),
      };
  }
}

function shiftRange(
  interval: IntervalType,
  current: { start: dayjs.Dayjs; end: dayjs.Dayjs },
  direction: -1 | 1,
): { start: dayjs.Dayjs; end: dayjs.Dayjs } {
  const unitMap: Record<IntervalType, dayjs.ManipulateType> = {
    hour: "day",
    day: "month",
    week: "month",
    month: "year",
    year: "year",
  };
  const amountMap: Record<IntervalType, number> = {
    hour: 1,
    day: 1,
    week: 3,
    month: 1,
    year: 5,
  };
  const unit = unitMap[interval];
  const amount = amountMap[interval] * direction;
  return {
    start: current.start.add(amount, unit),
    end: current.end.add(amount, unit),
  };
}

function formatTick(interval: IntervalType, localStr: string): string {
  const d = dayjs(localStr);
  switch (interval) {
    case "hour":
      return d.format("MM/DD HH:mm");
    case "day":
      return d.format("MM/DD");
    case "week":
      return d.format("MM/DD");
    case "month":
      return d.format("YYYY/MM");
    case "year":
      return d.format("YYYY");
  }
}

function formatRangeLabel(
  interval: IntervalType,
  range: { start: dayjs.Dayjs; end: dayjs.Dayjs },
): string {
  switch (interval) {
    case "hour":
    case "day":
    case "week":
      return `${range.start.format("YYYY-MM-DD")} - ${range.end.format("YYYY-MM-DD")}`;
    case "month":
      return `${range.start.format("YYYY-MM")} - ${range.end.format("YYYY-MM")}`;
    case "year":
      return `${range.start.format("YYYY")} - ${range.end.format("YYYY")}`;
  }
}

/** Generate all interval-aligned time slots (naive local-time strings). */
function generateTimeSlots(
  interval: IntervalType,
  rangeStart: dayjs.Dayjs,
  rangeEnd: dayjs.Dayjs,
): string[] {
  const stepUnit: dayjs.ManipulateType =
    interval === "hour"
      ? "hour"
      : interval === "day"
        ? "day"
        : interval === "week"
          ? "week"
          : interval === "month"
            ? "month"
            : "year";

  let cursor =
    interval === "week"
      ? rangeStart.startOf("week")
      : rangeStart.startOf(stepUnit);

  const slots: string[] = [];
  while (cursor.isBefore(rangeEnd) || cursor.isSame(rangeEnd, stepUnit)) {
    slots.push(cursor.format(NAIVE_FMT));
    cursor = cursor.add(1, stepUnit);
  }
  return slots;
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

// ── Color palette for stacked bars ────────────────────────────────────

const MODEL_COLORS = [
  "#729d39",
  "#9bc46a",
  "#2196F3",
  "#FF9800",
  "#E91E63",
  "#9C27B0",
  "#00BCD4",
  "#FF5722",
  "#607D8B",
  "#795548",
  "#3F51B5",
  "#FFC107",
];

// ── Component ─────────────────────────────────────────────────────────

export default function SettingsUsagePage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { sendRpc, isAdmin, connected } = backend;
  const muiTheme = useTheme();

  const [interval, setInterval] = useState<IntervalType>("hour");
  const [range, setRange] = useState(defaultRange("hour"));
  const [entries, setEntries] = useState<UsageEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const [metric, setMetric] = useState<MetricType>("estimatedCost");
  const [pricing, setPricing] = useState<PricingConfig>(loadPricing);
  const [pricingOpen, setPricingOpen] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);

  const updatePricing = useCallback((patch: Partial<PricingConfig>) => {
    setPricing((prev) => {
      const next = { ...prev, ...patch };
      savePricing(next);
      return next;
    });
  }, []);

  // Filters
  const [filterProvider, setFilterProvider] = useState<string>("");
  const [filterModel, setFilterModel] = useState<string>("");
  const [filterUser, setFilterUser] = useState<string>("");

  // Admin: user list
  const [users, setUsers] = useState<UserInfo[]>([]);

  useEffect(() => {
    if (isAdmin && connected) {
      sendRpc<{ users: UserInfo[] }>("users.list", {})
        .then((r) => setUsers(r.users || []))
        .catch(() => {});
    }
  }, [isAdmin, connected, sendRpc]);

  // Fetch usage data
  const fetchUsage = useCallback(() => {
    if (!connected) return;
    setLoading(true);
    setError("");

    const params: Record<string, string> = {
      intervalType: interval,
      startedAt: range.start.format("YYYY-MM-DDTHH:mm:ss"),
      endedAt: range.end.format("YYYY-MM-DDTHH:mm:ss"),
    };
    if (filterProvider) params.providerName = filterProvider;
    if (filterModel) params.modelName = filterModel;
    if (isAdmin && filterUser) params.userId = filterUser;

    sendRpc<UsageListResult>("usages.list", params)
      .then((result) => setEntries(result.entries || []))
      .catch((err) =>
        setError(err instanceof Error ? err.message : String(err)),
      )
      .finally(() => setLoading(false));
  }, [
    connected,
    sendRpc,
    interval,
    range,
    filterProvider,
    filterModel,
    filterUser,
    isAdmin,
  ]);

  useEffect(() => {
    fetchUsage();
  }, [fetchUsage]);

  // Handle interval change
  const handleIntervalChange = useCallback(
    (_: React.MouseEvent<HTMLElement>, value: IntervalType | null) => {
      if (!value) return;
      setInterval(value);
      setRange(defaultRange(value));
    },
    [],
  );

  // Available providers/models from data
  const { providers, models } = useMemo(() => {
    const provSet = new Set<string>();
    const modSet = new Set<string>();
    for (const e of entries) {
      if (e.providerName) provSet.add(e.providerName);
      if (e.modelName) modSet.add(e.modelName);
    }
    return {
      providers: Array.from(provSet).sort(),
      models: Array.from(modSet).sort(),
    };
  }, [entries]);

  // Build chart data: aggregate by time bucket, stack by model (or user for admin)
  const { xLabels, stackKeys, seriesData, colorMap } = useMemo(() => {
    const buckets = new Map<string, Map<string, number>>();
    const keySet = new Set<string>();

    for (const e of entries) {
      const time = toLocalKey(e.startedAt || "");
      const key =
        filterUser && isAdmin
          ? `${e.providerName}/${e.modelName}`
          : e.userId && isAdmin && !filterUser
            ? getUserLabel(e.userId, users)
            : `${e.providerName}/${e.modelName}`;

      keySet.add(key);
      if (!buckets.has(time)) buckets.set(time, new Map());
      const bucket = buckets.get(time)!;
      const value =
        metric === "estimatedCost" ? estimateCost(e, pricing) : e[metric] || 0;
      bucket.set(key, (bucket.get(key) || 0) + value);
    }

    const keys = Array.from(keySet).sort();
    const cMap: Record<string, string> = {};
    keys.forEach((k, i) => {
      cMap[k] = MODEL_COLORS[i % MODEL_COLORS.length];
    });

    const allSlots = generateTimeSlots(interval, range.start, range.end);
    const labels = allSlots.map((slot) => formatTick(interval, slot));
    const data: Record<string, number[]> = {};
    for (const k of keys) {
      data[k] = allSlots.map((slot) => buckets.get(slot)?.get(k) || 0);
    }

    return {
      xLabels: labels,
      stackKeys: keys,
      seriesData: data,
      colorMap: cMap,
    };
  }, [entries, filterUser, isAdmin, users, interval, range, metric, pricing]);

  // ECharts option
  const chartOption = useMemo(() => {
    const textColor = muiTheme.palette.text.secondary;
    const borderColor = muiTheme.palette.divider;

    return {
      tooltip: {
        trigger: "axis" as const,
        axisPointer: { type: "shadow" as const },
        valueFormatter: (value: number) =>
          metric === "estimatedCost"
            ? formatCost(value)
            : metric === "requestCount"
              ? value.toLocaleString()
              : `${value.toLocaleString()} tokens`,
      },
      legend: {
        show: stackKeys.length > 1,
        bottom: 0,
        textStyle: { fontSize: 11, color: textColor },
      },
      grid: {
        left: 8,
        right: 16,
        top: 16,
        bottom: stackKeys.length > 1 ? 32 : 8,
        containLabel: true,
      },
      xAxis: {
        type: "category" as const,
        data: xLabels,
        axisLabel: { fontSize: 11, color: textColor },
        axisLine: { lineStyle: { color: borderColor } },
        axisTick: { lineStyle: { color: borderColor } },
      },
      yAxis: {
        type: "value" as const,
        axisLabel: {
          fontSize: 11,
          color: textColor,
          formatter:
            metric === "estimatedCost"
              ? (v: number) => formatCost(v)
              : formatNumber,
        },
        splitLine: { lineStyle: { color: borderColor, opacity: 0.4 } },
      },
      series: stackKeys.map((key) => ({
        name: key,
        type: "bar" as const,
        stack: "usage",
        data: seriesData[key],
        itemStyle: { color: colorMap[key] },
        emphasis: { focus: "series" as const },
        barMaxWidth: 32,
      })),
    };
  }, [xLabels, stackKeys, seriesData, colorMap, muiTheme, metric]);

  // Detailed data table
  const tableRows = useMemo(() => {
    const agg = new Map<
      string,
      {
        providerName: string;
        modelName: string;
        userId: string;
        promptTokens: number;
        completionTokens: number;
        cacheCreationTokens: number;
        cacheReadTokens: number;
        totalTokens: number;
        requestCount: number;
        estimatedCost: number;
      }
    >();

    for (const e of entries) {
      const key = isAdmin
        ? `${e.userId}|${e.providerName}|${e.modelName}`
        : `${e.providerName}|${e.modelName}`;
      if (!agg.has(key)) {
        agg.set(key, {
          providerName: e.providerName || "",
          modelName: e.modelName || "",
          userId: e.userId || "",
          promptTokens: 0,
          completionTokens: 0,
          cacheCreationTokens: 0,
          cacheReadTokens: 0,
          totalTokens: 0,
          requestCount: 0,
          estimatedCost: 0,
        });
      }
      const row = agg.get(key)!;
      row.promptTokens += e.promptTokens || 0;
      row.completionTokens += e.completionTokens || 0;
      row.cacheCreationTokens += e.cacheCreationTokens || 0;
      row.cacheReadTokens += e.cacheReadTokens || 0;
      row.totalTokens += e.totalTokens || 0;
      row.requestCount += e.requestCount || 0;
      row.estimatedCost += estimateCost(e, pricing);
    }

    return Array.from(agg.values()).sort(
      (a, b) => b.totalTokens - a.totalTokens,
    );
  }, [entries, isAdmin, pricing]);

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Box sx={{ mb: 1 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t("settings.usage")}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {t("settings.usageDescription")}
            </Typography>
          </Box>

          {error && (
            <Typography variant="caption" color="error">
              {error}
            </Typography>
          )}

          {/* Controls */}
          <Box>
            <Box
              sx={{
                display: "flex",
                flexWrap: "wrap",
                gap: 1.5,
                alignItems: "center",
              }}
            >
              <ToggleButtonGroup
                value={interval}
                exclusive
                onChange={handleIntervalChange}
                size="small"
                sx={{ "& .MuiToggleButton-root": { textTransform: "none" } }}
              >
                <ToggleButton value="hour">{t("usage.hour")}</ToggleButton>
                <ToggleButton value="day">{t("usage.day")}</ToggleButton>
                <ToggleButton value="week">{t("usage.week")}</ToggleButton>
                <ToggleButton value="month">{t("usage.month")}</ToggleButton>
                <ToggleButton value="year">{t("usage.year")}</ToggleButton>
              </ToggleButtonGroup>

              <Box sx={{ flex: 1 }} />

              <FormControl size="small" sx={{ minWidth: 140 }}>
                <InputLabel>{t("usage.metric")}</InputLabel>
                <Select
                  value={metric}
                  label={t("usage.metric")}
                  onChange={(e) => setMetric(e.target.value as MetricType)}
                >
                  {METRIC_KEYS.map((m) => (
                    <MenuItem key={m} value={m}>
                      {t(METRIC_I18N[m])}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>

              {metric === "estimatedCost" && (
                <IconButton
                  size="small"
                  onClick={() => setPricingOpen((v) => !v)}
                  color={pricingOpen ? "primary" : "default"}
                >
                  <SettingsIcon fontSize="small" />
                </IconButton>
              )}

              <IconButton
                size="small"
                onClick={() => setFiltersOpen((v) => !v)}
                color={filtersOpen ? "primary" : "default"}
              >
                <FilterListIcon fontSize="small" />
              </IconButton>
            </Box>

            <Collapse in={filtersOpen}>
              <Box
                sx={{
                  display: "flex",
                  flexWrap: "wrap",
                  gap: 1.5,
                  mt: 1.5,
                  pt: 1.5,
                  borderTop: 1,
                  borderColor: "divider",
                }}
              >
                <FormControl size="small" sx={{ minWidth: 130 }}>
                  <InputLabel>{t("usage.provider")}</InputLabel>
                  <Select
                    value={filterProvider}
                    label={t("usage.provider")}
                    onChange={(e) => setFilterProvider(e.target.value)}
                  >
                    <MenuItem value="">{t("usage.all")}</MenuItem>
                    {providers.map((p) => (
                      <MenuItem key={p} value={p}>
                        {p}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>

                <FormControl size="small" sx={{ minWidth: 180 }}>
                  <InputLabel>{t("usage.model")}</InputLabel>
                  <Select
                    value={filterModel}
                    label={t("usage.model")}
                    onChange={(e) => setFilterModel(e.target.value)}
                  >
                    <MenuItem value="">{t("usage.all")}</MenuItem>
                    {models.map((m) => (
                      <MenuItem key={m} value={m}>
                        {m}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>

                {isAdmin && (
                  <FormControl size="small" sx={{ minWidth: 130 }}>
                    <InputLabel>{t("usage.user")}</InputLabel>
                    <Select
                      value={filterUser}
                      label={t("usage.user")}
                      onChange={(e) => setFilterUser(e.target.value)}
                    >
                      <MenuItem value="">{t("usage.allUsers")}</MenuItem>
                      {users.map((u) => (
                        <MenuItem key={u.id} value={u.id}>
                          {u.name || u.username}
                        </MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                )}
              </Box>
            </Collapse>

            <Collapse in={pricingOpen && metric === "estimatedCost"}>
              <Box
                sx={{
                  display: "flex",
                  flexWrap: "wrap",
                  gap: 1.5,
                  mt: 1.5,
                  pt: 1.5,
                  borderTop: 1,
                  borderColor: "divider",
                }}
              >
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ width: "100%" }}
                >
                  {t("usage.pricingConfig")}
                </Typography>
                <TextField
                  size="small"
                  label={t("usage.inputPrice")}
                  type="number"
                  value={pricing.inputPer1M}
                  onChange={(e) =>
                    updatePricing({
                      inputPer1M: parseFloat(e.target.value) || 0,
                    })
                  }
                  slotProps={{
                    input: {
                      startAdornment: (
                        <InputAdornment position="start">$</InputAdornment>
                      ),
                    },
                  }}
                  sx={{ width: 200 }}
                />
                <TextField
                  size="small"
                  label={t("usage.cachedInputPrice")}
                  type="number"
                  value={pricing.cachedInputPer1M}
                  onChange={(e) =>
                    updatePricing({
                      cachedInputPer1M: parseFloat(e.target.value) || 0,
                    })
                  }
                  slotProps={{
                    input: {
                      startAdornment: (
                        <InputAdornment position="start">$</InputAdornment>
                      ),
                    },
                  }}
                  sx={{ width: 200 }}
                />
                <TextField
                  size="small"
                  label={t("usage.outputPrice")}
                  type="number"
                  value={pricing.outputPer1M}
                  onChange={(e) =>
                    updatePricing({
                      outputPer1M: parseFloat(e.target.value) || 0,
                    })
                  }
                  slotProps={{
                    input: {
                      startAdornment: (
                        <InputAdornment position="start">$</InputAdornment>
                      ),
                    },
                  }}
                  sx={{ width: 200 }}
                />
              </Box>
            </Collapse>
          </Box>

          {/* Chart */}
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, my: 4 }}>
            <IconButton
              size="small"
              onClick={() => setRange((r) => shiftRange(interval, r, -1))}
              sx={{ flexShrink: 0 }}
            >
              <ChevronLeftIcon fontSize="small" />
            </IconButton>
            <Box
              sx={{ flex: 1, minWidth: 0, height: 300, position: "relative" }}
            >
              {loading && (
                <Box
                  sx={{
                    position: "absolute",
                    inset: 0,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    zIndex: 1,
                  }}
                >
                  <CircularProgress size={24} />
                </Box>
              )}
              {!loading && xLabels.length === 0 ? (
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    height: "100%",
                  }}
                >
                  <Typography variant="caption" color="text.secondary">
                    {t("usage.noData")}
                  </Typography>
                </Box>
              ) : (
                <ReactECharts
                  option={chartOption}
                  style={{ height: 300, opacity: loading ? 0.3 : 1 }}
                  notMerge
                />
              )}
            </Box>
            <IconButton
              size="small"
              onClick={() => setRange((r) => shiftRange(interval, r, 1))}
              sx={{ flexShrink: 0 }}
              disabled={!range.end.isBefore(dayjs())}
            >
              <ChevronRightIcon fontSize="small" />
            </IconButton>
          </Box>

          {/* Detail table */}
          {tableRows.length > 0 && (
            <Paper variant="outlined">
              <TableContainer>
                <Table size="small" sx={{ whiteSpace: "nowrap" }}>
                  <TableHead>
                    <TableRow>
                      {isAdmin && <TableCell>{t("usage.user")}</TableCell>}
                      <TableCell>{t("usage.model")}</TableCell>
                      <TableCell align="right">{t("usage.requests")}</TableCell>
                      <TableCell align="right">
                        {t("usage.promptTokens")}
                      </TableCell>
                      <TableCell align="right">
                        {t("usage.completionTokens")}
                      </TableCell>
                      <TableCell align="right">
                        {t("usage.cacheCreation")}
                      </TableCell>
                      <TableCell align="right">
                        {t("usage.cacheRead")}
                      </TableCell>
                      <TableCell align="right">
                        {t("usage.totalTokens")}
                      </TableCell>
                      <TableCell align="right">
                        {t("usage.estimatedCost")}
                      </TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {tableRows.map((row, idx) => (
                      <TableRow key={idx}>
                        {isAdmin && (
                          <TableCell>
                            {getUserLabel(row.userId, users)}
                          </TableCell>
                        )}
                        <TableCell>
                          {row.providerName}/{row.modelName}
                        </TableCell>
                        <TableCell align="right">
                          {formatNumber(row.requestCount)}
                        </TableCell>
                        <TableCell align="right">
                          {formatNumber(row.promptTokens)}
                        </TableCell>
                        <TableCell align="right">
                          {formatNumber(row.completionTokens)}
                        </TableCell>
                        <TableCell align="right">
                          {formatNumber(row.cacheCreationTokens)}
                        </TableCell>
                        <TableCell align="right">
                          {formatNumber(row.cacheReadTokens)}
                        </TableCell>
                        <TableCell align="right">
                          {formatNumber(row.totalTokens)}
                        </TableCell>
                        <TableCell align="right">
                          {formatCost(row.estimatedCost)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </Paper>
          )}
        </Box>
      </Container>
    </Box>
  );
}

function getUserLabel(userId: string, users: UserInfo[]): string {
  const user = users.find((u) => u.id === userId);
  return user ? user.name || user.username : userId;
}
