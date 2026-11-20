package app

import (
	"context"
	"testing"

	"github.com/riverqueue/river"

	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/user"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
	"app.local/internal/modules/user/events"
	"app.local/internal/modules/user/jobs"
)

type userDataFixture struct {
	rows map[string]string
}

func (f *userDataFixture) UserDataCleaner() user.DataCleaner { return f }

func (f *userDataFixture) DeleteUserData(_ context.Context, userID string) error {
	delete(f.rows, userID)
	return nil
}

func (f *userDataFixture) Sweep(ctx context.Context, profiles func(context.Context, []string) (map[string]domain.PublicProfile, error)) error {
	ids := make([]string, 0, len(f.rows))
	for id := range f.rows {
		ids = append(ids, id)
	}
	got, err := profiles(ctx, ids)
	if err != nil {
		return err
	}
	for id := range f.rows {
		profile, ok := got[id]
		if !ok || profile.Status == domain.UserStatusDeleted {
			delete(f.rows, id)
		}
	}
	return nil
}

func TestUserDataFixture_DeletedEventRunsCleaner(t *testing.T) {
	member := &userDataFixture{rows: map[string]string{"u1": "data", "u2": "keep"}}
	worker := jobs.NewUserDeletedWorker([]user.DataCleaner{member.UserDataCleaner()})
	job := &river.Job[events.UserDeleted]{Args: events.UserDeleted{UserID: "u1"}}
	if err := worker.Work(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if _, ok := member.rows["u1"]; ok {
		t.Fatal("注销事件没有删除业务数据")
	}
	if _, ok := member.rows["u2"]; !ok {
		t.Fatal("注销事件删除了其他用户的数据")
	}
}

func TestUserDataFixture_SweepRemovesDeletedAndMissingUsers(t *testing.T) {
	fakes := domaintest.NewFakes()
	deps := fakes.Deps()
	deps.UoW = uowtest.Immediate{}
	deps.Issuer = domaintest.Issuer{}
	deps.Config = domaintest.TestConfig()
	svc := domain.NewService(deps)
	active, deleted := domaintest.ValidUser(), domaintest.ValidUser()
	active.Status, deleted.Status = domain.UserStatusActive, domain.UserStatusDeleted
	for _, v := range []*domain.User{active, deleted} {
		if err := fakes.Users.Create(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	member := &userDataFixture{rows: map[string]string{active.ID: "keep", deleted.ID: "delete", "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6": "missing"}}
	if err := member.Sweep(t.Context(), svc.GetPublicProfiles); err != nil {
		t.Fatal(err)
	}
	if len(member.rows) != 1 || member.rows[active.ID] != "keep" {
		t.Fatalf("清扫后的业务数据 = %v", member.rows)
	}
}
