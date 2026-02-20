import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import en from './locales/en.json';
import zh from './locales/zh.json';
import ja from './locales/ja.json';

export const LANGUAGE_PREFERENCE_STORAGE_KEY = 'teanode-language-preference';
export type SupportedLanguage = 'en' | 'zh' | 'ja';
export type LanguagePreference = 'auto' | SupportedLanguage;

const SUPPORTED_LANGUAGES: SupportedLanguage[] = ['en', 'zh', 'ja'];

function normalizeLanguageTag(tag: string | null | undefined): string {
  return (tag || '').trim().toLowerCase();
}

function resolveSupportedLanguage(tag: string | null | undefined): SupportedLanguage | null {
  const normalized = normalizeLanguageTag(tag);
  if (!normalized) return null;

  if (SUPPORTED_LANGUAGES.includes(normalized as SupportedLanguage)) {
    return normalized as SupportedLanguage;
  }

  const base = normalized.split('-')[0];
  if (SUPPORTED_LANGUAGES.includes(base as SupportedLanguage)) {
    return base as SupportedLanguage;
  }

  return null;
}

export function resolveLanguageFromPreference(preference: LanguagePreference): SupportedLanguage {
  if (preference !== 'auto') return preference;
  if (typeof navigator !== 'undefined') {
    return resolveSupportedLanguage(navigator.language) || 'en';
  }
  return 'en';
}

function readInitialLanguagePreference(): LanguagePreference {
  if (typeof localStorage === 'undefined') return 'auto';
  const stored = normalizeLanguageTag(localStorage.getItem(LANGUAGE_PREFERENCE_STORAGE_KEY));
  if (stored === 'auto') return 'auto';
  return resolveSupportedLanguage(stored) || 'auto';
}

const initialPreference = readInitialLanguagePreference();

i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    zh: { translation: zh },
    ja: { translation: ja },
  },
  lng: resolveLanguageFromPreference(initialPreference),
  fallbackLng: 'en',
  interpolation: {
    escapeValue: false,
  },
});

export default i18n;
