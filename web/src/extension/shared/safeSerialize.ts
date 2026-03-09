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

  function replacer(_key: string, value: unknown): unknown {
    if (value === undefined) return null;
    if (typeof value === "function") return "[Function]";
    if (typeof value === "symbol") return value.toString();
    if (typeof value === "bigint") return value.toString();
    if (value instanceof Error) {
      return { message: value.message, name: value.name, stack: value.stack };
    }
    if (value instanceof RegExp) return value.toString();
    if (value !== null && typeof value === "object") {
      if (seen.has(value)) return "[Circular]";
      seen.add(value);
    }
    return value;
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
