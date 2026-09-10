/** A company as /api/companies returns it. */
export interface Company {
  id: string;
  name: string;
  description: string;
  website: string;
  domain: string;
  sector: string;
  stage: string;
  area: string;
  careers_url: string;
  lat: number | null;
  lng: number | null;
  job_count: number;
}

/**
 * One city the directory covers, with the camera framing the map uses for it.
 *
 * `query` is what goes to the API as the area filter, and it is deliberately
 * shorter than `name`: the label can say "Delhi NCR (Noida/Gurgaon)" while the
 * filter stays the string the backend matches.
 */
export interface TechHub {
  id: string;
  name: string;
  query: string;
  lat: number;
  lng: number;
  zoom: number;
  minZoom: number;
  maxZoom: number;
  bounds: [[number, number], [number, number]];
  icon: string;
}
