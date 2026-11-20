import { useSyncExternalStore } from 'react';

export type ThemeChoice = 'system' | 'light' | 'dark';
type ResolvedTheme = 'light' | 'dark';

const storageKey = 'safeagent-theme';
const systemDark = matchMedia('(prefers-color-scheme: dark)');
const listeners = new Set<() => void>();

function storedChoice(): ThemeChoice {
  const value = localStorage.getItem(storageKey);
  return value === 'light' || value === 'dark' ? value : 'system';
}

let snapshot = resolve(storedChoice());

function resolve(choice: ThemeChoice) {
  const theme: ResolvedTheme =
    choice === 'dark' || (choice === 'system' && systemDark.matches) ? 'dark' : 'light';
  return { choice, theme };
}

function publish(choice: ThemeChoice) {
  snapshot = resolve(choice);
  document.documentElement.dataset.theme = snapshot.theme;
  listeners.forEach((listener) => listener());
}

systemDark.addEventListener('change', () => publish(snapshot.choice));

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function selectTheme(choice: ThemeChoice) {
  if (choice === 'system') localStorage.removeItem(storageKey);
  else localStorage.setItem(storageKey, choice);
  publish(choice);
}

export function useTheme() {
  return useSyncExternalStore(subscribe, () => snapshot);
}
