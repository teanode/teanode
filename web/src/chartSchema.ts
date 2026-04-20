/**
 * Chart schema types and validation for chart fence blocks.
 *
 * The assistant emits a JSON object inside ```chart fences (backtick form)
 * or :::chart{title="..."} fences (legacy colon form). This module defines
 * the safe, constrained schema and validates it before rendering — no
 * arbitrary JS execution, only declarative data.
 */

/** Supported chart types. */
export type ChartType = "line" | "bar" | "scatter" | "pie";

const CHART_TYPES: ReadonlySet<string> = new Set<ChartType>([
  "line",
  "bar",
  "scatter",
  "pie",
]);

/** A single data series. */
export interface ChartSeries {
  name: string;
  data: number[];
  /** For pie charts, labels for each slice (parallel to data). */
  labels?: string[];
}

/** The validated chart specification. */
export interface ChartSpec {
  chartType: ChartType;
  title?: string;
  xAxis?: {
    label?: string;
    data?: (string | number)[];
  };
  yAxis?: {
    label?: string;
  };
  series: ChartSeries[];
}

export interface ChartParseResult {
  ok: true;
  spec: ChartSpec;
}

export interface ChartParseError {
  ok: false;
  error: string;
}

/**
 * Parse and validate a JSON string into a ChartSpec.
 * Returns a discriminated union so callers can show the error message.
 */
export function parseChartSpec(
  json: string,
): ChartParseResult | ChartParseError {
  let raw: unknown;
  try {
    raw = JSON.parse(json);
  } catch {
    return { ok: false, error: "Invalid JSON in chart block." };
  }

  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return { ok: false, error: "Chart spec must be a JSON object." };
  }

  const obj = raw as Record<string, unknown>;

  // chartType
  const chartType = obj.chartType;
  if (typeof chartType !== "string" || !CHART_TYPES.has(chartType)) {
    return {
      ok: false,
      error: `Invalid chartType: expected one of ${[...CHART_TYPES].join(", ")}.`,
    };
  }

  // series
  if (!Array.isArray(obj.series) || obj.series.length === 0) {
    return {
      ok: false,
      error: "Chart spec must have a non-empty series array.",
    };
  }
  const series: ChartSeries[] = [];
  for (const [index, item] of (obj.series as unknown[]).entries()) {
    if (typeof item !== "object" || item === null || Array.isArray(item)) {
      return { ok: false, error: `series[${index}] must be an object.` };
    }
    const seriesObj = item as Record<string, unknown>;
    if (typeof seriesObj.name !== "string") {
      return { ok: false, error: `series[${index}].name must be a string.` };
    }
    if (
      !Array.isArray(seriesObj.data) ||
      !seriesObj.data.every((value: unknown) => typeof value === "number")
    ) {
      return {
        ok: false,
        error: `series[${index}].data must be an array of numbers.`,
      };
    }
    const entry: ChartSeries = {
      name: seriesObj.name,
      data: seriesObj.data as number[],
    };
    if (Array.isArray(seriesObj.labels)) {
      entry.labels = (seriesObj.labels as unknown[]).map(String);
    }
    series.push(entry);
  }

  // xAxis (optional)
  let xAxis: ChartSpec["xAxis"];
  if (obj.xAxis && typeof obj.xAxis === "object" && !Array.isArray(obj.xAxis)) {
    const xObj = obj.xAxis as Record<string, unknown>;
    xAxis = {};
    if (typeof xObj.label === "string") xAxis.label = xObj.label;
    if (Array.isArray(xObj.data)) {
      xAxis.data = (xObj.data as unknown[]).map((value) =>
        typeof value === "number" ? value : String(value),
      );
    }
  }

  // yAxis (optional)
  let yAxis: ChartSpec["yAxis"];
  if (obj.yAxis && typeof obj.yAxis === "object" && !Array.isArray(obj.yAxis)) {
    const yObj = obj.yAxis as Record<string, unknown>;
    yAxis = {};
    if (typeof yObj.label === "string") yAxis.label = yObj.label;
  }

  // title (optional, override from spec JSON — fence title takes precedence in UI)
  const title = typeof obj.title === "string" ? obj.title : undefined;

  return {
    ok: true,
    spec: {
      chartType: chartType as ChartType,
      title,
      xAxis,
      yAxis,
      series,
    },
  };
}
