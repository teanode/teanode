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

func TestResolvedTransport(t *testing.T) {
	http := MCPServerTransportHTTP
	stdio := MCPServerTransportStdio
	empty := MCPServerTransport("")
	command := "my-server"
	url := "https://example.com/mcp"
	emptyString := ""

	cases := []struct {
		name   string
		server *MCPServerConfiguration
		want   MCPServerTransport
	}{
		{"nil server", nil, MCPServerTransportHTTP},
		{"empty defaults to http", &MCPServerConfiguration{}, MCPServerTransportHTTP},
		{"url only is http", &MCPServerConfiguration{URL: &url}, MCPServerTransportHTTP},
		{"explicit http", &MCPServerConfiguration{Transport: &http, Command: &command}, MCPServerTransportHTTP},
		{"explicit stdio", &MCPServerConfiguration{Transport: &stdio, URL: &url}, MCPServerTransportStdio},
		{"infer stdio from command", &MCPServerConfiguration{Command: &command}, MCPServerTransportStdio},
		{"command and url stays http", &MCPServerConfiguration{Command: &command, URL: &url}, MCPServerTransportHTTP},
		{"empty command is not stdio", &MCPServerConfiguration{Command: &emptyString}, MCPServerTransportHTTP},
		{"empty transport falls through to inference", &MCPServerConfiguration{Transport: &empty, Command: &command}, MCPServerTransportStdio},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.server.ResolvedTransport(); got != testCase.want {
				t.Errorf("ResolvedTransport() = %q, want %q", got, testCase.want)
			}
		})
	}
}
