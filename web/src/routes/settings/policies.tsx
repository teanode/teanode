import React, { useCallback, useEffect, useRef, useState } from "react";
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
import Alert from "@mui/material/Alert";
import Chip from "@mui/material/Chip";
import Tooltip from "@mui/material/Tooltip";
import BlockRounded from "@mui/icons-material/BlockRounded";
import AdminPanelSettingsRounded from "@mui/icons-material/AdminPanelSettingsRounded";
import GppMaybeRounded from "@mui/icons-material/GppMaybeRounded";
import VerifiedUserRounded from "@mui/icons-material/VerifiedUserRounded";
import LockOpenRounded from "@mui/icons-material/LockOpenRounded";
import { useAppContext } from "../../context";
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

export default function SettingsToolPoliciesPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const [tools, setTools] = useState<ToolActionEntry[]>([]);
  const [policies, setPolicies] = useState<PolicyMap>({});
  const [loading, setLoading] = useState(true);
  const [feedback, setFeedback] = useState<{
    type: "success" | "error";
    message: string;
  } | null>(null);
  const feedbackTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

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

  const showFeedback = (type: "success" | "error", message: string) => {
    clearTimeout(feedbackTimer.current);
    setFeedback({ type, message });
    feedbackTimer.current = setTimeout(
      () => setFeedback(null),
      type === "success" ? 2000 : 5000,
    );
  };

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
      showFeedback("success", t("settings.toolPolicySaved"));
    } catch (error) {
      console.error("toolPolicies.update:", error);
      // Revert optimistic update on failure.
      setPolicies(previous);
      showFeedback("error", t("settings.toolPolicySaveFailed"));
    }
  };

  // Build rows: one row per tool+group combination.
  type Row = {
    tool: string;
    groupEntry: ToolActionGroupEntry;
    isFirstOfTool: boolean;
    toolRowSpan: number;
    source: "builtin" | "skill";
    skill?: string;
  };
  const rows: Row[] = [];
  for (const toolEntry of tools) {
    const groups = toolEntry.groups;
    for (let index = 0; index < groups.length; index++) {
      rows.push({
        tool: toolEntry.name,
        groupEntry: groups[index],
        isFirstOfTool: index === 0,
        toolRowSpan: groups.length,
        source: toolEntry.source,
        skill: toolEntry.skill,
      });
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

        {feedback && (
          <Alert
            severity={feedback.type}
            sx={{ mb: 2 }}
            onClose={() => setFeedback(null)}
          >
            {feedback.message}
          </Alert>
        )}

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
                {rows.map((row) => {
                  const key = policyKey(row.tool, row.groupEntry.group);
                  const defaultLevel = row.groupEntry.defaultPolicy;
                  const overrideLevel = policies[key];
                  const effectiveLevel = overrideLevel ?? defaultLevel;
                  const isCustomized = overrideLevel !== undefined;
                  return (
                    <TableRow key={key}>
                      {row.isFirstOfTool && (
                        <TableCell
                          rowSpan={row.toolRowSpan}
                          sx={{
                            verticalAlign: "top",
                            borderRight: "1px solid",
                            borderRightColor: "divider",
                          }}
                        >
                          <Box
                            sx={{
                              display: "flex",
                              alignItems: "center",
                              gap: 0.75,
                            }}
                          >
                            <Typography
                              variant="body2"
                              sx={{
                                fontFamily: "monospace",
                                fontSize: "12px",
                              }}
                            >
                              {row.tool}
                            </Typography>
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
                          <ToggleButtonGroup
                            size="small"
                            exclusive
                            value={effectiveLevel}
                            onChange={(_, value: ToolPolicyLevel | null) => {
                              if (value !== null) {
                                void handleChange(
                                  row.tool,
                                  row.groupEntry.group,
                                  value,
                                );
                              }
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
                                    ...(isDefault &&
                                    effectiveLevel !== option.value
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
                                      (isDefault
                                        ? ` (${t("common.default")})`
                                        : "")
                                    }
                                    arrow
                                  >
                                    <Box
                                      component="span"
                                      sx={{
                                        display: "flex",
                                        alignItems: "center",
                                      }}
                                    >
                                      {option.icon}
                                    </Box>
                                  </Tooltip>
                                </ToggleButton>
                              );
                            })}
                          </ToggleButtonGroup>
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
