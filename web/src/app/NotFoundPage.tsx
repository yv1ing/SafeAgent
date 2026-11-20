import { Link } from '@tanstack/react-router';
import { Compass } from 'lucide-react';
import { buttonClass } from '../components/ui/button';
import { Empty } from '../components/ui/empty';
import { useLocale } from '../i18n';

export function NotFoundPage() {
  const { t } = useLocale();
  return (
    <div className="flex min-h-dvh items-center justify-center px-4">
      <Empty
        icon={Compass}
        title={t.app.notFoundTitle}
        description={t.app.notFoundHint}
        action={
          <Link to="/projects" className={buttonClass()}>
            {t.app.allProjects}
          </Link>
        }
      />
    </div>
  );
}
