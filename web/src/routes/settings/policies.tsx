import React, { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Paper from "@mui/material/Paper";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import ToggleButton from "@mui/material/ToggleButton";
import Chip from "@mui/material/Chip";
import Tooltip from "@mui/material/Tooltip";
import BlockRounded from "@mui/icons-material/BlockRounded";
import AdminPanelSettingsRounded from "@mui/icons-material/AdminPanelSettingsRounded";
import GppMaybeRounded from "@mui/icons-material/GppMaybeRounded";
import VerifiedUserRounded from "@mui/icons-material/VerifiedUserRounded";
import LockOpenRounded from "@mui/icons-material/LockOpenRounded";
import HubRounded from "@mui/icons-material/HubRounded";
import { useAppContext } from "../../context";
import { useAlert } from "../../components/AlertProvider";
import type {
  ToolPolicyLevel,
  ToolPolicyGroup,
  ToolPolicyConfiguration,
  ToolActionEntry,
  ToolActionGroupEntry,
  ToolPoliciesListResult,
} from "../../types";

const POLICY_LEVELS: {
  value: ToolPolicyLevel;
  labelKey: string;
  icon: React.ReactNode;
}[] = [
  {
    value: "anyone",
    labelKey: "settings.toolPolicyAnyone",
    icon: <LockOpenRounded sx={{ fontSize: 14 }} />,
  },
  {
    value: "anyone_approval",
    labelKey: "settings.toolPolicyAnyoneApproval",
    icon: <GppMaybeRounded sx={{ fontSize: 14 }} />,
  },
  {
    value: "admin_only",
    labelKey: "settings.toolPolicyAdminOnly",
    icon: <AdminPanelSettingsRounded sx={{ fontSize: 14 }} />,
  },
  {
    value: "admin_approval",
    labelKey: "settings.toolPolicyAdminApproval",
    icon: <VerifiedUserRounded sx={{ fontSize: 14 }} />,
  },
  {
    value: "disabled",
    labelKey: "settings.toolPolicyDisabled",
    icon: <BlockRounded sx={{ fontSize: 14 }} />,
  },
];

function groupLabelKey(group: ToolPolicyGroup): string {
  switch (group) {
    case "read":
      return "settings.toolPolicyGroupRead";
    case "write":
      return "settings.toolPolicyGroupWrite";
    default:
      return "settings.toolPolicyGroupAll";
  }
}

type PolicyMap = Record<string, ToolPolicyLevel>; // key: "tool:group"

function policyKey(tool: string, group: ToolPolicyGroup): string {
  return `${tool}:${group}`;
}

function policiesToMap(policies: ToolPolicyConfiguration[]): PolicyMap {
  const result: PolicyMap = {};
  for (const policy of policies) {
    result[policyKey(policy.tool, policy.group)] = policy.level;
  }
  return result;
}

function mapToPolicies(policyMap: PolicyMap): ToolPolicyConfiguration[] {
  return Object.entries(policyMap).map(([key, level]) => {
    const [tool, group] = key.split(":");
    return { tool, group: group as ToolPolicyGroup, level };
  });
}

/**
 * The five-way policy selector. `value` is the currently selected level, or null
 * to show nothing selected (used for an MCP server whose tools have mixed
 * policies). `defaultLevel` underlines the default option.
 */
function LevelToggleGroup({
  value,
  defaultLevel,
  onChange,
}: {
  value: ToolPolicyLevel | null;
  defaultLevel: ToolPolicyLevel | null;
  onChange: (level: ToolPolicyLevel) => void;
}) {
  const { t } = useTranslation();
  return (
    <ToggleButtonGroup
      size="small"
      exclusive
      value={value}
      onChange={(_, next: ToolPolicyLevel | null) => {
        if (next !== null) onChange(next);
      }}
      sx={{ flexWrap: "nowrap" }}
    >
      {POLICY_LEVELS.map((option) => {
        const isDefault = option.value === defaultLevel;
        return (
          <ToggleButton
            key={option.value}
            value={option.value}
            sx={{
              px: 0.75,
              py: 0.25,
              fontSize: "11px",
              textTransform: "none",
              whiteSpace: "nowrap",
              gap: 0.5,
              ...(isDefault && value !== option.value
                ? {
                    borderBottomWidth: 2,
                    borderBottomColor: "primary.main",
                    borderBottomStyle: "solid",
                  }
                : {}),
            }}
          >
            <Tooltip
              title={
                t(option.labelKey) +
                (isDefault ? ` (${t("common.default")})` : "")
              }
              arrow
            >
              <Box
                component="span"
                sx={{ display: "flex", alignItems: "center" }}
              >
                {option.icon}
              </Box>
            </Tooltip>
          </ToggleButton>
        );
      })}
    </ToggleButtonGroup>
  );
}

export default function SettingsToolPoliciesPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { showAlert } = useAlert();
  const [tools, setTools] = useState<ToolActionEntry[]>([]);
  const [policies, setPolicies] = useState<PolicyMap>({});
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    if (!backend.connected) return;
    setLoading(true);
    try {
      const result = await backend.sendRpc<ToolPoliciesListResult>(
        "toolPolicies.list",
        {},
      );
      setTools(result.tools || []);
      setPolicies(policiesToMap(result.policies || []));
    } catch (error) {
      console.error("toolPolicies.list:", error);
    } finally {
      setLoading(false);
    }
  }, [backend]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  // Build a map of tool:group -> defaultPolicy for quick lookup.
  const defaultPolicyMap = new Map<string, ToolPolicyLevel>();
  for (const toolEntry of tools) {
    for (const groupEntry of toolEntry.groups) {
      defaultPolicyMap.set(
        policyKey(toolEntry.name, groupEntry.group),
        groupEntry.defaultPolicy,
      );
    }
  }

  const handleChange = async (
    tool: string,
    group: ToolPolicyGroup,
    level: ToolPolicyLevel,
  ) => {
    const key = policyKey(tool, group);
    const defaultLevel = defaultPolicyMap.get(key);
    const isDefault = level === defaultLevel;

    // Optimistically update UI.
    const previous = { ...policies };
    setPolicies((current) => {
      const next = { ...current };
      if (isDefault) {
        delete next[key];
      } else {
        next[key] = level;
      }
      return next;
    });

    // Compute the new full map for the RPC call.
    const nextPolicies = { ...previous };
    if (isDefault) {
      delete nextPolicies[key];
    } else {
      nextPolicies[key] = level;
    }

    try {
      await backend.sendRpc("toolPolicies.update", {
        policies: mapToPolicies(nextPolicies),
      });
      showAlert(t("settings.toolPolicySaved"));
    } catch (error) {
      console.error("toolPolicies.update:", error);
      // Revert optimistic update on failure.
      setPolicies(previous);
      showAlert(t("settings.toolPolicySaveFailed"), "error");
    }
  };

  // Every (tool, group) pair belonging to one MCP server, with each pair's
  // default level — the targets of a server-wide batch change.
  const serverGroups = (
    server: string,
  ): { key: string; defaultLevel: ToolPolicyLevel }[] => {
    const result: { key: string; defaultLevel: ToolPolicyLevel }[] = [];
    for (const toolEntry of tools) {
      if (toolEntry.source !== "mcp" || (toolEntry.server ?? "") !== server) {
        continue;
      }
      for (const groupEntry of toolEntry.groups) {
        result.push({
          key: policyKey(toolEntry.name, groupEntry.group),
          defaultLevel: groupEntry.defaultPolicy,
        });
      }
    }
    return result;
  };

  // The level shared by every tool of a server, or null when they differ (or
  // the server has no discovered tools) — drives the batch selector's value.
  const sharedLevel = (
    targets: { key: string; defaultLevel: ToolPolicyLevel }[],
    pick: (target: {
      key: string;
      defaultLevel: ToolPolicyLevel;
    }) => ToolPolicyLevel,
  ): ToolPolicyLevel | null => {
    let common: ToolPolicyLevel | null = null;
    for (const target of targets) {
      const level = pick(target);
      if (common === null) common = level;
      else if (common !== level) return null;
    }
    return common;
  };

  // Apply one level to every tool of a server in a single save.
  const handleServerChange = async (server: string, level: ToolPolicyLevel) => {
    const previous = { ...policies };
    const next = { ...policies };
    for (const { key, defaultLevel } of serverGroups(server)) {
      // Selecting the default clears the override, matching per-tool behavior.
      if (level === defaultLevel) delete next[key];
      else next[key] = level;
    }
    setPolicies(next);
    try {
      await backend.sendRpc("toolPolicies.update", {
        policies: mapToPolicies(next),
      });
      showAlert(t("settings.toolPolicySaved"));
    } catch (error) {
      console.error("toolPolicies.update:", error);
      setPolicies(previous);
      showAlert(t("settings.toolPolicySaveFailed"), "error");
    }
  };

  // Build rows. Non-MCP tools (builtin + skill) render flat; MCP tools are
  // grouped under a per-server section header and shown by their short tool
  // name instead of the long "mcp__server__tool" namespaced name.
  type ToolRow = {
    kind: "tool";
    tool: string; // namespaced name — the key the backend stores/matches
    displayName: string; // what the user sees (short name for MCP)
    fullName?: string; // namespaced name, shown in a tooltip for MCP tools
    groupEntry: ToolActionGroupEntry;
    isFirstOfTool: boolean;
    toolRowSpan: number;
    source: "builtin" | "skill" | "mcp";
    skill?: string;
  };
  type SectionRow = { kind: "section"; label: string };
  type Row = ToolRow | SectionRow;

  const pushToolRows = (
    list: Row[],
    toolEntry: ToolActionEntry,
    displayName: string,
  ) => {
    const groups = toolEntry.groups;
    for (let index = 0; index < groups.length; index++) {
      list.push({
        kind: "tool",
        tool: toolEntry.name,
        displayName,
        fullName: toolEntry.source === "mcp" ? toolEntry.name : undefined,
        groupEntry: groups[index],
        isFirstOfTool: index === 0,
        toolRowSpan: groups.length,
        source: toolEntry.source,
        skill: toolEntry.skill,
      });
    }
  };

  const rows: Row[] = [];
  // Builtin + skill tools first, in the order the backend returned them.
  for (const toolEntry of tools) {
    if (toolEntry.source === "mcp") continue;
    pushToolRows(rows, toolEntry, toolEntry.name);
  }
  // MCP tools, grouped by server with a section header per server.
  const mcpByServer = new Map<string, ToolActionEntry[]>();
  for (const toolEntry of tools) {
    if (toolEntry.source !== "mcp") continue;
    const server = toolEntry.server ?? "";
    const existing = mcpByServer.get(server) ?? [];
    existing.push(toolEntry);
    mcpByServer.set(server, existing);
  }
  for (const server of [...mcpByServer.keys()].sort()) {
    rows.push({ kind: "section", label: server });
    for (const toolEntry of mcpByServer.get(server) ?? []) {
      pushToolRows(rows, toolEntry, toolEntry.toolName ?? toolEntry.name);
    }
  }

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ mb: 1 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
            {t("settings.toolPolicies")}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {t("settings.toolPoliciesDescription")}
          </Typography>
        </Box>

        {loading ? (
          <Typography variant="body2" color="text.secondary">
            {t("settings.loadingSettings")}
          </Typography>
        ) : (
          <TableContainer component={Paper} variant="outlined">
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell sx={{ fontWeight: 600 }}>Tool</TableCell>
                  <TableCell sx={{ fontWeight: 600 }}>Group</TableCell>
                  <TableCell sx={{ fontWeight: 600 }}>Policy</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((row, index) => {
                  if (row.kind === "section") {
                    const targets = serverGroups(row.label);
                    const batchValue = sharedLevel(
                      targets,
                      (target) => policies[target.key] ?? target.defaultLevel,
                    );
                    const batchDefault = sharedLevel(
                      targets,
                      (target) => target.defaultLevel,
                    );
                    return (
                      <TableRow key={`section-${row.label}-${index}`}>
                        <TableCell
                          colSpan={3}
                          sx={{ bgcolor: "action.hover", py: 0.75 }}
                        >
                          <Box
                            sx={{
                              display: "flex",
                              alignItems: "center",
                              justifyContent: "space-between",
                              flexWrap: "wrap",
                              gap: 1,
                            }}
                          >
                            <Box
                              sx={{
                                display: "flex",
                                alignItems: "center",
                                gap: 0.75,
                                minWidth: 0,
                              }}
                            >
                              <HubRounded
                                sx={{ fontSize: 15, color: "text.secondary" }}
                              />
                              <Typography
                                variant="body2"
                                sx={{ fontWeight: 600 }}
                              >
                                {row.label}
                              </Typography>
                              <Chip
                                label={t("settings.toolPolicyMcp")}
                                size="small"
                                variant="outlined"
                                sx={{ fontSize: "10px", height: 18 }}
                              />
                            </Box>
                            {targets.length > 0 && (
                              <Box
                                sx={{
                                  display: "flex",
                                  alignItems: "center",
                                  gap: 0.75,
                                  flexShrink: 0,
                                }}
                              >
                                <Typography
                                  variant="caption"
                                  color="text.secondary"
                                >
                                  {t("settings.toolPolicyServerAll")}
                                </Typography>
                                <LevelToggleGroup
                                  value={batchValue}
                                  defaultLevel={batchDefault}
                                  onChange={(level) =>
                                    void handleServerChange(row.label, level)
                                  }
                                />
                              </Box>
                            )}
                          </Box>
                        </TableCell>
                      </TableRow>
                    );
                  }
                  const key = policyKey(row.tool, row.groupEntry.group);
                  const defaultLevel = row.groupEntry.defaultPolicy;
                  const overrideLevel = policies[key];
                  const effectiveLevel = overrideLevel ?? defaultLevel;
                  const isCustomized = overrideLevel !== undefined;
                  const nameTypography = (
                    <Typography
                      variant="body2"
                      sx={{ fontFamily: "monospace", fontSize: "12px" }}
                    >
                      {row.displayName}
                    </Typography>
                  );
                  return (
                    <TableRow key={key}>
                      {row.isFirstOfTool && (
                        <TableCell
                          rowSpan={row.toolRowSpan}
                          sx={{
                            verticalAlign: "top",
                            borderRight: "1px solid",
                            borderRightColor: "divider",
                            ...(row.source === "mcp" ? { pl: 3 } : {}),
                          }}
                        >
                          <Box
                            sx={{
                              display: "flex",
                              alignItems: "center",
                              gap: 0.75,
                            }}
                          >
                            {row.fullName ? (
                              <Tooltip title={row.fullName} arrow>
                                {nameTypography}
                              </Tooltip>
                            ) : (
                              nameTypography
                            )}
                            {row.source === "skill" && row.skill && (
                              <Chip
                                label={row.skill}
                                size="small"
                                variant="outlined"
                                sx={{ fontSize: "10px", height: 18 }}
                              />
                            )}
                          </Box>
                        </TableCell>
                      )}
                      <TableCell>
                        <Typography variant="caption">
                          {t(groupLabelKey(row.groupEntry.group))}
                        </Typography>
                      </TableCell>
                      <TableCell sx={{ py: 0.5 }}>
                        <Box
                          sx={{
                            display: "flex",
                            alignItems: "center",
                            gap: 0.75,
                          }}
                        >
                          <LevelToggleGroup
                            value={effectiveLevel}
                            defaultLevel={defaultLevel}
                            onChange={(level) =>
                              void handleChange(
                                row.tool,
                                row.groupEntry.group,
                                level,
                              )
                            }
                          />
                          {isCustomized && (
                            <Tooltip
                              title={t("settings.toolPolicyCustomized")}
                              arrow
                            >
                              <Box
                                sx={{
                                  width: 6,
                                  height: 6,
                                  borderRadius: "50%",
                                  bgcolor: "warning.main",
                                  flexShrink: 0,
                                }}
                              />
                            </Tooltip>
                          )}
                        </Box>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Container>
    </Box>
  );
}
