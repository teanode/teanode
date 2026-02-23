import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Container from "@mui/material/Container";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import Paper from "@mui/material/Paper";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import AddCircleOutlineIcon from "@mui/icons-material/AddCircleOutline";
import AdminPanelSettingsOutlinedIcon from "@mui/icons-material/AdminPanelSettingsOutlined";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import EditOutlinedIcon from "@mui/icons-material/EditOutlined";
import PersonOutlineIcon from "@mui/icons-material/PersonOutline";
import { useAppContext } from "../../context";
import ConfirmDialog from "../../components/ConfirmDialog";
import { withToken } from "../../rpc";
import type { UserInfo } from "../../types";

export default function SettingsUsersPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const [users, setUsers] = useState<UserInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [createUsername, setCreateUsername] = useState("");
  const [createName, setCreateName] = useState("");
  const [createDescription, setCreateDescription] = useState("");
  const [createdCredentials, setCreatedCredentials] = useState<{
    username: string;
    password: string;
  } | null>(null);
  const [pendingDelete, setPendingDelete] = useState<UserInfo | null>(null);
  const [editingUser, setEditingUser] = useState<UserInfo | null>(null);
  const [editUsername, setEditUsername] = useState("");
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [editPassword, setEditPassword] = useState("");

  const loadUsers = useCallback(() => {
    backend
      .sendRpc<{ users: UserInfo[] }>("users.list", {})
      .then((result) => setUsers(result.users || []))
      .catch((err: unknown) =>
        setError(err instanceof Error ? err.message : "Failed to load users"),
      );
  }, [backend]);

  useEffect(() => {
    if (backend.connected && backend.isAdmin) {
      loadUsers();
    }
  }, [backend.connected, backend.isAdmin, loadUsers]);

  const sortedUsers = useMemo(
    () => [...users].sort((a, b) => a.username.localeCompare(b.username)),
    [users],
  );

  const generatePassword = useCallback((length = 18) => {
    const alphabet =
      "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*";
    const randomBytes = new Uint32Array(length);
    crypto.getRandomValues(randomBytes);
    let password = "";
    for (let index = 0; index < length; index += 1) {
      password += alphabet[randomBytes[index] % alphabet.length];
    }
    return password;
  }, []);

  const createUser = useCallback(async () => {
    setError(null);
    if (!createUsername.trim()) {
      setError(t("auth.usernameRequired"));
      return;
    }
    const generatedPassword = generatePassword();
    try {
      await backend.sendRpc("users.create", {
        username: createUsername.trim(),
        name: createName.trim() || undefined,
        description: createDescription.trim() || undefined,
        password: generatedPassword,
      });
      setCreatedCredentials({
        username: createUsername.trim(),
        password: generatedPassword,
      });
      setCreateUsername("");
      setCreateName("");
      setCreateDescription("");
      loadUsers();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create user");
    }
  }, [
    backend,
    createDescription,
    createName,
    createUsername,
    generatePassword,
    loadUsers,
    t,
  ]);

  const deleteUser = useCallback(async () => {
    if (!pendingDelete) return;
    try {
      await backend.sendRpc("users.delete", { userId: pendingDelete.id });
      setPendingDelete(null);
      loadUsers();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete user");
    }
  }, [backend, loadUsers, pendingDelete]);

  const openEditModal = useCallback((user: UserInfo) => {
    setEditingUser(user);
    setEditUsername(user.username);
    setEditName(user.name || "");
    setEditDescription(user.description || "");
    setEditPassword("");
  }, []);

  const closeEditModal = useCallback(() => {
    setEditingUser(null);
    setEditPassword("");
  }, []);

  const saveUserEdits = useCallback(async () => {
    if (!editingUser) return;
    setError(null);
    if (editingUser.id === backend.currentUserId) {
      setError(t("settings.cannotDeleteCurrentUser"));
      return;
    }
    if (!editUsername.trim()) {
      setError(t("auth.usernameRequired"));
      return;
    }
    if (editPassword.trim() && editPassword.length < 8) {
      setError(t("auth.passwordMinLength"));
      return;
    }
    try {
      await backend.sendRpc("users.update", {
        userId: editingUser.id,
        username: editUsername.trim(),
        name: editName.trim() || undefined,
        description: editDescription.trim(),
        ...(editPassword.trim() ? { newPassword: editPassword } : {}),
      });
      closeEditModal();
      loadUsers();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update user");
    }
  }, [
    backend,
    closeEditModal,
    editDescription,
    editName,
    editingUser,
    editPassword,
    editUsername,
    loadUsers,
    t,
  ]);

  const toggleUserRole = useCallback(
    async (user: UserInfo) => {
      if (user.id === backend.currentUserId) {
        setError(t("settings.cannotDeleteCurrentUser"));
        return;
      }
      try {
        await backend.sendRpc("users.setRole", {
          userId: user.id,
          admin: !user.admin,
        });
        loadUsers();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to change role");
      }
    },
    [backend, loadUsers, t],
  );

  if (!backend.isAdmin) {
    return (
      <Box sx={{ flex: 1, display: "flex", alignItems: "center", p: 3 }}>
        <Typography variant="body2" color="text.secondary">
          {t("settings.usersAdminOnly")}
        </Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Box sx={{ mb: 1 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {t("settings.users")}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {t("settings.usersDescription")}
            </Typography>
          </Box>

          {sortedUsers.map((user) => {
            const isCurrentUser = user.id === backend.currentUserId;
            const displayName = user.name || user.username;
            const fallback = displayName.trim().charAt(0).toUpperCase() || "U";
            return (
              <Paper key={user.id} variant="outlined" sx={{ p: 2 }}>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: 1.5,
                  }}
                >
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "center",
                      gap: 1.5,
                      minWidth: 0,
                      flex: 1,
                    }}
                  >
                    <Avatar
                      src={
                        user.avatarMediaId
                          ? withToken(`/api/v1/media/${user.avatarMediaId}`)
                          : undefined
                      }
                      sx={{ width: 40, height: 40 }}
                    >
                      {fallback}
                    </Avatar>
                    <Box sx={{ minWidth: 0 }}>
                      <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
                        {displayName}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        @{user.username}
                      </Typography>
                      {user.description && (
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          sx={{
                            display: "block",
                            maxWidth: "100%",
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                          }}
                        >
                          {user.description}
                        </Typography>
                      )}
                    </Box>
                  </Box>
                  <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
                    <Tooltip
                      title={
                        isCurrentUser
                          ? t("settings.cannotDeleteCurrentUser")
                          : user.admin
                            ? t("settings.roleAdmin")
                            : t("settings.roleUser")
                      }
                    >
                      <span>
                        <IconButton
                          size="small"
                          disabled={isCurrentUser}
                          onClick={() => toggleUserRole(user)}
                        >
                          {user.admin ? (
                            <AdminPanelSettingsOutlinedIcon fontSize="small" />
                          ) : (
                            <PersonOutlineIcon fontSize="small" />
                          )}
                        </IconButton>
                      </span>
                    </Tooltip>
                    <Tooltip
                      title={
                        isCurrentUser
                          ? t("settings.cannotDeleteCurrentUser")
                          : t("common.edit")
                      }
                    >
                      <span>
                        <IconButton
                          size="small"
                          disabled={isCurrentUser}
                          onClick={() => openEditModal(user)}
                        >
                          <EditOutlinedIcon fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                    <Tooltip
                      title={
                        isCurrentUser
                          ? t("settings.cannotDeleteCurrentUser")
                          : t("common.delete")
                      }
                    >
                      <span>
                        <IconButton
                          size="small"
                          disabled={isCurrentUser}
                          onClick={() => setPendingDelete(user)}
                        >
                          <DeleteOutlineIcon fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                  </Box>
                </Box>
              </Paper>
            );
          })}

          <Paper variant="outlined" sx={{ p: 2 }}>
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 1.5,
                mb: 1.5,
              }}
            >
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 1.5,
                  minWidth: 0,
                  flex: 1,
                }}
              >
                <Avatar sx={{ width: 40, height: 40 }}>+</Avatar>
                <Box sx={{ minWidth: 0, flex: 1 }}>
                  <TextField
                    variant="standard"
                    size="small"
                    placeholder={t("auth.name")}
                    value={createName}
                    onChange={(event) => setCreateName(event.target.value)}
                    InputProps={{ disableUnderline: true }}
                    sx={{
                      width: "100%",
                      "& .MuiInputBase-input": {
                        fontSize: "0.95rem",
                        fontWeight: 600,
                        py: 0.1,
                      },
                    }}
                  />
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "center",
                      color: "text.secondary",
                    }}
                  >
                    <Typography variant="caption" sx={{ mr: 0.25 }}>
                      @
                    </Typography>
                    <TextField
                      variant="standard"
                      size="small"
                      placeholder={t("auth.username")}
                      value={createUsername}
                      onChange={(event) =>
                        setCreateUsername(event.target.value)
                      }
                      InputProps={{ disableUnderline: true }}
                      sx={{
                        width: "100%",
                        "& .MuiInputBase-input": {
                          fontSize: "0.75rem",
                          color: "text.secondary",
                          py: 0,
                        },
                      }}
                    />
                  </Box>
                </Box>
              </Box>
              <Tooltip title={t("common.add")}>
                <IconButton size="small" onClick={createUser}>
                  <AddCircleOutlineIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            </Box>
            <TextField
              size="small"
              multiline
              minRows={2}
              placeholder={t("settings.userDescription")}
              value={createDescription}
              onChange={(event) => setCreateDescription(event.target.value)}
              sx={{ mt: 0.5 }}
            />
          </Paper>

          {error && (
            <Typography variant="caption" color="error">
              {error}
            </Typography>
          )}
        </Box>
      </Container>

      <Dialog
        open={!!editingUser}
        onClose={closeEditModal}
        fullWidth
        maxWidth="sm"
      >
        <DialogTitle>{t("common.edit")}</DialogTitle>
        <DialogContent sx={{ pt: 1.5 }}>
          <Box sx={{ display: "grid", gap: 1.5 }}>
            <TextField
              size="small"
              label={t("auth.username")}
              value={editUsername}
              onChange={(event) => setEditUsername(event.target.value)}
            />
            <TextField
              size="small"
              label={t("auth.name")}
              value={editName}
              onChange={(event) => setEditName(event.target.value)}
            />
            <TextField
              size="small"
              label={t("settings.userDescription")}
              value={editDescription}
              onChange={(event) => setEditDescription(event.target.value)}
              multiline
              minRows={3}
            />
            <TextField
              size="small"
              label={t("settings.newPasswordForUser")}
              type="password"
              value={editPassword}
              onChange={(event) => setEditPassword(event.target.value)}
              helperText={t("auth.passwordMinLength")}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={closeEditModal}>{t("common.cancel")}</Button>
          <Button variant="contained" onClick={saveUserEdits}>
            {t("common.save")}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={!!createdCredentials}
        onClose={() => setCreatedCredentials(null)}
        fullWidth
        maxWidth="sm"
      >
        <DialogTitle>{t("settings.addUser")}</DialogTitle>
        <DialogContent sx={{ pt: 1.5 }}>
          <Typography variant="body2" sx={{ mb: 1.5 }}>
            Username: {createdCredentials?.username}
          </Typography>
          <TextField
            fullWidth
            size="small"
            label={t("auth.password")}
            value={createdCredentials?.password || ""}
            InputProps={{ readOnly: true }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreatedCredentials(null)}>
            {t("common.cancel")}
          </Button>
        </DialogActions>
      </Dialog>

      <ConfirmDialog
        open={!!pendingDelete}
        title={t("settings.deleteUser")}
        message={t("settings.deleteUserConfirm", {
          name: pendingDelete?.name || pendingDelete?.username || "",
        })}
        confirmLabel={t("common.delete")}
        onConfirm={deleteUser}
        onClose={() => setPendingDelete(null)}
      />
    </Box>
  );
}
