package dag

import "time"

// Duration is a time.Duration that can be decoded from a TOML string literal
// such as "2m", "30s", or "100ms" (via encoding.TextUnmarshaler) as well as
// from a raw integer nanosecond value.
type Duration time.Duration

// String returns the duration in a human-readable form (e.g. "2m0s").
func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalText implements encoding.TextUnmarshaler so that go-toml/v2
// decodes TOML strings like timeout = "2m" into a Duration correctly.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}
