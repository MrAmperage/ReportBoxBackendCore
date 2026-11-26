package observe

import (
	"regexp"
	"sync"
)

const maxRPCProcedures = 256
const otherProcedure = "_other"

var procedurePattern = regexp.MustCompile(`^/(?:[A-Za-z_][A-Za-z0-9_]*\.)*[A-Za-z_][A-Za-z0-9_]*/[A-Za-z_][A-Za-z0-9_]*$`)

type procedureLabels struct {
	mu    sync.Mutex
	names map[string]struct{}
}

// label caps distinct labels even if a caller constructs clients dynamically.
// Invalid, oversized and overflow names share a single fixed bucket.
func (i *procedureLabels) label(name string) string {
	if len(name) > 256 || !procedurePattern.MatchString(name) {
		return otherProcedure
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, ok := i.names[name]; ok {
		return name
	}
	if len(i.names) >= maxRPCProcedures {
		return otherProcedure
	}
	if i.names == nil {
		i.names = make(map[string]struct{})
	}
	i.names[name] = struct{}{}
	return name
}
