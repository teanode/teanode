import React, { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import Box from "@mui/material/Box";
import CircularProgress from "@mui/material/CircularProgress";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import Chip from "@mui/material/Chip";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import InputAdornment from "@mui/material/InputAdornment";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemText from "@mui/material/ListItemText";
import ListSubheader from "@mui/material/ListSubheader";
import ClearIcon from "@mui/icons-material/Clear";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import KeyboardArrowDownIcon from "@mui/icons-material/KeyboardArrowDown";
import KeyboardArrowUpIcon from "@mui/icons-material/KeyboardArrowUp";
import SearchIcon from "@mui/icons-material/Search";
import { renderMarkdown } from "../../markdown";
import ConfirmDialog from "../../components/ConfirmDialog";
import { useAppContext } from "../../context";
import type { MemoryItem } from "../../types";

dayjs.extend(relativeTime);

type ScopeType = "user" | "agent" | "project";

interface ScopedEntity {
  scope: ScopeType;
  id: string;
  name: string;
}

interface SearchSnippet {
  itemId: string;
  title?: string;
  snippet?: string;
  content?: string;
  score?: number;
  tags?: string[];
  modifiedAt?: string;
}

interface MemoryListResult {
  items: MemoryItem[];
  total: number;
}

interface MemorySearchResult {
  snippets: SearchSnippet[];
  totalMatches: number;
  method?: string;
}

function encodeValue(scope: ScopeType, id: string): string {
  return `${scope}::${id}`;
}

function decodeValue(value: string): { scope: ScopeType; id: string } {
  const index = value.indexOf("::");
  if (index < 0) return { scope: "user", id: value };
  return {
    scope: value.slice(0, index) as ScopeType,
    id: value.slice(index + 2),
  };
}

export default function SettingsMemoryPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const { sendRpc, connected, isAdmin, currentUserId: userId } = backend;

  const [allEntities, setAllEntities] = useState<ScopedEntity[]>([]);
  const [selectedValue, setSelectedValue] = useState<string>(
    encodeValue("user", userId || ""),
  );
  const [items, setItems] = useState<MemoryItem[]>([]);
  const [query, setQuery] = useState("");
  const [statusText, setStatusText] = useState("");
  const [pendingDelete, setPendingDelete] = useState<MemoryItem | null>(null);
  const [loading, setLoading] = useState(false);
  const [searchScores, setSearchScores] = useState<Record<string, number>>({});
  const [filterTag, setFilterTag] = useState<string | null>(null);
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());
  // For search results, stores the full content keyed by item ID.
  const [fullContent, setFullContent] = useState<Record<string, string>>({});
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const { scope, id: scopeId } = decodeValue(selectedValue);

  const toggleExpanded = useCallback((itemId: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(itemId)) {
        next.delete(itemId);
      } else {
        next.add(itemId);
      }
      return next;
    });
  }, []);

  // Load all entities across scopes on mount.
  const loadAllEntities = useCallback(async () => {
    if (!connected) return;
    const entities: ScopedEntity[] = [];
    try {
      if (isAdmin) {
        const result = await sendRpc<{
          users: Array<{ id: string; username: string }>;
        }>("users.list", {});
        for (const user of result.users || []) {
          entities.push({ scope: "user", id: user.id, name: user.username });
        }
      } else {
        entities.push({ scope: "user", id: userId || "", name: "Me" });
      }

      const agentsResult = await sendRpc<{
        agents: Array<{ id: string; name?: string }>;
      }>("agents.list", {});
      for (const agent of agentsResult.agents || []) {
        entities.push({
          scope: "agent",
          id: agent.id,
          name: agent.name || agent.id,
        });
      }

      const projectsResult = await sendRpc<{
        projects: Array<{ id: string; name: string }>;
      }>("projects.list", {});
      for (const project of projectsResult.projects || []) {
        entities.push({
          scope: "project",
          id: project.id,
          name: project.name || project.id,
        });
      }
    } catch (error) {
      console.error("entity list:", error);
    }
    setAllEntities(entities);
  }, [connected, isAdmin, sendRpc, userId]);

  useEffect(() => {
    void loadAllEntities();
  }, [loadAllEntities]);

  // Fetch items (list or search).
  const fetchItems = useCallback(
    async (currentQuery: string) => {
      if (!connected || !scopeId) return;
      setLoading(true);
      setSearchScores({});
      setExpandedIds(new Set());
      setFullContent({});
      try {
        if (currentQuery.trim()) {
          const result = await sendRpc<MemorySearchResult>("memory.search", {
            scope,
            scopeId,
            query: currentQuery.trim(),
            maxResults: 50,
          });
          const scores: Record<string, number> = {};
          const contentMap: Record<string, string> = {};
          const mapped: MemoryItem[] = (result.snippets || []).map((s) => {
            if (s.score !== undefined) {
              scores[s.itemId] = s.score;
            }
            if (s.content) {
              contentMap[s.itemId] = s.content;
            }
            return {
              id: s.itemId,
              title: s.title,
              content: s.snippet,
              tags: s.tags,
              scope,
              scopeId,
              modifiedAt: s.modifiedAt,
            };
          });
          setSearchScores(scores);
          setFullContent(contentMap);
          setItems(mapped);
        } else {
          const result = await sendRpc<MemoryListResult>("memory.list", {
            scope,
            scopeId,
            limit: 50,
          });
          setItems(result.items || []);
        }
      } catch (error) {
        console.error("memory load:", error);
        setItems([]);
      } finally {
        setLoading(false);
      }
    },
    [connected, scope, scopeId, sendRpc],
  );

  // Load items on selection change (immediate).
  useEffect(() => {
    if (scopeId) {
      void fetchItems(query);
    }
  }, [selectedValue]); // eslint-disable-line react-hooks/exhaustive-deps

  // Debounce search on query change (3 seconds); immediate when cleared.
  useEffect(() => {
    if (debounceTimer.current) {
      clearTimeout(debounceTimer.current);
    }
    if (!query.trim()) {
      if (scopeId) {
        void fetchItems("");
      }
      return;
    }
    debounceTimer.current = setTimeout(() => {
      if (scopeId) {
        void fetchItems(query);
      }
    }, 3000);
    return () => {
      if (debounceTimer.current) {
        clearTimeout(debounceTimer.current);
      }
    };
  }, [query]); // eslint-disable-line react-hooks/exhaustive-deps

  const submitSearch = useCallback(() => {
    if (debounceTimer.current) {
      clearTimeout(debounceTimer.current);
    }
    if (scopeId) {
      void fetchItems(query);
    }
  }, [fetchItems, query, scopeId]);

  const confirmDelete = useCallback(async () => {
    if (!pendingDelete) return;
    try {
      await sendRpc("memory.delete", { memoryItemId: pendingDelete.id });
      setStatusText(t("settings.memoryDeleted"));
      setPendingDelete(null);
      await fetchItems(query);
    } catch (error) {
      console.error("memory.delete:", error);
      setStatusText(t("settings.memoryDeleteFailed"));
    }
  }, [fetchItems, pendingDelete, query, sendRpc, t]);

  const canDelete = useCallback(
    (_item: MemoryItem): boolean => {
      if (isAdmin) return true;
      return scope === "user" && scopeId === userId;
    },
    [isAdmin, scope, scopeId, userId],
  );

  const userEntities = allEntities.filter((e) => e.scope === "user");
  const agentEntities = allEntities.filter((e) => e.scope === "agent");
  const projectEntities = allEntities.filter((e) => e.scope === "project");

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ mb: 3 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
            {t("settings.memory")}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {t("settings.memoryDescription")}
          </Typography>
        </Box>

        {/* Dropdown + search on same line */}
        <Box sx={{ display: "flex", gap: 1.5, mb: 2 }}>
          <FormControl size="small" sx={{ minWidth: 180 }}>
            <InputLabel>{t("settings.memory")}</InputLabel>
            <Select
              value={selectedValue}
              label={t("settings.memory")}
              onChange={(e) => {
                setSelectedValue(e.target.value);
                setQuery("");
                setFilterTag(null);
                setItems([]);
                setStatusText("");
              }}
            >
              {userEntities.length > 0 && <ListSubheader>Users</ListSubheader>}
              {userEntities.map((entity) => (
                <MenuItem
                  key={encodeValue(entity.scope, entity.id)}
                  value={encodeValue(entity.scope, entity.id)}
                >
                  {entity.name}
                </MenuItem>
              ))}
              {agentEntities.length > 0 && (
                <ListSubheader>Agents</ListSubheader>
              )}
              {agentEntities.map((entity) => (
                <MenuItem
                  key={encodeValue(entity.scope, entity.id)}
                  value={encodeValue(entity.scope, entity.id)}
                >
                  {entity.name}
                </MenuItem>
              ))}
              {projectEntities.length > 0 && (
                <ListSubheader>Projects</ListSubheader>
              )}
              {projectEntities.map((entity) => (
                <MenuItem
                  key={encodeValue(entity.scope, entity.id)}
                  value={encodeValue(entity.scope, entity.id)}
                >
                  {entity.name}
                </MenuItem>
              ))}
            </Select>
          </FormControl>

          <TextField
            size="small"
            fullWidth
            placeholder={t("settings.memorySearchPlaceholder")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                submitSearch();
              }
            }}
            InputProps={{
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon fontSize="small" />
                </InputAdornment>
              ),
              endAdornment: loading ? (
                <InputAdornment position="end">
                  <CircularProgress size={18} />
                </InputAdornment>
              ) : query ? (
                <InputAdornment position="end">
                  <IconButton
                    size="small"
                    onClick={() => setQuery("")}
                    edge="end"
                  >
                    <ClearIcon fontSize="small" />
                  </IconButton>
                </InputAdornment>
              ) : undefined,
            }}
          />
        </Box>

        {!!statusText && (
          <Typography variant="caption" color="text.secondary" sx={{ mb: 1 }}>
            {statusText}
          </Typography>
        )}

        {filterTag && (
          <Box sx={{ mb: 1 }}>
            <Chip
              label={filterTag}
              size="small"
              onDelete={() => setFilterTag(null)}
              color="primary"
            />
          </Box>
        )}

        {/* Items as a flat list (session-style) */}
        {!loading && items.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            {t("settings.memoryNoItems")}
          </Typography>
        )}

        {items.length > 0 && (
          <List disablePadding>
            {items
              .filter(
                (item) =>
                  !filterTag || (item.tags && item.tags.includes(filterTag)),
              )
              .map((item) => {
                const isSearchResult = searchScores[item.id] !== undefined;
                const expanded = expandedIds.has(item.id);

                // Search results: show snippet when collapsed, full content when expanded.
                // List results: hidden when collapsed, full content when expanded.
                let displayContent: string | null = null;
                if (expanded) {
                  displayContent = fullContent[item.id] || item.content || null;
                } else if (isSearchResult && item.content) {
                  displayContent = item.content;
                }

                const hasModified = !!item.modifiedAt;
                const hasScore = isSearchResult;

                const hasContent = !!item.content || !!fullContent[item.id];

                return (
                  <ListItem
                    key={item.id}
                    disableGutters
                    disablePadding
                    secondaryAction={
                      <Box sx={{ display: "flex", gap: 0.5 }}>
                        {hasContent && (
                          <IconButton
                            size="small"
                            onClick={() => toggleExpanded(item.id)}
                          >
                            {expanded ? (
                              <KeyboardArrowUpIcon fontSize="small" />
                            ) : (
                              <KeyboardArrowDownIcon fontSize="small" />
                            )}
                          </IconButton>
                        )}
                        {canDelete(item) && (
                          <Tooltip title={t("common.delete")}>
                            <IconButton
                              size="small"
                              color="error"
                              onClick={() => setPendingDelete(item)}
                            >
                              <DeleteOutlineIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        )}
                      </Box>
                    }
                    sx={{
                      alignItems: "flex-start",
                      py: 0.5,
                      pr: 10,
                      "& .MuiListItemSecondaryAction-root": {
                        top: 16,
                        transform: "none",
                      },
                    }}
                  >
                    <ListItemText
                      primary={
                        <Box
                          sx={{
                            display: "flex",
                            alignItems: "center",
                            gap: 0.75,
                          }}
                        >
                          <Typography variant="body2">
                            {item.title || "Untitled"}
                          </Typography>
                          {item.tags &&
                            item.tags.map((tag) => (
                              <Chip
                                key={tag}
                                label={tag}
                                size="small"
                                variant="outlined"
                                clickable
                                color={
                                  filterTag === tag ? "primary" : "default"
                                }
                                onClick={() =>
                                  setFilterTag((prev) =>
                                    prev === tag ? null : tag,
                                  )
                                }
                                sx={{ height: 18, fontSize: "0.7rem" }}
                              />
                            ))}
                        </Box>
                      }
                      secondary={
                        <>
                          {displayContent && (
                            <Box
                              component="span"
                              sx={{
                                display: "block",
                                wordBreak: "break-word",
                                "& p": { m: 0 },
                                "& pre": {
                                  whiteSpace: "pre-wrap",
                                  m: 0,
                                },
                                fontSize: "0.75rem",
                                color: "text.secondary",
                              }}
                              dangerouslySetInnerHTML={{
                                __html: renderMarkdown(displayContent),
                              }}
                            />
                          )}
                          {(hasModified || hasScore) && (
                            <Typography
                              component="span"
                              variant="caption"
                              color="text.disabled"
                              sx={{ display: "block" }}
                            >
                              {hasModified && (
                                <Tooltip
                                  title={dayjs(item.modifiedAt).format(
                                    "YYYY-MM-DD HH:mm:ss",
                                  )}
                                  arrow
                                >
                                  <span>
                                    {dayjs(item.modifiedAt).fromNow()}
                                  </span>
                                </Tooltip>
                              )}
                              {hasModified && hasScore && " · "}
                              {hasScore &&
                                `Score: ${Math.round(searchScores[item.id] * 100)}%`}
                            </Typography>
                          )}
                        </>
                      }
                    />
                  </ListItem>
                );
              })}
          </List>
        )}
      </Container>

      <ConfirmDialog
        open={!!pendingDelete}
        title={t("settings.memoryDeleteTitle")}
        message={t("settings.memoryDeleteConfirm")}
        confirmLabel={t("common.delete")}
        onConfirm={() => void confirmDelete()}
        onClose={() => setPendingDelete(null)}
      />
    </Box>
  );
}
