import { Monitor, Moon, Sun } from 'lucide-react';

import { cn } from '@/lib/cn';
import { useThemePreference, type ThemePreference } from '@/lib/theme';

const OPTIONS: { value: ThemePreference; label: string; Icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', Icon: Sun },
  { value: 'system', label: 'System', Icon: Monitor },
  { value: 'dark', label: 'Dark', Icon: Moon },
];

/**
 * Light / System / Dark switch; the choice is shared by every Con Services
 * app. `rail` styles it for the admin console's dark sidebar; `compact` hides
 * the labels (they stay as tooltips).
 */
export function ThemeToggle({ tone = 'surface', compact = false }: { tone?: 'surface' | 'rail'; compact?: boolean }) {
  const { preference, setPreference } = useThemePreference();
  return (
    <div
      role="radiogroup"
      aria-label="Colour theme"
      className={cn(
        'flex items-center gap-0.5 rounded-full p-0.5',
        tone === 'rail' ? 'bg-white/10' : 'border border-den-border bg-den-surface',
        !compact && 'w-full',
      )}
    >
      {OPTIONS.map(({ value, label, Icon }) => {
        const active = preference === value;
        return (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={active}
            aria-label={label}
            title={label}
            onClick={() => setPreference(value)}
            className={cn(
              'flex items-center justify-center gap-1 rounded-full text-xs font-medium transition-colors',
              compact ? 'h-7 w-7' : 'min-w-0 flex-1 px-1.5 py-1.5',
              tone === 'rail'
                ? active
                  ? 'bg-white/15 text-white'
                  : 'text-slate-300 hover:text-white'
                : active
                  ? 'bg-den-primary text-den-on-primary shadow-sm'
                  : 'text-den-foreground/60 hover:text-den-foreground',
            )}
          >
            <Icon size={14} className="shrink-0" />
            {!compact && label}
          </button>
        );
      })}
    </div>
  );
}
