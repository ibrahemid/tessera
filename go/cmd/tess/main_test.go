package main

import "testing"

func TestResolveVersionPrefersLdflagsStamp(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	version = "1.2.3"
	if got := resolveVersion(); got != "1.2.3" {
		t.Fatalf("resolveVersion() = %q, want stamped %q", got, "1.2.3")
	}
}

func TestResolveVersionFallsBackForInTreeBuilds(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	// A test binary's build info reports "(devel)" (or is absent), so the
	// module-version fallback must not fire and "dev" must survive.
	version = "dev"
	if got := resolveVersion(); got != "dev" {
		t.Fatalf("resolveVersion() = %q, want %q for an in-tree build", got, "dev")
	}
}
