import React, { useState, useEffect, useRef } from 'react';
import { Search, Mic, MicOff, MapPin, Sparkles, X, ChevronDown } from 'lucide-react';

interface UnifiedSearchCapsuleProps {
  searchQuery: string;
  onSearchChange: (q: string) => void;
  selectedLocation: string;
  onLocationChange: (loc: string) => void;
  onSearchSubmit?: () => void;
  popularPills?: string[];
  onSelectPill?: (pill: string) => void;
}

const COMMON_LOCATIONS = [
  { label: 'All India', value: '' },
  { label: 'Bengaluru', value: 'Bengaluru' },
  { label: 'Mumbai & Suburbs', value: 'Mumbai' },
  { label: 'Vasai-Virar (Palghar)', value: 'Vasai' },
  { label: 'Delhi NCR (Gurugram/Noida)', value: 'Delhi' },
  { label: 'Hyderabad', value: 'Hyderabad' },
  { label: 'Pune', value: 'Pune' },
  { label: 'Remote / Pan-India', value: 'Remote' },
];

export function UnifiedSearchCapsule({
  searchQuery,
  onSearchChange,
  selectedLocation,
  onLocationChange,
  onSearchSubmit,
  popularPills = ['Golang', 'React', 'AI / ML', 'Backend', 'Fullstack', 'Vasai-Virar', 'Remote'],
  onSelectPill,
}: UnifiedSearchCapsuleProps) {
  const [isListening, setIsListening] = useState(false);
  const [showLocationDropdown, setShowLocationDropdown] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Close location dropdown on outside click
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setShowLocationDropdown(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Voice Search via Web Speech API
  const handleVoiceSearch = () => {
    const SpeechRecognition =
      (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;

    if (!SpeechRecognition) {
      alert('Voice search is not supported in this browser. Please use Google Chrome or Edge.');
      return;
    }

    if (isListening) {
      setIsListening(false);
      return;
    }

    try {
      const recognition = new SpeechRecognition();
      recognition.continuous = false;
      recognition.interimResults = false;
      recognition.lang = 'en-IN';

      recognition.onstart = () => setIsListening(true);
      recognition.onend = () => setIsListening(false);
      recognition.onerror = () => setIsListening(false);

      recognition.onresult = (event: any) => {
        const transcript = event.results?.[0]?.[0]?.transcript || '';
        if (transcript) {
          onSearchChange(transcript);
          if (onSearchSubmit) onSearchSubmit();
        }
        setIsListening(false);
      };

      recognition.start();
    } catch {
      setIsListening(false);
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (onSearchSubmit) onSearchSubmit();
  };

  return (
    <div className="w-full max-w-4xl mx-auto">
      {/* Floating Capsule Bar */}
      <form
        onSubmit={handleSubmit}
        className="relative flex flex-col md:flex-row items-center bg-paper/95 dark:bg-zinc-900/95 border border-line hover:border-accent/40 focus-within:border-accent shadow-xl rounded-2xl md:rounded-full p-2 transition-all backdrop-blur-md gap-2"
      >
        {/* Left: Search Input */}
        <div className="flex items-center flex-1 w-full px-3 gap-3 min-w-0">
          <Search className="w-5 h-5 text-ink-faint flex-shrink-0" />
          <input
            type="text"
            value={searchQuery}
            onChange={e => onSearchChange(e.target.value)}
            placeholder="Job title, company, or tech stack (e.g. Go, React, AI)..."
            className="w-full bg-transparent border-none text-sm text-ink placeholder:text-ink-faint focus:outline-none font-medium"
          />
          {searchQuery && (
            <button
              type="button"
              onClick={() => onSearchChange('')}
              className="p-1 rounded-full text-ink-faint hover:text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors"
            >
              <X className="w-4 h-4" />
            </button>
          )}

          {/* Voice Input Mic Button */}
          <button
            type="button"
            onClick={handleVoiceSearch}
            title={isListening ? 'Listening... Speak now' : 'Voice Search'}
            className={`p-2 rounded-full transition-all flex-shrink-0 ${
              isListening
                ? 'bg-rose-500 text-white animate-pulse shadow-md shadow-rose-500/30'
                : 'text-ink-faint hover:text-accent hover:bg-accent-soft'
            }`}
          >
            {isListening ? <Mic className="w-4 h-4" /> : <MicOff className="w-4 h-4" />}
          </button>
        </div>

        {/* Divider (Desktop) */}
        <div className="hidden md:block w-px h-7 bg-line" />

        {/* Center/Right: Location Selector Dropdown */}
        <div className="relative w-full md:w-auto" ref={dropdownRef}>
          <button
            type="button"
            onClick={() => setShowLocationDropdown(!showLocationDropdown)}
            className="w-full md:w-56 flex items-center justify-between gap-2 px-3 py-2 text-sm text-ink bg-zinc-50 dark:bg-zinc-800/60 rounded-xl md:rounded-full hover:bg-zinc-100 dark:hover:bg-zinc-800 border border-line/60 transition-colors"
          >
            <div className="flex items-center gap-2 truncate">
              <MapPin className="w-4 h-4 text-accent flex-shrink-0" />
              <span className="truncate font-medium">
                {selectedLocation || 'All India & Remote'}
              </span>
            </div>
            <ChevronDown className="w-3.5 h-3.5 text-ink-faint flex-shrink-0" />
          </button>

          {showLocationDropdown && (
            <div className="absolute right-0 top-full mt-2 w-64 bg-paper dark:bg-zinc-900 border border-line rounded-xl shadow-2xl z-50 py-1.5 overflow-hidden">
              <div className="px-3 py-1.5 text-[11px] font-mono uppercase text-ink-faint font-semibold tracking-wider">
                Select Tech Hub
              </div>
              {COMMON_LOCATIONS.map(loc => (
                <button
                  key={loc.label}
                  type="button"
                  onClick={() => {
                    onLocationChange(loc.value);
                    setShowLocationDropdown(false);
                  }}
                  className={`w-full text-left px-3 py-2 text-xs transition-colors flex items-center justify-between ${
                    selectedLocation === loc.value
                      ? 'bg-accent-soft text-accent font-semibold'
                      : 'text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800'
                  }`}
                >
                  <span>{loc.label}</span>
                  {selectedLocation === loc.value && <Sparkles className="w-3 h-3 text-accent" />}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Right: Search CTA Button */}
        <button
          type="submit"
          className="w-full md:w-auto px-6 py-2.5 rounded-xl md:rounded-full bg-accent hover:bg-accent/90 text-white text-sm font-semibold shadow-md shadow-accent/20 transition-all flex items-center justify-center gap-2 flex-shrink-0"
        >
          <span>Search</span>
          <span className="text-xs">➔</span>
        </button>
      </form>

      {/* Quick Filter Pills */}
      {popularPills.length > 0 && (
        <div className="flex items-center flex-wrap gap-2 mt-3 px-2">
          <span className="text-xs text-ink-faint font-medium">Popular:</span>
          {popularPills.map(pill => (
            <button
              key={pill}
              type="button"
              onClick={() => {
                if (onSelectPill) onSelectPill(pill);
                else onSearchChange(pill);
              }}
              className="px-2.5 py-1 rounded-full text-xs font-medium bg-paper dark:bg-zinc-800/80 border border-line hover:border-accent/40 text-ink-soft hover:text-accent transition-all shadow-xs"
            >
              {pill}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
