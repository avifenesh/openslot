// Package inboxworker connects an existing agent runtime to the inbox task MCP.
// Provider credentials remain in that runtime's environment and auth store.
package inboxworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/calnode/calnode/internal/netutil"
)

type Config struct {
	URL, APIKey, Engine, Binary, Model, WebhookURL, WebhookToken string
	Once                                                         bool
}
type Task struct {
	ID          string `json:"id"`
	MCPURL      string `json:"mcp_url"`
	Token       string `json:"token"`
	Expires     int64  `json:"expires_at"`
	Instruction string `json:"instruction"`
}

func client() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: netutil.MetadataSafeTransport(slog.Default(), "agent endpoint blocked"), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func validateURL(value string) error {
	u, e := url.Parse(value)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("use an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1")) {
		return errors.New("HTTPS is required except for a local agent or server")
	}
	return nil
}
func Run(ctx context.Context, c Config) error {
	if e := validateURL(c.URL); e != nil {
		return e
	}
	if c.APIKey == "" {
		return errors.New("set OPENSLOT_API_KEY to an administrator's agent key")
	}
	if c.Engine != "codex" && c.Engine != "claude" && c.Engine != "webhook" {
		return errors.New("engine must be codex, claude or webhook")
	}
	if c.Engine == "webhook" {
		if e := validateURL(c.WebhookURL); e != nil {
			return e
		}
	}
	for {
		task, e := claim(ctx, c)
		if e != nil {
			if c.Once {
				return e
			}
			slog.Warn("agent worker cannot claim; retrying in 30 seconds")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
				continue
			}
		}
		if task != nil {
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			e = execute(runCtx, c, *task)
			cancel()
			if e != nil {
				slog.Warn("agent task did not complete", "request_id", task.ID, "error", e)
				if c.Once {
					return e
				}
			} else {
				slog.Info("agent task completed", "request_id", task.ID)
			}
		}
		if c.Once {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
func claim(ctx context.Context, c Config) (*Task, error) {
	data, _ := json.Marshal(map[string]string{"engine": c.Engine})
	r, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.URL, "/")+"/v1/inbox/claim", bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Authorization", "Bearer "+c.APIKey)
	r.Header.Set("Content-Type", "application/json")
	res, e := client().Do(r)
	if e != nil {
		return nil, errors.New("cannot reach OpenSlot")
	}
	defer res.Body.Close()
	if res.StatusCode == 204 {
		return nil, nil
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("OpenSlot claim returned HTTP %d", res.StatusCode)
	}
	var t Task
	if e = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&t); e != nil {
		return nil, e
	}
	if t.ID == "" || len(t.Token) != 64 {
		return nil, errors.New("invalid task response")
	}
	// Derive the MCP URL from the operator's configured server, never a returned
	// third-party URL. This also works behind a local tunnel or reverse proxy.
	t.MCPURL = strings.TrimRight(c.URL, "/") + "/mcp/inbox"
	return &t, nil
}
func execute(ctx context.Context, c Config, t Task) error {
	if c.Engine == "webhook" {
		if e := dispatch(ctx, c, t); e != nil {
			return e
		}
	} else {
		if e := runCLI(ctx, c, t); e != nil {
			return e
		}
	}
	for {
		status, e := taskStatus(ctx, c, t.ID)
		if e != nil {
			return e
		}
		switch status {
		case "sent", "previewed", "ignored":
			return nil
		case "uncertain", "sending":
			return errors.New("delivery needs inspection in OpenSlot activity")
		case "cancelled":
			return errors.New("request was cancelled")
		}
		if c.Engine != "webhook" {
			return errors.New("agent exited without completing an MCP action")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
func taskStatus(ctx context.Context, c Config, id string) (string, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.URL, "/")+"/v1/inbox/requests/"+url.PathEscape(id), nil)
	if e != nil {
		return "", e
	}
	r.Header.Set("Authorization", "Bearer "+c.APIKey)
	res, e := client().Do(r)
	if e != nil {
		return "", errors.New("could not check task receipt")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("task receipt returned HTTP %d", res.StatusCode)
	}
	var out struct {
		Status string `json:"status"`
	}
	e = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&out)
	return out.Status, e
}
func dispatch(ctx context.Context, c Config, t Task) error {
	data, _ := json.Marshal(t)
	r, e := http.NewRequestWithContext(ctx, "POST", c.WebhookURL, bytes.NewReader(data))
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", t.ID)
	if c.WebhookToken != "" {
		r.Header.Set("Authorization", "Bearer "+c.WebhookToken)
	}
	res, e := client().Do(r)
	if e != nil {
		return errors.New("agent webhook could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("agent webhook returned HTTP %d", res.StatusCode)
	}
	return nil
}
func runtimeArgs(c Config, t Task, configFile string) []string {
	if c.Engine == "claude" {
		args := []string{"--print", "--restricted", "--strict-mcp-config", "--mcp-config", configFile, "--tools", "", "--allowedTools", "mcp__openslot_inbox__*", "--permission-mode", "dontAsk", "--no-session-persistence", "--system-prompt", t.Instruction}
		if c.Model != "" {
			args = append(args, "--model", c.Model)
		}
		return args
	}
	args := []string{"exec", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "-c", "approval_policy=\"never\"", "-c", "project_doc_max_bytes=0", "-c", "web_search=\"disabled\""}
	for _, feature := range []string{"shell_tool", "unified_exec", "js_repl", "apply_patch_freeform", "apps", "plugins", "hooks", "multi_agent", "multi_agent_v2", "computer_use", "browser_use", "image_generation", "view_image", "memories"} {
		args = append(args, "-c", "features."+feature+"=false")
	}
	args = append(args, "-c", "features.skip_host_skill_discovery=true", "-c", "mcp_servers.openslot_inbox.url="+strconv.Quote(t.MCPURL), "-c", "mcp_servers.openslot_inbox.bearer_token_env_var=\"OPENSLOT_TASK_TOKEN\"", "-c", "mcp_servers.openslot_inbox.required=true", "-c", "mcp_servers.openslot_inbox.default_tools_approval_mode=\"approve\"", "-c", "mcp_servers.openslot_inbox.enabled_tools=[\"read_email_request\",\"find_email_slots\",\"send_slot_offer\",\"ignore_email_request\"]")
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	return append(args, "-")
}
func runtimeEnv(t Task) []string {
	env := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "OPENSLOT_") || strings.HasPrefix(key, "CALNODE_") {
			continue
		}
		env = append(env, value)
	}
	return append(env, "OPENSLOT_TASK_TOKEN="+t.Token)
}
func runCLI(ctx context.Context, c Config, t Task) error {
	cache, e := os.UserCacheDir()
	if e != nil {
		return e
	}
	base := filepath.Join(cache, "openslot", "agent-tasks")
	if e = os.MkdirAll(base, 0700); e != nil {
		return e
	}
	dir, e := os.MkdirTemp(base, "task-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	configFile := filepath.Join(dir, "mcp.json")
	conf := map[string]any{"mcpServers": map[string]any{"openslot_inbox": map[string]any{"type": "http", "url": t.MCPURL, "headers": map[string]string{"Authorization": "Bearer " + t.Token}}}}
	data, _ := json.Marshal(conf)
	if e = os.WriteFile(configFile, data, 0600); e != nil {
		return e
	}
	binary := c.Binary
	if binary == "" {
		binary = c.Engine
	}
	cmd := exec.CommandContext(ctx, binary, runtimeArgs(c, t, configFile)...)
	cmd.Dir = dir
	cmd.Env = runtimeEnv(t)
	cmd.Stdin = strings.NewReader(t.Instruction)
	// Never persist raw email, model reasoning, or tokens in the server/worker log.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 5 * time.Second
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("%s runtime failed; check its login and supported CLI version", c.Engine)
	}
	return nil
}
