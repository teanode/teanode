import React from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import MenuItem from "@mui/material/MenuItem";
import Paper from "@mui/material/Paper";
import Select from "@mui/material/Select";
import Slider from "@mui/material/Slider";
import Typography from "@mui/material/Typography";
import Switch from "@mui/material/Switch";
import FormControlLabel from "@mui/material/FormControlLabel";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import DarkModeIcon from "@mui/icons-material/DarkMode";
import LightModeIcon from "@mui/icons-material/LightMode";
import SettingsBrightnessIcon from "@mui/icons-material/SettingsBrightness";
import { useAppContext, type ThemeMode } from "../context";
import type { LanguagePreference } from "../i18n/config";

export default function PreferencesArea() {
  const { t } = useTranslation();
  const {
    themeMode,
    setThemeMode,
    showToolCalls,
    setShowToolCalls,
    showTokenUsage,
    setShowTokenUsage,
    voiceAutoSend,
    setVoiceAutoSend,
    ttsVoice,
    setTtsVoice,
    voiceChimesEnabled,
    setVoiceChimesEnabled,
    voiceChimesVolume,
    setVoiceChimesVolume,
    voiceCallSttMode,
    setVoiceCallSttMode,
    languagePreference,
    setLanguagePreference,
    backend,
  } = useAppContext();

  const voiceAvailable = backend.audioCapability;

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ mb: 3 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
            {t("settings.preferences")}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {t("settings.preferencesDescription")}
          </Typography>
        </Box>

        {/* Theme */}
        <Paper variant="outlined" sx={{ p: 2, mb: 2 }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
            {t("settings.language")}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            {t("settings.languageDescription")}
          </Typography>
          <Select
            value={languagePreference}
            onChange={(event) =>
              setLanguagePreference(event.target.value as LanguagePreference)
            }
            size="small"
            sx={{ minWidth: 220 }}
          >
            <MenuItem value="auto">
              {t("settings.languageAuto", {
                locale:
                  typeof navigator !== "undefined" ? navigator.language : "en",
              })}
            </MenuItem>
            <MenuItem value="en">English</MenuItem>
            <MenuItem value="zh">中文</MenuItem>
            <MenuItem value="ja">日本語</MenuItem>
          </Select>
        </Paper>

        {/* Theme */}
        <Paper variant="outlined" sx={{ p: 2, mb: 2 }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
            {t("settings.theme")}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            {t("settings.themeDescription")}
          </Typography>
          <ToggleButtonGroup
            value={themeMode}
            exclusive
            onChange={(_event, value: ThemeMode | null) => {
              if (value) setThemeMode(value);
            }}
            size="small"
          >
            <ToggleButton value="dark" aria-label={t("settings.darkMode")}>
              <DarkModeIcon sx={{ fontSize: 18 }} />
            </ToggleButton>
            <ToggleButton value="light" aria-label={t("settings.lightMode")}>
              <LightModeIcon sx={{ fontSize: 18 }} />
            </ToggleButton>
            <ToggleButton value="system" aria-label={t("settings.systemMode")}>
              <SettingsBrightnessIcon sx={{ fontSize: 18 }} />
            </ToggleButton>
          </ToggleButtonGroup>
        </Paper>

        {/* Display */}
        <Paper variant="outlined" sx={{ p: 2 }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
            {t("settings.display")}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            {t("settings.displayDescription")}
          </Typography>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
            <FormControlLabel
              control={
                <Switch
                  checked={showToolCalls}
                  onChange={(event) => setShowToolCalls(event.target.checked)}
                  color="primary"
                />
              }
              label={
                <Box>
                  <Typography variant="body2" sx={{ fontWeight: 500 }}>
                    {t("settings.showToolCalls")}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {t("settings.showToolCallsDescription")}
                  </Typography>
                </Box>
              }
              sx={{ alignItems: "flex-start", ml: 0 }}
            />
            <FormControlLabel
              control={
                <Switch
                  checked={showTokenUsage}
                  onChange={(event) => setShowTokenUsage(event.target.checked)}
                  color="primary"
                />
              }
              label={
                <Box>
                  <Typography variant="body2" sx={{ fontWeight: 500 }}>
                    {t("settings.showTokenUsage")}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {t("settings.showTokenUsageDescription")}
                  </Typography>
                </Box>
              }
              sx={{ alignItems: "flex-start", ml: 0 }}
            />
          </Box>
        </Paper>

        {/* Voice */}
        {voiceAvailable && (
          <Paper variant="outlined" sx={{ p: 2, mt: 2 }}>
            <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
              {t("settings.voice")}
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
              {t("settings.voiceDescription")}
            </Typography>
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <FormControlLabel
                control={
                  <Switch
                    checked={voiceAutoSend}
                    onChange={(event) => setVoiceAutoSend(event.target.checked)}
                    color="primary"
                  />
                }
                label={
                  <Box>
                    <Typography variant="body2" sx={{ fontWeight: 500 }}>
                      {t("settings.voiceAutoSend")}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {t("settings.voiceAutoSendDescription")}
                    </Typography>
                  </Box>
                }
                sx={{ alignItems: "flex-start", ml: 0 }}
              />
              <Box sx={{ mt: 1 }}>
                <Typography variant="body2" sx={{ fontWeight: 500, mb: 0.5 }}>
                  {t("settings.ttsVoice")}
                </Typography>
                <Select
                  value={ttsVoice}
                  onChange={(event) => setTtsVoice(event.target.value)}
                  size="small"
                  sx={{ minWidth: 140 }}
                >
                  {["alloy", "echo", "fable", "onyx", "nova", "shimmer"].map(
                    (v) => (
                      <MenuItem key={v} value={v}>
                        {v.charAt(0).toUpperCase() + v.slice(1)}
                      </MenuItem>
                    ),
                  )}
                </Select>
              </Box>
              <FormControlLabel
                control={
                  <Switch
                    checked={voiceCallSttMode === "client"}
                    onChange={(event) =>
                      setVoiceCallSttMode(
                        event.target.checked ? "client" : "server",
                      )
                    }
                    color="primary"
                  />
                }
                label={
                  <Box>
                    <Typography variant="body2" sx={{ fontWeight: 500 }}>
                      {t("settings.voiceCallSttMode")}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {t("settings.voiceCallSttModeDescription")}
                    </Typography>
                  </Box>
                }
                sx={{ alignItems: "flex-start", ml: 0, mt: 1 }}
              />

              {/* Voice call indicator chimes */}
              <FormControlLabel
                control={
                  <Switch
                    checked={voiceChimesEnabled}
                    onChange={(event) =>
                      setVoiceChimesEnabled(event.target.checked)
                    }
                    color="primary"
                  />
                }
                label={
                  <Box>
                    <Typography variant="body2" sx={{ fontWeight: 500 }}>
                      {t("settings.voiceChimes")}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {t("settings.voiceChimesDescription")}
                    </Typography>
                  </Box>
                }
                sx={{ alignItems: "flex-start", ml: 0, mt: 1 }}
              />
              {voiceChimesEnabled && (
                <Box
                  sx={{
                    pl: 1,
                    mt: 1,
                    display: "flex",
                    flexDirection: "column",
                    gap: 1.5,
                  }}
                >
                  <Box>
                    <Typography
                      variant="body2"
                      sx={{ fontWeight: 500, mb: 0.5 }}
                    >
                      {t("settings.voiceChimesVolume")}
                    </Typography>
                    <Slider
                      value={voiceChimesVolume}
                      onChange={(_event, value) =>
                        setVoiceChimesVolume(value as number)
                      }
                      min={0}
                      max={1}
                      step={0.05}
                      size="small"
                      valueLabelDisplay="auto"
                      valueLabelFormat={(value) =>
                        `${Math.round(value * 100)}%`
                      }
                      sx={{ maxWidth: 200 }}
                    />
                  </Box>
                </Box>
              )}
            </Box>
          </Paper>
        )}
      </Container>
    </Box>
  );
}
