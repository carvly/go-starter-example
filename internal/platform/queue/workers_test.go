package queue

import (
	"context"
	"slices"
	"testing"

	"github.com/riverqueue/river"
)

type argsA struct{}

func (argsA) Kind() string { return "b_kind" }

type argsB struct{}

func (argsB) Kind() string { return "a_kind" }

type workerA struct{ river.WorkerDefaults[argsA] }

func (workerA) Work(context.Context, *river.Job[argsA]) error { return nil }

type workerB struct{ river.WorkerDefaults[argsB] }

func (workerB) Work(context.Context, *river.Job[argsB]) error { return nil }

func TestWorkersRecordKinds(t *testing.T) {
	w := NewWorkers()
	AddWorker(w, workerA{})
	AddWorker(w, workerB{})
	if got := w.Kinds(); !slices.Equal(got, []string{"a_kind", "b_kind"}) {
		t.Fatalf("Kinds = %v", got)
	}
}
