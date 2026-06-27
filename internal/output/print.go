package output

import (
	"fmt"
	"io"
)

// PrintComparison writes the RAW / HINTED / REWRITTEN comparison to w.
func PrintComparison(w io.Writer, raw, hinted, rewritten string) {
	fmt.Fprintf(w, "RAW (no hint):\n%s\n\nHINTED:\n%s\n\nREWRITTEN:\n%s\n", raw, hinted, rewritten)
}
