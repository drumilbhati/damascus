package config

import "testing"

func TestParseServiceTargetURLs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{name: "empty is optional", want: map[string]string{}},
		{
			name:  "valid mapping",
			input: `{" frontend ":" http://frontend:8080/ ","cartservice":"https://cart.internal/cart"}`,
			want:  map[string]string{"frontend": "http://frontend:8080/", "cartservice": "https://cart.internal/cart"},
		},
		{name: "invalid json", input: `not-json`, wantErr: true},
		{name: "relative url", input: `{"frontend":"/"}`, wantErr: true},
		{name: "unsupported scheme", input: `{"frontend":"grpc://frontend:8080"}`, wantErr: true},
		{name: "blank service", input: `{" ":"http://frontend:8080"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseServiceTargetURLs(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseServiceTargetURLs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for name, wantURL := range tt.want {
				if got[name] != wantURL {
					t.Errorf("URL for %q = %q, want %q", name, got[name], wantURL)
				}
			}
		})
	}
}
