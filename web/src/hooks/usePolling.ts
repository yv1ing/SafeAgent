import { useEffect, useState } from 'react';

/**
 * Returns a refetch interval, or false when polling is paused or the tab is hidden. One hook keeps
 * every page on the same visibility rule.
 */
export function usePollingInterval(intervalMs: number, paused = false) {
  const [visible, setVisible] = useState(() =>
    typeof document === 'undefined' ? true : !document.hidden,
  );
  useEffect(() => {
    const update = () => setVisible(!document.hidden);
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);
  return paused || !visible ? false : intervalMs;
}
