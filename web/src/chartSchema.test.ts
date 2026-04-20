import { describe, expect, it } from "vitest";
import { parseChartSpec } from "./chartSchema";

describe("parseChartSpec", () => {
  it("parses a valid line chart", () => {
    const result = parseChartSpec(
      JSON.stringify({
        chartType: "line",
        xAxis: { label: "Month", data: ["Jan", "Feb", "Mar"] },
        yAxis: { label: "Revenue" },
        series: [{ name: "2024", data: [100, 150, 200] }],
      }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.spec.chartType).toBe("line");
      expect(result.spec.series).toHaveLength(1);
      expect(result.spec.xAxis?.data).toEqual(["Jan", "Feb", "Mar"]);
    }
  });

  it("parses a valid bar chart with multiple series", () => {
    const result = parseChartSpec(
      JSON.stringify({
        chartType: "bar",
        xAxis: { data: ["A", "B", "C"] },
        series: [
          { name: "Group 1", data: [10, 20, 30] },
          { name: "Group 2", data: [15, 25, 35] },
        ],
      }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.spec.chartType).toBe("bar");
      expect(result.spec.series).toHaveLength(2);
    }
  });

  it("parses a pie chart with labels", () => {
    const result = parseChartSpec(
      JSON.stringify({
        chartType: "pie",
        series: [
          {
            name: "Share",
            data: [40, 30, 20, 10],
            labels: ["A", "B", "C", "D"],
          },
        ],
      }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.spec.chartType).toBe("pie");
      expect(result.spec.series[0].labels).toEqual(["A", "B", "C", "D"]);
    }
  });

  it("rejects invalid JSON", () => {
    const result = parseChartSpec("not json");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain("Invalid JSON");
    }
  });

  it("rejects missing chartType", () => {
    const result = parseChartSpec(
      JSON.stringify({ series: [{ name: "A", data: [1] }] }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain("chartType");
    }
  });

  it("rejects unknown chartType", () => {
    const result = parseChartSpec(
      JSON.stringify({
        chartType: "radar",
        series: [{ name: "A", data: [1] }],
      }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain("chartType");
    }
  });

  it("rejects empty series", () => {
    const result = parseChartSpec(
      JSON.stringify({ chartType: "line", series: [] }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain("series");
    }
  });

  it("rejects series with non-numeric data", () => {
    const result = parseChartSpec(
      JSON.stringify({
        chartType: "line",
        series: [{ name: "A", data: ["a", "b"] }],
      }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain("data");
    }
  });

  it("rejects non-object input", () => {
    expect(parseChartSpec("[]").ok).toBe(false);
    expect(parseChartSpec('"string"').ok).toBe(false);
    expect(parseChartSpec("42").ok).toBe(false);
  });

  it("accepts scatter chart type", () => {
    const result = parseChartSpec(
      JSON.stringify({
        chartType: "scatter",
        xAxis: { data: [1, 2, 3] },
        series: [{ name: "Points", data: [10, 20, 30] }],
      }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.spec.chartType).toBe("scatter");
    }
  });

  it("handles optional title field", () => {
    const result = parseChartSpec(
      JSON.stringify({
        chartType: "line",
        title: "Revenue Over Time",
        series: [{ name: "Revenue", data: [1, 2, 3] }],
      }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.spec.title).toBe("Revenue Over Time");
    }
  });
});
