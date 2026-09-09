/**
 * How a 0-10 interview score is graded and coloured.
 *
 * Three screens show the same grade for the same score — the dashboard's recent
 * list, the reports list, and a report's per-section feedback — and each had its
 * own copy of the 8-and-5 cutoffs. Three places to change, and nothing to catch
 * it if only two of them were.
 *
 * `chip` is the background alone and `text` the foreground, because the report
 * page pairs the background with a stripe of the solid colour while the two
 * lists use them together.
 */
export interface ScoreBand {
  label: string;
  chip: string;
  text: string;
  stripe: string;
}

export function scoreBand(score: number): ScoreBand {
  if (score >= 8) {
    return { label: 'excellent', chip: 'bg-pass-soft', text: 'text-pass', stripe: 'bg-pass' };
  }
  if (score >= 5) {
    return { label: 'average', chip: 'bg-warn-soft', text: 'text-warn', stripe: 'bg-warn' };
  }
  return { label: 'needs review', chip: 'bg-crit-soft', text: 'text-crit', stripe: 'bg-crit' };
}
