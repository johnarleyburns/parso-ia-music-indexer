package ia

import (
	"testing"
)

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

func TestIsLicenseURLCommerciallyUsable(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://creativecommons.org/licenses/by/4.0/", true},
		{"http://creativecommons.org/licenses/by-sa/4.0/", true},
		{"http://creativecommons.org/publicdomain/zero/1.0/", true},
		{"http://creativecommons.org/licenses/by-nc/3.0/", false},
		{"http://creativecommons.org/licenses/by-nc-sa/3.0/", false},
		{"http://creativecommons.org/licenses/by-nc-nd/3.0/", false},
		{"", false},
	}
	for _, c := range cases {
		got := IsLicenseURLCommerciallyUsable(c.url)
		if got != c.want {
			t.Errorf("IsLicenseURLCommerciallyUsable(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestFilterCommerciallyUsable(t *testing.T) {
	items := []ScrapeItem{
		{Identifier: "pd-item", LicenseURL: "http://creativecommons.org/publicdomain/zero/1.0/"},
		{Identifier: "by-item", LicenseURL: "http://creativecommons.org/licenses/by/4.0/"},
		{Identifier: "by-sa-item", LicenseURL: "http://creativecommons.org/licenses/by-sa/4.0/"},
		{Identifier: "nc-item", LicenseURL: "http://creativecommons.org/licenses/by-nc/3.0/"},
		{Identifier: "nc-nd-item", LicenseURL: "http://creativecommons.org/licenses/by-nc-nd/3.0/"},
		{Identifier: "empty-license", LicenseURL: ""},
		{Identifier: "unknown-license", LicenseURL: "https://example.com/license"},
	}
	filtered := FilterCommerciallyUsable(items)
	if len(filtered) != 3 {
		t.Errorf("FilterCommerciallyUsable: got %d items, want 3", len(filtered))
	}
	ids := make(map[string]bool)
	for _, item := range filtered {
		ids[item.Identifier] = true
	}
	for _, want := range []string{"pd-item", "by-item", "by-sa-item"} {
		if !ids[want] {
			t.Errorf("FilterCommerciallyUsable: missing %q", want)
		}
	}
}
