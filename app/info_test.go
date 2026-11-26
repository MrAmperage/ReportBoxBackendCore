package app

import "testing"

func TestNormalize(t *testing.T) {
	if _, err := (Info{Name: "  "}).Normalize(); err == nil {
		t.Fatal("empty name accepted")
	}
	i, err := (Info{Name: " test "}).Normalize()
	if err != nil || i.Name != "test" || i.Version != "dev" || i.Commit != "unknown" {
		t.Fatalf("%+v %v", i, err)
	}
}
