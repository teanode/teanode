/**
 * Shared date/time formatting utilities used by both the WebUI and extension overlay.
 */

/**
 * Returns a human-readable date label for a message timestamp.
 * "Today", "Yesterday", or a locale-formatted date string.
 */
export function dateLabelFor(
  timestamp: number,
  t: (key: string) => string,
): string {
  const messageDate = new Date(timestamp);
  const now = new Date();

  const messageDay = new Date(
    messageDate.getFullYear(),
    messageDate.getMonth(),
    messageDate.getDate(),
  );
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = today.getTime() - messageDay.getTime();

  if (diff === 0) return t("conversations.today");
  if (diff === 86_400_000) return t("conversations.yesterday");
  return messageDate.toLocaleDateString([], {
    weekday: "short",
    month: "short",
    day: "numeric",
    year:
      messageDate.getFullYear() !== now.getFullYear() ? "numeric" : undefined,
  });
}
