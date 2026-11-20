package specmap

import (
	"slices"
	"strings"
	"testing"
)

const sample = `openapi: 3.2.0
tags:
  - name: users
    summary: 用户
  # 商品的说明
  - name: products
    summary: 商品

paths:
  /users:
    get:
      operationId: listUsers
      tags: [users]
    post:
      operationId: createUser
      tags:
        - users
  # 商品
  /products:
    get:
      operationId: listProducts
      tags: [products]
components:
  schemas:
    User:
      type: object
    Product:
      type: object
`

func TestParse(t *testing.T) {
	d, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, op := range d.Operations {
		keys = append(keys, op.Key()+"="+strings.Join(op.Tags, ","))
	}
	want := []string{"GET /users=users", "POST /users=users", "GET /products=products"}
	if !slices.Equal(keys, want) {
		t.Fatalf("operations = %v, want %v", keys, want)
	}
	if got := d.Text(d.Operations[1].Range); !strings.Contains(got, "createUser") || strings.Contains(got, "# 商品") {
		t.Errorf("块状 tags 的操作范围不对：\n%s", got)
	}
	if got := d.Text(d.Paths["/products"]); !strings.HasPrefix(got, "  # 商品\n  /products:") {
		t.Errorf("path 范围应包含头部注释：\n%s", got)
	}
	if got := d.Text(d.TagDefs["products"]); !strings.HasPrefix(got, "  # 商品的说明") || strings.Contains(got, "paths") {
		t.Errorf("tag 定义范围不对：\n%s", got)
	}
	if got := d.Text(d.Schemas["Product"]); got != "    Product:\n      type: object" {
		t.Errorf("schema 范围不对：%q", got)
	}
	if d.SectionEnd["tags"] != 7 || d.SectionEnd["paths"] != 22 || d.SectionEnd["schemas"] != 28 {
		t.Errorf("SectionEnd = %v", d.SectionEnd)
	}
}

func TestDelete(t *testing.T) {
	d, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	got := d.Delete([]Range{d.Paths["/products"], d.TagDefs["products"], d.Schemas["Product"]})
	if strings.Contains(got, "products") || strings.Contains(got, "Product:") {
		t.Fatalf("没删干净：\n%s", got)
	}
	if _, err := Parse([]byte(got)); err != nil {
		t.Fatalf("删除后不是合法 YAML：%v\n%s", err, got)
	}
}
