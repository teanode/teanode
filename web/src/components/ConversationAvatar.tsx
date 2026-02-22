import React from "react";
import Avatar from "@mui/material/Avatar";
import { withToken } from "../rpc";

interface ConversationAvatarProps {
  avatarMediaId?: string;
  fallback: string;
  size?: number;
}

function normalizeFallback(value: string): string {
  const trimmed = value.trim();
  return (trimmed.charAt(0) || "?").toUpperCase();
}

export default function ConversationAvatar({
  avatarMediaId,
  fallback,
  size = 22,
}: ConversationAvatarProps) {
  return (
    <Avatar
      src={
        avatarMediaId ? withToken(`/api/v1/media/${avatarMediaId}`) : undefined
      }
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
