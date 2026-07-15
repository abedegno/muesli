package store

import "testing"

func TestValidateTemplatePhaseRejectsBogus(t *testing.T) {
	t.Parallel()
	if err := validateTemplatePhase("bogus"); err == nil {
		t.Fatal("bogus phase should fail validation")
	}
}
