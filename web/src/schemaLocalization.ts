import type { TFunction } from "i18next";
import type { JsonSchemaProperty, SchemaSection } from "./types";

function localizeText(
  t: TFunction,
  key: string | undefined,
  fallback?: string,
): string | undefined {
  if (!key) return fallback;
  const translated = t(key, { defaultValue: fallback ?? "" });
  return translated || fallback;
}

export function getPropertyTitle(
  t: TFunction,
  property: JsonSchemaProperty,
  propertyKey: string,
): string {
  return (
    localizeText(
      t,
      property.titleKey ?? property["x-titleKey"],
      property.title,
    ) ?? propertyKey
  );
}

export function getPropertyDescription(
  t: TFunction,
  property: JsonSchemaProperty,
): string | undefined {
  return localizeText(
    t,
    property.descriptionKey ?? property["x-descriptionKey"],
    property.description,
  );
}

export function getPropertyPlaceholder(
  t: TFunction,
  property: JsonSchemaProperty,
): string | undefined {
  return localizeText(
    t,
    property["x-placeholderKey"],
    property["x-placeholder"],
  );
}

export function getEnumLabel(
  t: TFunction,
  property: JsonSchemaProperty,
  value: string,
): string {
  const fallback = property["x-enumLabels"]?.[value] ?? value;
  return (
    localizeText(t, property["x-enumLabelKeys"]?.[value], fallback) ?? fallback
  );
}

export function getSectionTitle(t: TFunction, section: SchemaSection): string {
  return (
    localizeText(t, section.titleKey ?? section["x-titleKey"], section.title) ??
    section.id
  );
}

export function getSectionDescription(
  t: TFunction,
  section: SchemaSection,
): string | undefined {
  return localizeText(
    t,
    section.descriptionKey ?? section["x-descriptionKey"],
    section.description,
  );
}
