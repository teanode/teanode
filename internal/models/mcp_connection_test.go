package models

import "testing"

func TestResolvedAuthMode(t *testing.T) {
	static := MCPServerAuthStatic
	user := MCPServerAuthUser
	empty := MCPServerAuthMode("")
	bearer := "Bearer abc"
	emptyString := ""

	cases := []struct {
		name   string
		server *MCPServerConfiguration
		want   MCPServerAuthMode
	}{
		{"nil server", nil, MCPServerAuthNone},
		{"no auth, no authorization", &MCPServerConfiguration{}, MCPServerAuthNone},
		{"infer static from authorization", &MCPServerConfiguration{Authorization: &bearer}, MCPServerAuthStatic},
		{"empty authorization stays none", &MCPServerConfiguration{Authorization: &emptyString}, MCPServerAuthNone},
		{"explicit static", &MCPServerConfiguration{Auth: &static}, MCPServerAuthStatic},
		{"explicit user overrides authorization", &MCPServerConfiguration{Auth: &user, Authorization: &bearer}, MCPServerAuthUser},
		{"empty auth falls through to inference", &MCPServerConfiguration{Auth: &empty, Authorization: &bearer}, MCPServerAuthStatic},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.server.ResolvedAuthMode(); got != testCase.want {
				t.Errorf("ResolvedAuthMode() = %q, want %q", got, testCase.want)
			}
		})
	}
}
