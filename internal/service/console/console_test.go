package console

import (
	"errors"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

func TestAdminIsRequiredForGlobalOperations(t *testing.T) {
	if err := (Actor{UserID: "u1"}).RequireAdmin(); !isKind(err, KindForbidden) {
		t.Fatalf("a member must not pass an administrator check: %v", err)
	}
	if err := (Actor{UserID: "u1", Administrator: true}).RequireAdmin(); err != nil {
		t.Fatalf("administrator was rejected: %v", err)
	}
}

func TestTranslateKeepsUnknownErrors(t *testing.T) {
	if err := Translate(repository.ErrNotFound); !isKind(err, KindNotFound) {
		t.Fatalf("not found must map to not found, got %v", err)
	}
	if err := Translate(repository.ErrVersionConflict); !isKind(err, KindConflict) {
		t.Fatalf("version conflict must map to a conflict, got %v", err)
	}
	sentinel := errors.New("disk on fire")
	if err := Translate(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("an unexpected error must survive translation, got %v", err)
	}
}

func TestNamesAreTrimmedAndBounded(t *testing.T) {
	if name, err := RequireName("  gateway ", "invalid_name"); err != nil || name != "gateway" {
		t.Fatalf("valid name was rejected: %q %v", name, err)
	}
	if _, err := RequireName("   ", "invalid_name"); !isKind(err, KindInvalid) {
		t.Fatalf("blank name must be rejected")
	}
	long := make([]byte, 121)
	for index := range long {
		long[index] = 'a'
	}
	if _, err := RequireName(string(long), "invalid_name"); !isKind(err, KindInvalid) {
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
		if ValidPassword(password) != expected {
			t.Fatalf("%q: expected %v", password, expected)
		}
	}
}
