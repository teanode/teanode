import { useState, useCallback } from "react";
import type {
  AgentConfig,
  AgentsConfigListResult,
  AgentConfigSchemaResult,
  ConfigSchema,
} from "../types";

export function useAgents(
  sendRpc: <T = unknown>(method: string, params: unknown) => Promise<T>,
) {
  const [agents, setAgents] = useState<AgentConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [schema, setSchema] = useState<ConfigSchema | null>(null);
  const [suggestions, setSuggestions] = useState<Record<string, string[]>>({});

  const loadAgents = useCallback(() => {
    setLoading(true);
    sendRpc<AgentsConfigListResult>("agents.config.list", {})
      .then((result) => setAgents(result.agents || []))
      .catch((error) => console.error("agents.config.list:", error))
      .finally(() => setLoading(false));
  }, [sendRpc]);

  const loadSchema = useCallback(() => {
    sendRpc<AgentConfigSchemaResult>("agents.config.schema", {})
      .then((result) => {
        setSchema(result.schema ?? null);
        setSuggestions(result.suggestions ?? {});
      })
      .catch((error) => console.error("agents.config.schema:", error));
  }, [sendRpc]);

  const saveAgent = useCallback(
    (agent: AgentConfig) => {
      const { avatarMediaId: _avatarMediaId, ...configOnly } = agent;
      return sendRpc("agents.config.save", { agent: configOnly })
        .then(() => {
          loadAgents();
        })
        .catch((error) => {
          console.error("agents.config.save:", error);
          throw error;
        });
    },
    [sendRpc, loadAgents],
  );

  const deleteAgent = useCallback(
    (id: string) => {
      return sendRpc("agents.config.delete", { id })
        .then(() => {
          loadAgents();
        })
        .catch((error) => {
          console.error("agents.config.delete:", error);
          throw error;
        });
    },
    [sendRpc, loadAgents],
  );

  return {
    agents,
    loading,
    loadAgents,
    saveAgent,
    deleteAgent,
    schema,
    suggestions,
    loadSchema,
  };
}
