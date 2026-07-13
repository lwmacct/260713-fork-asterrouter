package auth

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type dingTalkRoundTrip func(*http.Request) (*http.Response, error)

func (fn dingTalkRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestDingTalkCompleteMapsEnterpriseIdentity(t *testing.T) {
	service, err := NewDingTalkService(DingTalkConfig{Enabled: true, ClientID: "app", ClientSecret: "secret", RedirectURL: "https://router.test/api/v1/auth/dingtalk/callback"})
	if err != nil {
		t.Fatalf("NewDingTalkService() error = %v", err)
	}
	entry, err := service.Begin(time.Now().UTC())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	responses := []string{
		`{"accessToken":"user-token","corpId":"corp"}`,
		`{"unionId":"union-1","nick":"Nick"}`,
		`{"accessToken":"app-token","expireIn":7200}`,
		`{"errcode":0,"result":{"userid":"staff-1"}}`,
		`{"errcode":0,"result":{"name":"Enterprise User","email":"USER@EXAMPLE.COM","dept_id_list":[42]}}`,
	}
	index := 0
	service.client = &http.Client{Transport: dingTalkRoundTrip(func(request *http.Request) (*http.Response, error) {
		if index >= len(responses) {
			t.Fatalf("unexpected request %s", request.URL)
			return nil, nil
		}
		body := responses[index]
		index++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	profile, err := service.Complete(t.Context(), entry.Value, "authorization-code", time.Now().UTC())
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if profile.Subject != "union-1" || profile.Email != "user@example.com" || profile.DisplayName != "Enterprise User" || profile.Department != "42" {
		t.Fatalf("profile = %+v", profile)
	}
	if index != len(responses) {
		t.Fatalf("request count = %d, want %d", index, len(responses))
	}
}
