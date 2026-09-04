import { Link } from 'react-router-dom';

export function NotFound() {
  return (
    <div className="min-h-screen bg-paper flex flex-col items-center justify-center gap-4 p-8 text-center">
      <h1 className="font-display text-2xl font-bold text-ink">Page not found</h1>
      <p className="text-ink-soft max-w-md">
        The page you're looking for doesn't exist, or the link may be out of date.
      </p>
      <Link to="/" className="text-accent font-semibold hover:underline">Go to NeuroFIQ</Link>
    </div>
  );
}
