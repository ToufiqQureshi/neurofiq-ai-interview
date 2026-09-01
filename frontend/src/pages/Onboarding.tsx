import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { 
  Check, 
  ArrowRight, 
  ArrowLeft, 
  Briefcase, 
  GraduationCap, 
  Target, 
  Code2, 
  Globe, 
  Sparkles,
  Loader2 
} from 'lucide-react';

const TECH_OPTIONS = [
  'Go', 'Python', 'TypeScript', 'React', 'Node.js', 
  'Java', 'Rust', 'Docker', 'PostgreSQL', 'FastAPI', 
  'Next.js', 'AWS', 'Kubernetes', 'GraphQL', 'Redis', 'C++'
];

export function Onboarding() {
  const { user, refreshUser } = useAuth();
  const navigate = useNavigate();

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [fullName, setFullName] = useState(user?.full_name || user?.github_username || '');
  const [experienceLevel, setExperienceLevel] = useState<'fresher' | 'mid' | 'senior'>('fresher');
  const [collegeOrCompany, setCollegeOrCompany] = useState('');
  
  const [targetRole, setTargetRole] = useState('Backend Engineer');
  const [selectedTech, setSelectedTech] = useState<string[]>(['Go', 'PostgreSQL']);
  
  const [linkedinUrl, setLinkedinUrl] = useState('');
  const [interviewGoal, setInterviewGoal] = useState<'faang' | 'startup' | 'practice'>('startup');
  
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const toggleTech = (tech: string) => {
    if (selectedTech.includes(tech)) {
      setSelectedTech(selectedTech.filter(t => t !== tech));
    } else {
      setSelectedTech([...selectedTech, tech]);
    }
  };

  const handleComplete = async () => {
    setError(null);
    setLoading(true);

    try {
      const api = import.meta.env.VITE_API_URL;
      const res = await fetch(`${api}/api/user/onboarding`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          full_name: fullName,
          experience_level: experienceLevel,
          college_or_company: collegeOrCompany,
          target_role: targetRole,
          tech_stack: selectedTech.join(', '),
          linkedin_url: linkedinUrl,
          interview_goal: interviewGoal,
        }),
      });

      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || 'Failed to save profile setup');
      }

      await refreshUser();
      navigate('/dashboard');
    } catch (err: any) {
      setError(err.message || 'Something went wrong');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-paper flex items-center justify-center p-4">
      <div className="bg-surface p-8 rounded-2xl shadow-sm w-full max-w-xl border border-line">
        
        {/* Step Progress Indicator */}
        <div className="mb-8">
          <div className="flex items-center justify-between text-xs font-mono uppercase tracking-wider text-ink-soft mb-2">
            <span>Step {step} of 3</span>
            <span className="text-ink font-semibold">
              {step === 1 && 'Experience & Background'}
              {step === 2 && 'Role & Tech Stack'}
              {step === 3 && 'Goals & Socials'}
            </span>
          </div>
          <div className="w-full bg-paper h-1.5 rounded-full overflow-hidden border border-line">
            <div 
              className="bg-ink h-full transition-all duration-300 rounded-full"
              style={{ width: `${(step / 3) * 100}%` }}
            />
          </div>
        </div>

        {error && (
          <div className="mb-6 p-3 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded-xl text-xs text-red-600 dark:text-red-400">
            {error}
          </div>
        )}

        {/* STEP 1: Experience & Background */}
        {step === 1 && (
          <div className="space-y-6">
            <div>
              <h2 className="font-display text-2xl font-extrabold text-ink mb-1">
                Tell us about your experience
              </h2>
              <p className="text-ink-soft text-xs">
                The AI interviewer modulates question depth, concurrency topics, and trade-off grilling based on your level.
              </p>
            </div>

            <div>
              <label className="block text-xs font-semibold text-ink-soft mb-1.5">Your Full Name</label>
              <input
                type="text"
                value={fullName}
                onChange={(e) => setFullName(e.target.value)}
                placeholder="e.g. Linus Torvalds"
                className="w-full px-4 py-2.5 bg-paper border border-line rounded-xl text-sm text-ink placeholder:text-ink-faint focus:outline-none focus:border-ink transition-colors"
              />
            </div>

            <div className="space-y-2.5">
              <label className="block text-xs font-semibold text-ink-soft">Experience Level</label>
              
              {/* Fresher */}
              <div 
                onClick={() => setExperienceLevel('fresher')}
                className={`p-4 rounded-xl border cursor-pointer transition-all flex items-start gap-3.5 ${
                  experienceLevel === 'fresher' 
                    ? 'bg-paper border-ink shadow-sm' 
                    : 'bg-surface border-line hover:border-ink-soft'
                }`}
              >
                <div className="p-2 rounded-lg bg-emerald-50 text-emerald-600 border border-emerald-200 mt-0.5">
                  <GraduationCap className="w-5 h-5" />
                </div>
                <div className="flex-1">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-ink">Fresher / Student (0-1 Yrs)</span>
                    {experienceLevel === 'fresher' && <Check className="w-4 h-4 text-ink" />}
                  </div>
                  <p className="text-xs text-ink-soft mt-0.5">
                    Focus on clean architecture, foundational DSA, and code readability.
                  </p>
                </div>
              </div>

              {/* Mid-Level */}
              <div 
                onClick={() => setExperienceLevel('mid')}
                className={`p-4 rounded-xl border cursor-pointer transition-all flex items-start gap-3.5 ${
                  experienceLevel === 'mid' 
                    ? 'bg-paper border-ink shadow-sm' 
                    : 'bg-surface border-line hover:border-ink-soft'
                }`}
              >
                <div className="p-2 rounded-lg bg-blue-50 text-blue-600 border border-blue-200 mt-0.5">
                  <Briefcase className="w-5 h-5" />
                </div>
                <div className="flex-1">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-ink">Mid-Level Software Engineer (2-4 Yrs)</span>
                    {experienceLevel === 'mid' && <Check className="w-4 h-4 text-ink" />}
                  </div>
                  <p className="text-xs text-ink-soft mt-0.5">
                    Focus on design patterns, API efficiency, database query tuning, and error handling.
                  </p>
                </div>
              </div>

              {/* Senior */}
              <div 
                onClick={() => setExperienceLevel('senior')}
                className={`p-4 rounded-xl border cursor-pointer transition-all flex items-start gap-3.5 ${
                  experienceLevel === 'senior' 
                    ? 'bg-paper border-ink shadow-sm' 
                    : 'bg-surface border-line hover:border-ink-soft'
                }`}
              >
                <div className="p-2 rounded-lg bg-purple-50 text-purple-600 border border-purple-200 mt-0.5">
                  <Sparkles className="w-5 h-5" />
                </div>
                <div className="flex-1">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-ink">Senior / Staff Engineer (5+ Yrs)</span>
                    {experienceLevel === 'senior' && <Check className="w-4 h-4 text-ink" />}
                  </div>
                  <p className="text-xs text-ink-soft mt-0.5">
                    Heavy grilling on distributed systems, race conditions, bottlenecks, and failure modes.
                  </p>
                </div>
              </div>
            </div>

            <div>
              <label className="block text-xs font-semibold text-ink-soft mb-1.5">College or Current Company (Optional)</label>
              <input
                type="text"
                value={collegeOrCompany}
                onChange={(e) => setCollegeOrCompany(e.target.value)}
                placeholder="e.g. IIT Bombay, Razorpay, or Independent Builder"
                className="w-full px-4 py-2.5 bg-paper border border-line rounded-xl text-sm text-ink placeholder:text-ink-faint focus:outline-none focus:border-ink transition-colors"
              />
            </div>
          </div>
        )}

        {/* STEP 2: Role & Tech Stack */}
        {step === 2 && (
          <div className="space-y-6">
            <div>
              <h2 className="font-display text-2xl font-extrabold text-ink mb-1">
                Target Role & Stack
              </h2>
              <p className="text-ink-soft text-xs">
                Select the primary engineering domain and technologies you want to be evaluated on.
              </p>
            </div>

            <div>
              <label className="block text-xs font-semibold text-ink-soft mb-2">Target Domain</label>
              <div className="grid grid-cols-2 gap-2">
                {[
                  'Backend Engineer',
                  'Frontend Engineer',
                  'Full-Stack Engineer',
                  'AI / ML Engineer',
                  'DevOps / Cloud Engineer',
                  'Systems / Embedded',
                ].map((role) => (
                  <button
                    key={role}
                    type="button"
                    onClick={() => setTargetRole(role)}
                    className={`py-2.5 px-3 rounded-xl text-xs font-semibold border text-left transition-colors flex items-center justify-between ${
                      targetRole === role
                        ? 'bg-ink text-white border-ink'
                        : 'bg-paper text-ink-soft border-line hover:border-ink-soft'
                    }`}
                  >
                    <span>{role}</span>
                    {targetRole === role && <Check className="w-3.5 h-3.5 text-white" />}
                  </button>
                ))}
              </div>
            </div>

            <div>
              <label className="block text-xs font-semibold text-ink-soft mb-2">
                Primary Technologies (Select at least 2)
              </label>
              <div className="flex flex-wrap gap-2">
                {TECH_OPTIONS.map((tech) => {
                  const active = selectedTech.includes(tech);
                  return (
                    <button
                      key={tech}
                      type="button"
                      onClick={() => toggleTech(tech)}
                      className={`px-3 py-1.5 rounded-lg text-xs font-mono font-medium border transition-colors flex items-center gap-1.5 ${
                        active
                          ? 'bg-ink text-white border-ink shadow-sm'
                          : 'bg-paper text-ink-soft border-line hover:border-ink-soft'
                      }`}
                    >
                      <Code2 className="w-3 h-3" />
                      <span>{tech}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          </div>
        )}

        {/* STEP 3: Goals & Socials */}
        {step === 3 && (
          <div className="space-y-6">
            <div>
              <h2 className="font-display text-2xl font-extrabold text-ink mb-1">
                Final Polish & Goals
              </h2>
              <p className="text-ink-soft text-xs">
                Add your social profile and specify what kind of interview preparation you need.
              </p>
            </div>

            <div>
              <label className="block text-xs font-semibold text-ink-soft mb-1.5">LinkedIn Profile URL</label>
              <div className="relative">
                <Globe className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-ink-faint" />
                <input
                  type="url"
                  value={linkedinUrl}
                  onChange={(e) => setLinkedinUrl(e.target.value)}
                  placeholder="https://linkedin.com/in/username"
                  className="w-full pl-10 pr-4 py-2.5 bg-paper border border-line rounded-xl text-sm text-ink placeholder:text-ink-faint focus:outline-none focus:border-ink transition-colors"
                />
              </div>
            </div>

            <div className="space-y-2.5">
              <label className="block text-xs font-semibold text-ink-soft">What is your primary goal?</label>
              
              {[
                { id: 'faang', title: '🎯 FAANG & Tier-1 Enterprise Prep', desc: 'Maximum rigor, trade-off challenge, and deep edge cases.' },
                { id: 'startup', title: '🚀 Fast-Paced Startup Ready', desc: 'Practical execution, speed, clean modularity, and API design.' },
                { id: 'practice', title: '💡 Benchmark & Skill Practice', desc: 'Identify blind spots and weaknesses in personal repositories.' },
              ].map((g) => (
                <div
                  key={g.id}
                  onClick={() => setInterviewGoal(g.id as any)}
                  className={`p-3.5 rounded-xl border cursor-pointer transition-all flex items-start justify-between gap-3 ${
                    interviewGoal === g.id
                      ? 'bg-paper border-ink shadow-sm'
                      : 'bg-surface border-line hover:border-ink-soft'
                  }`}
                >
                  <div>
                    <span className="font-semibold text-xs text-ink block">{g.title}</span>
                    <span className="text-[11px] text-ink-soft block mt-0.5">{g.desc}</span>
                  </div>
                  {interviewGoal === g.id && <Check className="w-4 h-4 text-ink flex-shrink-0 mt-0.5" />}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Navigation Buttons */}
        <div className="flex items-center justify-between mt-8 pt-4 border-t border-line">
          {step > 1 ? (
            <button
              type="button"
              onClick={() => setStep((s) => (s - 1) as any)}
              className="px-4 py-2 text-xs font-semibold text-ink-soft hover:text-ink flex items-center gap-1.5 rounded-lg transition-colors"
            >
              <ArrowLeft className="w-4 h-4" />
              <span>Back</span>
            </button>
          ) : <div />}

          {step < 3 ? (
            <button
              type="button"
              onClick={() => {
                if (step === 1 && !fullName.trim()) {
                  setError('Please enter your full name');
                  return;
                }
                setError(null);
                setStep((s) => (s + 1) as any);
              }}
              className="px-5 py-2.5 bg-ink hover:bg-black text-white text-xs font-semibold flex items-center gap-2 rounded-xl transition-colors shadow-sm"
            >
              <span>Continue</span>
              <ArrowRight className="w-4 h-4" />
            </button>
          ) : (
            <button
              type="button"
              disabled={loading}
              onClick={handleComplete}
              className="px-6 py-2.5 bg-ink hover:bg-black text-white text-xs font-semibold flex items-center gap-2 rounded-xl transition-colors shadow-sm disabled:opacity-60"
            >
              {loading ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <>
                  <Target className="w-4 h-4" />
                  <span>Start Interviewing</span>
                </>
              )}
            </button>
          )}
        </div>

      </div>
    </div>
  );
}
