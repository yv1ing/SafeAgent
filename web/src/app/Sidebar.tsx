import { Link } from '@tanstack/react-router';
import { PanelLeftClose, PanelLeftOpen, ShieldCheck, X } from 'lucide-react';
import { useLocale } from '../i18n';
import { cn } from '../lib/cn';
import { Badge } from '../components/ui/badge';
import { Tooltip } from '../components/ui/tooltip';
import { useCurrentProjectId, useCurrentUser } from './context';
import { globalGroups, globalItems, projectGroups, visibleItems, type NavItem } from './navigation';

const navItemClass =
  'flex h-8 items-center gap-2.5 rounded-md px-2.5 text-sm text-muted transition-colors hover:bg-sunken hover:text-ink aria-[current=page]:bg-accent-soft aria-[current=page]:font-medium aria-[current=page]:text-accent';

function NavLink({
  item,
  projectId,
  collapsed,
  onNavigate,
}: {
  item: NavItem;
  projectId?: string;
  collapsed: boolean;
  onNavigate?: () => void;
}) {
  const { t } = useLocale();
  const label = item.label(t);
  const badge = item.badge?.(t);
  const content = (
    <>
      <item.icon aria-hidden className="size-4 shrink-0" />
      <span className={cn('truncate', collapsed && 'sr-only')}>{label}</span>
      {badge && <Badge className={cn('ml-auto', collapsed && 'sr-only')}>{badge}</Badge>}
    </>
  );
  const link =
    item.scope === 'project' && projectId ? (
      <Link
        to={item.to}
        params={{ projectId }}
        onClick={onNavigate}
        aria-label={collapsed ? label : undefined}
        className={navItemClass}
      >
        {content}
      </Link>
    ) : item.scope === 'global' ? (
      <Link
        to={item.to}
        activeOptions={{ exact: item.exact }}
        onClick={onNavigate}
        aria-label={collapsed ? label : undefined}
        className={navItemClass}
      >
        {content}
      </Link>
    ) : null;
  if (!link) return null;
  return collapsed ? <Tooltip label={label}>{link}</Tooltip> : link;
}

function SectionTitle({ children, collapsed }: { children: string; collapsed: boolean }) {
  return (
    <h2
      className={cn(
        'px-2.5 pt-4 pb-1 text-xs font-medium tracking-wide text-faint uppercase',
        collapsed && 'sr-only',
      )}
    >
      {children}
    </h2>
  );
}

export function Sidebar({
  collapsed,
  mobile = false,
  onToggle,
  onNavigate,
}: {
  collapsed: boolean;
  mobile?: boolean;
  onToggle?: () => void;
  onNavigate?: () => void;
}) {
  const { t } = useLocale();
  const user = useCurrentUser();
  const projectId = useCurrentProjectId();
  const inProject = Boolean(projectId);
  const compact = collapsed && !mobile;

  return (
    <div className="flex h-full min-h-0 flex-col bg-surface">
      <div
        className={cn(
          'flex h-14 shrink-0 items-center gap-2.5 border-b px-4',
          compact && 'justify-center px-2',
        )}
      >
        <Link
          to="/projects"
          onClick={onNavigate}
          className="flex min-w-0 items-center gap-2.5"
          aria-label={t.app.name}
        >
          <span className="grid size-7 shrink-0 place-items-center rounded-md bg-accent text-accent-ink">
            <ShieldCheck aria-hidden className="size-4" />
          </span>
          {!compact && <span className="truncate font-semibold">{t.app.name}</span>}
        </Link>
        {mobile && (
          <button
            type="button"
            aria-label={t.app.closeMenu}
            onClick={onNavigate}
            className="ml-auto rounded-md p-1.5 text-muted hover:bg-sunken hover:text-ink"
          >
            <X aria-hidden className="size-4" />
          </button>
        )}
      </div>

      <nav aria-label={t.app.navigation} className="scroll-thin flex-1 overflow-y-auto px-2 pb-4">
        {inProject ? (
          <>
            {projectGroups.map((group) => {
              const items = visibleItems(group.items, user.administrator);
              if (items.length === 0) return null;
              return (
                <div key={group.key} className="flex flex-col gap-0.5">
                  <SectionTitle collapsed={compact}>{group.title(t)}</SectionTitle>
                  {items.map((item) => (
                    <NavLink
                      key={item.key}
                      item={item}
                      projectId={projectId}
                      collapsed={compact}
                      onNavigate={onNavigate}
                    />
                  ))}
                </div>
              );
            })}
            <div className="mt-3 flex flex-col gap-0.5 border-t pt-1">
              <SectionTitle collapsed={compact}>{t.app.consoleGroup}</SectionTitle>
              {visibleItems(globalItems, user.administrator).map((item) => (
                <NavLink key={item.key} item={item} collapsed={compact} onNavigate={onNavigate} />
              ))}
            </div>
          </>
        ) : (
          globalGroups.map((group) => {
            const items = visibleItems(group.items, user.administrator);
            if (items.length === 0) return null;
            return (
              <div key={group.key} className="flex flex-col gap-0.5">
                <SectionTitle collapsed={compact}>{group.title(t)}</SectionTitle>
                {items.map((item) => (
                  <NavLink key={item.key} item={item} collapsed={compact} onNavigate={onNavigate} />
                ))}
              </div>
            );
          })
        )}
      </nav>

      {!mobile && onToggle && (
        <button
          type="button"
          onClick={onToggle}
          aria-label={collapsed ? t.app.expand : t.app.collapse}
          aria-expanded={!collapsed}
          className={cn(
            'flex h-11 shrink-0 items-center gap-2.5 border-t px-4 text-sm text-muted transition-colors hover:bg-sunken hover:text-ink',
            compact && 'justify-center px-2',
          )}
        >
          {collapsed ? (
            <PanelLeftOpen aria-hidden className="size-4" />
          ) : (
            <PanelLeftClose aria-hidden className="size-4" />
          )}
          {!compact && <span>{t.app.collapse}</span>}
        </button>
      )}
    </div>
  );
}
