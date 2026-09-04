import { useState, useRef, useEffect } from 'react';
import { ChevronDown, Sparkles } from 'lucide-react';

interface PreferenceSentenceFilterProps {
  selectedRole: string;
  onRoleChange: (r: string) => void;
  selectedExp: string;
  onExpChange: (e: string) => void;
  selectedWorkType: string;
  onWorkTypeChange: (w: string) => void;
  selectedLocation: string;
  onLocationChange: (l: string) => void;
  onApplyPreferences?: () => void;
}

const ROLES = [
  { label: 'Software Engineer', value: 'Software Engineer' },
  { label: 'Backend Engineer (Go/Node/Python)', value: 'Backend' },
  { label: 'Frontend Engineer (React/Next.js)', value: 'Frontend' },
  { label: 'Fullstack Developer', value: 'Fullstack' },
  { label: 'AI / ML Engineer', value: 'AI' },
  { label: 'DevOps / SRE Engineer', value: 'DevOps' },
  { label: 'Mobile App Developer', value: 'Mobile' },
  { label: 'Data Analyst / Scientist', value: 'Data' },
];

const EXPERIENCES = [
  { label: 'Fresher / Early Career', value: 'Entry' },
  { label: 'Mid-Level (1-3 yrs)', value: 'Mid' },
  { label: 'Senior (4+ yrs)', value: 'Senior' },
  { label: 'Any Experience', value: '' },
];

const WORK_TYPES = [
  { label: 'Hybrid & In-Office', value: 'Hybrid' },
  { label: 'Remote / Work from Anywhere', value: 'Remote' },
  { label: 'Full-time Any Setup', value: '' },
];

const LOCATIONS = [
  { label: 'Bengaluru (HSR/Koramangala/E-City)', value: 'Bengaluru' },
  { label: 'Mumbai & Vasai-Virar Suburbs', value: 'Mumbai' },
  { label: 'Delhi NCR (Gurugram/Noida)', value: 'Delhi' },
  { label: 'Hyderabad (Hitec City)', value: 'Hyderabad' },
  { label: 'Pune (Hinjawadi/Baner)', value: 'Pune' },
  { label: 'Pan-India', value: '' },
];

export function PreferenceSentenceFilter({
  selectedRole,
  onRoleChange,
  selectedExp,
  onExpChange,
  selectedWorkType,
  onWorkTypeChange,
  selectedLocation,
  onLocationChange,
  onApplyPreferences,
}: PreferenceSentenceFilterProps) {
  const [activeDropdown, setActiveDropdown] = useState<'role' | 'exp' | 'work' | 'loc' | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setActiveDropdown(null);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const currentRoleLabel = ROLES.find(r => r.value === selectedRole)?.label || selectedRole || 'Software Engineer';
  const currentExpLabel = EXPERIENCES.find(e => e.value === selectedExp)?.label || (selectedExp ? selectedExp : 'Fresher / Any Level');
  const currentWorkLabel = WORK_TYPES.find(w => w.value === selectedWorkType)?.label || (selectedWorkType ? selectedWorkType : 'Any Setup');
  const currentLocLabel = LOCATIONS.find(l => l.value === selectedLocation)?.label || (selectedLocation ? selectedLocation : 'Pan-India');

  return (
    <div
      ref={containerRef}
      className="w-full max-w-4xl mx-auto mt-6 p-4 rounded-2xl bg-paper/60 dark:bg-zinc-900/60 border border-line/80 backdrop-blur-md shadow-sm"
    >
      <div className="flex items-center gap-2 mb-2">
        <Sparkles className="w-3.5 h-3.5 text-accent" />
        <span className="text-[11px] font-mono uppercase tracking-wider text-ink-faint font-semibold">
          Interactive AI Preference Selector
        </span>
      </div>

      {/* Natural Language Sentence */}
      <div className="flex flex-wrap items-center gap-y-2.5 text-sm sm:text-base text-ink leading-relaxed">
        <span className="text-ink-soft">I'm a</span>

        {/* 1. Role Selector */}
        <div className="relative inline-block mx-1.5">
          <button
            type="button"
            onClick={() => setActiveDropdown(activeDropdown === 'role' ? null : 'role')}
            className="inline-flex items-center gap-1 font-semibold text-accent border-b-2 border-accent/40 hover:border-accent pb-0.5 transition-colors"
          >
            <span>{currentRoleLabel}</span>
            <ChevronDown className="w-3.5 h-3.5" />
          </button>

          {activeDropdown === 'role' && (
            <div className="absolute left-0 top-full mt-2 w-72 bg-paper dark:bg-zinc-900 border border-line rounded-xl shadow-2xl z-50 py-1 overflow-hidden">
              {ROLES.map(r => (
                <button
                  key={r.label}
                  type="button"
                  onClick={() => {
                    onRoleChange(r.value);
                    setActiveDropdown(null);
                  }}
                  className={`w-full text-left px-3.5 py-2 text-xs transition-colors ${
                    selectedRole === r.value
                      ? 'bg-accent-soft text-accent font-semibold'
                      : 'text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800'
                  }`}
                >
                  {r.label}
                </button>
              ))}
            </div>
          )}
        </div>

        <span className="text-ink-soft">with</span>

        {/* 2. Experience Selector */}
        <div className="relative inline-block mx-1.5">
          <button
            type="button"
            onClick={() => setActiveDropdown(activeDropdown === 'exp' ? null : 'exp')}
            className="inline-flex items-center gap-1 font-semibold text-accent border-b-2 border-accent/40 hover:border-accent pb-0.5 transition-colors"
          >
            <span>{currentExpLabel}</span>
            <ChevronDown className="w-3.5 h-3.5" />
          </button>

          {activeDropdown === 'exp' && (
            <div className="absolute left-0 top-full mt-2 w-56 bg-paper dark:bg-zinc-900 border border-line rounded-xl shadow-2xl z-50 py-1 overflow-hidden">
              {EXPERIENCES.map(e => (
                <button
                  key={e.label}
                  type="button"
                  onClick={() => {
                    onExpChange(e.value);
                    setActiveDropdown(null);
                  }}
                  className={`w-full text-left px-3.5 py-2 text-xs transition-colors ${
                    selectedExp === e.value
                      ? 'bg-accent-soft text-accent font-semibold'
                      : 'text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800'
                  }`}
                >
                  {e.label}
                </button>
              ))}
            </div>
          )}
        </div>

        <span className="text-ink-soft">open to</span>

        {/* 3. Work Type Selector */}
        <div className="relative inline-block mx-1.5">
          <button
            type="button"
            onClick={() => setActiveDropdown(activeDropdown === 'work' ? null : 'work')}
            className="inline-flex items-center gap-1 font-semibold text-accent border-b-2 border-accent/40 hover:border-accent pb-0.5 transition-colors"
          >
            <span>{currentWorkLabel}</span>
            <ChevronDown className="w-3.5 h-3.5" />
          </button>

          {activeDropdown === 'work' && (
            <div className="absolute left-0 top-full mt-2 w-64 bg-paper dark:bg-zinc-900 border border-line rounded-xl shadow-2xl z-50 py-1 overflow-hidden">
              {WORK_TYPES.map(w => (
                <button
                  key={w.label}
                  type="button"
                  onClick={() => {
                    onWorkTypeChange(w.value);
                    setActiveDropdown(null);
                  }}
                  className={`w-full text-left px-3.5 py-2 text-xs transition-colors ${
                    selectedWorkType === w.value
                      ? 'bg-accent-soft text-accent font-semibold'
                      : 'text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800'
                  }`}
                >
                  {w.label}
                </button>
              ))}
            </div>
          )}
        </div>

        <span className="text-ink-soft">roles in</span>

        {/* 4. Location Selector */}
        <div className="relative inline-block mx-1.5">
          <button
            type="button"
            onClick={() => setActiveDropdown(activeDropdown === 'loc' ? null : 'loc')}
            className="inline-flex items-center gap-1 font-semibold text-accent border-b-2 border-accent/40 hover:border-accent pb-0.5 transition-colors"
          >
            <span>{currentLocLabel}</span>
            <ChevronDown className="w-3.5 h-3.5" />
          </button>

          {activeDropdown === 'loc' && (
            <div className="absolute left-0 top-full mt-2 w-64 bg-paper dark:bg-zinc-900 border border-line rounded-xl shadow-2xl z-50 py-1 overflow-hidden">
              {LOCATIONS.map(l => (
                <button
                  key={l.label}
                  type="button"
                  onClick={() => {
                    onLocationChange(l.value);
                    setActiveDropdown(null);
                  }}
                  className={`w-full text-left px-3.5 py-2 text-xs transition-colors ${
                    selectedLocation === l.value
                      ? 'bg-accent-soft text-accent font-semibold'
                      : 'text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800'
                  }`}
                >
                  {l.label}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Instant Apply / Match Action Button */}
        {onApplyPreferences && (
          <button
            type="button"
            onClick={onApplyPreferences}
            className="ml-2 inline-flex items-center justify-center p-2 rounded-full bg-accent text-white hover:bg-accent/90 transition-all shadow-sm hover:scale-105"
            title="Filter Live Jobs"
          >
            <span className="text-xs">➔</span>
          </button>
        )}
      </div>
    </div>
  );
}
