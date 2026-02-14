import React, { useState, useEffect } from 'react';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Autocomplete from '@mui/material/Autocomplete';
import SchemaField from './SchemaField';
import type { AgentConfig, ModelInfo, ConfigSchema } from '../types';

interface AgentEditorProps {
  agent: AgentConfig | null;
  models: ModelInfo[];
  schema: ConfigSchema | null;
  onSave: (agent: AgentConfig) => void;
  onDelete: (id: string) => void;
}

export default function AgentEditor({ agent, models, schema, onSave, onDelete }: AgentEditorProps) {
  const [draft, setDraft] = useState<AgentConfig | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState(false);

  useEffect(() => {
    setDraft(agent ? { ...agent } : null);
    setDeleteConfirm(false);
  }, [agent?.id]);

  if (!agent || !draft) {
    return (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography variant="body2" color="text.secondary">Select an agent to edit</Typography>
      </Box>
    );
  }

  function updateDraft(updates: Partial<AgentConfig>) {
    setDraft((previous) => (previous ? { ...previous, ...updates } : previous));
  }

  const isDirty = JSON.stringify(draft) !== JSON.stringify(agent);
  const modelOptions = models.map((modelInfo) => `${modelInfo.provider}:${modelInfo.id}`);

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600, fontFamily: 'monospace' }}>{agent.id}</Typography>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {isDirty && (
              <Typography variant="caption" color="warning.main">Unsaved changes</Typography>
            )}
            <Button
              variant={isDirty ? 'contained' : 'outlined'}
              size="small"
              disabled={!isDirty}
              onClick={() => onSave(draft)}
            >
              Save
            </Button>
          </Box>
        </Box>

        <Paper variant="outlined" sx={{ p: 2, mb: 2 }}>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {/* Model */}
            <Autocomplete
              freeSolo
              options={modelOptions}
              value={draft.model ?? ''}
              onInputChange={(_event, newValue) => updateDraft({ model: newValue || undefined })}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label="Model"
                  placeholder="Default"
                  size="small"
                />
              )}
            />

            {/* System Prompt */}
            <TextField
              label="System Prompt"
              multiline
              minRows={4}
              value={draft.systemPrompt ?? ''}
              placeholder="Per-agent system prompt override..."
              size="small"
              fullWidth
              onChange={(event) => updateDraft({ systemPrompt: event.target.value || undefined })}
            />

            {/* Can Message */}
            <FilterField
              label="Can Message"
              placeholder="Agent IDs (use * for all)"
              values={draft.canMessage ?? []}
              onChange={(canMessage) => updateDraft({ canMessage })}
            />

            {/* Tools Filter */}
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 500, display: 'block', mb: 1 }}>
                Tools Filter
              </Typography>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                <FilterField
                  label="Allow"
                  placeholder="Tool names to allow (empty = all)"
                  values={draft.tools?.allow ?? []}
                  onChange={(allow) =>
                    updateDraft({
                      tools: { ...draft.tools, allow: allow.length ? allow : undefined },
                    })
                  }
                />
                <FilterField
                  label="Deny"
                  placeholder="Tool names to deny"
                  values={draft.tools?.deny ?? []}
                  onChange={(deny) =>
                    updateDraft({
                      tools: { ...draft.tools, deny: deny.length ? deny : undefined },
                    })
                  }
                />
              </Box>
            </Box>

            {/* Skills Filter */}
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 500, display: 'block', mb: 1 }}>
                Skills Filter
              </Typography>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                <FilterField
                  label="Allow"
                  placeholder="Skill names to allow (empty = all)"
                  values={draft.skills?.allow ?? []}
                  onChange={(allow) =>
                    updateDraft({
                      skills: { ...draft.skills, allow: allow.length ? allow : undefined },
                    })
                  }
                />
                <FilterField
                  label="Deny"
                  placeholder="Skill names to deny"
                  values={draft.skills?.deny ?? []}
                  onChange={(deny) =>
                    updateDraft({
                      skills: { ...draft.skills, deny: deny.length ? deny : undefined },
                    })
                  }
                />
              </Box>
            </Box>
          </Box>
        </Paper>

        {/* Limits */}
        {schema?.sections
          .filter((section) => section.id === 'limits')
          .map((section) => (
            <Paper key={section.id} variant="outlined" sx={{ p: 2, mb: 2 }}>
              <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 500, display: 'block', mb: 1.5 }}>
                {section.label}
              </Typography>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                {section.fields.map((field) => (
                  <SchemaField
                    key={field.key}
                    field={field}
                    value={(draft as unknown as Record<string, unknown>)[field.key]}
                    onChange={(value) => updateDraft({ [field.key]: value } as unknown as Partial<AgentConfig>)}
                  />
                ))}
              </Box>
            </Paper>
          ))}

        {/* Delete */}
        <Paper variant="outlined" sx={{ p: 2 }}>
          {deleteConfirm ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
              <Typography variant="caption" color="error.main">Delete this agent permanently?</Typography>
              <Button size="small" variant="contained" color="error" onClick={() => onDelete(agent.id)}>
                Confirm Delete
              </Button>
              <Button size="small" onClick={() => setDeleteConfirm(false)}>
                Cancel
              </Button>
            </Box>
          ) : (
            <Button size="small" color="error" onClick={() => setDeleteConfirm(true)}>
              Delete Agent
            </Button>
          )}
        </Paper>
      </Container>
    </Box>
  );
}

function FilterField({
  label,
  placeholder,
  values,
  onChange,
}: {
  label: string;
  placeholder: string;
  values: string[];
  onChange: (values: string[]) => void;
}) {
  const [inputValue, setInputValue] = useState('');

  function addValue() {
    const trimmed = inputValue.trim();
    if (trimmed && !values.includes(trimmed)) {
      onChange([...values, trimmed]);
    }
    setInputValue('');
  }

  return (
    <Box>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, fontSize: '10px' }}>
        {label}
      </Typography>
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, mb: 0.75 }}>
        {values.map((item, index) => (
          <Chip
            key={index}
            label={item}
            size="small"
            sx={{ fontFamily: 'monospace', fontSize: '11px' }}
            onDelete={() => onChange(values.filter((_, valueIndex) => valueIndex !== index))}
          />
        ))}
      </Box>
      <Box sx={{ display: 'flex', gap: 0.75 }}>
        <TextField
          size="small"
          fullWidth
          value={inputValue}
          placeholder={placeholder}
          onChange={(event) => setInputValue(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault();
              addValue();
            }
          }}
          sx={{ '& .MuiInputBase-input': { fontSize: '0.75rem', py: 0.75 } }}
        />
        <Button size="small" variant="contained" onClick={addValue} sx={{ fontSize: '11px', minWidth: 'auto', px: 1.5 }}>
          Add
        </Button>
      </Box>
    </Box>
  );
}
