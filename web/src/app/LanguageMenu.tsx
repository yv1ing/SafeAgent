import { Languages } from 'lucide-react';
import { Button } from '../components/ui/button';
import {
  Dropdown,
  DropdownContent,
  DropdownLabel,
  DropdownRadioGroup,
  DropdownRadioItem,
  DropdownTrigger,
} from '../components/ui/dropdown';
import { locales, useLocale, type Locale } from '../i18n';

export function LanguageMenu() {
  const { t, locale, setLocale } = useLocale();
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t.app.language}>
          <Languages aria-hidden />
        </Button>
      </DropdownTrigger>
      <DropdownContent>
        <DropdownLabel>{t.app.language}</DropdownLabel>
        <DropdownRadioGroup value={locale} onValueChange={(next) => setLocale(next as Locale)}>
          {Object.entries(locales).map(([value, label]) => (
            <DropdownRadioItem key={value} value={value}>
              {label}
            </DropdownRadioItem>
          ))}
        </DropdownRadioGroup>
      </DropdownContent>
    </Dropdown>
  );
}
