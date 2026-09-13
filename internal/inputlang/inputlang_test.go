package inputlang

import "testing"

func TestNormalize(t *testing.T) {
	for _, test := range []struct {
		language string
		want     string
	}{
		{"he", "he"},
		{"iw", "he"},
		{"HE-il", "he"},
		{"en", "en"},
		{"en-US", "en"},
		{"en_GB", "en"},
		{" fr-FR ", ""},
		{"", ""},
	} {
		if got := normalize(test.language); got != test.want {
			t.Errorf("normalize(%q) = %q, want %q", test.language, got, test.want)
		}
	}
}
