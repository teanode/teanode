import React from "react";
import Typography from "@mui/material/Typography";

interface UsageIndicatorProps {
  text: string;
}

export default function UsageIndicator({ text }: UsageIndicatorProps) {
  return (
    <Typography
      variant="caption"
      color="text.secondary"
      sx={{
        alignSelf: "flex-start",
        fontSize: "11px",
        fontFamily: "monospace",
        px: 2,
        py: 0.25,
        opacity: 0.6,
      }}
    >
      {text}
    </Typography>
  );
}
