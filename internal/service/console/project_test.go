package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestProjectCreateDefaultsStreamReviewOff(t *testing.T) {
	resolved, err := (ProjectCreateInput{Name: "p"}).resolve().normalize()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.StreamReviewBytes != 0 || resolved.StreamReviewTimeoutMS != DefaultStreamReviewTimeoutMS {
		t.Fatalf("stream review defaults = %+v", resolved)
	}
}

func TestProjectStreamReviewDefaultsAndBounds(t *testing.T) {
	input := ProjectInput{Name: "p", RetentionDays: 30, StreamReviewBytes: 1024}
	normalized, err := input.normalize()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.StreamReviewTimeoutMS != DefaultStreamReviewTimeoutMS {
		t.Fatalf("timeout = %d", normalized.StreamReviewTimeoutMS)
	}
	for _, invalid := range []ProjectInput{
		{Name: "p", RetentionDays: 30, RequestsPerMinute: 1, Concurrency: 1, StreamReviewBytes: MaximumStreamReviewBytes + 1},
		{Name: "p", RetentionDays: 30, RequestsPerMinute: 1, Concurrency: 1, StreamReviewBytes: 1, StreamReviewTimeoutMS: MaximumStreamReviewTimeoutMS + 1},
	} {
		if _, err := invalid.normalize(); err == nil {
			t.Fatalf("accepted invalid stream review settings: %+v", invalid)
		}
	}
}

func TestStreamReviewIsAnAdministratorField(t *testing.T) {
	existing := &entity.Project{}
	if (ProjectInput{StreamReviewBytes: 1}).changesAdministratorFields(existing) == false {
		t.Fatal("stream review bytes must be administrator-only")
	}
	if (ProjectInput{StreamReviewTimeoutMS: 1}).changesAdministratorFields(existing) == false {
		t.Fatal("stream review timeout must be administrator-only")
	}
}

func TestOmittedStreamReviewTimeoutKeepsTheDefault(t *testing.T) {
	existing := &entity.Project{
		Name: "p", RetentionDays: 30, RequestsPerMinute: 1, Concurrency: 1,
		StreamReviewTimeoutMS: DefaultStreamReviewTimeoutMS,
		CoverageThreshold:     DefaultCoverageThreshold,
	}
	normalized, err := (ProjectInput{Name: "p", RetentionDays: 30, RequestsPerMinute: 1, Concurrency: 1}).normalize()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.StreamReviewTimeoutMS != DefaultStreamReviewTimeoutMS {
		t.Fatalf("timeout = %d", normalized.StreamReviewTimeoutMS)
	}
	if normalized.changesAdministratorFields(existing) {
		t.Fatal("an omitted timeout must not look like an administrator change")
	}
}
