package workspaces

import (
	"errors"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
		errType error // nil means any non-nil error is fine
	}{
		// Valid slugs.
		{name: "simple lowercase", slug: "acme", wantErr: false},
		{name: "with digits", slug: "acme123", wantErr: false},
		{name: "with hyphens", slug: "demo-launch", wantErr: false},
		{name: "single letter", slug: "a", wantErr: false},
		{name: "max length 31", slug: "a123456789012345678901234567890", wantErr: false},
		{name: "alphanumeric mix", slug: "my-workspace-v2", wantErr: false},

		// Empty.
		{name: "empty string", slug: "", wantErr: true},

		// Reserved names — all must be rejected.
		{name: "reserved main", slug: "main", wantErr: true, errType: ErrSlugReserved{}},
		{name: "reserved dev", slug: "dev", wantErr: true, errType: ErrSlugReserved{}},
		{name: "reserved prod", slug: "prod", wantErr: true, errType: ErrSlugReserved{}},
		{name: "reserved default", slug: "default", wantErr: true, errType: ErrSlugReserved{}},
		{name: "reserved current", slug: "current", wantErr: true, errType: ErrSlugReserved{}},
		{name: "reserved tokens", slug: "tokens", wantErr: true, errType: ErrSlugReserved{}},
		{name: "reserved trash", slug: "trash", wantErr: true, errType: ErrSlugReserved{}},

		// Leading dot.
		{name: "leading dot", slug: ".hidden", wantErr: true},
		// Double-underscore prefix.
		{name: "double underscore", slug: "__internal", wantErr: true},
		// Path traversal.
		{name: "slash in slug", slug: "a/b", wantErr: true},
		{name: "dot-dot", slug: "..", wantErr: true},
		{name: "path traversal embedded", slug: "a/../b", wantErr: true},

		// Regex violations.
		{name: "uppercase", slug: "MyWorkspace", wantErr: true, errType: ErrSlugInvalid{}},
		{name: "starts with digit", slug: "1workspace", wantErr: true, errType: ErrSlugInvalid{}},
		{name: "starts with hyphen", slug: "-workspace", wantErr: true, errType: ErrSlugInvalid{}},
		{name: "underscore", slug: "my_workspace", wantErr: true, errType: ErrSlugInvalid{}},
		{name: "space", slug: "my workspace", wantErr: true},
		{name: "too long (32 chars)", slug: "a1234567890123456789012345678901", wantErr: true, errType: ErrSlugInvalid{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSlug(tc.slug)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateSlug(%q) = nil; want error", tc.slug)
				}
				if tc.errType != nil {
					// Check type match using errors.As.
					switch tc.errType.(type) {
					case ErrSlugReserved:
						var target ErrSlugReserved
						if !errors.As(err, &target) {
							t.Errorf("ValidateSlug(%q) error type: want ErrSlugReserved, got %T", tc.slug, err)
						}
					case ErrSlugInvalid:
						var target ErrSlugInvalid
						if !errors.As(err, &target) {
							t.Errorf("ValidateSlug(%q) error type: want ErrSlugInvalid, got %T", tc.slug, err)
						}
					}
				}
			} else {
				if err != nil {
					t.Fatalf("ValidateSlug(%q) = %v; want nil", tc.slug, err)
				}
			}
		})
	}
}

func TestReservedSlugsIsSorted(t *testing.T) {
	for i := 1; i < len(ReservedSlugs); i++ {
		if ReservedSlugs[i-1] >= ReservedSlugs[i] {
			t.Errorf("ReservedSlugs not sorted at index %d: %q >= %q",
				i, ReservedSlugs[i-1], ReservedSlugs[i])
		}
	}
}

func TestAllReservedSlugsAreRejected(t *testing.T) {
	for _, slug := range ReservedSlugs {
		t.Run(slug, func(t *testing.T) {
			err := ValidateSlug(slug)
			if err == nil {
				t.Errorf("reserved slug %q should be rejected", slug)
			}
			var target ErrSlugReserved
			if !errors.As(err, &target) {
				t.Errorf("reserved slug %q should return ErrSlugReserved, got %T: %v", slug, err, err)
			}
		})
	}
}

// TestErrSlugInvalidErrorString exercises the Error() method so the string
// representation is covered.
func TestErrSlugInvalidErrorString(t *testing.T) {
	e := ErrSlugInvalid{Slug: "Bad-Slug"}
	msg := e.Error()
	if len(msg) == 0 {
		t.Error("ErrSlugInvalid.Error() returned empty string")
	}
	if msg[:len("workspaces:")] != "workspaces:" {
		t.Errorf("expected prefix 'workspaces:', got %q", msg)
	}
}

// TestErrSlugReservedErrorString exercises the Error() method so the string
// representation is covered.
func TestErrSlugReservedErrorString(t *testing.T) {
	e := ErrSlugReserved{Slug: "main"}
	msg := e.Error()
	if len(msg) == 0 {
		t.Error("ErrSlugReserved.Error() returned empty string")
	}
	if msg[:len("workspaces:")] != "workspaces:" {
		t.Errorf("expected prefix 'workspaces:', got %q", msg)
	}
}
