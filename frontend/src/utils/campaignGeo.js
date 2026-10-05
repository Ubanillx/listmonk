import worldMap from '../assets/world.json';

// Match the bundled map's abbreviated names to GeoIP's ISO country codes.
const aliases = {
  AX: 'Aland',
  TF: 'Fr. S. Antarctic Lands',
  AG: 'Antigua and Barb.',
  BA: 'Bosnia and Herz.',
  EH: 'W. Sahara',
  CF: 'Central African Rep.',
  CD: 'Dem. Rep. Congo',
  CG: 'Congo',
  KY: 'Cayman Is.',
  CZ: 'Czech Rep.',
  DO: 'Dominican Rep.',
  FK: 'Falkland Is.',
  FO: 'Faeroe Is.',
  GQ: 'Eq. Guinea',
  HM: 'Heard I. and McDonald Is.',
  IO: 'Br. Indian Ocean Ter.',
  KR: 'Korea',
  LA: 'Lao PDR',
  MK: 'Macedonia',
  MP: 'N. Mariana Is.',
  KP: 'Dem. Rep. Korea',
  PS: 'Palestine',
  PF: 'Fr. Polynesia',
  SS: 'S. Sudan',
  GS: 'S. Geo. and S. Sandw. Is.',
  SH: 'Saint Helena',
  SB: 'Solomon Is.',
  PM: 'St. Pierre and Miquelon',
  ST: 'São Tomé and Principe',
  SZ: 'Swaziland',
  TC: 'Turks and Caicos Is.',
  VC: 'St. Vin. and Gren.',
  VI: 'U.S. Virgin Is.',
  LC: 'Saint Lucia',
  MM: 'Myanmar',
  TT: 'Trinidad and Tobago',
  TR: 'Turkey',
};
const normalize = (name) => String(name || '').normalize('NFKD').toLowerCase().replace(/[^a-z0-9]/g, '');
const features = worldMap.features.filter((feature) => feature.properties.name);
const codesByName = new Map(Object.entries(aliases).map(([code, name]) => [normalize(name), code]));
const englishNames = new Intl.DisplayNames(['en'], { type: 'region' });
// Intl supplies current localized region names without another dataset or request.
for (let first = 65; first <= 90; first += 1) {
  for (let second = 65; second <= 90; second += 1) {
    const code = String.fromCharCode(first, second);
    const name = normalize(englishNames.of(code));
    if (!codesByName.has(name) && !aliases[code]) codesByName.set(name, code);
  }
}

export const mapCountries = features.map((feature) => ({
  key: codesByName.get(normalize(feature.properties.name)) || `map:${feature.properties.name}`,
  name: feature.properties.name,
  feature,
}));
const mapsByKey = new Map(mapCountries.map((country) => [country.key, country]));
const mapsByName = new Map(mapCountries.map((country) => [normalize(country.name), country]));

export function countryLabel(key, fallback, locale) {
  if (/^[A-Z]{2}$/.test(key)) {
    try {
      return new Intl.DisplayNames([locale || 'en'], { type: 'region' }).of(key);
    } catch (err) {
      return englishNames.of(key);
    }
  }
  return fallback || (mapsByKey.get(key) || {}).name || key.replace(/^map:/, '');
}

export function locationPoints(report) {
  if (!report || !Array.isArray(report.locations)) return [];
  return report.locations.filter((location) => Number.isFinite(location.latitude)
    && Number.isFinite(location.longitude) && Number.isFinite(location.count) && location.count > 0
    && location.latitude >= -90 && location.latitude <= 90
    && location.longitude >= -180 && location.longitude <= 180).map((location) => {
    const code = String(location.countryCode || '').trim().toUpperCase();
    const mappedCountry = mapsByName.get(normalize(location.country));
    const countryKey = /^[A-Z]{2}$/.test(code) ? code : ((mappedCountry || {}).key || 'unknown');
    return {
      ...location,
      countryKey,
      city: String(location.city || '').trim(),
      region: String(location.region || '').trim(),
      value: [location.longitude, location.latitude, location.count],
    };
  });
}

export const wrapLongitude = (longitude, anchor) => {
  const offset = (((longitude - anchor) + 180) % 360);
  return anchor + (((offset + 360) % 360) - 180);
};

// Keep equally named cities in different regions/countries separate. Locations
// without a city name remain explicitly unknown rather than inheriting a region.
export function cityTotals(points) {
  const cities = new Map();
  points.forEach((point) => {
    const key = JSON.stringify([point.countryKey, point.region, point.city]);
    const previous = cities.get(key);
    if (!previous) {
      cities.set(key, { ...point, key });
      return;
    }
    const count = previous.count + point.count;
    const longitude = wrapLongitude((previous.longitude * previous.count
      + wrapLongitude(point.longitude, previous.longitude) * point.count) / count, 0);
    const latitude = (previous.latitude * previous.count + point.latitude * point.count) / count;
    cities.set(key, {
      ...previous, count, longitude, latitude, value: [longitude, latitude, count],
    });
  });
  return [...cities.values()].sort((first, second) => second.count - first.count || first.key.localeCompare(second.key));
}

function wrapCoordinates(coordinates, anchor) {
  if (typeof coordinates[0] === 'number') return [wrapLongitude(coordinates[0], anchor), coordinates[1]];
  return coordinates.map((part) => wrapCoordinates(part, anchor));
}

function ringArea(ring) {
  let area = 0;
  ring.forEach(([x, y], index) => {
    const [nextX, nextY] = ring[(index + 1) % ring.length];
    area += x * nextY - nextX * y;
  });
  return Math.abs(area / 2);
}

function boundsFor(coordinates) {
  const longitudes = coordinates.map(([longitude]) => longitude);
  const latitudes = coordinates.map(([, latitude]) => latitude);
  const left = Math.min(...longitudes);
  const right = Math.max(...longitudes);
  const top = Math.max(...latitudes);
  const bottom = Math.min(...latitudes);
  const longitudePadding = Math.max((right - left) * 0.08, 0.15);
  const latitudePadding = Math.max((top - bottom) * 0.08, 0.15);
  return [[left - longitudePadding, Math.min(90, top + latitudePadding)],
    [right + longitudePadding, Math.max(-90, bottom - latitudePadding)]];
}

// Fit the main land mass plus this country's locations. Distant territories
// without opens do not shrink the useful view; dateline islands stay together.
export function countryView(key, points, focusedCity = null) {
  const country = mapsByKey.get(key);
  let mainRing = [];
  if (country) {
    const { geometry } = country.feature;
    const rings = (geometry.type === 'Polygon' ? [geometry.coordinates[0]] : geometry.coordinates.map((polygon) => polygon[0]))
      .filter((ring) => Array.isArray(ring) && ring.length >= 3);
    [mainRing = []] = rings.map((ring) => wrapCoordinates(ring, ring[0][0])).sort((first, second) => ringArea(second) - ringArea(first));
  }
  const anchor = mainRing.length ? (Math.min(...mainRing.map(([x]) => x)) + Math.max(...mainRing.map(([x]) => x))) / 2
    : ((points[0] || {}).longitude || 0);
  const coordinates = [...mainRing, ...points.map((point) => [wrapLongitude(point.longitude, anchor), point.latitude])];
  let bounds = coordinates.length ? boundsFor(coordinates) : null;
  if (focusedCity) {
    const longitude = wrapLongitude(focusedCity.longitude, anchor);
    bounds = [[longitude - 1.5, Math.min(90, focusedCity.latitude + 1)],
      [longitude + 1.5, Math.max(-90, focusedCity.latitude - 1)]];
  }
  const geoJSON = country ? {
    type: 'FeatureCollection',
    features: [{
      ...country.feature,
      geometry: { ...country.feature.geometry, coordinates: wrapCoordinates(country.feature.geometry.coordinates, anchor) },
    }],
  } : null;
  return { geoJSON, bounds, anchor };
}

export { worldMap };
