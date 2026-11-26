package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

func isKind(err error, kind access.Kind) bool {
	var failure *access.Failure
	return errors.As(err, &failure) && failure.Kind == kind
}

func TestAdminIsRequiredForGlobalOperations(t *testing.T) {
	if err := (access.Actor{UserID: "u1"}).RequireAdmin(); !isKind(err, access.KindForbidden) {
		t.Fatalf("a member must not pass an administrator check: %v", err)
	}
	if err := (access.Actor{UserID: "u1", Administrator: true}).RequireAdmin(); err != nil {
		t.Fatalf("administrator was rejected: %v", err)
	}
}

func TestTranslateKeepsUnknownErrors(t *testing.T) {
	if err := access.Translate(repository.ErrNotFound); !isKind(err, access.KindNotFound) {
		t.Fatalf("not found must map to not found, got %v", err)
	}
	if err := access.Translate(repository.ErrVersionConflict); !isKind(err, access.KindConflict) {
		t.Fatalf("version conflict must map to a conflict, got %v", err)
	}
	sentinel := errors.New("disk on fire")
	if err := access.Translate(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("an unexpected error must survive translation, got %v", err)
	}
}

func TestNamesAreTrimmedAndBounded(t *testing.T) {
	if name, err := access.RequireName("  gateway ", "invalid_name"); err != nil || name != "gateway" {
		t.Fatalf("valid name was rejected: %q %v", name, err)
	}
	if _, err := access.RequireName("   ", "invalid_name"); !isKind(err, access.KindInvalid) {
		t.Fatalf("blank name must be rejected")
	}
	long := make([]byte, 121)
	for index := range long {
		long[index] = 'a'
	}
	if _, err := access.RequireName(string(long), "invalid_name"); !isKind(err, access.KindInvalid) {
		t.Fatalf("over-long name must be rejected")
	}
}

type fakeProjectAccess struct {
	project    *entity.Project
	members    map[string]bool
	projectErr error
	memberErr  error
}

func (fake fakeProjectAccess) Project(_ context.Context, id string) (*entity.Project, error) {
	if fake.projectErr != nil {
		return nil, fake.projectErr
	}
	if fake.project == nil || fake.project.ID != id {
		return nil, repository.ErrNotFound
	}
	return fake.project, nil
}

func (fake fakeProjectAccess) Membership(_ context.Context, projectID, userID string) (bool, error) {
	if fake.memberErr != nil {
		return false, fake.memberErr
	}
	if fake.project == nil || fake.project.ID != projectID {
		return false, nil
	}
	return fake.members[userID], nil
}

func isNotFound(err error) bool {
	var failure *access.Failure
	return errors.As(err, &failure) && failure.Kind == access.KindNotFound
}

// TestRequireProjectAdmitsAdminsAndHidesNonMembers pins the gate every management read shares:
// administrators pass after a project-existence check, members pass, everyone else gets the same
// not-found answer a missing project produces.
func TestRequireProjectAdmitsAdminsAndHidesNonMembers(t *testing.T) {
	ctx := context.Background()
	store := fakeProjectAccess{project: &entity.Project{ID: "project_1"}, members: map[string]bool{"member": true}}
	administrator := access.Actor{UserID: "admin", Administrator: true}

	if err := administrator.RequireProject(ctx, store, "project_1"); err != nil {
		t.Fatalf("an administrator must pass an existing project: %v", err)
	}
	if err := (access.Actor{UserID: "member"}).RequireProject(ctx, store, "project_1"); err != nil {
		t.Fatalf("a member must pass its project: %v", err)
	}
	for name, err := range map[string]error{
		"outsider":          (access.Actor{UserID: "outsider"}).RequireProject(ctx, store, "project_1"),
		"member of nothing": (access.Actor{UserID: "member"}).RequireProject(ctx, store, "project_missing"),
		"admin of nothing":  administrator.RequireProject(ctx, store, "project_missing"),
	} {
		if !isNotFound(err) {
			t.Fatalf("%s must be answered as not found, got %v", name, err)
		}
	}
}

func TestRequireProjectSurfacesStorageFailures(t *testing.T) {
	ctx := context.Background()
	broken := errors.New("database unavailable")
	if err := (access.Actor{UserID: "member"}).RequireProject(ctx, fakeProjectAccess{memberErr: broken}, "project_1"); !errors.Is(err, broken) {
		t.Fatalf("a membership read failure must surface unchanged, got %v", err)
	}
	if err := (access.Actor{UserID: "admin", Administrator: true}).RequireProject(ctx, fakeProjectAccess{projectErr: broken}, "project_1"); !errors.Is(err, broken) {
		t.Fatalf("a project read failure must surface unchanged, got %v", err)
	}
}
