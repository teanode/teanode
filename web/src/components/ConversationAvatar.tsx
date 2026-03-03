import React from "react";
import Avatar from "@mui/material/Avatar";
import { withToken } from "../rpc";

interface ConversationAvatarProps {
  avatarMediaId?: string;
  /** Pre-resolved avatar URL (takes precedence over avatarMediaId). */
  src?: string;
  fallback: string;
  size?: number;
}

function normalizeFallback(value: string): string {
  const trimmed = value.trim();
  return (trimmed.charAt(0) || "?").toUpperCase();
}

export default function ConversationAvatar({
  avatarMediaId,
  src: srcProp,
  fallback,
  size = 22,
}: ConversationAvatarProps) {
  const resolvedSrc = srcProp
    ? srcProp
    : avatarMediaId
      ? withToken(`/api/v1/media/${avatarMediaId}`)
      : undefined;
  return (
    <Avatar
      src={resolvedSrc}
      sx={{
        width: { xs: Math.max(20, size - 2), sm: size },
        height: { xs: Math.max(20, size - 2), sm: size },
        fontSize: { xs: "0.65rem", sm: "0.7rem" },
        bgcolor: "action.selected",
        color: "text.secondary",
        flexShrink: 0,
      }}
    >
      {normalizeFallback(fallback)}
    </Avatar>
  );
}
