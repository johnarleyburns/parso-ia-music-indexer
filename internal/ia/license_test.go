package ia

import "testing"

func TestCommerciallyUsableLicensePolicy(t *testing.T) {
	cases := []struct {
		url      string
		wantCat  string
		wantFree bool
	}{
		{"http://creativecommons.org/publicdomain/zero/1.0/", "pd", true},
		{"https://creativecommons.org/publicdomain/zero/1.0/", "pd", true},
		{"http://creativecommons.org/publicdomain/mark/1.0/", "pd", true},
		{"http://creativecommons.org/licenses/by/3.0/", "cc-by", true},
		{"http://creativecommons.org/licenses/by/4.0/", "cc-by", true},
		{"http://creativecommons.org/licenses/by/3.0/us/", "cc-by", true},
		{"http://creativecommons.org/licenses/by-sa/4.0/", "cc-by-sa", true},
		{"http://creativecommons.org/licenses/by-nc/3.0/", "cc-by-nc", false},
		{"http://creativecommons.org/licenses/by-nd/4.0/", "cc-by-nd", false},
		{"http://creativecommons.org/licenses/by-nc-nd/3.0/", "cc-by-nc-nd", false},
		{"http://creativecommons.org/licenses/by-nc-sa/3.0/", "cc-by-nc-sa", false},
		{"", "unknown", false},
		{"https://example.com/some-other-license", "other", false},
	}

	for _, c := range cases {
		gotCat := ClassifyLicense(c.url)
		if gotCat != c.wantCat {
			t.Errorf("ClassifyLicense(%q) = %q, want %q", c.url, gotCat, c.wantCat)
		}
		if gotFree := IsCommerciallyUsable(gotCat); gotFree != c.wantFree {
			t.Errorf("IsCommerciallyUsable(ClassifyLicense(%q)=%q) = %v, want %v", c.url, gotCat, gotFree, c.wantFree)
		}
	}
}
