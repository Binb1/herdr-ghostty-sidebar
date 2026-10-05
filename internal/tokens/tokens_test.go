package tokens

import "testing"

func TestIndentSurvivesTrim(t *testing.T) {
	if got := Indent("x"); got != "​  x" {
		t.Fatalf("%q", got)
	}
}
