package encx

import (
	"net/http"
	"testing"
)

func TestHTTPRedirectDebugTarget(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://tech.en.cx/Administration/Games/LevelEditor.aspx?gid=1", nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		resp *http.Response
		want string
	}{
		{
			name: "relative location",
			resp: &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"/Login.aspx?return=%2f"}},
				Request:    req,
			},
			want: "https://tech.en.cx/Login.aspx?return=%2f",
		},
		{
			name: "absolute location",
			resp: &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"https://other.en.cx/home/"}},
				Request:    req,
			},
			want: "https://other.en.cx/home/",
		},
		{
			name: "refresh fallback",
			resp: &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Refresh": {"0;url=/home/"}},
				Request:    req,
			},
			want: "Refresh: 0;url=/home/",
		},
		{
			name: "missing location",
			resp: &http.Response{
				StatusCode: http.StatusFound,
				Request:    req,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpRedirectDebugTarget(req, tt.resp)
			if got != tt.want {
				t.Fatalf("httpRedirectDebugTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}
