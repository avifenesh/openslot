package inboxworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeProcessBoundary(t *testing.T) {
	t.Setenv("OPENSLOT_API_KEY", "workspace-secret-must-not-leak")
	t.Setenv("OPENSLOT_WEBHOOK_TOKEN", "webhook-secret-must-not-leak")
	t.Setenv("CALNODE_ENCRYPTION_KEY", "database-secret-must-not-leak")
	for _, engine := range []string{"codex", "claude"} {
		t.Run(engine, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "agent")
			script := `#!/bin/sh
[ -z "$OPENSLOT_API_KEY$OPENSLOT_WEBHOOK_TOKEN$CALNODE_ENCRYPTION_KEY" ] || exit 20
[ "$OPENSLOT_TASK_TOKEN" = "task-token" ] || exit 21
[ "$(cat)" = "Only the inbox tools." ] || exit 22
case "$*" in
 *--strict-mcp-config*) case "$*" in *--restricted*) exit 0;; esac;;
 *--ignore-user-config*) case "$*" in *features.shell_tool=false*) exit 0;; esac;;
esac
exit 23
`
			if e := os.WriteFile(binary, []byte(script), 0700); e != nil {
				t.Fatal(e)
			}
			if e := runCLI(context.Background(), Config{Engine: engine, Binary: binary}, Task{Token: "task-token", MCPURL: "http://127.0.0.1/mcp/inbox", Instruction: "Only the inbox tools."}); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestRuntimeNoTaskTokenInArgumentsAndNoBroadTools(t *testing.T) {
	for _, engine := range []string{"codex", "claude"} {
		args := strings.Join(runtimeArgs(Config{Engine: engine}, Task{Token: "sensitive-token", MCPURL: "https://scheduler.example/mcp/inbox", Instruction: "Use MCP"}, "config.json"), " ")
		if strings.Contains(args, "sensitive-token") {
			t.Fatal("token leaked to command line")
		}
		if engine == "codex" && !strings.Contains(args, `default_tools_approval_mode="approve"`) {
			t.Fatal("task MCP must execute the user-authorized automatic workflow")
		}
		if strings.Contains(args, "dangerously") {
			t.Fatal("agent sandbox bypassed")
		}
	}
}
func TestEndpointCredentialTransport(t *testing.T) {
	for _, endpoint := range []string{"http://remote.example/agent", "https://user:secret@example.com/agent", "https://example.com/agent?key=secret", "file:///etc/passwd"} {
		if validateURL(endpoint) == nil {
			t.Fatal("accepted unsafe endpoint", endpoint)
		}
	}
	for _, endpoint := range []string{"https://example.com/agent", "http://127.0.0.1:8788", "http://localhost:1234"} {
		if e := validateURL(endpoint); e != nil {
			t.Fatal(e)
		}
	}
}
