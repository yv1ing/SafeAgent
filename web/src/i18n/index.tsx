import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { errorCode } from '../api/client';
import { en, type Messages } from './messages';
import { zhCN } from './zh-CN';

export type Locale = 'en' | 'zh-CN';

export const locales: Record<Locale, string> = { en: 'English', 'zh-CN': '简体中文' };

const catalogs: Record<Locale, Messages> = { en, 'zh-CN': zhCN };
const storageKey = 'safeagent-locale';

function initialLocale(): Locale {
  const stored = localStorage.getItem(storageKey);
  if (stored === 'en' || stored === 'zh-CN') return stored;
  return navigator.language.startsWith('zh') ? 'zh-CN' : 'en';
}

interface LocaleValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: Messages;
}

const LocaleContext = createContext<LocaleValue | null>(null);

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);
  const value = useMemo<LocaleValue>(
    () => ({
      locale,
      t: catalogs[locale],
      setLocale: (next) => {
        localStorage.setItem(storageKey, next);
        setLocaleState(next);
      },
    }),
    [locale],
  );
  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}

export function useLocale() {
  const value = useContext(LocaleContext);
  if (!value) throw new Error('useLocale must be used inside LocaleProvider');
  return value;
}

export function interpolate(template: string, values: Record<string, string | number>) {
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in values ? String(values[name]) : match,
  );
}

export function errorMessage(t: Messages, error: unknown) {
  const code = errorCode(error);
  return code in t.errors ? t.errors[code as keyof Messages['errors']] : t.errors.internal_error;
}

export function findingLabel(t: Messages, code: string) {
  return code in t.findingCodes ? t.findingCodes[code as keyof Messages['findingCodes']] : code;
}
