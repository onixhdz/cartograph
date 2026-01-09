package extractors

import ts "github.com/odvcencio/gotreesitter"

// safeNodeText returns the source text covered by the node, clamping byte
// offsets to the source length so that malformed or out-of-sync trees never
// cause a slice-bounds panic.
func safeNodeText(n *ts.Node, source []byte) string {
	if n == nil {
		return ""
	}
	start := n.StartByte()
	end := n.EndByte()
	srcLen := uint32(len(source))
	if start >= srcLen {
		return ""
	}
	if end > srcLen {
		end = srcLen
	}
	return string(source[start:end])
}

// safeCaptureText returns the source text for a query capture, with the same
// bounds-safety guarantees as safeNodeText.
func safeCaptureText(c ts.QueryCapture, source []byte) string {
	if c.TextOverride != "" {
		return c.TextOverride
	}
	return safeNodeText(c.Node, source)
}
