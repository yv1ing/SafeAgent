import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Outlet, useRouterState } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import { useGetProject } from '../api/generated/projects/projects';
import { useLocale } from '../i18n';
import { slashFocusAttribute } from '../lib/shortcuts';
import { ProjectProvider, useCurrentProjectId } from './context';
import { Sidebar } from './Sidebar';
import { Topbar } from './Topbar';

const collapsedKey = 'safeagent-sidebar-collapsed';

function useSlashFocus() {
  useEffect(() => {
    const focus = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (event.key !== '/' || target.closest('input, textarea, select, [contenteditable]')) return;
      const input = document.querySelector<HTMLElement>(`[${slashFocusAttribute}]`);
      if (!input) return;
      event.preventDefault();
      input.focus();
    };
    document.addEventListener('keydown', focus);
    return () => document.removeEventListener('keydown', focus);
  }, []);
}

function MobileNavigation({
  open,
  onOpenChange,
  title,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 animate-fade-in bg-overlay lg:hidden" />
        <DialogPrimitive.Content className="fixed inset-y-0 left-0 z-50 w-72 max-w-[85vw] border-r bg-surface shadow-pop lg:hidden">
          <DialogPrimitive.Title className="sr-only">{title}</DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">{title}</DialogPrimitive.Description>
          <Sidebar collapsed={false} mobile onNavigate={() => onOpenChange(false)} />
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function Shell() {
  const { t } = useLocale();
  const projectId = useCurrentProjectId();
  const projectQuery = useGetProject(projectId ?? '', {
    query: { enabled: Boolean(projectId), retry: false },
  });
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(collapsedKey) === '1');
  const [mobileOpen, setMobileOpen] = useState(false);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  useSlashFocus();

  useEffect(() => setMobileOpen(false), [pathname]);

  const projectValue = useMemo(
    () => (projectId ? { projectId, project: projectQuery.data, query: projectQuery } : null),
    [projectId, projectQuery],
  );

  const toggle = () => {
    const next = !collapsed;
    localStorage.setItem(collapsedKey, next ? '1' : '0');
    setCollapsed(next);
  };

  return (
    <ProjectProvider value={projectValue}>
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-accent px-3 py-2 text-accent-ink focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
      >
        {t.app.skipToContent}
      </a>
      <div className="flex min-h-dvh">
        <aside
          className="sticky top-0 hidden h-dvh shrink-0 border-r transition-[width] duration-150 lg:block"
          style={{ width: collapsed ? 64 : 240 }}
        >
          <Sidebar collapsed={collapsed} onToggle={toggle} />
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          <Topbar onOpenMenu={() => setMobileOpen(true)} />
          <main id="main" className="min-w-0 flex-1 px-4 py-5 sm:px-6 lg:px-8 lg:py-8">
            <div className="mx-auto w-full max-w-[1440px]">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
      <MobileNavigation open={mobileOpen} onOpenChange={setMobileOpen} title={t.app.navigation} />
    </ProjectProvider>
  );
}
