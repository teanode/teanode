import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import CircularProgress from "@mui/material/CircularProgress";
import Logo from "./Logo";
import { authLogin, authSetup } from "../rpc";

interface LoginPageProps {
  mode: "login" | "setup";
  onSuccess: () => void;
}

export default function LoginPage({ mode, onSuccess }: LoginPageProps) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");

    if (mode === "setup") {
      if (password.length < 8) {
        setError(t("auth.passwordMinLength"));
        return;
      }
      if (password !== confirmPassword) {
        setError(t("auth.passwordsDoNotMatch"));
        return;
      }
    }

    setLoading(true);
    try {
      if (mode === "setup") {
        await authSetup(password);
      } else {
        await authLogin(password);
      }
      onSuccess();
    } catch (err) {
      setError(err instanceof Error ? err.message : "An error occurred");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        minHeight: "100vh",
        bgcolor: "background.default",
      }}
    >
      <Box
        component="form"
        onSubmit={handleSubmit}
        sx={{
          width: 360,
          p: 4,
          bgcolor: "background.paper",
          borderRadius: 3,
          border: 1,
          borderColor: "divider",
          textAlign: "center",
        }}
      >
        <Box sx={{ mb: 2, display: "flex", justifyContent: "center" }}>
          <Logo size={80} />
        </Box>

        <Typography variant="h6" sx={{ mb: 0.5 }}>
          {mode === "setup" ? t("auth.setupTitle") : t("auth.loginTitle")}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          {mode === "setup"
            ? t("auth.setupDescription")
            : t("auth.loginDescription")}
        </Typography>

        {error && (
          <Alert severity="error" sx={{ mb: 2, textAlign: "left" }}>
            {error}
          </Alert>
        )}

        <TextField
          fullWidth
          type="password"
          label={t("auth.password")}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoFocus
          autoComplete={mode === "setup" ? "new-password" : "current-password"}
          sx={{ mb: 2 }}
        />

        {mode === "setup" && (
          <TextField
            fullWidth
            type="password"
            label={t("auth.confirmPassword")}
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            autoComplete="new-password"
            sx={{ mb: 2 }}
          />
        )}

        <Button
          type="submit"
          variant="contained"
          fullWidth
          disabled={loading || !password}
          sx={{ py: 1.2 }}
        >
          {loading ? (
            <CircularProgress size={20} color="inherit" />
          ) : mode === "setup" ? (
            t("auth.setUp")
          ) : (
            t("auth.login")
          )}
        </Button>
      </Box>
    </Box>
  );
}
