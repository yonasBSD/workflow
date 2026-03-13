// Package tty provides terminal detection and colour helpers.
package tty

import "os"

// IsColourEnabled reports whether ANSI colour output should be produced.
//
// Colour is disabled when any of the following conditions is true:
//   - The NO_COLOR environment variable is non-empty (https://no-color.org)
//   - The TERM environment variable is "dumb"
//   - stdout is not a character device (i.e. it is a pipe or regular file)
func IsColourEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// Colourise wraps text in the given ANSI escape code when colour is enabled,
// and returns the plain text otherwise.  code should be an SGR parameter
// string such as "92" (bright green) or "91" (bright red).
func Colourise(text, code string) string {
	if !IsColourEnabled() {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}
