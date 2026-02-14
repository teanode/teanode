import { useState, useCallback, useEffect } from 'react';
import type { ConfigSchema, ConfigSchemaResult, ConfigGetResult } from '../types';

export function useSettings(
  sendRpc: <T = unknown>(method: string, params: unknown) => Promise<T>,
  active: boolean,
  connected: boolean
) {
  const [schema, setSchema] = useState<ConfigSchema | null>(null);
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([
      sendRpc<ConfigSchemaResult>('config.schema', {}),
      sendRpc<ConfigGetResult>('config.get', {}),
    ])
      .then(([schemaResult, configResult]) => {
        setSchema(schemaResult.schema);
        setValues(configResult.config);
        setDirty(false);
      })
      .catch((error) => console.error('settings load:', error))
      .finally(() => setLoading(false));
  }, [sendRpc]);

  useEffect(() => {
    if (active && connected) load();
  }, [active, connected, load]);

  const getValue = useCallback(
    (dotPath: string): unknown => {
      const parts = dotPath.split('.');
      let current: unknown = values;
      for (const part of parts) {
        if (current == null || typeof current !== 'object') return undefined;
        current = (current as Record<string, unknown>)[part];
      }
      return current;
    },
    [values]
  );

  const setValue = useCallback(
    (dotPath: string, value: unknown) => {
      setValues((previous) => {
        const parts = dotPath.split('.');
        const result = structuredClone(previous);
        let current: Record<string, unknown> = result;
        for (let index = 0; index < parts.length - 1; index++) {
          const part = parts[index];
          if (current[part] == null || typeof current[part] !== 'object') {
            current[part] = {};
          }
          current = current[part] as Record<string, unknown>;
        }
        current[parts[parts.length - 1]] = value;
        return result;
      });
      setDirty(true);
    },
    []
  );

  const save = useCallback(() => {
    setSaving(true);
    sendRpc('config.update', { config: values })
      .then(() => {
        setDirty(false);
        // Reload to get fresh masked values.
        load();
      })
      .catch((error) => console.error('settings save:', error))
      .finally(() => setSaving(false));
  }, [sendRpc, values, load]);

  return { schema, values, loading, saving, dirty, getValue, setValue, save, reload: load };
}
