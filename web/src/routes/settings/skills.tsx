import React, { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemText from "@mui/material/ListItemText";
import ListSubheader from "@mui/material/ListSubheader";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import CircularProgress from "@mui/material/CircularProgress";
import Switch from "@mui/material/Switch";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import UpdateIcon from "@mui/icons-material/Update";
import CheckIcon from "@mui/icons-material/Check";
import { useAppContext } from "../../context";
import { useAlert } from "../../components/AlertProvider";
import { setInstalledSkillEnabled } from "./skills.helpers";

interface InstalledSkill {
  name: string;
  description?: string;
  version: string;
  enabled?: boolean;
}

type UpdateIndicatorState = "idle" | "loading" | "success";

export default function SettingsSkillsPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const [installedSkills, setInstalledSkills] = useState<InstalledSkill[]>([]);
  const [loading, setLoading] = useState(false);
  const [busySkillName, setBusySkillName] = useState<string | null>(null);
  const [updateStates, setUpdateStates] = useState<
    Record<string, UpdateIndicatorState>
  >({});

  const loadSkills = useCallback(async () => {
    if (!backend.connected) return;
    setLoading(true);
    try {
      const installedResult = await backend.sendRpc<{
        skills: InstalledSkill[];
      }>("skills.installed.list", {});
      setInstalledSkills(installedResult.skills || []);
    } catch (error) {
      console.error("skills load failed:", error);
    } finally {
      setLoading(false);
    }
  }, [backend]);

  useEffect(() => {
    void loadSkills();
  }, [loadSkills]);

  const setUpdateState = useCallback(
    (key: string, state: UpdateIndicatorState) => {
      setUpdateStates((previous) => ({ ...previous, [key]: state }));
    },
    [],
  );

  const completeUpdateState = useCallback(
    (key: string) => {
      setUpdateState(key, "success");
      window.setTimeout(() => {
        setUpdateState(key, "idle");
      }, 900);
    },
    [setUpdateState],
  );

  const uninstallSkill = useCallback(
    async (name: string) => {
      setBusySkillName(name);
      try {
        await backend.sendRpc("skills.uninstall", { name });
        showAlert(t("settings.skillUninstalled", { name }));
        await loadSkills();
      } catch (error) {
        console.error("skills.uninstall:", error);
        showAlert(t("settings.skillUninstallFailed", { name }), "error");
      } finally {
        setBusySkillName(null);
      }
    },
    [backend, loadSkills, showAlert, t],
  );

  const toggleSkillEnabled = useCallback(
    async (name: string, enabled: boolean) => {
      setBusySkillName(name);
      try {
        await setInstalledSkillEnabled(backend, name, enabled);
        showAlert(
          t(enabled ? "settings.skillEnabled" : "settings.skillDisabled", {
            name,
          }),
        );
        await loadSkills();
      } catch (error) {
        console.error("skills.setEnabled:", error);
        showAlert(
          t(
            enabled
              ? "settings.skillEnableFailed"
              : "settings.skillDisableFailed",
            { name },
          ),
          "error",
        );
      } finally {
        setBusySkillName(null);
      }
    },
    [backend, loadSkills, showAlert, t],
  );

  const checkUpdate = useCallback(
    async (name?: string) => {
      const key = name || "__all__";
      setBusySkillName(key);
      setUpdateState(key, "loading");
      try {
        const result = await backend.sendRpc<{ updated: InstalledSkill[] }>(
          "skills.update",
          name ? { name } : {},
        );
        await loadSkills();
        completeUpdateState(key);
        const updated = result.updated || [];
        if (name) {
          showAlert(
            t(
              updated.length > 0
                ? "settings.skillUpdated"
                : "settings.skillNoUpdate",
              { name },
            ),
          );
        } else {
          showAlert(
            t(
              updated.length > 0
                ? "settings.skillsUpdatedCount"
                : "settings.skillsNoUpdates",
              { count: updated.length },
            ),
          );
        }
      } catch (error) {
        console.error("skills.update:", error);
        setUpdateState(key, "idle");
        showAlert(
          t(
            name ? "settings.skillUpdateFailed" : "settings.skillsUpdateFailed",
            {
              name,
            },
          ),
          "error",
        );
      } finally {
        setBusySkillName(null);
      }
    },
    [backend, completeUpdateState, loadSkills, setUpdateState, showAlert, t],
  );

  const renderUpdateIcon = useCallback(
    (key: string) => {
      const state = updateStates[key] || "idle";
      if (state === "loading") {
        return <CircularProgress size={14} />;
      }
      if (state === "success") {
        return <CheckIcon fontSize="small" color="success" />;
      }
      return <UpdateIcon fontSize="small" />;
    },
    [updateStates],
  );

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box>
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <Box>
              <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
                {t("settings.skills")}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {t("settings.skillsDescription")}
              </Typography>
            </Box>
            <Box sx={{ display: "flex", gap: 1 }}>
              <Tooltip title={t("settings.checkAllUpdates")}>
                <span>
                  <IconButton
                    size="small"
                    onClick={() => void checkUpdate()}
                    disabled={loading || busySkillName !== null}
                  >
                    {renderUpdateIcon("__all__")}
                  </IconButton>
                </span>
              </Tooltip>
            </Box>
          </Box>

          <Box sx={{ mt: 3 }}>
            <List
              disablePadding
              subheader={
                <ListSubheader disableGutters disableSticky>
                  {t("settings.installedSkills")}
                </ListSubheader>
              }
            >
              {installedSkills.length === 0 ? (
                <ListItem disableGutters>
                  <ListItemText
                    primary={
                      <Typography variant="body2" color="text.secondary">
                        {t("settings.noInstalledSkills")}
                      </Typography>
                    }
                  />
                </ListItem>
              ) : (
                installedSkills.map((skill) => (
                  <ListItem
                    key={`${skill.name}-${skill.version}`}
                    disableGutters
                    secondaryAction={
                      <Box
                        sx={{ display: "flex", alignItems: "center", gap: 0.5 }}
                      >
                        <Tooltip
                          title={`${t("settings.skillEnabledToggle")}: ${t(
                            (skill.enabled ?? true)
                              ? "settings.skillEnabled"
                              : "settings.skillDisabled",
                            { name: skill.name },
                          )}`}
                        >
                          <span>
                            <Switch
                              size="small"
                              checked={skill.enabled ?? true}
                              onChange={(_, checked) =>
                                void toggleSkillEnabled(skill.name, checked)
                              }
                              disabled={busySkillName !== null}
                            />
                          </span>
                        </Tooltip>
                        <Tooltip title={t("settings.checkUpdate")}>
                          <span>
                            <IconButton
                              size="small"
                              onClick={() => void checkUpdate(skill.name)}
                              disabled={busySkillName !== null}
                            >
                              {renderUpdateIcon(skill.name)}
                            </IconButton>
                          </span>
                        </Tooltip>
                        <Tooltip title={t("common.delete")}>
                          <span>
                            <IconButton
                              size="small"
                              color="error"
                              onClick={() => void uninstallSkill(skill.name)}
                              disabled={busySkillName !== null}
                            >
                              <DeleteOutlineIcon fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
                      </Box>
                    }
                  >
                    <ListItemText
                      primary={
                        <Typography variant="body2" sx={{ fontWeight: 600 }}>
                          {skill.name}{" "}
                          <Typography
                            component="span"
                            variant="caption"
                            color="text.secondary"
                          >
                            v{skill.version}
                          </Typography>
                        </Typography>
                      }
                      secondary={
                        skill.description ? (
                          <Typography
                            variant="caption"
                            color="text.secondary"
                            sx={{ display: "block" }}
                          >
                            {skill.description}
                          </Typography>
                        ) : undefined
                      }
                    />
                  </ListItem>
                ))
              )}
            </List>
          </Box>
        </Box>
      </Container>
    </Box>
  );
}
