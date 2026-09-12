package pagination

import (
	"net/url"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    Pagination
		wantErr bool
	}{
		{name: "defaults on empty", query: "", want: Pagination{Page: 1, Size: 20}},
		{name: "explicit values", query: "page=3&size=50", want: Pagination{Page: 3, Size: 50}},
		{name: "negative page", query: "page=-1&size=10", wantErr: true},
		{name: "zero size", query: "size=0", wantErr: true},
		{name: "size above cap clamps to 200", query: "page=2&size=500", want: Pagination{Page: 2, Size: 200}},
		{name: "non numeric page", query: "page=abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}
			got, err := Parse(q)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestOffsetAndResult(t *testing.T) {
	p := Pagination{Page: 3, Size: 20}
	if got := p.Offset(); got != 40 {
		t.Fatalf("Offset = %d, want 40", got)
	}
	r := Result([]string{"a"}, 11, p)
	if r.Total != 11 || r.Page != 3 || r.Size != 20 || len(r.Items) != 1 {
		t.Fatalf("unexpected result body: %+v", r)
	}
	nilItems := Result[string](nil, 0, Pagination{Page: 1, Size: 20})
	if nilItems.Items == nil {
		t.Fatal("items must be empty slice, not nil")
	}
}
