import { useState, useRef, useEffect } from 'react';
import { ChevronDown, Sparkles } from 'lucide-react';
import { LOCATION_OPTIONS, type LocationOption } from '../lib/locations';

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

/**
 * One underlined choice inside the sentence.
 *
 * Deliberately not CustomDropdown: that renders a bordered pill, and the whole
 * point of this control is that each choice reads as a word in a sentence.
 * What the four choices here shared was the markup below, written out four
 * times.
 */
function SentenceChoice({
  open,
  onToggle,
  display,
  options,
  value,
  onChange,
  menuWidth,
}: {
  open: boolean;
  onToggle: () => void;
  display: string;
  options: LocationOption[];
  value: string;
  onChange: (v: string) => void;
  menuWidth: string;
}) {
  return (
    <div className="relative inline-block mx-1.5">
      <button
        type="button"
        onClick={onToggle}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="inline-flex items-center gap-1 font-semibold text-accent border-b-2 border-accent/40 hover:border-accent pb-0.5 transition-colors"
      >
        <span>{display}</span>
        <ChevronDown className="w-3.5 h-3.5" />
      </button>

      {open && (
        <div
          role="listbox"
          className={`absolute left-0 top-full mt-2 ${menuWidth} bg-paper dark:bg-zinc-900 border border-line rounded-xl shadow-2xl z-50 py-1 overflow-hidden`}
        >
          {options.map(opt => (
            <button
              key={opt.value}
              type="button"
              role="option"
              aria-selected={value === opt.value}
              onClick={() => onChange(opt.value)}
              className={`w-full text-left px-3.5 py-2 text-xs transition-colors ${
                value === opt.value
                  ? 'bg-accent-soft text-accent font-semibold'
                  : 'text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800'
              }`}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

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
  const [activeDropdown, setActiveDropdown] = useState<string | null>(null);
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

  function labelFor(options: LocationOption[], value: string, fallback: string) {
    return options.find(o => o.value === value)?.label || value || fallback;
  }

  // The sentence, as data: a lead-in phrase and the choice that follows it.
  const parts = [
    {
      key: 'role',
      lead: "I'm a",
      options: ROLES,
      value: selectedRole,
      onChange: onRoleChange,
      display: labelFor(ROLES, selectedRole, 'Software Engineer'),
      menuWidth: 'w-72',
    },
    {
      key: 'exp',
      lead: 'with',
      options: EXPERIENCES,
      value: selectedExp,
      onChange: onExpChange,
      display: labelFor(EXPERIENCES, selectedExp, 'Fresher / Any Level'),
      menuWidth: 'w-56',
    },
    {
      key: 'work',
      lead: 'open to',
      options: WORK_TYPES,
      value: selectedWorkType,
      onChange: onWorkTypeChange,
      display: labelFor(WORK_TYPES, selectedWorkType, 'Any Setup'),
      menuWidth: 'w-64',
    },
    {
      key: 'loc',
      lead: 'roles in',
      options: LOCATION_OPTIONS,
      value: selectedLocation,
      onChange: onLocationChange,
      display: labelFor(LOCATION_OPTIONS, selectedLocation, 'Pan-India'),
      menuWidth: 'w-64',
    },
  ];

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
        {parts.map(part => (
          <span key={part.key} className="contents">
            <span className="text-ink-soft">{part.lead}</span>
            <SentenceChoice
              open={activeDropdown === part.key}
              onToggle={() => setActiveDropdown(activeDropdown === part.key ? null : part.key)}
              display={part.display}
              options={part.options}
              value={part.value}
              onChange={v => {
                part.onChange(v);
                setActiveDropdown(null);
              }}
              menuWidth={part.menuWidth}
            />
          </span>
        ))}

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
