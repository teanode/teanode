import React, { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemText from "@mui/material/ListItemText";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import TextField from "@mui/material/TextField";
import CheckIcon from "@mui/icons-material/Check";
import SaveIcon from "@mui/icons-material/Save";
import ClearIcon from "@mui/icons-material/Clear";
import Chip from "@mui/material/Chip";
import { useAlert } from "../../components/AlertProvider";
import { useAppContext } from "../../context";

interface SecretEntry {
  key: string;
  description?: string;
  skills: string[];
  configured: boolean;
}

export default function SettingsSecretsPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const [secrets, setSecrets] = useState<SecretEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [values, setValues] = useState<Record<string, string>>({});
  const [savedKeys, setSavedKeys] = useState<Set<string>>(new Set());

  const loadSecrets = useCallback(async () => {
    if (!backend.connected) return;
    setLoading(true);
    try {
      const result = await backend.sendRpc<{ secrets: SecretEntry[] }>(
        "secrets.list",
        {},
      );
      setSecrets(result.secrets || []);
    } catch (error) {
      console.error("secrets.list:", error);
    } finally {
      setLoading(false);
    }
  }, [backend]);

  useEffect(() => {
    void loadSecrets();
  }, [loadSecrets]);

  const saveSecret = useCallback(
    async (key: string) => {
      const value = values[key] ?? "";
      try {
        await backend.sendRpc("secrets.set", { key, value });
        showAlert(t("settings.secretSaved"));
        setSavedKeys((prev) => new Set(prev).add(key));
        setTimeout(() => {
          setSavedKeys((prev) => {
            const next = new Set(prev);
            next.delete(key);
            return next;
          });
        }, 1500);
        setValues((prev) => {
          const next = { ...prev };
          delete next[key];
          return next;
        });
        await loadSecrets();
      } catch (err) {
        showAlert(
          err instanceof Error ? err.message : t("settings.secretSaveFailed"),
          "error",
        );
      }
    },
    [backend, values, loadSecrets, showAlert, t],
  );

  const clearSecret = useCallback(
    async (key: string) => {
      try {
        await backend.sendRpc("secrets.set", { key, value: "" });
        showAlert(t("settings.secretCleared"));
        setValues((prev) => {
          const next = { ...prev };
          delete next[key];
          return next;
        });
        await loadSecrets();
      } catch (err) {
        showAlert(
          err instanceof Error ? err.message : t("settings.secretClearFailed"),
          "error",
        );
      }
    },
    [backend, loadSecrets, showAlert, t],
  );

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
            {t("settings.secrets")}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {t("settings.secretsDescription")}
          </Typography>
        </Box>

        <Box sx={{ mt: 3 }}>
          {!loading && secrets.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              {t("settings.noSecretsRequired")}
            </Typography>
          ) : (
            <List disablePadding>
              {secrets.map((secret) => {
                const hasInput = (values[secret.key] ?? "").length > 0;
                const justSaved = savedKeys.has(secret.key);
                return (
                  <ListItem
                    key={secret.key}
                    disableGutters
                    sx={{
                      flexDirection: "column",
                      alignItems: "stretch",
                      py: 1.5,
                    }}
                  >
                    <Box
                      sx={{
                        display: "flex",
                        alignItems: "center",
                        gap: 1,
                        mb: 0.5,
                      }}
                    >
                      <ListItemText
                        primary={
                          <Typography variant="body2" sx={{ fontWeight: 600 }}>
                            {secret.key}
                          </Typography>
                        }
                        secondary={secret.description}
                        secondaryTypographyProps={{
                          variant: "caption",
                          color: "text.secondary",
                        }}
                        sx={{ m: 0 }}
                      />
                      <Chip
                        label={
                          secret.configured
                            ? t("settings.secretConfigured")
                            : t("settings.secretMissing")
                        }
                        size="small"
                        color={secret.configured ? "success" : "warning"}
                        variant="outlined"
                        sx={{ fontSize: "11px", height: 22 }}
                      />
                    </Box>

                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{ mb: 1, display: "block" }}
                    >
                      {t("settings.secretUsedBy", {
                        skills: secret.skills.join(", "),
                      })}
                    </Typography>

                    <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                      <TextField
                        size="small"
                        type="password"
                        placeholder={
                          secret.configured ? "••••••••" : "Enter value..."
                        }
                        value={values[secret.key] ?? ""}
                        onChange={(e) =>
                          setValues((prev) => ({
                            ...prev,
                            [secret.key]: e.target.value,
                          }))
                        }
                        sx={{ flex: 1 }}
                        inputProps={{ autoComplete: "off" }}
                      />
                      <Tooltip
                        title={justSaved ? t("common.save") : t("common.save")}
                      >
                        <span>
                          <IconButton
                            size="small"
                            color={justSaved ? "success" : "primary"}
                            onClick={() => void saveSecret(secret.key)}
                            disabled={!hasInput}
                          >
                            {justSaved ? (
                              <CheckIcon fontSize="small" />
                            ) : (
                              <SaveIcon fontSize="small" />
                            )}
                          </IconButton>
                        </span>
                      </Tooltip>
                      {secret.configured && (
                        <Tooltip title={t("common.remove")}>
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => void clearSecret(secret.key)}
                          >
                            <ClearIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      )}
                    </Box>
                  </ListItem>
                );
              })}
            </List>
          )}
        </Box>
      </Container>
    </Box>
  );
}
