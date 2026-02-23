import React, { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import List from "@mui/material/List";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemText from "@mui/material/ListItemText";
import Divider from "@mui/material/Divider";
import LogoutIcon from "@mui/icons-material/Logout";
import { authLogout } from "../rpc";
import type { SchemaSection, ConfigSchemaResult } from "../types";
import type { useBackend } from "../hooks/useBackend";
import { getSectionTitle } from "../schemaLocalization";
import SidebarSectionTitle from "./SidebarSectionTitle";

interface SettingsNavProps {
  backend: ReturnType<typeof useBackend>;
  activeSectionId: string | null;
  onNavigate: (path: string) => void;
}

export default function SettingsNav({
  backend,
  activeSectionId,
  onNavigate,
}: SettingsNavProps) {
  const { t } = useTranslation();
  const [sections, setSections] = useState<SchemaSection[]>([]);

  useEffect(() => {
    if (!backend.isAdmin) return;
    if (backend.connected && sections.length === 0) {
      backend
        .sendRpc<ConfigSchemaResult>("config.schema", {})
        .then((result) => setSections(result.schema?.["x-sections"] || []))
        .catch((error) => console.error("config.schema:", error));
    }
  }, [backend.connected, backend.isAdmin, backend.sendRpc, sections.length]);

  return (
    <>
      <Box sx={{ flex: 1, overflowY: "auto", p: 1 }}>
        <List disablePadding>
          <ListItemButton
            dense
            onClick={() => onNavigate("/settings/profile")}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeSectionId === "profile"
                ? {
                    bgcolor: "accentDim",
                    color: "#fff",
                    "&:hover": { bgcolor: "accentDim" },
                  }
                : {}),
            }}
          >
            <ListItemText
              primary={t("settings.profile")}
              primaryTypographyProps={{
                variant: "caption",
                fontSize: "13px",
                color:
                  activeSectionId === "profile" ? "#fff" : "text.secondary",
              }}
            />
          </ListItemButton>

          {backend.isAdmin && (
            <ListItemButton
              dense
              onClick={() => onNavigate("/settings/agents")}
              sx={{
                borderRadius: 1,
                mb: 0.25,
                ...(activeSectionId === "agents"
                  ? {
                      bgcolor: "accentDim",
                      color: "#fff",
                      "&:hover": { bgcolor: "accentDim" },
                    }
                  : {}),
              }}
            >
              <ListItemText
                primary={t("settings.agents")}
                primaryTypographyProps={{
                  variant: "caption",
                  fontSize: "13px",
                  color:
                    activeSectionId === "agents" ? "#fff" : "text.secondary",
                }}
              />
            </ListItemButton>
          )}

          <ListItemButton
            dense
            onClick={() => onNavigate("/settings/jobs")}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeSectionId === "jobs"
                ? {
                    bgcolor: "accentDim",
                    color: "#fff",
                    "&:hover": { bgcolor: "accentDim" },
                  }
                : {}),
            }}
          >
            <ListItemText
              primary={t("settings.jobs")}
              primaryTypographyProps={{
                variant: "caption",
                fontSize: "13px",
                color: activeSectionId === "jobs" ? "#fff" : "text.secondary",
              }}
            />
          </ListItemButton>

          {backend.isAdmin && (
            <ListItemButton
              dense
              onClick={() => onNavigate("/settings/projects")}
              sx={{
                borderRadius: 1,
                mb: 0.25,
                ...(activeSectionId === "projects"
                  ? {
                      bgcolor: "accentDim",
                      color: "#fff",
                      "&:hover": { bgcolor: "accentDim" },
                    }
                  : {}),
              }}
            >
              <ListItemText
                primary={t("settings.projects")}
                primaryTypographyProps={{
                  variant: "caption",
                  fontSize: "13px",
                  color:
                    activeSectionId === "projects" ? "#fff" : "text.secondary",
                }}
              />
            </ListItemButton>
          )}

          {backend.isAdmin && (
            <ListItemButton
              dense
              onClick={() => onNavigate("/settings/skills")}
              sx={{
                borderRadius: 1,
                mb: 0.25,
                ...(activeSectionId === "skills"
                  ? {
                      bgcolor: "accentDim",
                      color: "#fff",
                      "&:hover": { bgcolor: "accentDim" },
                    }
                  : {}),
              }}
            >
              <ListItemText
                primary={t("settings.skills")}
                primaryTypographyProps={{
                  variant: "caption",
                  fontSize: "13px",
                  color:
                    activeSectionId === "skills" ? "#fff" : "text.secondary",
                }}
              />
            </ListItemButton>
          )}

          <SidebarSectionTitle>Security</SidebarSectionTitle>
          <ListItemButton
            dense
            onClick={() => onNavigate("/settings/sessions")}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeSectionId === "sessions"
                ? {
                    bgcolor: "accentDim",
                    color: "#fff",
                    "&:hover": { bgcolor: "accentDim" },
                  }
                : {}),
            }}
          >
            <ListItemText
              primary={t("auth.sessions")}
              primaryTypographyProps={{
                variant: "caption",
                fontSize: "13px",
                color:
                  activeSectionId === "sessions" ? "#fff" : "text.secondary",
              }}
            />
          </ListItemButton>

          <ListItemButton
            dense
            onClick={() => onNavigate("/settings/password")}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeSectionId === "password"
                ? {
                    bgcolor: "accentDim",
                    color: "#fff",
                    "&:hover": { bgcolor: "accentDim" },
                  }
                : {}),
            }}
          >
            <ListItemText
              primary={t("auth.passwordTitle")}
              primaryTypographyProps={{
                variant: "caption",
                fontSize: "13px",
                color:
                  activeSectionId === "password" ? "#fff" : "text.secondary",
              }}
            />
          </ListItemButton>

          <ListItemButton
            dense
            onClick={() => onNavigate("/settings/tokens")}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeSectionId === "tokens"
                ? {
                    bgcolor: "accentDim",
                    color: "#fff",
                    "&:hover": { bgcolor: "accentDim" },
                  }
                : {}),
            }}
          >
            <ListItemText
              primary={t("auth.tokensTitle")}
              primaryTypographyProps={{
                variant: "caption",
                fontSize: "13px",
                color: activeSectionId === "tokens" ? "#fff" : "text.secondary",
              }}
            />
          </ListItemButton>

          {backend.isAdmin && (
            <ListItemButton
              dense
              onClick={() => onNavigate("/settings/users")}
              sx={{
                borderRadius: 1,
                mb: 0.25,
                ...(activeSectionId === "users"
                  ? {
                      bgcolor: "accentDim",
                      color: "#fff",
                      "&:hover": { bgcolor: "accentDim" },
                    }
                  : {}),
              }}
            >
              <ListItemText
                primary={t("settings.users")}
                primaryTypographyProps={{
                  variant: "caption",
                  fontSize: "13px",
                  color:
                    activeSectionId === "users" ? "#fff" : "text.secondary",
                }}
              />
            </ListItemButton>
          )}

          <SidebarSectionTitle>Settings</SidebarSectionTitle>
          <ListItemButton
            dense
            onClick={() => onNavigate("/settings/preferences")}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeSectionId === "preferences"
                ? {
                    bgcolor: "accentDim",
                    color: "#fff",
                    "&:hover": { bgcolor: "accentDim" },
                  }
                : {}),
            }}
          >
            <ListItemText
              primary={t("settings.preferences")}
              primaryTypographyProps={{
                variant: "caption",
                fontSize: "13px",
                color:
                  activeSectionId === "preferences" ? "#fff" : "text.secondary",
              }}
            />
          </ListItemButton>

          {backend.isAdmin &&
            sections.map((section) => {
            const isActive = activeSectionId === section.id;
            return (
              <ListItemButton
                key={section.id}
                dense
                onClick={() => onNavigate(`/settings/${section.id}`)}
                sx={{
                  borderRadius: 1,
                  mb: 0.25,
                  ...(isActive
                    ? {
                        bgcolor: "accentDim",
                        color: "#fff",
                        "&:hover": { bgcolor: "accentDim" },
                      }
                    : {}),
                }}
              >
                <ListItemText
                  primary={getSectionTitle(t, section)}
                  primaryTypographyProps={{
                    variant: "caption",
                    fontSize: "13px",
                    color: isActive ? "#fff" : "text.secondary",
                  }}
                />
                </ListItemButton>
            );
            })}
        </List>
      </Box>

      <Divider />
      <Box sx={{ p: 1 }}>
        <ListItemButton
          dense
          onClick={() => {
            authLogout().then(() => window.location.reload());
          }}
          sx={{ borderRadius: 1 }}
        >
          <LogoutIcon sx={{ fontSize: 14, mr: 0.5, color: "text.secondary" }} />
          <ListItemText
            primary={t("auth.logout")}
            primaryTypographyProps={{
              variant: "caption",
              fontSize: "13px",
              color: "text.secondary",
            }}
          />
        </ListItemButton>
      </Box>
    </>
  );
}
