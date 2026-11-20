package console_test

import (
	"errors"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

func TestAdminIsRequiredForGlobalOperations(t *testing.T) {
	if err := (console.Actor{UserID: "u1"}).RequireAdmin(); !isKind(err, console.KindForbidden) {
		t.Fatalf("a member must not pass an administrator check: %v", err)
	}
	if err := (console.Actor{UserID: "u1", Administrator: true}).RequireAdmin(); err != nil {
		t.Fatalf("administrator was rejected: %v", err)
	}
}

func TestTranslateKeepsUnknownErrors(t *testing.T) {
	if err := console.Translate(repository.ErrNotFound); !isKind(err, console.KindNotFound) {
		t.Fatalf("not found must map to not found, got %v", err)
	}
	if err := console.Translate(repository.ErrVersionConflict); !isKind(err, console.KindConflict) {
		t.Fatalf("version conflict must map to a conflict, got %v", err)
	}
	sentinel := errors.New("disk on fire")
	if err := console.Translate(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("an unexpected error must survive translation, got %v", err)
	}
}

func TestNamesAreTrimmedAndBounded(t *testing.T) {
	if name, err := console.RequireName("  gateway ", "invalid_name"); err != nil || name != "gateway" {
		t.Fatalf("valid name was rejected: %q %v", name, err)
	}
	if _, err := console.RequireName("   ", "invalid_name"); !isKind(err, console.KindInvalid) {
		t.Fatalf("blank name must be rejected")
	}
	long := make([]byte, 121)
	for index := range long {
		long[index] = 'a'
	}
	if _, err := console.RequireName(string(long), "invalid_name"); !isKind(err, console.KindInvalid) {
		t.Fatalf("over-long name must be rejected")
	}
}

func TestPasswordPolicyMatchesTheHasher(t *testing.T) {
	cases := map[string]bool{
		"Short1A":                  false,
		"lowercaseonlypassword123": false,
		"alllowercasepassword1234": false,
		"LongEnoughWithUpperAnd1":  true,
	}
	for password, expected := range cases {
		if console.ValidPassword(password) != expected {
			t.Fatalf("%q: expected %v", password, expected)
		}
	}
}

func isKind(err error, kind console.Kind) bool {
	var failure *console.Failure
	return errors.As(err, &failure) && failure.Kind == kind
}
