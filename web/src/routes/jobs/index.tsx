import React, { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import CircularProgress from "@mui/material/CircularProgress";
/** /jobs/ — legacy path redirect to settings jobs page. */
export default function JobsIndex() {
  const navigate = useNavigate();

  useEffect(() => {
    navigate({ to: "/settings/jobs", replace: true });
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
