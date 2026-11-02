package extension

import (
	"context"
	"testing"
)

func TestOriginInAContext(t *testing.T) {
	type key struct{}
	tests := []struct {
		name string
		ctx  context.Context
		want Origin
	}{
		{"a context with none", context.Background(), HostOrigin},
		{"a view's origin", WithOrigin(context.Background(), 7), 7},
		{"the host's origin named", WithOrigin(context.Background(), HostOrigin), HostOrigin},
		{"the innermost wins", WithOrigin(WithOrigin(context.Background(), 2), 3), 3},
		{"other values do not hide it", context.WithValue(WithOrigin(context.Background(), 4), key{}, "x"), 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OriginFromContext(tt.ctx); got != tt.want {
				t.Errorf("OriginFromContext = %d, want %d", got, tt.want)
			}
		})
	}
}
