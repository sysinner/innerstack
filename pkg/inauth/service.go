// Copyright 2020 Eryx <evorui at gmail dot com>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inauth

type ServiceStatus struct {
	Code    string `json:"code,omitempty" toml:"code,omitempty"`
	Message string `json:"message,omitempty" toml:"message,omitempty"`
}

// type AuthLoginRequest struct {
// 	LoginToken string `json:"login_token" toml:"login_token"`
// }

// type AuthLoginResponse struct {
// 	Error         string        `json:"error,omitempty" toml:"error,omitempty"`
// 	AccessToken   string        `json:"access_token" toml:"access_token"`
// 	IdentityToken IdentityToken `json:"identity_token" toml:"identity_token"`
// }

// type AuthRequest struct {
// 	AppID        string `json:"app_id" toml:"app_id"`
// 	AccessToken  string `json:"access_token,omitempty" toml:"access_token,omitempty"`
// 	RedirectURI  string `json:"redirect_uri" toml:"redirect_uri"`
// 	State        string `json:"state" toml:"state"`         // 防 CSRF 随机串
// 	ResponseType string `json:"response_type" toml:"response_type"` // 固定为 "code"
// }

// type AuthTokenResponse struct {
// 	Status        ServiceStatus  `json:"status,omitempty" toml:"status,omitempty"`
// 	AccessToken   string         `json:"access_token" toml:"access_token"`  // 访问令牌（可以是 JWT 或 随机不透明字符串）
// 	RefreshToken  string         `json:"refresh_token" toml:"refresh_token"` // 刷新令牌
// 	IdentityToken *IdentityToken `json:"identity_token" toml:"identity_token"`
// }

func NewServiceStatus(code, message string) ServiceStatus {
	return ServiceStatus{
		Code:    code,
		Message: message,
	}
}
