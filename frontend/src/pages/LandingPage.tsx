import { Link } from 'react-router-dom';
import { ArrowRight, GitBranch, Mic } from 'lucide-react';
import { GithubIcon } from '../components/GithubIcon';

export function LandingPage() {
  return (
    <div className="min-h-screen bg-paper text-ink overflow-hidden">
      {/* Nav */}
      <header className="relative z-10 flex items-center justify-between px-6 md:px-10 h-20 max-w-7xl mx-auto">
        <div className="flex items-center gap-2 font-display font-extrabold text-lg">
          <span className="w-7 h-7 rounded-md bg-ink text-white flex items-center justify-center text-xs">N</span>
          NeuroFIQ
        </div>
        <Link
          to="/auth"
          className="text-sm font-semibold px-4 py-2 rounded-lg border border-line-strong hover:bg-surface transition-colors"
        >
          Sign In
        </Link>
      </header>

      {/* Hero */}
      <section className="relative">
        {/* Diagonal background shape */}
        <div
          aria-hidden="true"
          className="absolute inset-0 -z-10 opacity-70"
          style={{
            background: 'linear-gradient(115deg, transparent 0%, transparent 42%, #f0f1ee 42%, #f0f1ee 100%)',
          }}
        />

        <div className="relative max-w-7xl mx-auto px-6 md:px-10 pt-10 pb-24 grid grid-cols-1 lg:grid-cols-2 gap-14 items-center">
          {/* Left: copy */}
          <div>
            <h1 className="font-display font-extrabold text-5xl md:text-6xl leading-[1.05] tracking-tight">
              Interview prep,
              <br />
              built from <span className="text-accent">your own code.</span>
            </h1>
            <p className="mt-6 text-lg text-ink-soft max-w-lg leading-relaxed">
              Connect a GitHub repo and NeuroFIQ reads it — architecture, patterns, the decisions you actually made — then interviews you on it. No generic LeetCode. Your codebase is the question bank.
            </p>
            <div className="mt-8 flex flex-wrap items-center gap-4">
              <Link
                to="/auth"
                className="inline-flex items-center gap-2 bg-ink hover:bg-black text-white px-6 py-3.5 rounded-lg font-semibold transition-colors shadow-sm"
              >
                <GithubIcon className="w-5 h-5" />
                Connect GitHub &amp; Start
              </Link>
              <a href="#how-it-works" className="inline-flex items-center gap-1.5 text-sm font-semibold text-ink-soft hover:text-ink transition-colors">
                See how it works <ArrowRight className="w-4 h-4" />
              </a>
            </div>
          </div>

          {/* Right: product mockup panel */}
          <div className="relative">
            <div className="rounded-2xl bg-[#0c0d10] border border-black/10 shadow-2xl overflow-hidden">
              <div className="flex items-center gap-1.5 px-4 py-3 border-b border-white/10">
                <span className="w-2.5 h-2.5 rounded-full bg-[#ff5f57]"></span>
                <span className="w-2.5 h-2.5 rounded-full bg-[#febc2e]"></span>
                <span className="w-2.5 h-2.5 rounded-full bg-[#28c840]"></span>
                <span className="ml-3 font-mono text-[11px] text-white/40">interview_report.json</span>
              </div>
              <div className="p-5 font-mono text-[12.5px] leading-relaxed">
                <div className="text-white/40">// AgentKit &middot; Question 3 of 5</div>
                <div className="mt-3 text-white/90">
                  "Your <span className="text-accent">safe-page-fetch</span> function implements DNS pinning.
                  How does that protect against rebinding attacks?"
                </div>
                <div className="mt-4 flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-accent"></span>
                  <span className="text-white/50">scoring answer&hellip;</span>
                </div>
                <div className="mt-4 rounded-lg bg-white/5 border border-white/10 p-3 space-y-1.5">
                  <div className="flex justify-between"><span className="text-white/50">strengths</span><span className="text-accent">2 found</span></div>
                  <div className="flex justify-between"><span className="text-white/50">gaps</span><span className="text-[#ff9b6b]">1 found</span></div>
                  <div className="flex justify-between"><span className="text-white/50">score</span><span className="text-white font-bold">7 / 10</span></div>
                </div>
              </div>
            </div>
            <div className="absolute -bottom-5 left-2 sm:-left-5 bg-surface border border-line rounded-xl px-4 py-3 shadow-lg flex items-center gap-3">
              <Mic className="w-4 h-4 text-accent" />
              <div>
                <div className="text-xs font-bold">Voice mode</div>
                <div className="text-[10px] text-ink-faint font-mono">speak your answer</div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* How it works */}
      <section id="how-it-works" className="max-w-7xl mx-auto px-6 md:px-10 py-20 border-t border-line">
        <h2 className="font-display font-extrabold text-3xl md:text-4xl">How it works</h2>
        <p className="mt-2 text-ink-soft">From repo to interview in three steps.</p>

        <div className="mt-10 grid grid-cols-1 md:grid-cols-3 gap-6">
          <div>
            <h3 className="font-display font-bold text-lg mb-2">Connect a repo</h3>
            <p className="text-sm text-ink-soft leading-relaxed">Sign in with GitHub and pick a repository. We read the structure, the stack, and how it's actually built.</p>
            <div className="mt-5 bg-[#f0f1ee] border border-line rounded-xl p-5 h-40 flex items-center justify-center">
              <div className="flex items-center gap-2">
                <div className="w-9 h-9 rounded-lg bg-surface border border-line-strong flex items-center justify-center">
                  <GitBranch className="w-4 h-4 text-ink-soft" />
                </div>
                <ArrowRight className="w-4 h-4 text-ink-faint" />
                <div className="w-9 h-9 rounded-lg bg-ink text-white flex items-center justify-center font-mono text-[10px] font-bold">N</div>
              </div>
            </div>
          </div>

          <div>
            <h3 className="font-display font-bold text-lg mb-2">AI reads the code</h3>
            <p className="text-sm text-ink-soft leading-relaxed">Architecture patterns, complexity, strengths, and the spots worth probing — surfaced before the interview starts.</p>
            <div className="mt-5 bg-[#f0f1ee] border border-line rounded-xl p-5 h-40 flex flex-col justify-center gap-2">
              <span className="self-start text-[11px] font-mono px-2 py-1 rounded-full bg-surface border border-line-strong">layered architecture</span>
              <span className="self-start text-[11px] font-mono px-2 py-1 rounded-full bg-surface border border-line-strong ml-4">rate limiting</span>
              <span className="self-start text-[11px] font-mono px-2 py-1 rounded-full bg-surface border border-line-strong ml-2">ssrf protection</span>
            </div>
          </div>

          <div>
            <h3 className="font-display font-bold text-lg mb-2">Interview, then a report</h3>
            <p className="text-sm text-ink-soft leading-relaxed">Answer by text or voice. Get a scored report with per-question feedback and what an ideal answer looks like.</p>
            <div className="mt-5 bg-[#f0f1ee] border border-line rounded-xl p-5 h-40 flex items-center justify-center">
              <div className="text-center">
                <div className="font-mono text-3xl font-extrabold text-accent">8.5<span className="text-base text-ink-faint">/10</span></div>
                <div className="text-[10px] font-mono uppercase tracking-wide text-ink-faint mt-1">overall score</div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Closing CTA */}
      <section className="max-w-7xl mx-auto px-6 md:px-10 pb-24">
        <div className="rounded-2xl bg-ink px-8 py-12 md:py-16 text-center">
          <h2 className="font-display font-extrabold text-3xl md:text-4xl text-white">Ready to be interviewed on your own work?</h2>
          <p className="mt-3 text-white/60 max-w-md mx-auto">Free to start. Connect a repo and get your first tailored interview in minutes.</p>
          <Link
            to="/auth"
            className="mt-7 inline-flex items-center gap-2 bg-white text-ink px-6 py-3.5 rounded-lg font-semibold hover:bg-white/90 transition-colors"
          >
            <GithubIcon className="w-5 h-5" />
            Connect GitHub &amp; Start
          </Link>
        </div>
      </section>
    </div>
  );
}
