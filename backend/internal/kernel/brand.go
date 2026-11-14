package kernel

import "strings"

// BrandJoin 把品牌名接到中文后缀前：含拉丁字母的品牌中间加空格，纯中文品牌不加。
func BrandJoin(brand, suffix string) string {
	brand = strings.TrimSpace(brand)
	suffix = strings.TrimSpace(suffix)
	if brand == "" {
		return suffix
	}
	if suffix == "" {
		return brand
	}
	if brandNeedsSpace(brand) {
		return brand + " " + suffix
	}
	return brand + suffix
}

// BrandPrefix 把中文前缀接到品牌名前：含拉丁字母的品牌前加空格，纯中文品牌不加。
func BrandPrefix(prefix, brand string) string {
	brand = strings.TrimSpace(brand)
	prefix = strings.TrimSpace(prefix)
	if brand == "" {
		return prefix
	}
	if prefix == "" {
		return brand
	}
	if brandNeedsSpace(brand) {
		return prefix + " " + brand
	}
	return prefix + brand
}

func brandNeedsSpace(brand string) bool {
	for _, r := range brand {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return true
		}
	}
	return false
}
