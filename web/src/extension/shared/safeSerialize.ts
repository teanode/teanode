/**
 * Safely serialize a value to a JSON-compatible object, handling cycles,
 * non-serializable values, and enforcing a size cap.
 */

const MAX_RESULT_SIZE = 512 * 1024; // 512 KB

/**
 * Serialize a value to JSON-safe form, detecting cycles and capping size.
 * Returns { value, truncated }.
 */
export function safeSerialize(
  input: unknown,
  maxSize: number = MAX_RESULT_SIZE,
): { value: unknown; truncated: boolean } {
  const seen = new WeakSet();

  function replacer(_key: string, val: unknown): unknown {
    if (val === undefined) return null;
    if (typeof val === "function") return "[Function]";
    if (typeof val === "symbol") return val.toString();
    if (typeof val === "bigint") return val.toString();
    if (val instanceof Error) {
      return { message: val.message, name: val.name, stack: val.stack };
    }
    if (val instanceof RegExp) return val.toString();
    if (val !== null && typeof val === "object") {
      if (seen.has(val)) return "[Circular]";
      seen.add(val);
    }
    return val;
  }

  let json: string;
  try {
    json = JSON.stringify(input, replacer);
  } catch {
    return { value: "[unserializable]", truncated: false };
  }

  if (json.length > maxSize) {
    // Try to truncate gracefully: if it's a string value, truncate the string.
    const parsed = JSON.parse(json);
    if (typeof parsed === "string") {
      return { value: parsed.slice(0, maxSize), truncated: true };
    }
    // Otherwise return truncated JSON as a string.
    return { value: json.slice(0, maxSize), truncated: true };
  }

  return { value: JSON.parse(json), truncated: false };
}
