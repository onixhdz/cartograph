package plugin

// ParseHandshakeForTest exposes parseHandshake for unit tests.
// This file is only compiled in the plugin package's test binary.
func ParseHandshakeForTest(line string) (name string, version string, err error) {
	return parseHandshake(line)
}
