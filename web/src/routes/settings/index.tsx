import React, { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import CircularProgress from "@mui/material/CircularProgress";

/** /settings/ — redirects to profile page (first item in settings nav). */
export default function SettingsIndexPage() {
  const navigate = useNavigate();

  useEffect(() => {
    navigate({ to: "/settings/profile", replace: true });
  }, [navigate]);

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        flex: 1,
      }}
    >
      <CircularProgress />
    </Box>
  );
}
