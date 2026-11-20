import { Link } from '@tanstack/react-router';
import { SeverityBadge, StateBadge } from '../../../components/feedback/StatusBadge';
import { useLocale } from '../../../i18n';
import { formatDate, formatDuration, formatTokens } from '../../../lib/format';
import type { TreeBranch } from '../tree';

export function TreeTimeline({
  branches,
  projectId,
}: {
  branches: TreeBranch[];
  projectId: string;
}) {
  const { t, locale } = useLocale();
  return (
    <ol className="flex flex-col gap-2">
      {branches.map(({ node, children }) => (
        <li key={node.call.id} className="flex flex-col gap-2">
          <Link
            to="/projects/$projectId/calls"
            params={{ projectId }}
            search={{ call: node.call.id }}
            className="flex flex-col gap-1 rounded-md border bg-surface px-3 py-2 hover:border-accent"
          >
            <div className="flex flex-wrap items-center gap-2">
              <StateBadge state={node.call.state} />
              <span className="font-mono text-sm">{node.call.public_model}</span>
              <SeverityBadge severity={node.call.max_severity} />
              <span className="ml-auto text-xs text-faint">
                {formatDate(node.call.created_at, locale)}
              </span>
            </div>
            <p className="truncate text-sm text-muted">
              {node.summary || t.conversations.noSummary}
            </p>
            <p className="num text-xs text-faint">
              {formatDuration(node.call.duration_ms)} · {formatTokens(node.call.input_tokens)} /{' '}
              {formatTokens(node.call.output_tokens)}
            </p>
          </Link>
          {children.length > 0 && (
            <div className="ml-3 border-l-2 pl-4">
              <TreeTimeline branches={children} projectId={projectId} />
            </div>
          )}
        </li>
      ))}
    </ol>
  );
}
