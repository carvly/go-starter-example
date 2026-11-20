package domaintest

import (
	"context"
	"slices"
	"sync"
	"time"

	"app.local/internal/modules/user/domain"
)

// SessionRepository 是 domain.SessionRepository 的内存实现。
type SessionRepository struct {
	mu          sync.Mutex
	byID        map[string]*domain.Session
	nextID      int
	idleTimeout time.Duration
}

func NewSessionRepository() *SessionRepository {
	return &SessionRepository{byID: make(map[string]*domain.Session), idleTimeout: 7 * 24 * time.Hour}
}

func (f *SessionRepository) next() string {
	f.nextID++
	return identityID(10000 + f.nextID)
}

func (f *SessionRepository) Create(_ context.Context, v *domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v.ID == "" {
		v.ID = f.next()
	}
	f.byID[v.ID] = new(*v)
	return nil
}

func (f *SessionRepository) GetValid(_ context.Context, sid string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[sid]
	if !ok || !v.ValidAt(time.Now(), f.idleTimeout) {
		return nil, domain.ErrNotFound
	}
	return new(*v), nil
}

func (f *SessionRepository) GetByFamily(_ context.Context, family string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.byID {
		if v.RefreshFamily == family {
			return new(*v), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *SessionRepository) Get(_ context.Context, sid string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[sid]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return new(*v), nil
}

// TryRotateRefresh 复刻 Postgres 条件 UPDATE 的语义，含 10 秒宽限期内允许上一代。
func (f *SessionRepository) TryRotateRefresh(_ context.Context, family, presentedHash, newHash string, grace, idleTimeout time.Duration, now time.Time) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.byID {
		if v.RefreshFamily != family || v.RevokedAt != nil || !v.ValidAt(now, idleTimeout) {
			continue
		}
		matchCurrent := v.RefreshTokenHash == presentedHash
		matchPrevious := v.PreviousRefreshTokenHash != nil && *v.PreviousRefreshTokenHash == presentedHash &&
			v.RotatedAt != nil && now.Sub(*v.RotatedAt) <= grace
		if !matchCurrent && !matchPrevious {
			continue
		}
		next := *v
		prev := v.RefreshTokenHash
		next.PreviousRefreshTokenHash = &prev
		next.RefreshTokenHash = newHash
		t := now
		next.RotatedAt = &t
		next.LastUsedAt = now
		next.UpdatedAt = now
		f.byID[v.ID] = &next
		return new(next), nil
	}
	return nil, nil
}

func (f *SessionRepository) UpdateAuthTime(_ context.Context, sid string, authTime time.Time, method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[sid]
	if !ok {
		return domain.ErrNotFound
	}
	next := *v
	next.AuthTime = authTime
	next.AMR = append(slices.Clone(next.AMR), method)
	next.UpdatedAt = authTime
	f.byID[sid] = &next
	return nil
}

func (f *SessionRepository) Revoke(_ context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[sid]
	if !ok {
		return domain.ErrNotFound
	}
	next := *v
	now := time.Now()
	next.RevokedAt = &now
	next.UpdatedAt = now
	f.byID[sid] = &next
	return nil
}

func (f *SessionRepository) RevokeAllByUser(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for id, v := range f.byID {
		if v.UserID == userID && v.RevokedAt == nil {
			next := *v
			next.RevokedAt = &now
			next.UpdatedAt = now
			f.byID[id] = &next
		}
	}
	return nil
}

func (f *SessionRepository) RevokeByLoginIdentity(_ context.Context, identityID, exceptSID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for id, v := range f.byID {
		if v.LoginIdentityID != nil && *v.LoginIdentityID == identityID && v.RevokedAt == nil && v.ID != exceptSID {
			next := *v
			next.RevokedAt = &now
			next.UpdatedAt = now
			f.byID[id] = &next
		}
	}
	return nil
}

func (f *SessionRepository) RepointLoginIdentity(_ context.Context, sid, identityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[sid]
	if !ok {
		return domain.ErrNotFound
	}
	next := *v
	next.LoginIdentityID = &identityID
	f.byID[sid] = &next
	return nil
}

func (f *SessionRepository) ClearLoginIdentity(_ context.Context, sid, identityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[sid]
	if !ok {
		return domain.ErrNotFound
	}
	if v.LoginIdentityID != nil && *v.LoginIdentityID == identityID {
		next := *v
		next.LoginIdentityID = nil
		f.byID[sid] = &next
	}
	return nil
}

func (f *SessionRepository) ListByUser(_ context.Context, userID string) ([]*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Session
	for _, v := range f.byID {
		if v.UserID == userID && v.RevokedAt == nil {
			out = append(out, new(*v))
		}
	}
	slices.SortFunc(out, func(a, b *domain.Session) int { return b.LastUsedAt.Compare(a.LastUsedAt) })
	return out, nil
}

func (f *SessionRepository) PageByUser(_ context.Context, userID, cursor string, limit int) ([]*domain.Session, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []*domain.Session
	for _, v := range f.byID {
		if v.UserID == userID && v.RevokedAt == nil {
			all = append(all, new(*v))
		}
	}
	return pageDesc(all, cursor, limit, func(v *domain.Session) (time.Time, string) { return v.CreatedAt, v.ID })
}

func (f *SessionRepository) PurgeExpired(_ context.Context, now time.Time, revokedGrace time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for id, v := range f.byID {
		if now.After(v.ExpiresAt) || (v.RevokedAt != nil && now.Sub(*v.RevokedAt) > revokedGrace) {
			delete(f.byID, id)
			n++
		}
	}
	return n, nil
}

var _ domain.SessionRepository = (*SessionRepository)(nil)

// OAuthStateRepository 是 domain.OAuthStateRepository 的内存实现。
type OAuthStateRepository struct {
	mu     sync.Mutex
	byHash map[string]*domain.OAuthState
}

func NewOAuthStateRepository() *OAuthStateRepository {
	return &OAuthStateRepository{byHash: make(map[string]*domain.OAuthState)}
}

func (f *OAuthStateRepository) Create(_ context.Context, v *domain.OAuthState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byHash[v.IDHash] = new(*v)
	return nil
}

// Take 只返回未过期的行并删除（一次性）；过期行保留在表里（和 Postgres 的
// DELETE ... WHERE expires_at > now() 行为一致），等 PurgeExpired 清理。
func (f *OAuthStateRepository) Take(_ context.Context, idHash string) (*domain.OAuthState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byHash[idHash]
	if !ok || time.Now().After(v.ExpiresAt) {
		return nil, domain.ErrNotFound
	}
	delete(f.byHash, idHash)
	return new(*v), nil
}

func (f *OAuthStateRepository) PurgeExpired(_ context.Context, now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for k, v := range f.byHash {
		if now.After(v.ExpiresAt) {
			delete(f.byHash, k)
			n++
		}
	}
	return n, nil
}

var _ domain.OAuthStateRepository = (*OAuthStateRepository)(nil)

// ListAll 返回全部会话（含已吊销），测试断言用。
func (f *SessionRepository) ListAll() []*domain.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.Session, 0, len(f.byID))
	for _, v := range f.byID {
		out = append(out, new(*v))
	}
	return out
}
