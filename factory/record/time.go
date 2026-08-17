package record

import "time"

// TimeLayout is how every record's timestamp is written: RFC 3339 in UTC, with
// the fraction always nine digits and the zone always the literal Z. The width
// is fixed rather than trimmed because the decision log hashes the stored text
// of this field, and a field that is hashed has to read back byte for byte.
const TimeLayout = "2006-01-02T15:04:05.000000000Z"

// FormatTime writes t in [TimeLayout], converting it to UTC first.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// Now is the current time in [TimeLayout]. The writer of a record calls this
// rather than taking a timestamp from its caller, so what a record says about
// when it was written is what the writer observed.
func Now() string { return FormatTime(time.Now()) }
