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

// The public Job Map's Practice buttons link into /repositories, which sits
// behind ProtectedRoute. A visitor who is not logged in — the whole audience
// this feature is for, since a logged-in one would already be past the map —
// gets bounced to /auth, and the target in the URL would be lost there: the
// GitHub OAuth leg is a full-page redirect through GitHub's own domain, so
// nothing on the query string survives it, and the server-side callback can
// only send the browser back to a fixed path. sessionStorage is what
// survives that round trip; the target rides in it rather than in the URL for
// exactly this one hop.
const RETURN_TO_KEY = 'nf_return_to';

// rememberReturnTo records where an unauthenticated visitor was headed, so
// they can be sent back there once they are signed in.
export function rememberReturnTo(pathWithSearch: string): void {
  try {
    sessionStorage.setItem(RETURN_TO_KEY, pathWithSearch);
  } catch {
    // Private browsing or a blocked storage API. Worst case is the same
    // behaviour this replaces — landing on /dashboard — so failing silently
    // is the right call rather than blocking sign-in over it.
  }
}

// consumeReturnTo reads and clears the remembered path. Cleared on read so a
// path visited once cannot keep redirecting a later, unrelated login.
export function consumeReturnTo(): string | null {
  try {
    const value = sessionStorage.getItem(RETURN_TO_KEY);
    if (value) sessionStorage.removeItem(RETURN_TO_KEY);
    return value;
  } catch {
    return null;
  }
}
