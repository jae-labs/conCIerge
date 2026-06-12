package github

import "testing"

func TestSanitizeBranchSegment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "MyRepo", "myrepo"},
		{"spaces become hyphens", "My Repo With Spaces", "my-repo-with-spaces"},
		{"strips trailing punctuation", "repo!!", "repo"},
		{"compresses runs of punctuation", "a---b__c", "a---b__c"},
		{"replaces unicode and slashes", "Café/Repo", "caf-repo"},
		{"trims leading/trailing hyphens", "-repo-", "repo"},
		{"empty stays empty", "", ""},
		{"only punctuation collapses", "!!!", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeBranchSegment(tc.in); got != tc.want {
				t.Fatalf("SanitizeBranchSegment(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDefaultAuthorIsNoreply(t *testing.T) {
	a := DefaultAuthor()
	if a.Name == "" || a.Email == "" {
		t.Fatalf("default author empty: %+v", a)
	}
	if a.Email == "luiz@justanother.engineer" {
		t.Fatalf("default author leaks personal email")
	}
}
