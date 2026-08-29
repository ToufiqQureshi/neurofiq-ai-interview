import { GithubIcon } from '../components/GithubIcon';

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
