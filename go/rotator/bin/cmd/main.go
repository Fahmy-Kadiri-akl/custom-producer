package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/akeylesslabs/custom-producer/go/rotator/internal/handler"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/registry"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/aerospike"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/ansible"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/argocd"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/azuredevops"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/cloudflare"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/confluent"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/datadog"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/echo"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/github"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/gitlab"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/grafana"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/jfrog"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/newrelic"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/okta"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/openobserve"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/pagerduty"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/sendgrid"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/servicenow"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/slack"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/targets/terraform"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().Timestamp().Caller().Logger()

	port := envOr("PORT", "8080")

	// Build registry with all supported targets
	reg := registry.New()

	// Test/validation
	reg.Register(echo.New())

	// Ansible AAP/AWX
	reg.Register(ansible.NewPasswordTarget())
	reg.Register(ansible.NewAPIKeyTarget())

	// Cloud & DevOps
	reg.Register(azuredevops.NewPATTarget())
	reg.Register(azuredevops.NewSPTokenTarget())
	reg.Register(github.New())
	reg.Register(gitlab.New())
	reg.Register(cloudflare.New())
	reg.Register(terraform.New())
	reg.Register(argocd.New())

	// Artifact registries
	reg.Register(jfrog.New())

	// Monitoring & observability
	reg.Register(datadog.New())
	reg.Register(grafana.New())
	reg.Register(openobserve.New())
	reg.Register(pagerduty.New())
	reg.Register(newrelic.New())

	// Communication & email
	reg.Register(slack.New())
	reg.Register(sendgrid.New())

	// Enterprise platforms
	reg.Register(confluent.New())
	reg.Register(servicenow.New())
	reg.Register(okta.New())

	// Databases
	reg.Register(aerospike.New())

	log.Info().
		Strs("targets", reg.Types()).
		Str("port", port).
		Msg("registered rotation targets")

	// Create handler
	h := handler.New(reg, handler.Config{
		AccessID: os.Getenv("AKEYLESS_ACCESS_ID"),
		ItemName: os.Getenv("AKEYLESS_ITEM_NAME"),
		SkipAuth: strings.EqualFold(os.Getenv("SKIP_AUTH"), "true"),
	})

	log.Info().Str("port", port).Msg("starting unified custom producer")
	if err := http.ListenAndServe(":"+port, h); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
