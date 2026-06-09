import React from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import DashboardCustomizeRounded from "@mui/icons-material/DashboardCustomizeRounded";
import SurfaceRenderer from "./SurfaceRenderer";
import type { Surface, SurfaceActionPayload } from "../types";

interface SurfaceSidePanelProps {
  surfaces: Surface[];
  onAction: (action: SurfaceActionPayload) => Promise<void> | void;
  disabled?: boolean;
}

/**
 * Right-hand panel that renders right_panel-located generative-UI surfaces.
 * Sibling to the artifact side panel; only renders when there is content.
 */
export default function SurfaceSidePanel({
  surfaces,
  onAction,
  disabled = false,
}: SurfaceSidePanelProps) {
  const { t } = useTranslation();
  const panelSurfaces = surfaces.filter(
    (surface) => surface.location === "right_panel",
  );
  if (panelSurfaces.length === 0) return null;

  return (
    <Box
      sx={{
        width: { md: 440, lg: 520, xl: 600 },
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
        borderLeft: 1,
        borderColor: "divider",
        bgcolor: "background.default",
        minHeight: 0,
      }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          px: 3,
          py: 1.5,
          borderBottom: 1,
          borderColor: "divider",
          flexShrink: 0,
        }}
      >
        <DashboardCustomizeRounded
          sx={{ fontSize: 20, color: "primary.main", flexShrink: 0 }}
        />
        <Typography variant="subtitle2">{t("surface.panel")}</Typography>
      </Box>
      <Box
        sx={{
          flex: 1,
          overflowY: "auto",
          px: 2,
          py: 2,
          display: "flex",
          flexDirection: "column",
          gap: 2,
        }}
      >
        {panelSurfaces.map((surface) => (
          <SurfaceRenderer
            key={surface.surfaceId}
            surface={surface}
            onAction={onAction}
            disabled={disabled}
          />
        ))}
      </Box>
    </Box>
  );
}
