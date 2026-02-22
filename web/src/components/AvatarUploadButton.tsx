import React, { useRef } from "react";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import { withToken } from "../rpc";

interface AvatarUploadButtonProps {
  avatarMediaId?: string;
  fallback: string;
  busy?: boolean;
  size?: number;
  onUpload: (file: File) => Promise<void> | void;
  onRemove: () => Promise<void> | void;
}

export default function AvatarUploadButton({
  avatarMediaId,
  fallback,
  busy = false,
  size = 48,
  onUpload,
  onRemove,
}: AvatarUploadButtonProps) {
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  async function handleClick() {
    if (busy) return;
    if (avatarMediaId) {
      await onRemove();
      return;
    }
    fileInputRef.current?.click();
  }

  async function handleInputChange(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      await onUpload(file);
    } finally {
      event.target.value = "";
    }
  }

  return (
    <>
      <IconButton
        onClick={handleClick}
        sx={{ p: 0, borderRadius: "50%" }}
        disabled={busy}
      >
        <Box sx={{ position: "relative" }}>
          <Avatar
            src={
              avatarMediaId
                ? withToken(`/api/v1/media/${avatarMediaId}`)
                : undefined
            }
            sx={{ width: size, height: size }}
          >
            {busy ? <CircularProgress size={18} /> : fallback}
          </Avatar>
          {!busy && avatarMediaId && (
            <Box
              sx={{
                position: "absolute",
                right: -2,
                bottom: -2,
                width: 18,
                height: 18,
                borderRadius: "50%",
                bgcolor: "error.main",
                color: "common.white",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                border: 1,
                borderColor: "background.paper",
              }}
            >
              <DeleteOutlineIcon sx={{ fontSize: 12 }} />
            </Box>
          )}
        </Box>
      </IconButton>
      <input
        hidden
        type="file"
        accept="image/*"
        ref={fileInputRef}
        onChange={handleInputChange}
      />
    </>
  );
}
