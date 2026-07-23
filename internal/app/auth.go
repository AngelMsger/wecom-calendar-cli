package app

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect and manage stored credentials",
	}
	cmd.AddCommand(newAuthStatusCmd(s), newAuthLoginCmd(s), newAuthLogoutCmd(s))
	return cmd
}

// authStatus is the result shape for `auth status`.
type authStatus struct {
	Server     string `json:"server"`
	Scheme     string `json:"scheme"`
	Username   string `json:"username,omitempty"`
	Configured bool   `json:"configured"`
	Secret     string `json:"secret,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

func newAuthStatusCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether a usable credential is configured",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			st := authStatus{
				Server: cfg.BaseURL,
				Scheme: cfg.Auth.Scheme, Username: cfg.Auth.Username,
			}
			cred, err := auth.Resolve(cfg, s.resolved.Secrets, s.store)
			if err != nil {
				st.Configured = false
				st.Detail = cerrors.AsCLIError(err).Message
			} else {
				st.Configured = true
				st.Secret = cred.Redacted().Secret
			}
			return s.emit(st)
		},
	}
}

func newAuthLoginCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Store a credential for the configured server",
		Long:  "Prompt for the app-specific password and store it securely. Run `config init` first if the server URL is not set.",
		Example: "  wecom-calendar-cli auth login\n" +
			"  wecom-calendar-cli --use-context personal auth login",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			if cfg.BaseURL == "" {
				return cerrors.New(cerrors.CategoryConfig, "NO_SERVER",
					"no server URL configured").
					WithNextSteps("wecom-calendar-cli config init")
			}
			if !stdinIsTTY() {
				return cerrors.New(cerrors.CategoryConfig, "AUTH_LOGIN_NEEDS_TTY",
					"auth login needs an interactive terminal to prompt for the password").
					WithHint("Run `wecom-calendar-cli auth login` yourself in a terminal, or provide credentials via environment variables (WECOM_CALENDAR_USERNAME + WECOM_CALENDAR_PASSWORD).")
			}
			r := bufio.NewReader(os.Stdin)
			cred := auth.Credential{Scheme: cfg.Auth.Scheme, Username: cfg.Auth.Username}
			if cred.Scheme == "" {
				cred.Scheme = auth.SchemeBasic
			}
			if cred.Username == "" {
				cred.Username = ask(r, "WeCom email")
			}
			fmt.Fprintln(os.Stderr, "Note: fetching a new password in WeCom invalidates the previous one.")
			secret, err := askSecret("App-specific password")
			if err != nil {
				return err
			}
			cred.Secret = secret
			if err := cred.Validate(); err != nil {
				return err
			}
			backend, err := auth.Save(cfg.BaseURL, cred, s.store)
			if err != nil {
				return err
			}
			return s.emit(map[string]any{
				"server":             cfg.BaseURL,
				"scheme":             cred.Scheme,
				"credential_backend": fmt.Sprint(backend),
				"status":             "stored",
			})
		},
	}
}

func newAuthLogoutCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored credential for the configured server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			if cfg.BaseURL == "" {
				return cerrors.New(cerrors.CategoryConfig, "NO_SERVER",
					"no server URL configured")
			}
			scheme := cfg.Auth.Scheme
			if scheme == "" {
				scheme = auth.SchemeBasic
			}
			if err := auth.Forget(cfg.BaseURL, scheme, s.store); err != nil {
				return err
			}
			return s.emit(map[string]any{"server": cfg.BaseURL, "status": "removed"})
		},
	}
}

func ask(r *bufio.Reader, label string) string {
	// Prompts are human interaction — write them to stderr so stdout stays
	// clean JSON.
	fmt.Fprintf(os.Stderr, "%s: ", label)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

// askSecret reads a secret without echoing it. auth login already requires an
// interactive stdin, so terminal input is expected; the prompt and the trailing
// newline go to stderr to keep stdout clean.
func askSecret(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", cerrors.Wrap(err, cerrors.CategoryConfig, "READ_SECRET",
			"could not read the password from the terminal")
	}
	return strings.TrimSpace(string(b)), nil
}
