import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import Switch from "@mui/material/Switch";
import FormControlLabel from "@mui/material/FormControlLabel";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Button from "@mui/material/Button";
import Paper from "@mui/material/Paper";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import Autocomplete from "@mui/material/Autocomplete";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import type { JSONSchemaProperty } from "../types";
import {
  getEnumLabel,
  getPropertyDescription,
  getPropertyPlaceholder,
  getPropertyTitle,
} from "../schemaLocalization";

interface ProviderEntry {
  name: string;
  baseUrl: string;
  apiKey: string;
}

interface ModelRuntimeLimitEntry {
  model?: string;
  maxToolRounds?: number;
  compressionThreshold?: number;
  minKeepMessages?: number;
  maxToolResultCharacters?: number;
  maxWorkspaceFileCharacters?: number;
}

interface SkillsRegistryEntry {
  id?: string;
  publisher?: string;
  indexUrl?: string;
  publicKeys?: string[];
  ignoreSignatures?: boolean;
  ignoreUpdates?: boolean;
}

interface SchemaFieldProps {
  property: JSONSchemaProperty;
  propertyKey: string;
  value: unknown;
  onChange: (value: unknown) => void;
  suggestions?: string[];
}

function getWidgetType(property: JSONSchemaProperty): string {
  if (property["x-widget"]) return property["x-widget"];
  if (property.format === "password") return "password";
  if (property.enum) return "select";
  if (property.type === "number") return "number";
  if (property.type === "boolean") return "boolean";
  if (property.type === "array") return "stringArray";
  return "string";
}

export default function SchemaField({
  property,
  propertyKey,
  value,
  onChange,
  suggestions,
}: SchemaFieldProps) {
  const { t } = useTranslation();
  const [showPassword, setShowPassword] = useState(false);

  const widgetType = getWidgetType(property);
  const label = getPropertyTitle(t, property, propertyKey);
  const description = getPropertyDescription(t, property);
  const placeholder = getPropertyPlaceholder(t, property);

  switch (widgetType) {
    case "string": {
      if (suggestions?.length) {
        return (
          <Box>
            <Autocomplete
              freeSolo
              options={suggestions}
              value={(value as string) ?? ""}
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
          value={(value as string) ?? ""}
          placeholder={placeholder}
          size="small"
          fullWidth
          onChange={(event) => onChange(event.target.value)}
        />
      );
    }

    case "number": {
      const hasValue = value != null && value !== 0;
      return (
        <TextField
          label={label}
          helperText={description}
          type="number"
          value={hasValue ? String(value) : ""}
          placeholder={placeholder}
          size="small"
          fullWidth
          onChange={(event) => {
            const parsed =
              event.target.value === ""
                ? undefined
                : Number(event.target.value);
            onChange(parsed);
          }}
        />
      );
    }

    case "boolean":
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
              <Typography variant="body2" sx={{ fontWeight: 500 }}>
                {label}
              </Typography>
              {description && (
                <Typography variant="caption" color="text.secondary">
                  {description}
                </Typography>
              )}
            </Box>
          }
          sx={{ alignItems: "flex-start", ml: 0 }}
        />
      );

    case "select":
      return (
        <TextField
          select
          label={label}
          helperText={description}
          value={(value as string) ?? (property.default as string) ?? ""}
          size="small"
          fullWidth
          onChange={(event) => onChange(event.target.value)}
        >
          {property.enum?.map((enumValue) => (
            <MenuItem key={enumValue} value={enumValue}>
              {getEnumLabel(t, property, enumValue)}
            </MenuItem>
          ))}
        </TextField>
      );

    case "password":
      return (
        <TextField
          label={label}
          helperText={description}
          type={showPassword ? "text" : "password"}
          value={(value as string) ?? ""}
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
                    {showPassword ? (
                      <VisibilityOffIcon fontSize="small" />
                    ) : (
                      <VisibilityIcon fontSize="small" />
                    )}
                  </IconButton>
                </InputAdornment>
              ),
            },
          }}
        />
      );

    case "textarea":
      return (
        <TextField
          label={label}
          helperText={description}
          multiline
          minRows={4}
          value={(value as string) ?? ""}
          placeholder={placeholder}
          size="small"
          fullWidth
          onChange={(event) => onChange(event.target.value)}
        />
      );

    case "stringArray":
      return (
        <StringArrayField
          property={property}
          value={value}
          onChange={onChange}
          suggestions={suggestions}
        />
      );

    case "providers":
      return (
        <ProvidersField property={property} value={value} onChange={onChange} />
      );

    case "modelRuntimeLimits":
      return (
        <ModelRuntimeLimitsField
          property={property}
          value={value}
          onChange={onChange}
          suggestions={suggestions}
        />
      );

    case "skillsRegistries":
      return (
        <SkillsRegistriesField
          property={property}
          value={value}
          onChange={onChange}
        />
      );

    default:
      return null;
  }
}

function StringArrayField({
  property,
  value,
  onChange,
  suggestions,
}: {
  property: JSONSchemaProperty;
  value: unknown;
  onChange: (value: unknown) => void;
  suggestions?: string[];
}) {
  const { t } = useTranslation();
  const items: string[] = Array.isArray(value) ? (value as string[]) : [];
  const [inputValue, setInputValue] = useState("");
  const label = getPropertyTitle(t, property, "items");
  const description = getPropertyDescription(t, property);

  function addItem(text?: string) {
    const trimmed = (text ?? inputValue).trim();
    if (trimmed && !items.includes(trimmed)) {
      onChange([...items, trimmed]);
    }
    setInputValue("");
  }

  function removeItem(index: number) {
    onChange(items.filter((_, itemIndex) => itemIndex !== index));
  }

  // Filter out already-selected items from suggestions.
  const available = suggestions?.filter((option) => !items.includes(option));

  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 500, mb: 0.5 }}>
        {label}
      </Typography>
      {description && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: "block", mb: 1 }}
        >
          {description}
        </Typography>
      )}
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75, mb: 1 }}>
        {items.map((item, index) => (
          <Chip
            key={index}
            label={item}
            size="small"
            onDelete={() => removeItem(index)}
          />
        ))}
      </Box>
      <Box sx={{ display: "flex", gap: 1 }}>
        {available?.length ? (
          <Autocomplete
            freeSolo
            fullWidth
            options={available}
            inputValue={inputValue}
            onInputChange={(_event, newValue) => setInputValue(newValue)}
            onChange={(_event, newValue) => {
              if (newValue)
                addItem(typeof newValue === "string" ? newValue : "");
            }}
            renderInput={(params) => (
              <TextField
                {...params}
                size="small"
                placeholder={t("schema.addItemPlaceholder")}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    addItem();
                  }
                }}
              />
            )}
          />
        ) : (
          <TextField
            size="small"
            fullWidth
            value={inputValue}
            placeholder={t("schema.addItemPlaceholder")}
            onChange={(event) => setInputValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                addItem();
              }
            }}
          />
        )}
        <Button variant="contained" size="small" onClick={() => addItem()}>
          {t("common.add")}
        </Button>
      </Box>
    </Box>
  );
}

const SUPPORTED_PROVIDERS = ["openai", "anthropic", "openrouter"] as const;

const PROVIDER_LABELS: Record<string, string> = {
  openai: "OpenAI Compatible",
  anthropic: "Anthropic",
  openrouter: "OpenRouter",
};

function ProvidersField({
  property,
  value,
  onChange,
}: {
  property: JSONSchemaProperty;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const { t } = useTranslation();
  const entries: ProviderEntry[] = (Array.isArray(value) ? value : []).map(
    (entry: ProviderEntry) => ({
      name: entry.name ?? "",
      baseUrl: entry.baseUrl ?? "",
      apiKey: entry.apiKey ?? "",
    }),
  );

  const [newName, setNewName] = useState("");
  const label = getPropertyTitle(t, property, "providers");
  const description = getPropertyDescription(t, property);

  function updateEntry(index: number, updates: Partial<ProviderEntry>) {
    const updated = [...entries];
    updated[index] = { ...updated[index], ...updates };
    onChange(updated);
  }

  function removeEntry(index: number) {
    onChange(entries.filter((_, entryIndex) => entryIndex !== index));
  }

  function addEntry() {
    const trimmed = newName.trim();
    if (!trimmed || entries.some((entry) => entry.name === trimmed)) return;
    onChange([...entries, { name: trimmed, baseUrl: "", apiKey: "" }]);
    setNewName("");
  }

  // Only show providers that haven't been added yet.
  const availableProviders = SUPPORTED_PROVIDERS.filter(
    (provider) => !entries.some((entry) => entry.name === provider),
  );

  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 500, mb: 0.5 }}>
        {label}
      </Typography>
      {description && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: "block", mb: 1 }}
        >
          {description}
        </Typography>
      )}
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
        {entries.map((entry, index) => (
          <Paper key={entry.name} variant="outlined" sx={{ p: 1.5 }}>
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                mb: 1,
              }}
            >
              <Typography
                variant="body2"
                sx={{ fontWeight: 500, fontFamily: "monospace" }}
              >
                {PROVIDER_LABELS[entry.name] || entry.name}
              </Typography>
              <Button
                size="small"
                color="error"
                onClick={() => removeEntry(index)}
              >
                {t("common.delete")}
              </Button>
            </Box>
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <TextField
                size="small"
                fullWidth
                value={entry.baseUrl}
                placeholder={t("schema.baseUrlPlaceholder")}
                onChange={(event) =>
                  updateEntry(index, { baseUrl: event.target.value })
                }
              />
              <TextField
                size="small"
                fullWidth
                type="password"
                value={entry.apiKey}
                placeholder={t("schema.apiKeyPlaceholder")}
                onChange={(event) =>
                  updateEntry(index, { apiKey: event.target.value })
                }
              />
            </Box>
          </Paper>
        ))}
      </Box>
      {availableProviders.length > 0 && (
        <Box sx={{ display: "flex", gap: 1, mt: 1.5 }}>
          <TextField
            select
            size="small"
            fullWidth
            value={newName}
            onChange={(event) => setNewName(event.target.value)}
            label={t("schema.providerNamePlaceholder")}
          >
            {availableProviders.map((provider) => (
              <MenuItem key={provider} value={provider}>
                {PROVIDER_LABELS[provider] || provider}
              </MenuItem>
            ))}
          </TextField>
          <Button
            variant="contained"
            size="small"
            onClick={addEntry}
            disabled={!newName}
          >
            {t("schema.addProvider")}
          </Button>
        </Box>
      )}
    </Box>
  );
}

function ModelRuntimeLimitsField({
  property,
  value,
  onChange,
  suggestions,
}: {
  property: JSONSchemaProperty;
  value: unknown;
  onChange: (value: unknown) => void;
  suggestions?: string[];
}) {
  const { t } = useTranslation();
  const entries: ModelRuntimeLimitEntry[] = Array.isArray(value)
    ? (value as ModelRuntimeLimitEntry[])
    : [];
  const itemProperties = property.items?.properties ?? {};
  const limitFields = Object.entries(itemProperties).filter(
    ([key]) => key !== "model",
  );
  const label = getPropertyTitle(t, property, "limits");
  const description = getPropertyDescription(t, property);
  const modelLabel = itemProperties.model
    ? getPropertyTitle(t, itemProperties.model, "model")
    : "Model";
  const modelDescription = itemProperties.model
    ? getPropertyDescription(t, itemProperties.model)
    : undefined;

  function updateEntry(
    index: number,
    updates: Partial<ModelRuntimeLimitEntry>,
  ) {
    const updated = [...entries];
    updated[index] = { ...updated[index], ...updates };
    onChange(updated);
  }

  function removeEntry(index: number) {
    onChange(entries.filter((_, entryIndex) => entryIndex !== index));
  }

  function addEntry() {
    onChange([...entries, {}]);
  }

  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 500, mb: 0.5 }}>
        {label}
      </Typography>
      {description && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: "block", mb: 1 }}
        >
          {description}
        </Typography>
      )}
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
        {entries.map((entry, index) => (
          <Paper
            key={`${entry.model ?? "model"}-${index}`}
            variant="outlined"
            sx={{ p: 1.5 }}
          >
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                mb: 1,
              }}
            >
              <Typography variant="body2" sx={{ fontWeight: 500 }}>
                {modelLabel}
              </Typography>
              <Button
                size="small"
                color="error"
                onClick={() => removeEntry(index)}
              >
                {t("common.delete")}
              </Button>
            </Box>
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              {suggestions?.length ? (
                <Autocomplete
                  freeSolo
                  options={suggestions}
                  value={entry.model ?? ""}
                  onInputChange={(_event, newValue) =>
                    updateEntry(index, { model: newValue })
                  }
                  renderInput={(params) => (
                    <TextField
                      {...params}
                      size="small"
                      fullWidth
                      label={modelLabel}
                      helperText={modelDescription}
                    />
                  )}
                />
              ) : (
                <TextField
                  size="small"
                  fullWidth
                  label={modelLabel}
                  helperText={modelDescription}
                  value={entry.model ?? ""}
                  onChange={(event) =>
                    updateEntry(index, { model: event.target.value })
                  }
                />
              )}
              {limitFields.map(([fieldKey, fieldProperty]) => {
                const rawValue = (entry as Record<string, unknown>)[fieldKey];
                const hasValue = rawValue != null && rawValue !== 0;
                const fieldLabel = getPropertyTitle(t, fieldProperty, fieldKey);
                const fieldDescription = getPropertyDescription(
                  t,
                  fieldProperty,
                );
                return (
                  <TextField
                    key={fieldKey}
                    type="number"
                    size="small"
                    fullWidth
                    label={fieldLabel}
                    helperText={fieldDescription}
                    value={hasValue ? String(rawValue) : ""}
                    onChange={(event) => {
                      const parsed =
                        event.target.value === ""
                          ? undefined
                          : Number(event.target.value);
                      updateEntry(index, {
                        [fieldKey]: parsed,
                      } as Partial<ModelRuntimeLimitEntry>);
                    }}
                  />
                );
              })}
            </Box>
          </Paper>
        ))}
      </Box>
      <Box sx={{ mt: 1.5 }}>
        <Button variant="contained" size="small" onClick={addEntry}>
          {t("common.add")}
        </Button>
      </Box>
    </Box>
  );
}

function SkillsRegistriesField({
  property: _property,
  value,
  onChange,
}: {
  property: JSONSchemaProperty;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const { t } = useTranslation();
  const entries: SkillsRegistryEntry[] = Array.isArray(value)
    ? (value as SkillsRegistryEntry[])
    : [];
  const [newPublicKeyByIndex, setNewPublicKeyByIndex] = useState<
    Record<number, string>
  >({});

  function addRegistry() {
    onChange([
      ...entries,
      {
        id: "",
        publisher: "",
        indexUrl: "",
        publicKeys: [],
        ignoreSignatures: false,
        ignoreUpdates: false,
      },
    ]);
  }

  function updateRegistry(
    index: number,
    updates: Partial<SkillsRegistryEntry>,
  ) {
    const updated = [...entries];
    updated[index] = { ...updated[index], ...updates };
    onChange(updated);
  }

  function removeRegistry(index: number) {
    onChange(entries.filter((_, entryIndex) => entryIndex !== index));
  }

  function addPublicKey(index: number) {
    const text = (newPublicKeyByIndex[index] || "").trim();
    if (!text) return;
    const keys = entries[index].publicKeys || [];
    if (keys.includes(text)) return;
    updateRegistry(index, { publicKeys: [...keys, text] });
    setNewPublicKeyByIndex((previous) => ({ ...previous, [index]: "" }));
  }

  function removePublicKey(index: number, publicKeyIndex: number) {
    const keys = entries[index].publicKeys || [];
    updateRegistry(index, {
      publicKeys: keys.filter((_, idx) => idx !== publicKeyIndex),
    });
  }

  return (
    <Box>
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
        {entries.map((entry, index) => (
          <Paper
            key={`${entry.id || "registry"}-${index}`}
            variant="outlined"
            sx={{ p: 1.5 }}
          >
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                mb: 1,
              }}
            >
              <Typography variant="body2" sx={{ fontWeight: 600 }}>
                {entry.id || t("settings.skillsRegistryUntitled")}
              </Typography>
              <Button
                size="small"
                color="error"
                onClick={() => removeRegistry(index)}
              >
                {t("common.delete")}
              </Button>
            </Box>

            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <TextField
                size="small"
                fullWidth
                label="ID"
                value={entry.id || ""}
                onChange={(event) =>
                  updateRegistry(index, { id: event.target.value })
                }
              />
              <TextField
                size="small"
                fullWidth
                label="Publisher"
                value={entry.publisher || ""}
                onChange={(event) =>
                  updateRegistry(index, { publisher: event.target.value })
                }
              />
              <TextField
                size="small"
                fullWidth
                label="Index URL"
                value={entry.indexUrl || ""}
                onChange={(event) =>
                  updateRegistry(index, { indexUrl: event.target.value })
                }
              />

              <FormControlLabel
                control={
                  <Switch
                    checked={!!entry.ignoreSignatures}
                    onChange={(_event, checked) =>
                      updateRegistry(index, { ignoreSignatures: checked })
                    }
                  />
                }
                label={t("settings.ignoreSignatures")}
              />
              <FormControlLabel
                control={
                  <Switch
                    checked={!!entry.ignoreUpdates}
                    onChange={(_event, checked) =>
                      updateRegistry(index, { ignoreUpdates: checked })
                    }
                  />
                }
                label={t("settings.ignoreUpdates")}
              />

              <Box>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ display: "block", mb: 0.5 }}
                >
                  {t("settings.publicKeys")}
                </Typography>
                <Box
                  sx={{ display: "flex", flexWrap: "wrap", gap: 0.75, mb: 1 }}
                >
                  {(entry.publicKeys || []).map((publicKey, publicKeyIndex) => (
                    <Chip
                      key={`${publicKeyIndex}-${publicKey.slice(0, 16)}`}
                      label={
                        publicKey.length > 36
                          ? `${publicKey.slice(0, 36)}...`
                          : publicKey
                      }
                      size="small"
                      onDelete={() => removePublicKey(index, publicKeyIndex)}
                    />
                  ))}
                </Box>
                <Box sx={{ display: "flex", gap: 1 }}>
                  <TextField
                    size="small"
                    fullWidth
                    value={newPublicKeyByIndex[index] || ""}
                    onChange={(event) =>
                      setNewPublicKeyByIndex((previous) => ({
                        ...previous,
                        [index]: event.target.value,
                      }))
                    }
                    placeholder={t("settings.addPublicKey")}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") {
                        event.preventDefault();
                        addPublicKey(index);
                      }
                    }}
                  />
                  <Button
                    variant="contained"
                    size="small"
                    onClick={() => addPublicKey(index)}
                  >
                    {t("common.add")}
                  </Button>
                </Box>
              </Box>
            </Box>
          </Paper>
        ))}
      </Box>

      <Box sx={{ mt: 1.5 }}>
        <Button variant="contained" size="small" onClick={addRegistry}>
          {t("settings.addSkillsRegistry")}
        </Button>
      </Box>
    </Box>
  );
}
