package fulfillment

import (
	"app.local/internal/kit/uow"
	"app.local/internal/modules/fulfillment/jobs"
	"app.local/internal/platform/queue"
)

type Deps struct {
	UoW    uow.UnitOfWork
	Ledger EventLedger
	// Stock 由 internal/app/adapters.go 提供，负责把 product 的错误翻译成 ErrStockUnavailable。
	Stock  StockReserver
	Orders OrderStatus
}

type Module struct {
	process *Process
}

func New(d Deps) *Module {
	return &Module{process: NewProcess(d.UoW, d.Ledger, d.Stock, d.Orders)}
}

// RegisterJobs 注册本模块消费的全部任务。
func (m *Module) RegisterJobs(workers *queue.Workers) {
	queue.AddWorker(workers, jobs.NewOrderCreatedWorker(m.process))
}
