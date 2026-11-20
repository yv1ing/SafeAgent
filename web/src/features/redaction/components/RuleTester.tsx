import { useMutation } from '@tanstack/react-query';
import { useEffect, useId, useState } from 'react';
import type { ConsoleRuleInput } from '../../../api/generated/models';
import { testRule } from '../../../api/generated/rules/rules';
import { Badge } from '../../../components/ui/badge';
import { Label } from '../../../components/ui/label';
import { Textarea } from '../../../components/ui/textarea';
import { errorMessage, interpolate, useLocale } from '../../../i18n';
import { splitByByteRanges } from '../../../lib/highlight';

const debounceMilliseconds = 400;

export function RuleTester({ projectId, rule }: { projectId: string; rule: ConsoleRuleInput }) {
  const { t } = useLocale();
  const sampleId = useId();
  const [sample, setSample] = useState('');
  const test = useMutation({
    mutationFn: (input: { rule: ConsoleRuleInput; text: string }) => testRule(projectId, input),
  });
  const ruleKey = JSON.stringify(rule);
  const { mutate, reset } = test;
  useEffect(() => {
    if (sample === '') {
      reset();
      return;
    }
    const timer = setTimeout(
      () => mutate({ rule: JSON.parse(ruleKey), text: sample }),
      debounceMilliseconds,
    );
    return () => clearTimeout(timer);
  }, [ruleKey, sample, mutate, reset]);
  const result = test.data;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={sampleId}>{t.redaction.sample}</Label>
        <Textarea
          id={sampleId}
          rows={5}
          className="font-mono"
          placeholder={t.redaction.samplePlaceholder}
          value={sample}
          onChange={(event) => setSample(event.target.value)}
        />
      </div>
      <div aria-live="polite" className="flex flex-col gap-2">
        {test.isError && <p className="text-sm text-danger">{errorMessage(t, test.error)}</p>}
        {result && (
          <>
            <div className="flex items-center gap-2 text-xs text-muted">
              <span className="font-medium">{t.redaction.result}</span>
              {result.hits.length === 0 ? (
                <span>{t.redaction.noHits}</span>
              ) : (
                <Badge tone="accent">
                  {interpolate(result.hits.length === 1 ? t.redaction.hitsOne : t.redaction.hits, {
                    count: result.hits.length,
                  })}
                </Badge>
              )}
              {result.blocked && <Badge tone="danger">{t.redaction.blocked}</Badge>}
            </div>
            <pre className="rounded-md border bg-sunken p-2.5 font-mono text-xs break-words whitespace-pre-wrap">
              {splitByByteRanges(sample, result.hits).map((segment, index) =>
                segment.range === undefined ? (
                  segment.text
                ) : (
                  <mark
                    key={index}
                    title={result.hits[segment.range]?.label}
                    className="rounded-sm bg-warning-soft text-warning"
                  >
                    {segment.text}
                  </mark>
                ),
              )}
            </pre>
            {result.hits.length > 0 && (
              <pre className="rounded-md border bg-surface p-2.5 font-mono text-xs break-words whitespace-pre-wrap">
                {result.replaced}
              </pre>
            )}
          </>
        )}
      </div>
    </div>
  );
}
