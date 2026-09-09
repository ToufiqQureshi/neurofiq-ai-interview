import { useState } from 'react';

interface CompanyLogoProps {
  domain?: string;
  name: string;
  /** Size and shape of the box, e.g. "w-10 h-10 rounded-xl". */
  className?: string;
  /** Look of the initials box when there is no usable favicon. */
  fallbackClassName?: string;
  /** Text size for the initials. */
  fallbackTextClassName?: string;
}

/**
 * A company's favicon, falling back to its initials.
 *
 * Four screens drew this by hand and only one of them handled failure properly.
 * The others degraded in ways a visitor sees: the jobs rail rendered a broken
 * image icon, and the map drawer hid the <img> and left an empty white box.
 * Both now behave like the best of the four did.
 *
 * The naturalWidth check is the part worth keeping. Google's favicon service
 * answers for a domain it has nothing for with a small generic icon rather than
 * a 404, so onError never fires and the company appears to have a logo that is
 * not its own. A returned image far below the requested size is that case.
 */
export default function CompanyLogo({
  domain,
  name,
  className = 'w-10 h-10 rounded-xl',
  fallbackClassName = 'bg-paper border border-line text-ink-soft',
  fallbackTextClassName = 'text-xs',
}: CompanyLogoProps) {
  const [failed, setFailed] = useState(false);

  if (failed || !domain) {
    return (
      <div
        className={`${className} ${fallbackClassName} ${fallbackTextClassName} flex items-center justify-center font-mono font-bold uppercase flex-shrink-0 shadow-sm`}
      >
        {name.slice(0, 2)}
      </div>
    );
  }

  return (
    <img
      src={`https://www.google.com/s2/favicons?domain=${domain}&sz=128`}
      alt={name}
      className={`${className} border border-line object-contain bg-white flex-shrink-0 p-1 shadow-sm`}
      onError={() => setFailed(true)}
      onLoad={e => {
        if (e.currentTarget.naturalWidth < 32) setFailed(true);
      }}
    />
  );
}
