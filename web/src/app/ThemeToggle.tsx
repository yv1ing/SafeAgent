import { Monitor, Moon, Sun } from 'lucide-react';
import { Button } from '../components/ui/button';
import {
  Dropdown,
  DropdownContent,
  DropdownLabel,
  DropdownRadioGroup,
  DropdownRadioItem,
  DropdownTrigger,
} from '../components/ui/dropdown';
import { useLocale } from '../i18n';
import { selectTheme, useTheme, type ThemeChoice } from '../lib/theme';

const icons = { light: Sun, dark: Moon, system: Monitor };

export function ThemeToggle() {
  const { t } = useLocale();
  const { choice } = useTheme();
  const Icon = icons[choice];
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t.app.theme}>
          <Icon aria-hidden />
        </Button>
      </DropdownTrigger>
      <DropdownContent>
        <DropdownLabel>{t.app.theme}</DropdownLabel>
        <DropdownRadioGroup
          value={choice}
          onValueChange={(next) => selectTheme(next as ThemeChoice)}
        >
          {(['light', 'dark', 'system'] as const).map((option) => {
            const OptionIcon = icons[option];
            return (
              <DropdownRadioItem key={option} value={option}>
                <OptionIcon aria-hidden />
                {t.app.themes[option]}
              </DropdownRadioItem>
            );
          })}
        </DropdownRadioGroup>
      </DropdownContent>
    </Dropdown>
  );
}
