import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import CircularProgress from "@mui/material/CircularProgress";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemText from "@mui/material/ListItemText";
import ListSubheader from "@mui/material/ListSubheader";
import Tooltip from "@mui/material/Tooltip";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import type { SessionInfo, SessionsListResult } from "../types";
import type { useBackend } from "../hooks/useBackend";

dayjs.extend(relativeTime);

interface SessionsManagerProps {
  backend: ReturnType<typeof useBackend>;
}

function shortenUserAgent(userAgent: string): string {
  if (!userAgent) return "-";
  if (userAgent.length > 60) return userAgent.slice(0, 57) + "...";
  return userAgent;
}

function SessionItem({
  session,
  onRevoke,
}: {
  session: SessionInfo;
  onRevoke?: () => void;
}) {
  const lastSeen = dayjs(session.lastSeenAt);
  return (
    <ListItem
      disableGutters
      secondaryAction={
        onRevoke ? (
          <IconButton
            size="small"
            edge="end"
            sx={{ opacity: 0.4 }}
            onClick={onRevoke}
          >
            <DeleteOutlineIcon fontSize="small" />
          </IconButton>
        ) : undefined
      }
    >
      <ListItemText
        primary={
          <Typography variant="body2">
            {shortenUserAgent(session.userAgent)}
          </Typography>
        }
        secondary={
          <>
            {session.remoteAddr}
            {" · "}
            <Tooltip title={lastSeen.format("YYYY-MM-DD HH:mm:ss")} arrow>
              <span>{lastSeen.fromNow()}</span>
            </Tooltip>
          </>
        }
      />
    </ListItem>
  );
}

export default function SessionsManager({ backend }: SessionsManagerProps) {
  const { t } = useTranslation();
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState("");
  const [loading, setLoading] = useState(true);

  function loadSessions() {
    setLoading(true);
    backend
      .sendRpc<SessionsListResult>("sessions.list", {})
      .then((result) => {
        setSessions(result.sessions || []);
        setCurrentSessionId(result.currentSessionId || "");
      })
      .catch((error) => console.error("sessions.list:", error))
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    if (backend.connected) loadSessions();
  }, [backend.connected]);

  function handleRevoke(sessionId: string) {
    backend
      .sendRpc("sessions.revoke", { sessionId })
      .then(() => {
        setSessions((previous) =>
          previous.filter((session) => session.id !== sessionId),
        );
      })
      .catch((error) => console.error("sessions.revoke:", error));
  }

  if (loading) {
    return (
      <Box sx={{ p: 3, textAlign: "center" }}>
        <CircularProgress size={24} />
      </Box>
    );
  }

  const currentSession = sessions.find(
    (session) => session.id === currentSessionId,
  );
  const otherSessions = sessions
    .filter((session) => session.id !== currentSessionId)
    .sort(
      (a, b) =>
        new Date(b.lastSeenAt).getTime() - new Date(a.lastSeenAt).getTime(),
    );

  return (
    <Box>
      <Box sx={{ mb: 3 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
          {t("auth.sessions")}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {t("auth.sessionsDescription")}
        </Typography>
      </Box>
      {sessions.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {t("auth.noSessions")}
        </Typography>
      ) : (
        <>
          {currentSession && (
            <List
              disablePadding
              subheader={
                <ListSubheader disableGutters disableSticky>
                  {t("auth.currentSession")}
                </ListSubheader>
              }
            >
              <SessionItem session={currentSession} />
            </List>
          )}
          {otherSessions.length > 0 && (
            <List
              disablePadding
              subheader={
                <ListSubheader disableGutters disableSticky>
                  {t("auth.otherSessions")}
                </ListSubheader>
              }
            >
              {otherSessions.map((session) => (
                <SessionItem
                  key={session.id}
                  session={session}
                  onRevoke={() => handleRevoke(session.id)}
                />
              ))}
            </List>
          )}
        </>
      )}
    </Box>
  );
}
