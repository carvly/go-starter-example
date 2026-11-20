// Package crypto 提供 Argon2id 密码哈希和 JWT（HS256）签发/校验。它不认识
// 任何业务模块，staff 模块的端口由它的方法签名直接满足。
package crypto

import (
	"github.com/alexedwards/argon2id"
)

type PasswordVerifier struct {
	params *argon2id.Params
}

// NewPasswordVerifier 要求显式传入参数，不隐式用 argon2id.DefaultParams：
// 参数决定暴力破解一次哈希的内存和时间成本，必须由部署方按环境压测决定。
func NewPasswordVerifier(params *argon2id.Params) *PasswordVerifier {
	return &PasswordVerifier{params: params}
}

func (v *PasswordVerifier) Hash(password string) (string, error) {
	return argon2id.CreateHash(password, v.params)
}

func (v *PasswordVerifier) Compare(hash, password string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}
