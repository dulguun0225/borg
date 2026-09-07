// Rendering a record's own values. Every record writes an instant in UTC, and
// the client is the only thing that renders one — in the viewer's locale and
// zone, which is what lets two holders in two zones read one deadline. A
// calendar value a human authors travels the other way, with the zone it was
// authored in beside it, and viewerZone is that zone.
//
// One copy per screen directory, identical in all four. A screen imports api/
// and state/ and nothing else outside its own directory, and this repetition
// is what buys that: a defect found in one copy is found in the rest by one
// search for the same name.

// An RFC 3339 UTC instant in the viewer's locale and zone. Empty where the
// record carries none, and the raw value where it carries one this runtime
// cannot read, because dropping it would hide a record the store holds.
export function atInstant(value: string): string {
  if (value === '') {
    return '';
  }
  const at = new Date(value);
  if (Number.isNaN(at.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(at);
}

// A calendar date the record carries, rendered in the viewer's locale. It is
// not converted to the viewer's zone: the value is a date and not an instant,
// and the zone it was authored in is a field beside it.
export function atDate(value: string): string {
  if (value === '') {
    return '';
  }
  const parts = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (parts === null) {
    return value;
  }
  const [, year, month, day] = parts;
  const at = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day)));
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeZone: 'UTC' }).format(at);
}

// The IANA zone the viewer is in, sent beside every calendar value a form
// takes, because every reader of that value computes in that zone and no
// other.
export function viewerZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

// A span of seconds in plain words, for the one reading the design asks for
// as an age: how long the oldest item dispatch holds unmatched has waited. A
// last check row never uses this — it names what has not been checked and
// since when, so the reader is not asked to read a number.
export function spanOf(seconds: number): string {
  const units: [number, string][] = [
    [86400, 'day'],
    [3600, 'hour'],
    [60, 'minute'],
  ];
  for (const [size, name] of units) {
    if (seconds >= size) {
      const many = Math.floor(seconds / size);
      return `${many} ${name}${many === 1 ? '' : 's'}`;
    }
  }
  return `${Math.floor(seconds)} second${Math.floor(seconds) === 1 ? '' : 's'}`;
}
