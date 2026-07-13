package exportutil

import (
	"fmt"
	"strings"
)

// DedupeFilename returns name unchanged on first use and appends a numeric
// suffix before the extension on subsequent uses.
func DedupeFilename(name string, seen map[string]int) string {
	count := seen[name] + 1
	seen[name] = count
	if count == 1 {
		return name
	}

	base := name
	ext := ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		base = name[:i]
		ext = name[i:]
	}
	return fmt.Sprintf("%s-%d%s", base, count, ext)
}
