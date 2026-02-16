import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import SchemaField from './SchemaField';
import type { AgentConfig, ModelInfo, ConfigSchema, JsonSchemaProperty, SchemaSection } from '../types';

interface AgentEditorProps {
  agent: AgentConfig | null;
  models: ModelInfo[];
  schema: ConfigSchema | null;
  onSave: (agent: AgentConfig) => void;
}

/** Resolved entry: either a single leaf field or a group of fields under a heading. */
type SectionEntry =
  | { type: 'field'; key: string; property: JsonSchemaProperty; dotPath: string }
  | { type: 'group'; title: string; description?: string; fields: { key: string; property: JsonSchemaProperty; dotPath: string }[] };

/** Resolve the fields for a section from the JSON Schema properties tree. */
function resolveSectionEntries(schema: ConfigSchema, section: SchemaSection): SectionEntry[] {
  const entries: SectionEntry[] = [];
  const rootProperties = schema.properties;

  const collected: [string, JsonSchemaProperty, string][] = [];

  if (section.path) {
    const parts = section.path.split('.');
    let current: Record<string, JsonSchemaProperty> = rootProperties;
    for (const part of parts) {
      current = current[part]?.properties ?? {};
    }
    for (const [key, property] of Object.entries(current)) {
      collected.push([key, property, section.path]);
    }
  } else if (section.properties) {
    for (const key of section.properties) {
      if (rootProperties[key]) {
        collected.push([key, rootProperties[key], '']);
      }
    }
  }

  for (const [key, property, prefix] of collected) {
    const dotPath = prefix ? `${prefix}.${key}` : key;

    if (property.type === 'object' && !property['x-widget'] && property.properties) {
      entries.push({
        type: 'group',
        title: property.title ?? key,
        description: property.description,
        fields: Object.entries(property.properties).map(([childKey, childProperty]) => ({
          key: childKey,
          property: childProperty,
          dotPath: `${dotPath}.${childKey}`,
        })),
      });
    } else {
      entries.push({ type: 'field', key, property, dotPath });
    }
  }

  return entries;
}

export default function AgentEditor({ agent, models, schema, onSave }: AgentEditorProps) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<Record<string, unknown>>({});

  useEffect(() => {
    setDraft(agent ? { ...agent } : {});
  }, [agent?.id]);

  const getValue = useCallback(
    (dotPath: string): unknown => {
      const parts = dotPath.split('.');
      let current: unknown = draft;
      for (const part of parts) {
        if (current == null || typeof current !== 'object') return undefined;
        current = (current as Record<string, unknown>)[part];
      }
      return current;
    },
    [draft]
  );

  const setValue = useCallback(
    (dotPath: string, value: unknown) => {
      setDraft((previous) => {
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
    },
    []
  );

  if (!agent || !draft.id) {
    return (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography variant="body2" color="text.secondary">{t('agent.selectToEdit')}</Typography>
      </Box>
    );
  }

  const isDirty = JSON.stringify(draft) !== JSON.stringify(agent);
  const suggestionMap: Record<string, string[]> = {
    model: models.map((modelInfo) => `${modelInfo.provider}:${modelInfo.id}`),
  };

  const sections = schema?.['x-sections'] ?? [];

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{agent.name || agent.id}</Typography>
            {agent.name && (
              <Typography variant="caption" color="text.secondary" sx={{ fontFamily: 'monospace' }}>{agent.id}</Typography>
            )}
          </Box>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {isDirty && (
              <Typography variant="caption" color="warning.main">{t('common.unsavedChanges')}</Typography>
            )}
            <Button
              variant={isDirty ? 'contained' : 'outlined'}
              size="small"
              disabled={!isDirty}
              onClick={() => onSave(draft as unknown as AgentConfig)}
            >
              {t('common.save')}
            </Button>
          </Box>
        </Box>

        {sections.map((section) => {
          const entries = schema ? resolveSectionEntries(schema, section) : [];

          // Group consecutive top-level fields into panels; each named group gets its own panel.
          const panels: { title?: string; description?: string; fields: SectionEntry[] }[] = [];
          for (const entry of entries) {
            if (entry.type === 'group') {
              panels.push({ title: entry.title, description: entry.description, fields: [entry] });
            } else {
              const last = panels[panels.length - 1];
              if (last && !last.title) {
                last.fields.push(entry);
              } else {
                panels.push({ fields: [entry] });
              }
            }
          }

          // If the section has only one panel, use the section title/description as the panel heading.
          const useSectionHeading = panels.length === 1;
          if (useSectionHeading && panels[0]) {
            panels[0].title = panels[0].title ?? section.title;
            panels[0].description = panels[0].description ?? section.description;
          }

          return (
            <React.Fragment key={section.id}>
              {!useSectionHeading && (
                <Typography variant="subtitle2" sx={{ fontWeight: 600, mt: 1 }}>
                  {section.title}
                </Typography>
              )}
              {panels.map((panel, panelIndex) => (
                <Paper key={panel.title ?? panelIndex} variant="outlined" sx={{ p: 2, mb: 2 }}>
                  {panel.title && (
                    <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: panel.description ? 0.5 : 1 }}>
                      {panel.title}
                    </Typography>
                  )}
                  {panel.description && (
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                      {panel.description}
                    </Typography>
                  )}
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    {panel.fields.flatMap((entry) => {
                      if (entry.type === 'group') {
                        return entry.fields.map((field) => (
                          <SchemaField
                            key={field.dotPath}
                            property={field.property}
                            propertyKey={field.key}
                            value={getValue(field.dotPath)}
                            onChange={(value) => setValue(field.dotPath, value)}
                            suggestions={field.property['x-suggest'] ? suggestionMap[field.property['x-suggest']] : undefined}
                          />
                        ));
                      }
                      return (
                        <SchemaField
                          key={entry.dotPath}
                          property={entry.property}
                          propertyKey={entry.key}
                          value={getValue(entry.dotPath)}
                          onChange={(value) => setValue(entry.dotPath, value)}
                          suggestions={entry.property['x-suggest'] ? suggestionMap[entry.property['x-suggest']] : undefined}
                        />
                      );
                    })}
                  </Box>
                </Paper>
              ))}
            </React.Fragment>
          );
        })}

      </Container>
    </Box>
  );
}
