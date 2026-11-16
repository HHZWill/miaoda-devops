package larkcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 用原生 Git 验证 helper 优先级和无凭据时的行为，而非只检查环境变量。
func TestGitEnvironmentCredentialIsolation(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	unwantedMarker := filepath.Join(dir, "unwanted-helper")
	unwanted := write("system-helper", "#!/bin/sh\nprintf called >> \"$UNWANTED_MARKER\"\n", 0700)
	helper := strconv.Quote("!" + shellQuote(unwanted))
	global := write("gitconfig", "[credential]\nhelper = "+helper+
		"\n[credential \"https://git.example/app\"]\nhelper = "+helper+"\n", 0600)
	binary := write("fake cli", `#!/bin/sh
[ "$1" = apps ] && [ "$2" = git-credential-helper ] || exit 1
[ "$3" = --app-id ] && [ "$4" = "$EXPECTED_APP" ] || exit 1
[ "$5" = --profile ] && [ "$6" = "$EXPECTED_PROFILE" ] || exit 1
[ "$7" = get ] || exit 1
printf '%s\n' "$6" >> "$CLI_MARKER"
# CLI 仅为匹配的仓库路径提供凭据，只有 host 时会返回空。
request="$(cat)"
case "$request" in
  *"path=app"*) ;;
  *) exit 0 ;;
esac
[ "$EMPTY_CREDENTIAL" = 1 ] && exit 0
printf 'username=test-user\npassword=test-credential\n\n'
`, 0700)
	t.Setenv("MIAODA_LARK_CLI", binary)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("UNWANTED_MARKER", unwantedMarker)
	t.Setenv("GIT_ASKPASS", unwanted)
	t.Setenv("SSH_ASKPASS", unwanted)
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GCM_INTERACTIVE", "always")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "!"+shellQuote(unwanted))
	t.Setenv("GIT_CONFIG_PARAMETERS", "'credential.helper="+unwanted+"'")

	client := NewClient(dir)
	for _, scope := range []string{"deployment", "packaging"} {
		t.Run(scope, func(t *testing.T) {
			for i, profile := range []string{"tenant-one", "tenant 'two' $(false)"} {
				app := "app_test_" + strconv.Itoa(i)
				t.Setenv("EXPECTED_APP", app)
				t.Setenv("EXPECTED_PROFILE", profile)
				marker := filepath.Join(dir, scope+"-cli-"+strconv.Itoa(i))
				t.Setenv("CLI_MARKER", marker)
				var env []string
				var err error
				if scope == "packaging" {
					work := t.TempDir()
					env, err = client.ScopedEnvironment(work, profile, app)
				} else {
					env, err = client.GitEnvironment(profile, app)
				}
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				cmd := exec.CommandContext(ctx, "git", "credential", "fill")
				cmd.Env, cmd.Dir = env, dir
				cmd.Stdin = strings.NewReader("protocol=https\nhost=git.example\npath=app\n\n")
				output, err := cmd.Output()
				cancel()
				if err != nil || !strings.Contains(string(output), "username=test-user") {
					t.Fatalf("任务专属 helper 未能提供凭据：%v", err)
				}
				invocations, err := os.ReadFile(marker)
				if err != nil || string(invocations) != profile+"\n" {
					t.Fatalf("Git 使用了错误账号或未调用 CLI helper：%v", err)
				}
				if _, err := os.Stat(unwantedMarker); !os.IsNotExist(err) {
					t.Fatal("Git 调用了继承的 helper 或图形 askpass")
				}

				// 凭据缺失时必须直接失败，不回退到钥匙串或交互询问。
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				cmd = exec.CommandContext(ctx, "git", "credential", "fill")
				cmd.Env, cmd.Dir = append(env, "EMPTY_CREDENTIAL=1"), dir
				cmd.Stdin = strings.NewReader("protocol=https\nhost=git.example\npath=app\n\n")
				_, err = cmd.Output()
				timedOut := ctx.Err() != nil
				cancel()
				if err == nil || timedOut {
					t.Fatal("缺少凭据时应立即失败")
				}
				if _, err := os.Stat(unwantedMarker); !os.IsNotExist(err) {
					t.Fatal("缺少凭据时触发了系统 helper 或密码弹窗")
				}
			}
		})
	}
}
