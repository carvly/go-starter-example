package user

import "context"

// DataCleaner 由持有前台用户数据的模块实现，必须幂等。
type DataCleaner interface {
	DeleteUserData(ctx context.Context, userID string) error
}

// AddDataCleaner 在组合根完成模块构造后登记注销清理器。
func (m *Module) AddDataCleaner(c DataCleaner) {
	m.dataCleaners = append(m.dataCleaners, c)
}
