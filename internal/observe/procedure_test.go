package observe

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestProcedureCardinalityCap(t *testing.T) {
	var o procedureLabels
	for n := 0; n < maxRPCProcedures; n++ {
		name := fmt.Sprintf("/demo.Service/Method%d", n)
		if got := o.label(name); got != name {
			t.Fatalf("unexpected fallback %s", got)
		}
	}
	for _, name := range []string{"/demo.Service/Overflow", "/secret?token=private", strings.Repeat("a", 257)} {
		if o.label(name) != otherProcedure {
			t.Fatal("unbounded procedure")
		}
	}
	if o.label("/demo.Service/Method0") != "/demo.Service/Method0" {
		t.Fatal("existing series lost")
	}
}
func TestProcedureConcurrentCardinality(t *testing.T) {
	var labels procedureLabels
	var wg sync.WaitGroup
	for n := 0; n < 1024; n++ {
		wg.Add(1)
		go func(n int) { defer wg.Done(); labels.label(fmt.Sprintf("/test.Service/M%d", n)) }(n)
	}
	wg.Wait()
	if len(labels.names) != maxRPCProcedures {
		t.Fatalf("unexpected cardinality %d", len(labels.names))
	}
}
