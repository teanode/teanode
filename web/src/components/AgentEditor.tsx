import React, { useState, useEffect, useCallback, useImperativeHandle } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import SchemaField from './SchemaField';
import type { AgentConfig, ModelInfo, ConfigSchema, JsonSchemaProperty, SchemaSection } from '../types';
import { getPropertyDescription, getPropertyTitle, getSectionDescription, getSectionTitle } from '../schemaLocalization';

interface AgentEditorProps {
  agent: AgentConfig | null;
  models: ModelInfo[];
  schema: ConfigSchema | null;
  suggestions?: Record<string, string[]>;
  onSave: (agent: AgentConfig) => void;
  showIdentityHeader?: boolean;
  flat?: boolean;
  showSaveControls?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
  hiddenDotPaths?: string[];
}

export interface AgentEditorHandle {
  save: () => void;
  isDirty: () => boolean;
  setField: (dotPath: string, value: unknown) => void;
}

/** Resolved entry: either a single leaf field or a group of fields under a heading. */
type SectionEntry =
  | { type: 'field'; key: string; property: JsonSchemaProperty; dotPath: string }
  | { type: 'group'; key: string; property: JsonSchemaProperty; fields: { key: string; property: JsonSchemaProperty; dotPath: string }[] };

function normalizeForDirtyCheck(value: Record<string, unknown>): Record<string, unknown> {
  const cloned = structuredClone(value);
  delete (cloned as { avatarMediaId?: unknown }).avatarMediaId;
  return cloned;
}

/** Resolve the fields for a section from the JSON Schema properties tree. */
function resolveSectionEntries(schema: ConfigSchema, section: SchemaSection, hiddenDotPaths: Set<string>): SectionEntry[] {
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
    if (hiddenDotPaths.has(dotPath)) {
      continue;
    }

    if (property.type === 'object' && !property['x-widget'] && property.properties) {
      const groupFields = Object.entries(property.properties).map(([childKey, childProperty]) => ({
        key: childKey,
        property: childProperty,
        dotPath: `${dotPath}.${childKey}`,
      })).filter((field) => !hiddenDotPaths.has(field.dotPath));
      if (groupFields.length === 0) {
        continue;
      }
      entries.push({
        type: 'group',
        key,
        property,
        fields: groupFields,
      });
    } else {
      entries.push({ type: 'field', key, property, dotPath });
    }
  }

  return entries;
}

const AgentEditor = React.forwardRef<AgentEditorHandle, AgentEditorProps>(function AgentEditor({
  agent,
  models,
  schema,
  suggestions = {},
  onSave,
  showIdentityHeader = true,
  flat = false,
  showSaveControls = true,
  onDirtyChange,
  hiddenDotPaths = [],
}, ref) {
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

  const canEdit = !!agent && !!draft.id;
  const isDirty = canEdit && agent
    ? JSON.stringify(normalizeForDirtyCheck(draft)) !== JSON.stringify(normalizeForDirtyCheck(agent as unknown as Record<string, unknown>))
    : false;
  useEffect(() => {
    onDirtyChange?.(isDirty);
  }, [isDirty, onDirtyChange]);

  const saveDraft = useCallback(() => {
    if (!canEdit) return;
    onSave(draft as unknown as AgentConfig);
  }, [onSave, draft, canEdit]);

  useImperativeHandle(ref, () => ({
    save: saveDraft,
    isDirty: () => isDirty,
    setField: setValue,
  }), [saveDraft, isDirty, setValue]);

  if (!canEdit || !agent) {
    return (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography variant="body2" color="text.secondary">{t('agent.selectToEdit')}</Typography>
      </Box>
    );
  }
  const suggestionMap: Record<string, string[]> = {
    model: models.map((modelInfo) => `${modelInfo.provider}:${modelInfo.id}`),
    ...suggestions,
  };

  const sections = schema?.['x-sections'] ?? [];
  const hidden = new Set(hiddenDotPaths);

  const body = (
    <>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
        {showIdentityHeader ? (
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{agent.name || agent.id}</Typography>
            {agent.name && (
              <Typography variant="caption" color="text.secondary" sx={{ fontFamily: 'monospace' }}>{agent.id}</Typography>
            )}
          </Box>
        ) : <Box />}
        {showSaveControls && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {isDirty && (
              <Typography variant="caption" color="warning.main">{t('common.unsavedChanges')}</Typography>
            )}
            <Button
              variant={isDirty ? 'contained' : 'outlined'}
              size="small"
              disabled={!isDirty}
              onClick={saveDraft}
            >
              {t('common.save')}
            </Button>
          </Box>
        )}
      </Box>

      {sections.map((section) => {
        const entries = schema ? resolveSectionEntries(schema, section, hidden) : [];
        const sectionTitle = getSectionTitle(t, section);
        const sectionDescription = getSectionDescription(t, section);

        // Group consecutive top-level fields into panels; each named group gets its own panel.
        const panels: { title?: string; description?: string; fields: SectionEntry[] }[] = [];
        for (const entry of entries) {
          if (entry.type === 'group') {
            panels.push({
              title: getPropertyTitle(t, entry.property, entry.key),
              description: getPropertyDescription(t, entry.property),
              fields: [entry],
            });
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
          panels[0].title = panels[0].title ?? sectionTitle;
          panels[0].description = panels[0].description ?? sectionDescription;
        }

        return (
          <React.Fragment key={section.id}>
            {!useSectionHeading && (
              <Typography variant="subtitle2" sx={{ fontWeight: 600, mt: 1 }}>
                {sectionTitle}
              </Typography>
            )}
            {panels.map((panel, panelIndex) => (
              flat ? (
                <Box key={panel.title ?? panelIndex} sx={{ py: 1.25, borderBottom: 1, borderColor: 'divider' }}>
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
                    {panel.fields.flatMap((entry) => entry.type === 'group'
                      ? entry.fields.map((field) => (
                        <SchemaField
                          key={field.dotPath}
                          property={field.property}
                          propertyKey={field.key}
                          value={getValue(field.dotPath)}
                          onChange={(value) => setValue(field.dotPath, value)}
                          suggestions={field.property['x-suggest'] ? suggestionMap[field.property['x-suggest']] : undefined}
                        />
                      ))
                      : (
                        <SchemaField
                          key={entry.dotPath}
                          property={entry.property}
                          propertyKey={entry.key}
                          value={getValue(entry.dotPath)}
                          onChange={(value) => setValue(entry.dotPath, value)}
                          suggestions={entry.property['x-suggest'] ? suggestionMap[entry.property['x-suggest']] : undefined}
                        />
                      ))}
                  </Box>
                </Box>
              ) : (
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
                    {panel.fields.flatMap((entry) => entry.type === 'group'
                      ? entry.fields.map((field) => (
                        <SchemaField
                          key={field.dotPath}
                          property={field.property}
                          propertyKey={field.key}
                          value={getValue(field.dotPath)}
                          onChange={(value) => setValue(field.dotPath, value)}
                          suggestions={field.property['x-suggest'] ? suggestionMap[field.property['x-suggest']] : undefined}
                        />
                      ))
                      : (
                        <SchemaField
                          key={entry.dotPath}
                          property={entry.property}
                          propertyKey={entry.key}
                          value={getValue(entry.dotPath)}
                          onChange={(value) => setValue(entry.dotPath, value)}
                          suggestions={entry.property['x-suggest'] ? suggestionMap[entry.property['x-suggest']] : undefined}
                        />
                      ))}
                  </Box>
                </Paper>
              )
            ))}
          </React.Fragment>
        );
      })}
    </>
  );

  if (flat) {
    return <Box sx={{ py: 1 }}>{body}</Box>;
  }

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        {body}
      </Container>
    </Box>
  );
});

export default AgentEditor;
