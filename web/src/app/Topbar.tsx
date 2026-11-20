import { Link, useRouterState } from '@tanstack/react-router';
import { Menu } from 'lucide-react';
import { useLocale } from '../i18n';
import { cn } from '../lib/cn';
import { useCurrentUser, useOptionalProject } from './context';
import { LanguageMenu } from './LanguageMenu';
import {
  globalPageLabelKey,
  normalizePath,
  projectSectionFromPath,
  projectSections,
} from './navigation';
import { ProjectSwitcher } from './ProjectSwitcher';
import { ThemeToggle } from './ThemeToggle';
import { UserMenu } from './UserMenu';

function Breadcrumb() {
  const { t } = useLocale();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const project = useOptionalProject();
  const path = normalizePath(pathname);

  if (project) {
    const section = projectSectionFromPath(pathname);
    const sectionLabel = projectSections.find((item) => item.section === section)?.label(t);
    return (
      <nav aria-label={t.app.breadcrumb} className="flex min-w-0 items-center gap-1.5 text-sm">
        <Link to="/projects" className="hidden shrink-0 text-muted hover:text-ink sm:inline">
          {t.nav.projects}
        </Link>
        <span aria-hidden className="hidden text-faint sm:inline">
          /
        </span>
        <ProjectSwitcher />
        {section !== 'overview' && sectionLabel && (
          <>
            <span aria-hidden className="hidden text-faint sm:inline">
              /
            </span>
            <span className="hidden truncate font-medium text-ink sm:inline">{sectionLabel}</span>
          </>
        )}
      </nav>
    );
  }

  const key = globalPageLabelKey(pathname);
  return (
    <nav aria-label={t.app.breadcrumb} className="flex min-w-0 items-center gap-1.5 text-sm">
      <Link to="/projects" className="hidden shrink-0 text-muted hover:text-ink sm:inline">
        {t.nav.projects}
      </Link>
      {path === '/projects' && (
        <span className="truncate font-medium text-ink sm:hidden">{t.nav.projects}</span>
      )}
      {path !== '/projects' && (
        <>
          <span aria-hidden className="hidden text-faint sm:inline">
            /
          </span>
          <span className="truncate font-medium text-ink">{t.nav[key]}</span>
        </>
      )}
    </nav>
  );
}

export function Topbar({ onOpenMenu }: { onOpenMenu: () => void }) {
  const { t } = useLocale();
  const project = useOptionalProject();
  const user = useCurrentUser();

  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b bg-surface/90 px-3 backdrop-blur sm:px-4">
      <button
        type="button"
        aria-label={t.app.openMenu}
        onClick={onOpenMenu}
        className="rounded-md p-1.5 text-muted hover:bg-sunken hover:text-ink lg:hidden"
      >
        <Menu aria-hidden className="size-5" />
      </button>
      <div className={cn('min-w-0 flex-1', !project && 'pl-0.5')}>
        <Breadcrumb />
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <LanguageMenu />
        <ThemeToggle />
        <UserMenu username={user.username} />
      </div>
    </header>
  );
}
