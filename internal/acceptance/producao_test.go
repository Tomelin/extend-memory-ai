//go:build integration

package acceptance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"

	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres"
	httptransport "github.com/Tomelin/extend-memory-ai/internal/transport/http"
)

// prodScenario runs features/producao.feature.
type prodScenario struct {
	*scenario
	seeded  map[string]uuid.UUID
	metrics http.Handler
}

func initProdScenario(sc *godog.ScenarioContext, e *env) {
	s := &prodScenario{scenario: &scenario{env: e, target: e.app.Handler, projects: map[string]uuid.UUID{}}}

	sc.Step(`^que a API está configurada com limite de (\d+) requisição por segundo e rajada de (\d+)$`, func(ctx context.Context, rps, burst int) error {
		pool, err := postgres.NewPool(ctx, s.db.AppURL, 2)
		if err != nil {
			return err
		}
		// Earlier suites restart the app, which replaces and shuts down the
		// global meter provider; this router gets its own registry.
		reg := prometheus.NewRegistry()
		exp, err := otelprom.New(otelprom.WithRegisterer(reg))
		if err != nil {
			return err
		}
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(exp)))
		s.metrics = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
		store := postgres.New(pool)
		authn, err := auth.NewAuthenticator(store, auth.Options{})
		if err != nil {
			return err
		}
		s.target, err = httptransport.NewRouter(httptransport.Deps{
			ServiceName: "memory-api", Logger: slog.New(slog.DiscardHandler), Health: store, Projects: store,
			Authenticator: authn, MaxBodyBytes: 1 << 20, RateLimitRPS: rps, RateLimitBurst: burst,
		})
		return err
	})
	sc.Step(`^que estou autenticado com uma chave válida$`, s.newProjectAndKey)
	sc.Step(`^envio (\d+) requisições seguidas com essa chave$`, func(n int) {
		for range n {
			s.get("/v1/project", s.token)
		}
	})
	sc.Step(`^a última resposta deve ter status (\d+) e código "([^"]+)"$`, func(status int, code string) error {
		if err := s.statusIs(status); err != nil {
			return err
		}
		if !strings.Contains(s.resp.Body.String(), `"code":"`+code+`"`) {
			return fmt.Errorf("body %s", s.resp.Body)
		}
		return nil
	})
	sc.Step(`^o cabeçalho "([^"]+)" deve estar presente$`, func(h string) error {
		if s.resp.Header().Get(h) == "" {
			return fmt.Errorf("header %s missing", h)
		}
		return nil
	})
	sc.Step(`^deve existir a métrica "([^"]+)"$`, func(name string) error {
		w := httptest.NewRecorder()
		s.metrics.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if !strings.Contains(w.Body.String(), name) {
			return fmt.Errorf("metric %s not found in:\n%s", name, grepLines(w.Body.String(), "ratelimit"))
		}
		return nil
	})

	sc.Step(`^executo "memory-admin bootstrap extensions" duas vezes$`, func() error {
		return errors.Join(s.cli("bootstrap", "extensions"), s.cli("bootstrap", "extensions"))
	})
	sc.Step(`^a saída deve informar as extensões "([^"]+)"$`, func(list string) error {
		if !strings.Contains(s.stdout, "extensões prontas: "+list) {
			return fmt.Errorf("output %q", s.stdout)
		}
		return nil
	})

	sc.Step(`^que existe uma memória ativa, uma expirada, uma excluída há 40 dias e uma excluída há 1 dia$`, s.seedPurge)
	sc.Step(`^executo "memory-admin purge --deleted-retention 720h"$`, func() error {
		return s.cli("purge", "--deleted-retention", "720h")
	})
	sc.Step(`^devem restar somente a memória ativa e a excluída há 1 dia$`, func(ctx context.Context) error {
		var remaining []string
		for name, id := range s.seeded {
			var n int
			if err := s.adminDB.QueryRow(ctx, "SELECT count(*) FROM memories WHERE id = $1", id).Scan(&n); err != nil {
				return err
			}
			if n == 1 {
				remaining = append(remaining, name)
			}
		}
		slices.Sort(remaining)
		if strings.Join(remaining, ",") != "ativa,recente" {
			return fmt.Errorf("remaining = %v", remaining)
		}
		return nil
	})
	sc.Step(`^a saída deve informar (\d+) memória expirada e (\d+) excluída removidas$`, func(expired, deleted int) error {
		// Other scenarios may leave expired rows behind, so only a lower bound is exact.
		var gotExpired, gotDeleted int
		if _, err := fmt.Sscanf(s.stdout, "memórias expiradas removidas: %d\nmemórias excluídas removidas: %d", &gotExpired, &gotDeleted); err != nil {
			return fmt.Errorf("output %q: %w", s.stdout, err)
		}
		if gotExpired < expired || gotDeleted < deleted {
			return fmt.Errorf("output %q", s.stdout)
		}
		return nil
	})
}

func (s *prodScenario) seedPurge(ctx context.Context) error {
	slug := "purge-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	var projectID, spaceID uuid.UUID
	if err := s.adminDB.QueryRow(ctx, "INSERT INTO projects (slug, name) VALUES ($1, $2) RETURNING id", slug, slug).Scan(&projectID); err != nil {
		return err
	}
	if err := s.adminDB.QueryRow(ctx,
		"INSERT INTO memory_spaces (project_id, external_id, name) VALUES ($1, 'u', 'u') RETURNING id", projectID).Scan(&spaceID); err != nil {
		return err
	}
	rows := map[string]string{
		"ativa":    "now() - interval '1 day', NULL, NULL",
		"expirada": "now() - interval '2 days', now() - interval '1 day', NULL",
		"antiga":   "now() - interval '50 days', NULL, now() - interval '40 days'",
		"recente":  "now() - interval '2 days', NULL, now() - interval '1 day'",
	}
	s.seeded = map[string]uuid.UUID{}
	for name, cols := range rows {
		var id uuid.UUID
		q := "INSERT INTO memories (project_id, space_id, content, occurred_at, expires_at, deleted_at) VALUES ($1, $2, $3, " + cols + ") RETURNING id"
		if err := s.adminDB.QueryRow(ctx, q, projectID, spaceID, "memória "+name).Scan(&id); err != nil {
			return err
		}
		s.seeded[name] = id
	}
	return nil
}

func grepLines(text, sub string) string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, sub) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
