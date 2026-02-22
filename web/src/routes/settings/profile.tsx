import React, { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import CircularProgress from "@mui/material/CircularProgress";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import AvatarUploadButton from "../../components/AvatarUploadButton";
import {
  profileGetRpc,
  profileUpdateRpc,
  removeProfileAvatarRpc,
  uploadProfileAvatar,
} from "../../rpc";
import type { Profile } from "../../types";

export default function SettingsProfilePage() {
  const { t } = useTranslation();
  const [profile, setProfile] = useState<Profile>({
    name: "",
    bio: "",
    avatarMediaId: "",
  });
  const [name, setName] = useState("");
  const [bio, setBio] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [avatarBusy, setAvatarBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  useEffect(() => {
    setLoading(true);
    profileGetRpc()
      .then((loaded) => {
        setProfile(loaded);
        setName(loaded.name || "");
        setBio(loaded.bio || "");
      })
      .catch((err) =>
        setError(err instanceof Error ? err.message : "Failed to load profile"),
      )
      .finally(() => setLoading(false));
  }, []);

  const normalizedName = name.trim();
  const normalizedBio = bio.trim();
  const dirty = useMemo(
    () =>
      normalizedName !== (profile.name || "").trim() ||
      normalizedBio !== (profile.bio || "").trim(),
    [normalizedName, normalizedBio, profile.name, profile.bio],
  );

  async function handleSave() {
    setError("");
    setSuccess("");
    if (!normalizedName) {
      setError(t("settings.profileNameRequired"));
      return;
    }
    setSaving(true);
    try {
      const saved = await profileUpdateRpc({
        name: normalizedName,
        bio: normalizedBio,
      });
      setProfile(saved);
      setName(saved.name || "");
      setBio(saved.bio || "");
      setSuccess(t("settings.profileSaved"));
    } catch (err) {
      setError(
        err instanceof Error ? err.message : t("settings.profileSaveFailed"),
      );
    } finally {
      setSaving(false);
    }
  }

  async function handleAvatarUpload(file: File) {
    setError("");
    setSuccess("");
    setAvatarBusy(true);
    try {
      const saved = await uploadProfileAvatar(file);
      setProfile(saved);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : t("settings.profileSaveFailed"),
      );
    } finally {
      setAvatarBusy(false);
    }
  }

  async function handleAvatarRemove() {
    setError("");
    setSuccess("");
    setAvatarBusy(true);
    try {
      const saved = await removeProfileAvatarRpc();
      setProfile(saved);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : t("settings.profileSaveFailed"),
      );
    } finally {
      setAvatarBusy(false);
    }
  }

  if (loading) {
    return (
      <Box
        sx={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ mb: 1 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
            {t("settings.profile")}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {t("settings.profileDescription")}
          </Typography>
        </Box>

        <Paper variant="outlined" sx={{ p: 2 }}>
          {error && (
            <Alert severity="error" sx={{ mb: 1.5, fontSize: "0.8rem" }}>
              {error}
            </Alert>
          )}
          {success && (
            <Alert severity="success" sx={{ mb: 1.5, fontSize: "0.8rem" }}>
              {success}
            </Alert>
          )}

          <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
              <AvatarUploadButton
                avatarMediaId={profile.avatarMediaId}
                fallback={(normalizedName || "?").charAt(0).toUpperCase()}
                busy={avatarBusy}
                onUpload={handleAvatarUpload}
                onRemove={handleAvatarRemove}
              />
              <TextField
                variant="standard"
                size="small"
                value={name}
                placeholder={t("auth.name")}
                onChange={(event) => setName(event.target.value)}
                InputProps={{ disableUnderline: true }}
                sx={{
                  minWidth: { xs: 0, sm: 220 },
                  width: "100%",
                  maxWidth: "100%",
                  "& .MuiInputBase-input": {
                    fontSize: "0.95rem",
                    fontWeight: 600,
                    py: 0.25,
                  },
                }}
              />
            </Box>

            <TextField
              label={t("settings.profileBio")}
              value={bio}
              onChange={(event) => setBio(event.target.value)}
              size="small"
              fullWidth
              multiline
              minRows={3}
            />

            <Box>
              <Button
                variant="contained"
                size="small"
                disabled={saving || !normalizedName || !dirty}
                onClick={handleSave}
              >
                {saving ? t("common.saving") : t("common.save")}
              </Button>
            </Box>
          </Box>
        </Paper>
      </Container>
    </Box>
  );
}
