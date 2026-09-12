// Package pagination 实现所有列表接口统一的分页解析与响应封装(04 §4.3)。
package pagination

import (
	"fmt"
	"net/url"
	"strconv"
)

const (
	defaultPage = 1
	defaultSize = 20
	minSize     = 1
	maxSize     = 200
)

// Pagination 是解析后的分页参数。
type Pagination struct {
	Page int `json:"page"`
	Size int `json:"size"`
}

// Offset 返回 SQL/LIMIT 风格的偏移量。
func (p Pagination) Offset() int { return (p.Page - 1) * p.Size }

// Parse 从 query 参数解析分页,非法值返回错误;空值取默认值,size 超出上限取上限。
func Parse(query url.Values) (Pagination, error) {
	p := Pagination{Page: defaultPage, Size: defaultSize}
	if v := query.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return p, fmt.Errorf("invalid page %q: %w", v, err)
		}
		p.Page = n
	}
	if v := query.Get("size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return p, fmt.Errorf("invalid size %q: %w", v, err)
		}
		switch {
		case n < minSize:
			return p, fmt.Errorf("size %d below minimum %d", n, minSize)
		case n > maxSize:
			n = maxSize
		}
		p.Size = n
	}
	return p, nil
}

// Result 封装分页响应体,形状为 {items,total,page,size}。
func Result[T any](items []T, total int64, p Pagination) ResultBody[T] {
	if items == nil {
		items = []T{}
	}
	return ResultBody[T]{Items: items, Total: total, Page: p.Page, Size: p.Size}
}

// ResultBody 是统一的分页响应结构。
type ResultBody[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}
