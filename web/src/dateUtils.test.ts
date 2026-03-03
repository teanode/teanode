import { describe, it, expect, vi, afterEach } from "vitest";
import { dateLabelFor } from "./dateUtils";

// Minimal translation mock: returns the key as the label.
const t = (key: string) => key;

describe("dateLabelFor", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns 'Today' label for timestamps from today", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 2, 15, 0, 0)); // 2026-03-02 15:00
    const todayTimestamp = new Date(2026, 2, 2, 9, 30, 0).getTime();
    expect(dateLabelFor(todayTimestamp, t)).toBe("conversations.today");
  });

  it("returns 'Yesterday' label for timestamps from yesterday", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 2, 15, 0, 0));
    const yesterdayTimestamp = new Date(2026, 2, 1, 20, 0, 0).getTime();
    expect(dateLabelFor(yesterdayTimestamp, t)).toBe("conversations.yesterday");
  });

  it("returns formatted date for older timestamps", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 2, 15, 0, 0));
    const olderTimestamp = new Date(2026, 1, 25, 10, 0, 0).getTime();
    const result = dateLabelFor(olderTimestamp, t);
    // Should contain the month abbreviation and day number, not "Today"/"Yesterday".
    expect(result).not.toBe("conversations.today");
    expect(result).not.toBe("conversations.yesterday");
    expect(result).toContain("25");
  });

  it("includes year for timestamps from a different year", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 5, 12, 0, 0)); // Jan 5, 2026
    const lastYearTimestamp = new Date(2025, 11, 20, 10, 0, 0).getTime();
    const result = dateLabelFor(lastYearTimestamp, t);
    expect(result).toContain("2025");
  });

  it("omits year for timestamps from the same year", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 5, 15, 12, 0, 0)); // Jun 15, 2026
    const sameYearTimestamp = new Date(2026, 0, 10, 10, 0, 0).getTime();
    const result = dateLabelFor(sameYearTimestamp, t);
    expect(result).not.toContain("2026");
  });

  it("handles midnight boundary correctly", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 2, 0, 0, 1)); // just after midnight
    // A timestamp from 23:59 the previous day should be "Yesterday"
    const lateLastNight = new Date(2026, 2, 1, 23, 59, 59).getTime();
    expect(dateLabelFor(lateLastNight, t)).toBe("conversations.yesterday");
    // A timestamp from 00:01 today should be "Today"
    const earlyToday = new Date(2026, 2, 2, 0, 0, 0).getTime();
    expect(dateLabelFor(earlyToday, t)).toBe("conversations.today");
  });
});
