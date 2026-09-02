// The Job Map's "Practice" buttons choose what an interview is practice for —
// one open role, or a company — three screens before the interview that uses
// it. Repo selection and analysis sit in between, and both are full
// navigations, so the choice rides in the URL rather than in component state.
//
// It stays an id the whole way. The backend resolves it against the database
// and ignores anything it cannot find, so a link that outlives its listing
// costs the framing and nothing else.

export type InterviewTarget = {
  job?: string;
  company?: string;
};

// readTarget pulls the target out of a location's query string.
export function readTarget(search: string): InterviewTarget {
  const params = new URLSearchParams(search);
  const target: InterviewTarget = {};
  const job = params.get('job');
  const company = params.get('company');
  if (job) target.job = job;
  // A role already names its company, so sending both would only be a way for
  // them to disagree.
  else if (company) target.company = company;
  return target;
}

// targetQuery renders the target for an internal link: "?job=<id>", or "" when
// there is nothing to carry.
export function targetQuery(target: InterviewTarget): string {
  const params = new URLSearchParams();
  if (target.job) params.set('job', target.job);
  else if (target.company) params.set('company', target.company);
  const qs = params.toString();
  return qs ? `?${qs}` : '';
}

// targetApiParams renders the target for the questions endpoint, which names
// the same ids job_id and company_id. Returned without a leading separator so
// the caller decides between "?" and "&".
export function targetApiParams(target: InterviewTarget): string {
  const params = new URLSearchParams();
  if (target.job) params.set('job_id', target.job);
  else if (target.company) params.set('company_id', target.company);
  return params.toString();
}
