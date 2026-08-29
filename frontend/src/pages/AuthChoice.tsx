const GithubIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M12 2A10 10 0 0 0 2 12c0 4.42 2.87 8.17 6.84 9.5c.5.08.66-.23.66-.5v-1.69c-2.77.6-3.36-1.34-3.36-1.34c-.46-1.16-1.11-1.47-1.11-1.47c-.91-.62.07-.6.07-.6c1 .07 1.53 1.03 1.53 1.03c.87 1.52 2.34 1.07 2.91.83c.09-.65.35-1.09.63-1.34c-2.22-.25-4.55-1.11-4.55-4.92c0-1.11.38-2 1.03-2.71c-.1-.25-.45-1.29.1-2.64c0 0 .84-.27 2.75 1.02c.79-.22 1.65-.33 2.5-.33c.85 0 1.71.11 2.5.33c1.91-1.29 2.75-1.02 2.75-1.02c.55 1.35.2 2.39.1 2.64c.65.71 1.03 1.6 1.03 2.71c0 3.82-2.34 4.66-4.57 4.91c.36.31.69.92.69 1.85V21c0 .27.16.59.67.5C19.14 20.16 22 16.42 22 12A10 10 0 0 0 12 2z" />
  </svg>
);

export function AuthChoice() {
  return (
    <div className="min-h-screen bg-paper flex items-center justify-center p-4">
      <div className="bg-surface p-8 rounded-2xl shadow-sm w-full max-w-md border border-line">
        <h2 className="font-display text-3xl font-extrabold text-ink text-center mb-2">Sign In</h2>
        <p className="text-ink-soft text-center mb-8">Choose a provider to continue</p>

        <div className="space-y-3">
          <button
            disabled
            title="Google sign-in isn't available yet"
            className="w-full bg-paper text-ink-faint border border-line flex items-center justify-center gap-3 py-3 px-4 rounded-full font-semibold cursor-not-allowed"
          >
            <img src="https://www.svgrepo.com/show/475656/google-color.svg" alt="" className="w-5 h-5 opacity-40 grayscale" />
            Continue with Google
            <span className="ml-1 text-[10px] font-mono uppercase tracking-wider text-ink-faint border border-line rounded-full px-2 py-0.5">
              Soon
            </span>
          </button>

          <div className="relative flex py-2 items-center">
            <div className="flex-grow border-t border-line"></div>
            <span className="flex-shrink-0 mx-4 text-ink-faint text-sm">or</span>
            <div className="flex-grow border-t border-line"></div>
          </div>

          <button
            onClick={() => {
              const api = import.meta.env.VITE_API_URL;
              if (!api) {
                alert('VITE_API_URL is not set. Check the frontend .env.');
                return;
              }
              window.location.href = `${api}/auth/github/login`;
            }}
            className="w-full bg-ink hover:bg-black text-white flex items-center justify-center gap-3 py-3 px-4 rounded-full font-semibold transition-colors"
          >
            <GithubIcon className="w-5 h-5" />
            Continue with GitHub
          </button>
        </div>
      </div>
    </div>
  );
}
