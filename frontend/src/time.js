import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';

dayjs.extend(utc);
dayjs.extend(timezone);

// The admin UI has exactly one clock: the deployment's. The server reports its
// IANA zone in /api/config, every timestamp is rendered in that zone, and date
// pickers convert the picked wall clock back to an absolute instant. A browser
// in another zone therefore never shifts a campaign's scheduled time, a quota
// day boundary, or a reported open.
const FALLBACK_ZONE = 'Asia/Shanghai';

let zoneName = null;
let zoneOffset = null;

// HAS_ZONE matches a trailing Z or numeric UTC offset, i.e. a string that
// already names an absolute instant.
const HAS_ZONE = /(?:Z|[+-]\d{2}:?\d{2})$/i;

const isValidZone = (name) => {
  if (!name || typeof name !== 'string') {
    return false;
  }
  try {
    dayjs.tz('2000-01-01T00:00:00', name);
    return true;
  } catch (e) {
    return false;
  }
};

// configureTimezone is called once the server config is loaded. It makes the
// deployment zone dayjs' default, so every existing dayjs() call in the app
// already parses, renders and compares in that zone.
export function configureTimezone(config) {
  const cfg = config || {};
  let name = cfg.timezone;
  if (!isValidZone(name)) {
    name = isValidZone(FALLBACK_ZONE) ? FALLBACK_ZONE : null;
  }

  zoneName = name;
  zoneOffset = typeof cfg.timezone_offset === 'string' ? cfg.timezone_offset : null;

  if (name) {
    dayjs.tz.setDefault(name);
  }
  return zoneName;
}

export function zone() {
  return zoneName;
}

export function offsetLabel() {
  return zoneOffset || '';
}

// now returns the current instant in the deployment zone.
export function now() {
  return zoneName ? dayjs().tz(zoneName) : dayjs();
}

// parse converts any API timestamp (offset-aware or naive) or Date into the
// deployment zone. A timestamp that carries its own offset already names an
// absolute instant; its zone is only swapped for display, never re-interpreted.
// Missing values yield an invalid dayjs (not null) so callers keep the forgiving
// behaviour of dayjs(null) and never dereference a null.
export function parse(value) {
  if (!value) {
    return dayjs(null);
  }
  // A string without an offset is a wall clock reading from the deployment,
  // not from the browser: "2026-10-21 09:00" means 09:00 where the server runs.
  // dayjs.tz throws on unparseable input, so validate before re-interpreting and
  // hand back the invalid instance instead of an exception.
  if (typeof value === 'string' && !HAS_ZONE.test(value.trim())) {
    const naive = dayjs(value);
    if (!naive.isValid()) {
      return naive;
    }
    return zoneName ? naive.tz(zoneName, true) : naive;
  }

  const d = dayjs(value);
  return zoneName ? d.tz(zoneName) : d;
}

// toDate converts a value into a JS Date carrying the same absolute instant,
// which is what the API expects on the wire.
export function toDate(value) {
  return parse(value).toDate();
}

// today returns the deployment zone's calendar day as YYYY-MM-DD.
export function today() {
  return now().format('YYYY-MM-DD');
}

// wallClockISO converts a native datetime-local value ("YYYY-MM-DDTHH:mm",
// which carries no zone) into an absolute ISO instant read on the deployment's
// clock. Values that already carry an offset are passed through.
export function wallClockISO(value) {
  if (!value) {
    return '';
  }
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(String(value).trim());
  if (!match) {
    const parsed = parse(value);
    return parsed.isValid() ? parsed.toISOString() : '';
  }
  // Build the instant explicitly in the deployment zone; dayjs' default zone
  // is already that, but stating it keeps the helper correct if a caller runs
  // before configureTimezone().
  const stamp = `${match[1]}-${match[2]}-${match[3]}T${match[4]}:${match[5]}:${match[6] || '00'}`;
  return (zoneName ? dayjs.tz(stamp, zoneName) : dayjs(stamp)).toDate().toISOString();
}

export default {
  configureTimezone, zone, offsetLabel, now, parse, toDate, today, wallClockISO,
};
