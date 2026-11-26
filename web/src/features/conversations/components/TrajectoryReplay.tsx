import { Pause, Play, RotateCcw, SkipBack, SkipForward } from 'lucide-react';
import type {
  ConsoleTrajectoryNodeView,
  ConsoleTrajectoryStepView,
} from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { interpolate, useLocale } from '../../../i18n';
import { formatDate } from '../../../lib/format';
import { stepAfter, stepBefore } from '../trajectory';
import { summarizeNode } from './nodeView';

export function TrajectoryReplay({
  steps,
  index,
  playing,
  touched,
  nodes,
  onStep,
  onTogglePlay,
  onSelect,
}: {
  steps: ConsoleTrajectoryStepView[];
  index: number;
  playing: boolean;
  touched: Set<string>;
  nodes: ConsoleTrajectoryNodeView[];
  onStep: (index: number) => void;
  onTogglePlay: () => void;
  onSelect: (nodeId: string) => void;
}) {
  const { t, locale } = useLocale();
  const step = steps[index];
  const total = steps.length;
  const current = index < 0 ? 0 : Math.min(index + 1, total);
  const touchedNodes = [...touched]
    .map((id) => nodes.find((node) => node.id === id))
    .filter((node): node is ConsoleTrajectoryNodeView => node !== undefined);
  return (
    <div className="flex flex-col gap-2 border-t px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs font-medium text-muted">{t.trajectory.replay.title}</span>
        <div className="flex gap-1">
          <Button
            size="icon"
            variant="ghost"
            aria-label={t.trajectory.replay.reset}
            disabled={index < 0}
            onClick={() => onStep(-1)}
          >
            <RotateCcw aria-hidden />
          </Button>
          <Button
            size="icon"
            variant="ghost"
            aria-label={t.trajectory.replay.previous}
            disabled={index < 0}
            onClick={() => onStep(stepBefore(index))}
          >
            <SkipBack aria-hidden />
          </Button>
          <Button
            size="icon"
            variant={playing ? 'primary' : 'secondary'}
            aria-label={playing ? t.trajectory.replay.pause : t.trajectory.replay.play}
            aria-pressed={playing}
            disabled={total === 0}
            onClick={onTogglePlay}
          >
            {playing ? <Pause aria-hidden /> : <Play aria-hidden />}
          </Button>
          <Button
            size="icon"
            variant="ghost"
            aria-label={t.trajectory.replay.next}
            disabled={index >= total}
            onClick={() => onStep(stepAfter(index, total))}
          >
            <SkipForward aria-hidden />
          </Button>
        </div>
        <span className="num text-xs text-faint">
          {interpolate(t.trajectory.replay.step, { current, total })}
        </span>
        <span className="min-w-0 truncate text-xs text-muted">
          {step
            ? `#${step.sequence} ${
                t.call.timelineLabels[step.label as keyof typeof t.call.timelineLabels] ??
                step.label
              }`
            : t.trajectory.replay.start}
        </span>
        {step && (
          <span className="ml-auto shrink-0 text-[11px] text-faint">
            {formatDate(step.at, locale)}
          </span>
        )}
      </div>
      {touchedNodes.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5" aria-live="polite">
          <span className="text-[11px] text-faint">{t.trajectory.replay.touched}</span>
          {touchedNodes.slice(0, 8).map((node) => (
            <button
              key={node.id}
              type="button"
              onClick={() => onSelect(node.id)}
              className="rounded-sm border bg-surface px-1.5 py-0.5 text-[11px] text-muted hover:border-accent hover:text-ink"
            >
              {summarizeNode(t, node).title}
            </button>
          ))}
          {touchedNodes.length > 8 && <Badge>+{touchedNodes.length - 8}</Badge>}
        </div>
      )}
      {step && step.node_ids.length === 0 && (
        <p className="text-[11px] text-faint">{t.trajectory.replay.noNodes}</p>
      )}
      {total === 0 && <p className="text-[11px] text-faint">{t.trajectory.replay.empty}</p>}
      {index >= total && total > 0 && (
        <p className="text-[11px] text-faint">{t.trajectory.replay.done}</p>
      )}
    </div>
  );
}
