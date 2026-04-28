package creds

import "testing"

func TestRedactStripsAuthAndTokens(t *testing.T) {
	cases := []struct {
		key, val, want string
	}{
		{"Authorization", "Bearer xyz", "***"},
		{"authorization", "Bearer xyz", "***"},
		{"device_token", "tok-abc", "***"},
		{"pairing_token", "jwt.payload.sig", "***"},
		{"DEVICE_TOKEN", "tok-abc", "***"},
		{"some_token_field", "secret", "***"},
		{"hostname", "mahits-mac", "mahits-mac"},
		{"os", "macos", "macos"},
	}
	for _, c := range cases {
		got := RedactField(c.key, c.val)
		if got != c.want {
			t.Errorf("RedactField(%q, %q) = %q want %q", c.key, c.val, got, c.want)
		}
	}
}
