/**
 * The locations a visitor can filter by, and the exact strings the API expects.
 *
 * JobsPortal renders three controls that all set the same `location` filter —
 * the search capsule, the preference sentence, and the sub-hub rail — and each
 * carried its own copy of this list. They had drifted: the preference filter
 * offered six options where the other two offered eight, so a visitor could
 * pick Vasai-Virar or Remote in one control and then find the sentence beside
 * it unable to say either.
 *
 * `value` is what goes to the API. `applyAreaFilter` in the Go backend expands
 * the metro names into their suburbs (Delhi also matches Noida and Gurgaon,
 * Mumbai also matches Thane and Navi Mumbai); the rest match the stored area
 * literally, which is why Vasai and Remote have to be spelled the same way
 * everywhere. Empty means no location filter at all.
 */
export interface LocationOption {
  label: string;
  value: string;
}

export const LOCATION_OPTIONS: LocationOption[] = [
  { label: 'Pan-India', value: '' },
  { label: 'Bengaluru (HSR / Koramangala)', value: 'Bengaluru' },
  { label: 'Mumbai (BKC / Andheri / Powai)', value: 'Mumbai' },
  { label: 'Vasai-Virar (Palghar Suburbs)', value: 'Vasai' },
  { label: 'Delhi NCR (Gurugram / Noida)', value: 'Delhi' },
  { label: 'Hyderabad (Hitec City)', value: 'Hyderabad' },
  { label: 'Pune (Hinjawadi / Baner)', value: 'Pune' },
  { label: 'Remote Roles', value: 'Remote' },
];
