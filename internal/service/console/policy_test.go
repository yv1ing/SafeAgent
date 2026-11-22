package console

import (
	"context"
	"errors"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type policyAccess struct{}

func (policyAccess) Project(_ context.Context, id string) (*entity.Project, error) {
	return &entity.Project{ID: id}, nil
}

func (policyAccess) Membership(context.Context, string, string) (bool, error) { return true, nil }

func TestPolicyWritesRequireAnAdministrator(t *testing.T) {
	service := &PolicyService{access: policyAccess{}}
	actor := Actor{UserID: "member"}
	ctx := context.Background()
	document := policy.DefaultDocument()

	checks := map[string]func() error{
		"save": func() error {
			_, err := service.Save(ctx, actor, "project_1", document, "")
			return err
		},
		"validate": func() error {
			return service.Validate(ctx, actor, "project_1", document)
		},
		"test": func() error {
			_, err := service.Test(ctx, actor, "project_1", document, nil)
			return err
		},
		"rollback": func() error {
			_, err := service.Rollback(ctx, actor, "project_1", 1)
			return err
		},
	}
	for name, check := range checks {
		var failure *Failure
		if err := check(); !errors.As(err, &failure) || failure.Kind != KindForbidden {
			t.Fatalf("%s must require an administrator: %v", name, err)
		}
	}
}
