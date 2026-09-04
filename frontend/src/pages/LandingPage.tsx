import { Link } from 'react-router-dom';
import { ArrowRight, ShieldCheck, Cpu, Code2, Play } from 'lucide-react';

export function LandingPage() {
  return (
    <div className="min-h-screen bg-[#0A0A0A] text-white overflow-hidden selection:bg-accent/30 selection:text-white">
      {/* Background Effects */}
      <div className="fixed inset-0 z-0 pointer-events-none">
        <div className="absolute top-[-20%] left-[-10%] w-[50%] h-[50%] rounded-full bg-accent/20 blur-[120px] mix-blend-screen" />
        <div className="absolute bottom-[-20%] right-[-10%] w-[50%] h-[50%] rounded-full bg-indigo-500/10 blur-[120px] mix-blend-screen" />
        <div className="absolute inset-0 bg-[url('https://grainy-gradients.vercel.app/noise.svg')] opacity-20 mix-blend-overlay" />
      </div>

      {/* Nav */}
      <header className="relative z-10 flex items-center justify-between px-6 md:px-12 h-24 max-w-7xl mx-auto">
        <div className="flex items-center gap-3 font-display font-extrabold text-xl tracking-tight">
          <div className="relative flex items-center justify-center w-8 h-8 rounded-lg bg-gradient-to-br from-accent to-indigo-600 shadow-[0_0_15px_rgba(93,95,239,0.5)]">
            <span className="text-white text-sm">N</span>
          </div>
          NeuroFIQ
        </div>
        <div className="flex items-center gap-6">
          <Link to="/auth" className="text-sm font-medium text-white/70 hover:text-white transition-colors">Sign In</Link>
          <Link
            to="/auth"
            className="text-sm font-semibold px-5 py-2.5 rounded-full bg-white/10 hover:bg-white/20 border border-white/10 backdrop-blur-md transition-all shadow-[0_0_15px_rgba(255,255,255,0.05)] hover:shadow-[0_0_20px_rgba(255,255,255,0.1)]"
          >
            Get Started
          </Link>
        </div>
      </header>

      {/* Hero */}
      <section className="relative z-10 max-w-7xl mx-auto px-6 md:px-12 pt-20 pb-32 text-center md:text-left grid grid-cols-1 lg:grid-cols-2 gap-16 items-center">
        <div>
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-accent/10 border border-accent/20 text-accent text-xs font-semibold uppercase tracking-wider mb-8">
            <span className="w-2 h-2 rounded-full bg-accent animate-pulse"></span>
            Enterprise Grade AI Interviewer
          </div>
          <h1 className="font-display font-extrabold text-5xl md:text-6xl lg:text-7xl leading-[1.1] tracking-tight text-transparent bg-clip-text bg-gradient-to-b from-white to-white/70">
            Hire <span className="text-accent bg-clip-text text-transparent bg-gradient-to-r from-accent to-indigo-400">10x Engineers</span><br />
            with Uncheatable AI.
          </h1>
          <p className="mt-6 text-lg md:text-xl text-white/60 max-w-lg mx-auto md:mx-0 leading-relaxed font-light">
            Stop relying on generic LeetCode puzzles. NeuroFIQ autonomously evaluates candidates based on their real GitHub repositories and specific Job Descriptions using live, proctored voice interviews.
          </p>
          <div className="mt-10 flex flex-wrap items-center justify-center md:justify-start gap-4">
            <Link
              to="/auth"
              className="inline-flex items-center gap-2 bg-white text-black hover:bg-white/90 px-8 py-4 rounded-full font-bold transition-all hover:scale-105 shadow-[0_0_30px_rgba(255,255,255,0.15)]"
            >
              Start Free Trial <ArrowRight className="w-5 h-5" />
            </Link>
            <a href="#demo" className="inline-flex items-center gap-2 px-8 py-4 rounded-full font-bold text-white bg-white/5 border border-white/10 hover:bg-white/10 backdrop-blur-md transition-all">
              <Play className="w-5 h-5" /> View Demo
            </a>
          </div>
        </div>

        {/* Right: Premium Dashboard Mockup */}
        <div className="relative group perspective">
          <div className="relative rounded-2xl bg-[#111] border border-white/10 shadow-[0_20px_50px_rgba(0,0,0,0.5)] overflow-hidden transform transition-transform duration-700 group-hover:-rotate-y-2 group-hover:rotate-x-2">
            <div className="flex items-center gap-2 px-4 py-3 bg-[#1A1A1A] border-b border-white/5">
              <span className="w-3 h-3 rounded-full bg-[#ff5f57]"></span>
              <span className="w-3 h-3 rounded-full bg-[#febc2e]"></span>
              <span className="w-3 h-3 rounded-full bg-[#28c840]"></span>
              <span className="ml-2 font-mono text-xs text-white/30">NeuroFIQ Proctoring Engine</span>
            </div>
            <div className="p-6">
              <div className="flex justify-between items-start mb-6">
                <div>
                  <div className="text-white/50 text-xs font-mono mb-1">CANDIDATE SESSION</div>
                  <div className="text-white font-semibold flex items-center gap-2">
                    John Doe <span className="px-2 py-0.5 rounded text-[10px] bg-green-500/20 text-green-400 border border-green-500/30">VERIFIED</span>
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-accent text-xs font-mono mb-1 flex items-center justify-end gap-1"><span className="w-1.5 h-1.5 rounded-full bg-accent animate-ping"></span> AI SPEAKING</div>
                  <div className="text-white/80 text-sm">"Can you explain the O(n) complexity in your sorting algorithm?"</div>
                </div>
              </div>
              
              <div className="grid grid-cols-2 gap-4">
                <div className="bg-black/50 border border-white/5 rounded-xl p-4">
                  <div className="flex items-center gap-2 text-white/50 text-xs font-mono mb-3">
                    <ShieldCheck className="w-4 h-4 text-accent" /> TRUST SCORE
                  </div>
                  <div className="text-3xl font-display font-bold text-white">99%</div>
                  <div className="text-[10px] text-green-400 mt-1">Screen Share Active</div>
                </div>
                <div className="bg-black/50 border border-white/5 rounded-xl p-4">
                  <div className="flex items-center gap-2 text-white/50 text-xs font-mono mb-3">
                    <Cpu className="w-4 h-4 text-indigo-400" /> TECH SCORE
                  </div>
                  <div className="text-3xl font-display font-bold text-white">8.5<span className="text-lg text-white/30">/10</span></div>
                  <div className="text-[10px] text-indigo-400 mt-1">Senior Level</div>
                </div>
              </div>
            </div>
          </div>
          
          {/* Decorative floating elements */}
          <div className="absolute -bottom-6 -left-6 bg-[#1A1A1A] border border-white/10 rounded-2xl p-4 shadow-2xl backdrop-blur-xl animate-bounce-slow">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-full bg-accent/20 flex items-center justify-center border border-accent/30">
                <Code2 className="w-5 h-5 text-accent" />
              </div>
              <div>
                <div className="text-xs font-bold text-white">Context-Aware AI</div>
                <div className="text-[10px] text-white/50">Evaluates against your Job Description</div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Features */}
      <section className="relative z-10 bg-[#0F0F0F] border-t border-white/5 py-24">
        <div className="max-w-7xl mx-auto px-6 md:px-12">
          <div className="text-center max-w-2xl mx-auto mb-16">
            <h2 className="font-display font-extrabold text-3xl md:text-4xl text-white">The Engineering Hiring Standard</h2>
            <p className="mt-4 text-white/50 text-lg">Replace expensive engineering hours with an AI that conducts technical deep-dives with zero bias and infinite scale.</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            {[
              {
                title: "Job Context Awareness",
                desc: "Paste your Job Description. The AI will strictly evaluate the candidate's skills against your exact requirements.",
                icon: <Cpu className="w-6 h-6 text-accent" />
              },
              {
                title: "Bulletproof Anti-Cheat",
                desc: "Mandatory screen-sharing, tab-switch detection, prompt poisoning, and copy-paste blocking ensures absolute integrity.",
                icon: <ShieldCheck className="w-6 h-6 text-indigo-400" />
              },
              {
                title: "Deep GitHub Analysis",
                desc: "The AI reads their actual codebase architecture and challenges them on the decisions they made, not generic riddles.",
                icon: <Code2 className="w-6 h-6 text-emerald-400" />
              }
            ].map((feature, i) => (
              <div key={i} className="p-8 rounded-2xl bg-white/[0.02] border border-white/5 hover:bg-white/[0.04] transition-colors group">
                <div className="w-12 h-12 rounded-xl bg-white/5 flex items-center justify-center mb-6 border border-white/10 group-hover:scale-110 transition-transform">
                  {feature.icon}
                </div>
                <h3 className="text-xl font-bold text-white mb-3">{feature.title}</h3>
                <p className="text-white/50 leading-relaxed text-sm">{feature.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Closing CTA */}
      <section className="relative z-10 max-w-7xl mx-auto px-6 md:px-12 py-32">
        <div className="relative rounded-3xl bg-gradient-to-br from-accent/20 to-indigo-600/20 border border-white/10 p-12 md:p-20 text-center overflow-hidden">
          <div className="absolute inset-0 bg-[url('https://grainy-gradients.vercel.app/noise.svg')] opacity-30 mix-blend-overlay" />
          <div className="relative z-10">
            <h2 className="font-display font-extrabold text-4xl md:text-5xl text-white tracking-tight">Scale your engineering<br />hiring today.</h2>
            <p className="mt-6 text-white/70 max-w-md mx-auto text-lg">Send magic link invites to your candidates and get scored radar reports in minutes.</p>
            <div className="mt-10">
              <Link
                to="/auth"
                className="inline-flex items-center gap-2 bg-white text-black px-8 py-4 rounded-full font-bold hover:scale-105 transition-transform shadow-[0_0_30px_rgba(255,255,255,0.2)]"
              >
                Create Recruiter Account
              </Link>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
