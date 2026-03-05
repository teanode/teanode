import React from "react";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";

interface DateSeparatorProps {
  label: string;
}

/**
 * Shared date separator divider used in both the main MessageList
 * and the extension overlay ChatView.
 */
export default function DateSeparator({ label }: DateSeparatorProps) {
  return (
    <Divider sx={{ my: 1 }}>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ fontSize: "11px", fontWeight: 500 }}
      >
        {label}
      </Typography>
    </Divider>
  );
}
