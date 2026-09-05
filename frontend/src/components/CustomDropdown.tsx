import { useState, useRef, useEffect, ReactNode } from 'react';
import { ChevronDown, Check } from 'lucide-react';

interface Option {
  label: string;
  value: string;
}

interface CustomDropdownProps {
  value: string;
  onChange: (value: string) => void;
  options: (string | Option)[];
  placeholder?: string;
  icon?: ReactNode;
  className?: string;
}

export function CustomDropdown({
  value,
  onChange,
  options,
  placeholder = 'Select...',
  icon,
  className = '',
}: CustomDropdownProps) {
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Normalize options to { label, value }
  const normalizedOptions: Option[] = options.map(opt =>
    typeof opt === 'string' ? { label: opt, value: opt } : opt
  );

  const selectedOption = normalizedOptions.find(opt => opt.value === value);
  const displayText = selectedOption?.label || placeholder;
  const isSelected = !!value;

  // Handle click outside to close popover
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    }
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setIsOpen(false);
      }
    }
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      document.addEventListener('keydown', handleKeyDown);
    }
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen]);

  return (
    <div className={`relative inline-block ${className}`} ref={dropdownRef}>
      {/* Trigger Button */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        className={`appearance-none bg-surface border rounded-full py-2.5 pl-4 pr-10 text-sm focus:outline-none transition-all duration-200 flex items-center gap-2 cursor-pointer shadow-2xs ${
          isSelected
            ? 'border-ink text-ink font-semibold bg-paper/80 ring-1 ring-ink/10'
            : 'border-line text-ink-soft hover:text-ink hover:border-line-strong hover:bg-paper'
        } ${isOpen ? 'border-accent ring-2 ring-accent/20 bg-surface' : ''}`}
      >
        {icon && <span className="text-ink-soft flex-shrink-0">{icon}</span>}
        <span className="truncate max-w-[140px] text-left">{displayText}</span>
        <ChevronDown
          className={`w-4 h-4 text-ink-soft absolute right-3.5 top-1/2 -translate-y-1/2 transition-transform duration-200 pointer-events-none ${
            isOpen ? 'rotate-180 text-accent' : ''
          }`}
        />
      </button>

      {/* Floating Popover Menu */}
      {isOpen && (
        <div
          role="listbox"
          className="absolute left-0 mt-2 min-w-[210px] max-w-[280px] max-h-72 overflow-y-auto bg-surface/95 backdrop-blur-xl border border-line rounded-2xl shadow-2xl p-1.5 z-50 animate-fade-in focus:outline-none scrollbar-thin scrollbar-thumb-line"
        >
          {normalizedOptions.map(opt => {
            const active = opt.value === value;
            return (
              <button
                key={opt.value}
                type="button"
                role="option"
                aria-selected={active}
                onClick={() => {
                  onChange(opt.value);
                  setIsOpen(false);
                }}
                className={`w-full text-left px-3.5 py-2.5 rounded-xl text-xs font-medium transition-all flex items-center justify-between group cursor-pointer ${
                  active
                    ? 'bg-ink text-white font-semibold shadow-xs'
                    : 'text-ink-soft hover:text-ink hover:bg-paper'
                }`}
              >
                <span className="truncate">{opt.label}</span>
                {active && <Check className="w-3.5 h-3.5 text-white flex-shrink-0 ml-2" />}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
