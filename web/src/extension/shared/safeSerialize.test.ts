import { describe, it, expect } from "vitest";
import { safeSerialize } from "./safeSerialize";

describe("safeSerialize", () => {
  it("serializes primitive values", () => {
    expect(safeSerialize(42)).toEqual({ value: 42, truncated: false });
    expect(safeSerialize("hello")).toEqual({ value: "hello", truncated: false });
    expect(safeSerialize(true)).toEqual({ value: true, truncated: false });
    expect(safeSerialize(null)).toEqual({ value: null, truncated: false });
  });

  it("serializes objects and arrays", () => {
    const obj = { a: 1, b: [2, 3] };
    expect(safeSerialize(obj)).toEqual({ value: obj, truncated: false });
  });

  it("handles undefined by converting to null", () => {
    const obj = { a: undefined };
    const result = safeSerialize(obj);
    expect(result.value).toEqual({ a: null });
    expect(result.truncated).toBe(false);
  });

  it("handles functions by replacing with marker", () => {
    const obj = { fn: () => {} };
    const result = safeSerialize(obj);
    expect((result.value as Record<string, unknown>).fn).toBe("[Function]");
  });

  it("handles circular references", () => {
    const obj: Record<string, unknown> = { a: 1 };
    obj.self = obj;
    const result = safeSerialize(obj);
    expect((result.value as Record<string, unknown>).self).toBe("[Circular]");
    expect(result.truncated).toBe(false);
  });

  it("truncates large strings", () => {
    const big = "x".repeat(1000);
    const result = safeSerialize(big, 100);
    expect(result.truncated).toBe(true);
    expect(typeof result.value).toBe("string");
    expect((result.value as string).length).toBeLessThanOrEqual(100);
  });

  it("truncates large objects", () => {
    const obj: Record<string, string> = {};
    for (let i = 0; i < 1000; i++) {
      obj[`key${i}`] = "value".repeat(10);
    }
    const result = safeSerialize(obj, 500);
    expect(result.truncated).toBe(true);
  });

  it("handles Error objects", () => {
    const err = new Error("test error");
    const result = safeSerialize(err);
    const val = result.value as Record<string, unknown>;
    expect(val.message).toBe("test error");
    expect(val.name).toBe("Error");
  });

  it("handles RegExp", () => {
    const result = safeSerialize(/foo/gi);
    expect(result.value).toBe("/foo/gi");
  });

  it("handles bigint", () => {
    const result = safeSerialize(BigInt(123));
    expect(result.value).toBe("123");
  });

  it("handles symbol", () => {
    const result = safeSerialize(Symbol("test"));
    expect(result.value).toBe("Symbol(test)");
  });

  // ---- Tab tool–specific edge cases ----

  it("handles nested Error objects (eval error reporting)", () => {
    const err = new TypeError("Cannot read property 'x'");
    const wrapper = { error: err, context: "eval" };
    const result = safeSerialize(wrapper);
    const val = result.value as Record<string, unknown>;
    const errorObj = val.error as Record<string, unknown>;
    expect(errorObj.message).toBe("Cannot read property 'x'");
    expect(errorObj.name).toBe("TypeError");
    expect(val.context).toBe("eval");
  });

  it("handles deeply nested objects within size cap", () => {
    const deep: Record<string, unknown> = { level: 0 };
    let current = deep;
    for (let i = 1; i < 20; i++) {
      const next: Record<string, unknown> = { level: i };
      current.child = next;
      current = next;
    }
    const result = safeSerialize(deep);
    expect(result.truncated).toBe(false);
    expect((result.value as Record<string, unknown>).level).toBe(0);
  });

  it("handles mixed types in arrays (DOM-like results)", () => {
    const domLike = {
      results: [
        { tagName: "div", content: "Hello", attributes: { id: "main" } },
        { tagName: "span", content: "World", attributes: { class: "text" } },
      ],
      totalMatches: 2,
      truncated: false,
    };
    const result = safeSerialize(domLike);
    expect(result.truncated).toBe(false);
    const val = result.value as Record<string, unknown>;
    expect((val.results as unknown[]).length).toBe(2);
  });

  it("handles null and empty string values (localStorage)", () => {
    const entries = { key1: "", key2: null, key3: "value" };
    const result = safeSerialize(entries);
    const val = result.value as Record<string, unknown>;
    expect(val.key1).toBe("");
    expect(val.key2).toBe(null);
    expect(val.key3).toBe("value");
  });

  it("returns unserializable marker for objects that throw on stringify", () => {
    const tricky = {
      get prop(): string {
        throw new Error("getter throws");
      },
    };
    const result = safeSerialize(tricky);
    // Should not throw, should return a safe fallback
    expect(result).toBeDefined();
  });
});
