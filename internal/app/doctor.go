package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/update"
	"github.com/angelmsger/wecom-calendar-cli/pkg/caldav"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

// updateCheckTimeout caps the release-update lookup so an offline or slow
// network never stalls `doctor` for the full request timeout.
const updateCheckTimeout = 5 * time.Second

// doctorCheck is a single diagnostic result.
type doctorCheck struct {
	Name          string `json:"name"`
	OK            bool   `json:"ok"`
	Status        string `json:"status"`
	Detail        string `json:"detail"`
	RecoveryScope string `json:"recovery_scope,omitempty"`
}

// doctorReport is the result shape for `doctor`.
type doctorReport struct {
	Healthy bool           `json:"healthy"`
	Checks  []doctorCheck  `json:"checks"`
	Update  *update.Status `json:"update,omitempty"`
}

func newDoctorCmd(s *appState) *cobra.Command {
	var skipUpdate bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, credentials and connectivity",
		Example: "  wecom-calendar-cli doctor\n" +
			"  wecom-calendar-cli doctor --no-update-check",
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := runDoctor(s, skipUpdate)
			if err := s.emit(report); err != nil {
				return err
			}
			if !report.Healthy {
				err := cerrors.New(cerrors.CategoryConfig, "DOCTOR_UNHEALTHY",
					"one or more diagnostic checks failed").
					WithNextSteps(
						"Review the failing checks and their status/recovery_scope fields above.",
						"When recovery_scope is host, retry `wecom-calendar-cli doctor` with access to the host user environment.",
						"Only configure credentials if the host retry also reports them missing.")
				if reportNeedsHostRetry(report) {
					err.WithRecovery(cerrors.Recovery{
						Action: "retry_current_command", Scope: "host",
						Requires: []string{"user_home", "os_keychain"},
					})
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&skipUpdate, "no-update-check", false,
		"skip the check for a newer wecom-calendar-cli release")
	return cmd
}

func runDoctor(s *appState, skipUpdate bool) doctorReport {
	var checks []doctorCheck
	cfg := s.cfg()

	// 1. Configuration.
	cfgOK := cfg.BaseURL != ""
	checks = append(checks, doctorCheck{
		Name: "configuration", OK: cfgOK, Status: pick(cfgOK, "ok", "missing"),
		Detail: pick(cfgOK, "server URL = "+cfg.BaseURL, "no server URL configured"),
	})

	// 2. Credentials.
	cred, credErr := auth.Resolve(cfg, s.resolved.Secrets, s.store)
	credOK := credErr == nil
	checks = append(checks, doctorCheck{
		Name: "credentials", OK: credOK,
		Status: diagnosticStatus(credErr), RecoveryScope: diagnosticRecoveryScope(credErr),
		Detail: pick(credOK, "scheme = "+cred.Scheme, detailOf(credErr)),
	})

	// 3. Connectivity (only when prerequisites pass).
	var (
		client    caldav.Client
		reachable bool
		doctorCtx context.Context
	)
	if cfgOK && credOK {
		ctx, cancel := cmdContext(s)
		defer cancel()
		doctorCtx = ctx
		c, err := caldav.Build(caldav.BuildParams{
			BaseURL:       cfg.BaseURL,
			AuthDecorator: cred.Decorator(),
			Timeout:       cfg.Defaults.Timeout,
			MaxRetries:    cfg.Defaults.MaxRetries,
		})
		if err != nil {
			checks = append(checks, doctorCheck{Name: "connectivity", OK: false, Status: diagnosticStatus(err), Detail: detailOf(err)})
		} else {
			pingErr := c.Ping(ctx)
			reachable = pingErr == nil
			client = c
			checks = append(checks, doctorCheck{
				Name: "connectivity", OK: reachable, Status: diagnosticStatus(pingErr),
				Detail: pick(reachable, "reachable at "+cfg.BaseURL, detailOf(pingErr)),
			})
		}
	} else {
		checks = append(checks, doctorCheck{
			Name: "connectivity", OK: false, Status: "skipped",
			Detail: "skipped: fix configuration and credentials first",
		})
	}

	healthy := true
	for _, c := range checks {
		if !c.OK {
			healthy = false
		}
	}

	// 4. Calendars — informational only. A failure here does not affect the
	// healthy verdict.
	if client != nil && reachable {
		cals, err := client.ListCalendars(doctorCtx)
		checks = append(checks, doctorCheck{
			Name: "calendars", OK: err == nil, Status: diagnosticStatus(err),
			Detail: pick(err == nil, fmt.Sprintf("%d calendars visible", len(cals)), detailOf(err)),
		})
	}

	report := doctorReport{Healthy: healthy, Checks: checks}

	if !skipUpdate {
		ctx, cancel := updateContext(s)
		defer cancel()
		st := update.Check(ctx, &http.Client{Timeout: updateCheckTimeout}, constants.Version)
		report.Update = &st
	}
	return report
}

func diagnosticStatus(err error) string {
	if err == nil {
		return "ok"
	}
	ce := cerrors.AsCLIError(err)
	switch ce.Code {
	case "CREDENTIAL_STORE_INACCESSIBLE":
		return "inaccessible"
	case "CREDENTIAL_NOT_VISIBLE_OR_MISSING":
		return "missing_or_inaccessible"
	}
	switch ce.Category {
	case cerrors.CategoryAuth, cerrors.CategoryPermission:
		return "rejected_by_server"
	case cerrors.CategoryNetwork, cerrors.CategoryServer:
		return "unreachable"
	default:
		return "invalid"
	}
}

func diagnosticRecoveryScope(err error) string {
	if err == nil {
		return ""
	}
	if recovery := cerrors.AsCLIError(err).Recovery; recovery != nil {
		return recovery.Scope
	}
	return ""
}

func reportNeedsHostRetry(report doctorReport) bool {
	for _, check := range report.Checks {
		if check.RecoveryScope == "host" {
			return true
		}
	}
	return false
}

// updateContext bounds the release-update lookup by updateCheckTimeout, or the
// configured request timeout when that is shorter.
func updateContext(s *appState) (context.Context, context.CancelFunc) {
	d := updateCheckTimeout
	if t := s.timeout(); t > 0 && t < d {
		d = t
	}
	return context.WithTimeout(context.Background(), d)
}

func pick(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func detailOf(err error) string {
	if err == nil {
		return ""
	}
	return cerrors.AsCLIError(err).Message
}
