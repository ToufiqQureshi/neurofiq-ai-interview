// A pending recruiter invite, held between the invite link and the moment the
// candidate submits their interview.
//
// This is ambient state: nothing on the interview screen mentions it, but it
// decides which recruiter the finished report goes to and it spends one of the
// link's uses. Left unbounded, a candidate who opens an invite, wanders off,
// and later runs an ordinary practice interview would silently have that
// practice run attributed to the recruiter — and burn the invite on it.
//
// So it expires, and it is cleared after every submission attempt rather than
// only on success.

const KEY = 'neurofiq_invite';
const TTL_MS = 2 * 60 * 60 * 1000; // 2 hours

type StoredInvite = { token: string; roleTitle: string; savedAt: number };

export function saveInvite(token: string, roleTitle: string) {
  try {
    const payload: StoredInvite = { token, roleTitle, savedAt: Date.now() };
    sessionStorage.setItem(KEY, JSON.stringify(payload));
  } catch {
    // Private mode or blocked site data: the interview still works, it just
    // won't be linked back to the recruiter.
  }
}

export function readInvite(): StoredInvite | null {
  try {
    const raw = sessionStorage.getItem(KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as StoredInvite;
    if (!parsed?.token || Date.now() - parsed.savedAt > TTL_MS) {
      sessionStorage.removeItem(KEY);
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

export function clearInvite() {
  try {
    sessionStorage.removeItem(KEY);
  } catch {
    // Nothing to do — see saveInvite.
  }
}
