// Package oapi 是 oapi-codegen 从 api/openapi.yaml（tag=user）生成的类型和
// ServerInterface，禁止手改 api.gen.go。改接口先改 spec，再跑 go generate ./...
package oapi

//go:generate go tool oapi-codegen -config config.yaml ../../../../../api/openapi.yaml
