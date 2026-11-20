import { ShieldCheck } from 'lucide-react';
import { useEffect, type ReactNode } from 'react';
import { LanguageMenu } from '../../app/LanguageMenu';
import { ThemeToggle } from '../../app/ThemeToggle';
import { useLocale } from '../../i18n';

export function AuthLayout({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  const { t } = useLocale();
  useEffect(() => {
    document.title = `${title} · ${t.app.name}`;
  }, [title, t.app.name]);
  const points = [t.auth.pointProtocols, t.auth.pointRedaction, t.auth.pointAudit];
  return (
    <div className="grid min-h-dvh bg-canvas lg:grid-cols-2">
      <aside className="hidden flex-col justify-between border-r bg-surface p-10 lg:flex">
        <div className="flex items-center gap-2.5">
          <span className="grid size-8 place-items-center rounded-md bg-accent text-accent-ink">
            <ShieldCheck aria-hidden className="size-5" />
          </span>
          <span className="text-base font-semibold">{t.app.name}</span>
        </div>
        <div className="max-w-sm">
          <p className="text-2xl leading-9 font-semibold tracking-tight">{t.auth.tagline}</p>
          <ul className="mt-6 flex flex-col gap-3">
            {points.map((point) => (
              <li key={point} className="flex items-start gap-2.5 text-sm text-muted">
                <span aria-hidden className="mt-1.5 size-1.5 shrink-0 rounded-full bg-accent" />
                {point}
              </li>
            ))}
          </ul>
        </div>
        <p className="text-xs text-faint">SafeAgent</p>
      </aside>
      <main className="flex flex-col">
        <div className="flex items-center justify-between p-3 lg:justify-end">
          <span className="flex items-center gap-2 px-2 lg:hidden">
            <span className="grid size-7 place-items-center rounded-md bg-accent text-accent-ink">
              <ShieldCheck aria-hidden className="size-4" />
            </span>
            <span className="text-sm font-semibold">{t.app.name}</span>
          </span>
          <div className="flex items-center gap-1">
            <LanguageMenu />
            <ThemeToggle />
          </div>
        </div>
        <div className="flex flex-1 items-center justify-center px-4 pb-20">
          <div className="w-full max-w-sm">
            <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
            <p className="mt-1.5 text-sm text-muted">{description}</p>
            <div className="mt-7">{children}</div>
          </div>
        </div>
      </main>
    </div>
  );
}
