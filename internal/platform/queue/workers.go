package queue

import (
	"slices"

	"github.com/riverqueue/river"
)

// Workers 包装 river.Workers 并记录注册过的 kind：worker 注册是"本二进制能处理哪些任务"的唯一来源，
// 孤儿任务的检查和清理都从这里取 kind，不另写清单。
type Workers struct {
	river *river.Workers
	kinds []string
}

func NewWorkers() *Workers { return &Workers{river: river.NewWorkers()} }

// AddWorker 注册 worker；同一个 kind 注册两次会 panic（river 的行为）。
func AddWorker[T river.JobArgs](w *Workers, worker river.Worker[T]) {
	river.AddWorker(w.river, worker)
	var args T
	w.kinds = append(w.kinds, args.Kind())
}

// Kinds 是已注册的 kind，已排序。
func (w *Workers) Kinds() []string {
	out := slices.Clone(w.kinds)
	slices.Sort(out)
	return out
}
