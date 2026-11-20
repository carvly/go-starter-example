package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveFromSpec_KeepsReferencedSchemas(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "admin.yaml")
	content := `paths:
  /users:
    get:
      operationId: listUsers
      tags: [users]
      parameters:
        - name: status
          in: query
          schema:
            $ref: "#/components/schemas/UserStatus"
  /products:
    get:
      operationId: listProducts
      tags: [products]
components:
  schemas:
    UserStatus:
      type: string
    ProductList:
      type: object
`
	if err := os.WriteFile(spec, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := removeFromSpec(content, "products")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "UserStatus:") {
		t.Error("保留的 UserStatus 被误删")
	}
	if strings.Contains(got, "ProductList:") {
		t.Error("未被引用的 ProductList 没有删除")
	}
}

func TestRemoveFromSpec_BlockStyleTags(t *testing.T) {
	content := `tags:
  - name: users
  - name: products
paths:
  /products:
    parameters:
      - name: id
        in: path
    get:
      operationId: getProduct
      tags:
        - products
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Product"
  /users:
    get:
      operationId: listUsers
      tags: [users]
components:
  schemas:
    Product:
      type: object
`
	got, err := removeFromSpec(content, "products")
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"/products", "getProduct", "name: products", "Product:"} {
		if strings.Contains(got, gone) {
			t.Errorf("块状 tags 的操作没删干净，仍有 %q：\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "listUsers") {
		t.Errorf("误删了其它操作：\n%s", got)
	}
}
