import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import TextField from '@mui/material/TextField';
import MenuItem from '@mui/material/MenuItem';
import Switch from '@mui/material/Switch';
import FormControlLabel from '@mui/material/FormControlLabel';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Paper from '@mui/material/Paper';
import IconButton from '@mui/material/IconButton';
import InputAdornment from '@mui/material/InputAdornment';
import Autocomplete from '@mui/material/Autocomplete';
import VisibilityIcon from '@mui/icons-material/Visibility';
import VisibilityOffIcon from '@mui/icons-material/VisibilityOff';
import type { JsonSchemaProperty } from '../types';

interface ProviderEntry {
  name: string;
  baseUrl: string;
  apiKey: string;
}

interface SchemaFieldProps {
  property: JsonSchemaProperty;
  propertyKey: string;
  value: unknown;
  onChange: (value: unknown) => void;
  suggestions?: string[];
}

function getWidgetType(property: JsonSchemaProperty): string {
  if (property['x-widget']) return property['x-widget'];
  if (property.format === 'password') return 'password';
  if (property.enum) return 'select';
  if (property.type === 'number') return 'number';
  if (property.type === 'boolean') return 'boolean';
  if (property.type === 'array') return 'stringArray';
  return 'string';
}

export default function SchemaField({ property, propertyKey, value, onChange, suggestions }: SchemaFieldProps) {
  const { t } = useTranslation();
  const [showPassword, setShowPassword] = useState(false);

  const widgetType = getWidgetType(property);
  const label = property.title ?? propertyKey;
  const description = property.description;
  const placeholder = property['x-placeholder'];

  switch (widgetType) {
    case 'string': {
      if (suggestions?.length) {
        return (
          <Box>
            <Autocomplete
              freeSolo
              options={suggestions}
              value={(value as string) ?? ''}
              onInputChange={(_event, newValue) => onChange(newValue)}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={label}
                  helperText={description}
                  placeholder={placeholder}
                  size="small"
                />
              )}
            />
          </Box>
        );
      }
      return (
        <TextField
          label={label}
          helperText={description}
          value={(value as string) ?? ''}
          placeholder={placeholder}
          size="small"
          fullWidth
          onChange={(event) => onChange(event.target.value)}
        />
      );
    }

    case 'number': {
      const hasValue = value != null && value !== 0;
      return (
        <TextField
          label={label}
          helperText={description}
          type="number"
          value={hasValue ? String(value) : ''}
          placeholder={placeholder}
          size="small"
          fullWidth
          onChange={(event) => {
            const parsed = event.target.value === '' ? undefined : Number(event.target.value);
            onChange(parsed);
          }}
        />
      );
    }

    case 'boolean':
      return (
        <FormControlLabel
          control={
            <Switch
              checked={!!value}
              onChange={() => onChange(!value)}
              color="primary"
            />
          }
          label={
            <Box>
              <Typography variant="body2" sx={{ fontWeight: 500 }}>{label}</Typography>
              {description && (
                <Typography variant="caption" color="text.secondary">{description}</Typography>
              )}
            </Box>
          }
          sx={{ alignItems: 'flex-start', ml: 0 }}
        />
      );

    case 'select':
      return (
        <TextField
          select
          label={label}
          helperText={description}
          value={(value as string) ?? (property.default as string) ?? ''}
          size="small"
          fullWidth
          onChange={(event) => onChange(event.target.value)}
        >
          {property.enum?.map((enumValue) => (
            <MenuItem key={enumValue} value={enumValue}>
              {property['x-enumLabels']?.[enumValue] ?? enumValue}
            </MenuItem>
          ))}
        </TextField>
      );

    case 'password':
      return (
        <TextField
          label={label}
          helperText={description}
          type={showPassword ? 'text' : 'password'}
          value={(value as string) ?? ''}
          placeholder={placeholder}
          size="small"
          fullWidth
          onChange={(event) => onChange(event.target.value)}
          slotProps={{
            input: {
              endAdornment: (
                <InputAdornment position="end">
                  <IconButton
                    size="small"
                    onClick={() => setShowPassword(!showPassword)}
                    edge="end"
                  >
                    {showPassword ? <VisibilityOffIcon fontSize="small" /> : <VisibilityIcon fontSize="small" />}
                  </IconButton>
                </InputAdornment>
              ),
            },
          }}
        />
      );

    case 'textarea':
      return (
        <TextField
          label={label}
          helperText={description}
          multiline
          minRows={4}
          value={(value as string) ?? ''}
          placeholder={placeholder}
          size="small"
          fullWidth
          onChange={(event) => onChange(event.target.value)}
        />
      );

    case 'stringArray':
      return <StringArrayField property={property} value={value} onChange={onChange} />;

    case 'providers':
      return <ProvidersField property={property} value={value} onChange={onChange} />;

    default:
      return null;
  }
}

function StringArrayField({
  property,
  value,
  onChange,
}: {
  property: JsonSchemaProperty;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const { t } = useTranslation();
  const items: string[] = Array.isArray(value) ? (value as string[]) : [];
  const [inputValue, setInputValue] = useState('');

  function addItem() {
    const trimmed = inputValue.trim();
    if (trimmed && !items.includes(trimmed)) {
      onChange([...items, trimmed]);
    }
    setInputValue('');
  }

  function removeItem(index: number) {
    onChange(items.filter((_, itemIndex) => itemIndex !== index));
  }

  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 500, mb: 0.5 }}>{property.title}</Typography>
      {property.description && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>{property.description}</Typography>
      )}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mb: 1 }}>
        {items.map((item, index) => (
          <Chip
            key={index}
            label={item}
            size="small"
            onDelete={() => removeItem(index)}
          />
        ))}
      </Box>
      <Box sx={{ display: 'flex', gap: 1 }}>
        <TextField
          size="small"
          fullWidth
          value={inputValue}
          placeholder={t('schema.addItemPlaceholder')}
          onChange={(event) => setInputValue(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault();
              addItem();
            }
          }}
        />
        <Button variant="contained" size="small" onClick={addItem}>
          {t('common.add')}
        </Button>
      </Box>
    </Box>
  );
}

function ProvidersField({
  property,
  value,
  onChange,
}: {
  property: JsonSchemaProperty;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const { t } = useTranslation();
  const providers: Record<string, { baseUrl: string; apiKey: string }> =
    (value as Record<string, { baseUrl: string; apiKey: string }>) ?? {};

  const entries: ProviderEntry[] = Object.entries(providers).map(([name, config]) => ({
    name,
    baseUrl: config.baseUrl ?? '',
    apiKey: config.apiKey ?? '',
  }));

  const [newName, setNewName] = useState('');

  function updateEntry(index: number, updates: Partial<ProviderEntry>) {
    const updated = [...entries];
    updated[index] = { ...updated[index], ...updates };
    const result: Record<string, { baseUrl: string; apiKey: string }> = {};
    for (const entry of updated) {
      result[entry.name] = { baseUrl: entry.baseUrl, apiKey: entry.apiKey };
    }
    onChange(result);
  }

  function removeEntry(index: number) {
    const updated = entries.filter((_, entryIndex) => entryIndex !== index);
    const result: Record<string, { baseUrl: string; apiKey: string }> = {};
    for (const entry of updated) {
      result[entry.name] = { baseUrl: entry.baseUrl, apiKey: entry.apiKey };
    }
    onChange(result);
  }

  function addEntry() {
    const trimmed = newName.trim();
    if (!trimmed || providers[trimmed]) return;
    onChange({ ...providers, [trimmed]: { baseUrl: '', apiKey: '' } });
    setNewName('');
  }

  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 500, mb: 0.5 }}>{property.title}</Typography>
      {property.description && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>{property.description}</Typography>
      )}
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        {entries.map((entry, index) => (
          <Paper key={entry.name} variant="outlined" sx={{ p: 1.5 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1 }}>
              <Typography variant="body2" sx={{ fontWeight: 500, fontFamily: 'monospace' }}>{entry.name}</Typography>
              <Button size="small" color="error" onClick={() => removeEntry(index)}>
                {t('common.delete')}
              </Button>
            </Box>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
              <TextField
                size="small"
                fullWidth
                value={entry.baseUrl}
                placeholder={t('schema.baseUrlPlaceholder')}
                onChange={(event) => updateEntry(index, { baseUrl: event.target.value })}
              />
              <TextField
                size="small"
                fullWidth
                type="password"
                value={entry.apiKey}
                placeholder={t('schema.apiKeyPlaceholder')}
                onChange={(event) => updateEntry(index, { apiKey: event.target.value })}
              />
            </Box>
          </Paper>
        ))}
      </Box>
      <Box sx={{ display: 'flex', gap: 1, mt: 1.5 }}>
        <TextField
          size="small"
          fullWidth
          value={newName}
          placeholder={t('schema.providerNamePlaceholder')}
          onChange={(event) => setNewName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault();
              addEntry();
            }
          }}
        />
        <Button variant="contained" size="small" onClick={addEntry}>
          {t('schema.addProvider')}
        </Button>
      </Box>
    </Box>
  );
}
