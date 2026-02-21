import React from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import { useAppContext } from "../../context";
import { useSettingsContext } from "../../hooks/useSettingsContext";
import SchemaField from "../../components/SchemaField";
import type {
  ConfigSchema,
  JsonSchemaProperty,
  SchemaSection,
} from "../../types";
import {
  getPropertyDescription,
  getPropertyTitle,
  getSectionDescription,
  getSectionTitle,
} from "../../schemaLocalization";

/** Resolved entry: either a single leaf field or a group of fields under a heading. */
type SectionEntry =
  | {
      type: "field";
      key: string;
      property: JsonSchemaProperty;
      dotPath: string;
    }
  | {
      type: "group";
      key: string;
      property: JsonSchemaProperty;
      fields: { key: string; property: JsonSchemaProperty; dotPath: string }[];
    };

/** Resolve the fields for a section from the JSON Schema properties tree. */
function resolveSectionEntries(
  schema: ConfigSchema,
  section: SchemaSection,
): SectionEntry[] {
  const entries: SectionEntry[] = [];
  const rootProperties = schema.properties;

  // Collect the (key, property, dotPathPrefix) tuples for this section.
  const collected: [string, JsonSchemaProperty, string][] = [];

  if (section.path) {
    // Navigate to the nested object at `path`.
    const parts = section.path.split(".");
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
        collected.push([key, rootProperties[key], ""]);
      }
    }
  }

  for (const [key, property, prefix] of collected) {
    const dotPath = prefix ? `${prefix}.${key}` : key;

    // Objects without a custom widget are rendered as sub-groups.
    if (
      property.type === "object" &&
      !property["x-widget"] &&
      property.properties
    ) {
      entries.push({
        type: "group",
        key,
        property,
        fields: Object.entries(property.properties).map(
          ([childKey, childProperty]) => ({
            key: childKey,
            property: childProperty,
            dotPath: `${dotPath}.${childKey}`,
          }),
        ),
      });
    } else {
      entries.push({ type: "field", key, property, dotPath });
    }
  }

  return entries;
}

/** /settings/$sectionId — individual config section page. */
export default function SettingsSectionPage() {
  const { t } = useTranslation();
  const { sectionId } = useParams({ strict: false }) as { sectionId: string };
  const settings = useSettingsContext();
  const { backend } = useAppContext();

  if (settings.loading && !settings.schema) {
    return (
      <Box
        sx={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Typography variant="body2" color="text.secondary">
          {t("settings.loadingSettings")}
        </Typography>
      </Box>
    );
  }

  const section = settings.schema?.["x-sections"].find(
    (candidate) => candidate.id === sectionId,
  );

  if (!section || !settings.schema) {
    return (
      <Box
        sx={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Typography variant="body2" color="text.secondary">
          {t("settings.sectionNotFound")}
        </Typography>
      </Box>
    );
  }

  const entries = resolveSectionEntries(settings.schema, section);
  const sectionTitle = getSectionTitle(t, section);
  const sectionDescription = getSectionDescription(t, section);
  const unwrappedPanel = section.id === "skillsRegistries";

  const suggestionMap: Record<string, string[]> = {
    model: backend.models.map(
      (modelInfo) => `${modelInfo.provider}:${modelInfo.id}`,
    ),
    agent: backend.agents.map((agent) => agent.id),
  };

  return (
    <Box sx={{ flex: 1, overflowY: "auto" }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            mb: 3,
          }}
        >
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {sectionTitle}
            </Typography>
            {sectionDescription && (
              <Typography variant="caption" color="text.secondary">
                {sectionDescription}
              </Typography>
            )}
          </Box>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            {settings.dirty && (
              <Typography variant="caption" color="warning.main">
                {t("common.unsavedChanges")}
              </Typography>
            )}
            <Button
              variant={settings.dirty ? "contained" : "outlined"}
              size="small"
              disabled={!settings.dirty || settings.saving}
              onClick={settings.save}
            >
              {settings.saving ? t("common.saving") : t("common.save")}
            </Button>
          </Box>
        </Box>

        {(() => {
          // Group consecutive top-level fields into panels; each named group gets its own panel.
          const panels: {
            title?: string;
            description?: string;
            fields: SectionEntry[];
          }[] = [];
          for (const entry of entries) {
            if (entry.type === "group") {
              panels.push({
                title: getPropertyTitle(t, entry.property, entry.key),
                description: getPropertyDescription(t, entry.property),
                fields: [entry],
              });
            } else {
              // Append to the last panel if it has no title (consecutive loose fields).
              const last = panels[panels.length - 1];
              if (last && !last.title) {
                last.fields.push(entry);
              } else {
                panels.push({ fields: [entry] });
              }
            }
          }

          return (
            <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
              {panels.map((panel, panelIndex) => {
                const content = (
                  <>
                    {panel.title && (
                      <Typography
                        variant="subtitle2"
                        sx={{
                          fontWeight: 600,
                          mb: panel.description ? 0.5 : 1,
                        }}
                      >
                        {panel.title}
                      </Typography>
                    )}
                    {panel.description && (
                      <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ mb: 1.5 }}
                      >
                        {panel.description}
                      </Typography>
                    )}
                    <Box
                      sx={{ display: "flex", flexDirection: "column", gap: 2 }}
                    >
                      {panel.fields.flatMap((entry) => {
                        if (entry.type === "group") {
                          return entry.fields.map((field) => (
                            <SchemaField
                              key={field.dotPath}
                              property={field.property}
                              propertyKey={field.key}
                              value={settings.getValue(field.dotPath)}
                              onChange={(value) =>
                                settings.setValue(field.dotPath, value)
                              }
                              suggestions={
                                field.property["x-suggest"]
                                  ? suggestionMap[field.property["x-suggest"]]
                                  : undefined
                              }
                            />
                          ));
                        }
                        return (
                          <SchemaField
                            key={entry.dotPath}
                            property={entry.property}
                            propertyKey={entry.key}
                            value={settings.getValue(entry.dotPath)}
                            onChange={(value) =>
                              settings.setValue(entry.dotPath, value)
                            }
                            suggestions={
                              entry.property["x-suggest"]
                                ? suggestionMap[entry.property["x-suggest"]]
                                : undefined
                            }
                          />
                        );
                      })}
                    </Box>
                  </>
                );

                if (unwrappedPanel) {
                  return <Box key={panel.title ?? panelIndex}>{content}</Box>;
                }

                return (
                  <Paper
                    key={panel.title ?? panelIndex}
                    variant="outlined"
                    sx={{ p: 2 }}
                  >
                    {content}
                  </Paper>
                );
              })}
            </Box>
          );
        })()}
      </Container>
    </Box>
  );
}
