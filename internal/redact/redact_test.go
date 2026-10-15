package redact

import "testing"

func TestTextRedactsHighConfidenceCredentials(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"OPENAI_API_KEY=sk-proj-Fx7Qm2Lp9Rt4Vw8Yz1Bc3Dn5Gh6Jk0Ms", "OPENAI_API_KEY=[REDACTED:openai_key]"},
		{"key sk-ant-api03-Fx7Qm2Lp9Rt4Vw8Yz1Bc3Dn5Gh6Jk0Ms end", "key [REDACTED:anthropic_key] end"},
		{"stripe sk_live_51Hx7Qm2Lp9Rt4Vw8Yz", "stripe [REDACTED:stripe_key]"},
		{"token ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789", "token [REDACTED:github_token]"},
		{"github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuv", "[REDACTED:github_token]"},
		{"SLACK=xoxb-123456789012-abcdefghij", "SLACK=[REDACTED:slack_token]"},
		{"post https://hooks.slack.com/services/T000/B000/XXXXXXXX now", "post [REDACTED:slack_webhook] now"},
		{"maps AIzaSyA1234567890abcdefghijklmnopqrstuv", "maps [REDACTED:google_key]"},
		{"oauth ya29.a0AfH6SMBx1234567890abcdef", "oauth [REDACTED:google_token]"},
		{"id AKIAIOSFODNN7EXAMPLE", "id [REDACTED:aws_key]"},
		{"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "aws_secret_access_key = [REDACTED:aws_secret]"},
		{"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N", "jwt [REDACTED:jwt]"},
		{"Authorization: Basic dXNlcjpwYXNzd29yZA==", "Authorization: Basic [REDACTED:authorization]"},
		{"use bearer abcdefghijklmnop1234 here", "use bearer [REDACTED:bearer_token] here"},
		{"HTTP/1.1 200\nSet-Cookie: session=abc123; HttpOnly\nok", "HTTP/1.1 200\nSet-Cookie: [REDACTED:cookie]\nok"},
		{"DATABASE_URL=postgres://acorn:s3cr3t!@db.internal:5432/acorn", "DATABASE_URL=postgres://acorn:[REDACTED:password]@db.internal:5432/acorn"},
		{"redis://:hunter22@cache:6379", "redis://:[REDACTED:password]@cache:6379"},
		{`{"password": "hunter22"}`, `{"password": "[REDACTED:password]"}`},
		{"路由器密码是 Tp-Link2026", "路由器密码是 [REDACTED:password]"},
		{"Your verification code is 482913.", "Your verification code is [REDACTED:otp]."},
		{"OTP: 4821", "OTP: [REDACTED:otp]"},
		{"【招商银行】验证码：482913，5分钟内有效", "【招商银行】验证码：[REDACTED:otp]，5分钟内有效"},
		{"验证码是 482913", "验证码是 [REDACTED:otp]"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----\nnext", "[REDACTED:private_key]\nnext"},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA", "[REDACTED:private_key]"},
	} {
		if got := Text(tc.in); got != tc.want {
			t.Errorf("Text(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
		if got := Text(tc.want); got != tc.want {
			t.Errorf("placeholder text changed: %q -> %q", tc.want, got)
		}
	}
}

func TestTextKeepsOrdinaryFacts(t *testing.T) {
	for _, in := range []string{
		"我不吃香菜，周三下午 3 点去医院复查",
		"订单号 20261015 已发货，快递单号 SF1234567890",
		"验证码 10 分钟内有效",
		"密码是什么来着",
		"请输入密码：",
		"password: ${DB_PASSWORD}",
		"risk-assessment-framework-document-v2",
		"the bearer of bad news",
		"https://github.com/ycvk/acorn/pull/12",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample owner@laptop",
		"-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE\n-----END PUBLIC KEY-----",
		"Authorization required",
	} {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) = %q", in, got)
		}
	}
}
