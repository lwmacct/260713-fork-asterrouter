package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DingTalkConfig struct {
	Enabled                             bool
	ClientID, ClientSecret, RedirectURL string
}
type DingTalkProfile struct{ Subject, Email, DisplayName, Department string }
type DingTalkService struct {
	config DingTalkConfig
	state  *OIDCService
	client *http.Client
}

func NewDingTalkService(config DingTalkConfig) (*DingTalkService, error) {
	if config.Enabled && (strings.TrimSpace(config.ClientID) == "" || strings.TrimSpace(config.ClientSecret) == "" || strings.TrimSpace(config.RedirectURL) == "") {
		return nil, errors.New("DingTalk client id, client secret, and redirect URL are required")
	}
	state, _ := NewOIDCService(OIDCConfig{Enabled: config.Enabled, IssuerURL: "https://login.dingtalk.com", ClientID: config.ClientID, RedirectURL: config.RedirectURL})
	return &DingTalkService{config: config, state: state, client: http.DefaultClient}, nil
}

func (s *DingTalkService) Begin(now time.Time) (OIDCState, error) { return s.state.Begin(now) }
func (s *DingTalkService) AuthorizationURL(entry OIDCState) string {
	values := url.Values{"client_id": {s.config.ClientID}, "response_type": {"code"}, "scope": {"openid"}, "state": {entry.Value}, "redirect_uri": {s.config.RedirectURL}, "prompt": {"consent"}}
	return "https://login.dingtalk.com/oauth2/auth?" + values.Encode()
}

func (s *DingTalkService) Complete(ctx context.Context, state, code string, now time.Time) (DingTalkProfile, error) {
	if _, err := s.state.Consume(state, now); err != nil {
		return DingTalkProfile{}, err
	}
	var userToken struct {
		AccessToken string `json:"accessToken"`
		CorpID      string `json:"corpId"`
	}
	if err := s.postJSON(ctx, "https://api.dingtalk.com/v1.0/oauth2/userAccessToken", map[string]string{"clientId": s.config.ClientID, "clientSecret": s.config.ClientSecret, "code": code, "grantType": "authorization_code"}, &userToken); err != nil {
		return DingTalkProfile{}, err
	}
	var me struct {
		UnionID string `json:"unionId"`
		Nick    string `json:"nick"`
	}
	if err := s.getJSON(ctx, "https://api.dingtalk.com/v1.0/contact/users/me", userToken.AccessToken, &me); err != nil {
		return DingTalkProfile{}, err
	}
	if me.UnionID == "" {
		return DingTalkProfile{}, errors.New("DingTalk user has no unionId")
	}
	var appToken struct {
		AccessToken string `json:"accessToken"`
	}
	if err := s.postJSON(ctx, "https://api.dingtalk.com/v1.0/oauth2/accessToken", map[string]string{"appKey": s.config.ClientID, "appSecret": s.config.ClientSecret}, &appToken); err != nil {
		return DingTalkProfile{}, err
	}
	var byUnion struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Result  struct {
			UserID      string `json:"userid"`
			ContactType int    `json:"contact_type"`
		} `json:"result"`
	}
	endpoint := "https://oapi.dingtalk.com/topapi/user/getbyunionid?access_token=" + url.QueryEscape(appToken.AccessToken)
	if err := s.postJSON(ctx, endpoint, map[string]string{"unionid": me.UnionID}, &byUnion); err != nil {
		return DingTalkProfile{}, err
	}
	if byUnion.ErrCode != 0 || byUnion.Result.UserID == "" {
		return DingTalkProfile{}, fmt.Errorf("DingTalk union lookup failed: %s", byUnion.ErrMsg)
	}
	var staff struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Result  struct {
			Name, Email string
			DeptIDList  []int64 `json:"dept_id_list"`
		} `json:"result"`
	}
	endpoint = "https://oapi.dingtalk.com/topapi/v2/user/get?access_token=" + url.QueryEscape(appToken.AccessToken)
	if err := s.postJSON(ctx, endpoint, map[string]string{"userid": byUnion.Result.UserID, "language": "zh_CN"}, &staff); err != nil {
		return DingTalkProfile{}, err
	}
	if staff.ErrCode != 0 {
		return DingTalkProfile{}, fmt.Errorf("DingTalk staff lookup failed: %s", staff.ErrMsg)
	}
	email := strings.ToLower(strings.TrimSpace(staff.Result.Email))
	if email == "" {
		return DingTalkProfile{}, errors.New("DingTalk enterprise email is required")
	}
	name := strings.TrimSpace(staff.Result.Name)
	if name == "" {
		name = strings.TrimSpace(me.Nick)
	}
	department := ""
	if len(staff.Result.DeptIDList) > 0 {
		department = fmt.Sprintf("%d", staff.Result.DeptIDList[0])
	}
	return DingTalkProfile{Subject: me.UnionID, Email: email, DisplayName: name, Department: department}, nil
}

func (s *DingTalkService) getJSON(ctx context.Context, endpoint, token string, output any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("x-acs-dingtalk-access-token", token)
	return s.do(req, output)
}
func (s *DingTalkService) postJSON(ctx context.Context, endpoint string, input map[string]string, output any) error {
	payload, _ := json.Marshal(input)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return s.do(req, output)
}
func (s *DingTalkService) do(req *http.Request, output any) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("DingTalk returned HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(raw, output)
}
