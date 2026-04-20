import React, { useMemo } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { useTheme } from "@mui/material/styles";
import BarChartRounded from "@mui/icons-material/BarChartRounded";
import ReactECharts from "echarts-for-react";
import { parseChartSpec } from "../chartSchema";
import type { ChartSpec } from "../chartSchema";

interface ChartRendererProps {
  /** The chart fence title (shown in the header). */
  title: string;
  /** Raw JSON content from inside the :::chart fence. */
  content: string;
  /** Whether the chart block is still being streamed. */
  isStreaming?: boolean;
}

/** Build an ECharts option object from a validated ChartSpec. */
function buildEChartsOption(
  spec: ChartSpec,
  isDark: boolean,
): Record<string, unknown> {
  const textColor = isDark ? "#ccc" : "#333";

  if (spec.chartType === "pie") {
    const firstSeries = spec.series[0];
    const pieData = firstSeries.data.map((value, index) => ({
      value,
      name:
        firstSeries.labels?.[index] ??
        spec.xAxis?.data?.[index] ??
        `Item ${index + 1}`,
    }));
    return {
      tooltip: { trigger: "item" },
      legend: { bottom: 0, textStyle: { color: textColor } },
      series: [
        {
          type: "pie",
          radius: ["40%", "70%"],
          data: pieData,
          emphasis: {
            itemStyle: {
              shadowBlur: 10,
              shadowOffsetX: 0,
              shadowColor: "rgba(0, 0, 0, 0.5)",
            },
          },
        },
      ],
    };
  }

  // line / bar / scatter
  return {
    tooltip: { trigger: "axis" },
    legend: {
      bottom: 0,
      textStyle: { color: textColor },
    },
    grid: { left: 60, right: 20, top: 20, bottom: 40 },
    xAxis: {
      type: "category",
      data: spec.xAxis?.data ?? [],
      name: spec.xAxis?.label,
      nameLocation: "middle",
      nameGap: 30,
      axisLabel: { color: textColor },
      nameTextStyle: { color: textColor },
    },
    yAxis: {
      type: "value",
      name: spec.yAxis?.label,
      nameTextStyle: { color: textColor },
      axisLabel: { color: textColor },
      splitLine: {
        lineStyle: { color: isDark ? "#333" : "#e0e0e0" },
      },
    },
    series: spec.series.map((seriesItem) => ({
      name: seriesItem.name,
      type: spec.chartType,
      data: seriesItem.data,
      smooth: spec.chartType === "line",
    })),
  };
}

export default function ChartRenderer({
  title,
  content,
  isStreaming,
}: ChartRendererProps) {
  const { t } = useTranslation();
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";

  const result = useMemo(() => {
    const trimmed = content.trim();
    if (!trimmed) return null;
    return parseChartSpec(trimmed);
  }, [content]);

  // While streaming and no valid JSON yet, show a placeholder.
  if (!result || (isStreaming && !result.ok)) {
    return (
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1.25,
          px: 2,
          my: 1,
          height: 56,
          borderRadius: 1,
          border: 1,
          borderColor: "divider",
          bgcolor: isDark ? "rgba(255,255,255,0.04)" : "rgba(0,0,0,0.02)",
          ...(isStreaming && {
            animation: "chart-pulse 2s ease-in-out infinite",
            "@keyframes chart-pulse": {
              "0%, 100%": { borderColor: theme.palette.primary.main },
              "50%": { borderColor: theme.palette.divider },
            },
          }),
        }}
      >
        <BarChartRounded
          sx={{ fontSize: 18, color: "primary.main", flexShrink: 0 }}
        />
        <Typography variant="body2" sx={{ fontWeight: 500 }}>
          {title}
          {isStreaming ? "…" : ""}
        </Typography>
      </Box>
    );
  }

  // Parse error — show the error inline.
  if (!result.ok) {
    return (
      <Box
        sx={{
          px: 2,
          py: 1.5,
          my: 1,
          borderRadius: 1,
          border: 1,
          borderColor: "error.main",
          bgcolor: isDark ? "rgba(255,0,0,0.06)" : "rgba(255,0,0,0.04)",
        }}
      >
        <Typography variant="body2" color="error.main">
          {t("chart.error", { message: result.error })}
        </Typography>
      </Box>
    );
  }

  const option = buildEChartsOption(result.spec, isDark);

  return (
    <Box
      sx={{
        my: 1,
        borderRadius: 1,
        border: 1,
        borderColor: "divider",
        bgcolor: isDark ? "rgba(255,255,255,0.02)" : "rgba(0,0,0,0.01)",
        overflow: "hidden",
      }}
    >
      {/* Header */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          px: 2,
          py: 1,
          borderBottom: 1,
          borderColor: "divider",
        }}
      >
        <BarChartRounded
          sx={{ fontSize: 16, color: "primary.main", flexShrink: 0 }}
        />
        <Typography
          variant="body2"
          sx={{
            fontWeight: 500,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {title}
        </Typography>
      </Box>
      {/* Chart */}
      <Box sx={{ px: 1, py: 1 }}>
        <ReactECharts
          option={option}
          style={{ height: 300, width: "100%" }}
          opts={{ renderer: "svg" }}
          theme={isDark ? "dark" : undefined}
        />
      </Box>
    </Box>
  );
}
