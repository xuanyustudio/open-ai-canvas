package kernel

import "testing"

func TestBrandJoinAndPrefix(t *testing.T) {
	if got := BrandJoin("Canvas", "服务协议"); got != "Canvas 服务协议" {
		t.Fatalf("BrandJoin latin = %q", got)
	}
	if got := BrandJoin("星野", "服务协议"); got != "星野服务协议" {
		t.Fatalf("BrandJoin cjk = %q", got)
	}
	if got := BrandPrefix("请先同意", "Canvas 服务协议"); got != "请先同意 Canvas 服务协议" {
		t.Fatalf("BrandPrefix latin = %q", got)
	}
	if got := BrandPrefix("请先同意", "星野服务协议"); got != "请先同意星野服务协议" {
		t.Fatalf("BrandPrefix cjk = %q", got)
	}
}
