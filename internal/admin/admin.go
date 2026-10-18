// Package admin implements the memory-admin command line.
package admin

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/config"
	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres/sqlcdb"
)

const usage = `uso: memory-admin <comando>

  migrate up | down N | down --all | version | force V
  project create --slug <slug> --name <nome> [--description <texto>]
  key create --project <slug> --name <nome> --scopes memories:read,memories:write [--expires-in 720h]
  key revoke --prefix <prefixo>
  key list --project <slug>

migrate usa MEMORY_MIGRATOR_DATABASE_URL; os demais comandos usam MEMORY_ADMIN_DATABASE_URL.`

// ErrUsage is returned for invalid command lines.
var ErrUsage = errors.New("comando inválido")

// Run executes the command in args. Command output goes to stdout; messages
// for the operator go to stderr.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, cfg config.Admin) error {
	c := cli{stdout: stdout, stderr: stderr, cfg: cfg}
	if len(args) < 2 {
		fmt.Fprintln(stderr, usage)
		return ErrUsage
	}
	switch args[0] {
	case "migrate":
		return c.migrate(args[1], args[2:])
	case "project":
		if args[1] == "create" {
			return c.projectCreate(ctx, args[2:])
		}
	case "key":
		switch args[1] {
		case "create":
			return c.keyCreate(ctx, args[2:])
		case "revoke":
			return c.keyRevoke(ctx, args[2:])
		case "list":
			return c.keyList(ctx, args[2:])
		}
	}
	fmt.Fprintln(stderr, usage)
	return ErrUsage
}

type cli struct {
	stdout, stderr io.Writer
	cfg            config.Admin
}

func (c cli) migrate(sub string, args []string) (err error) {
	if c.cfg.MigratorDatabaseURL == "" {
		return errors.New("MEMORY_MIGRATOR_DATABASE_URL não definida")
	}
	m, err := postgres.NewMigrator(c.cfg.MigratorDatabaseURL)
	if err != nil {
		return err
	}
	defer func() {
		srcErr, dbErr := m.Close()
		err = errors.Join(err, srcErr, dbErr)
	}()

	switch sub {
	case "up":
		err = ignoreNoChange(m.Up())
	case "down":
		fs := flag.NewFlagSet("migrate down", flag.ContinueOnError)
		fs.SetOutput(c.stderr)
		all := fs.Bool("all", false, "reverte todas as migrações")
		if err := fs.Parse(args); err != nil {
			return err
		}
		switch {
		case *all && fs.NArg() == 0:
			err = ignoreNoChange(m.Down())
		case !*all && fs.NArg() == 1:
			n, convErr := strconv.Atoi(fs.Arg(0))
			if convErr != nil || n <= 0 {
				return fmt.Errorf("%w: N deve ser um inteiro positivo", ErrUsage)
			}
			err = ignoreNoChange(m.Steps(-n))
		default:
			return fmt.Errorf("%w: use \"migrate down N\" ou \"migrate down --all\"", ErrUsage)
		}
	case "version":
	case "force":
		if len(args) != 1 {
			return fmt.Errorf("%w: use \"migrate force V\"", ErrUsage)
		}
		v, convErr := strconv.Atoi(args[0])
		if convErr != nil {
			return fmt.Errorf("%w: V deve ser um inteiro", ErrUsage)
		}
		err = m.Force(v)
	default:
		return fmt.Errorf("%w: subcomando de migrate desconhecido %q", ErrUsage, sub)
	}
	if err != nil {
		return err
	}
	return c.printVersion(m)
}

func (c cli) printVersion(m *migrate.Migrate) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Fprintln(c.stdout, "versão: nenhuma migração aplicada")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "versão: %d dirty: %t\n", v, dirty)
	return nil
}

func ignoreNoChange(err error) error {
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

func (c cli) queries(ctx context.Context) (*sqlcdb.Queries, func(), error) {
	if c.cfg.AdminDatabaseURL == "" {
		return nil, nil, errors.New("MEMORY_ADMIN_DATABASE_URL não definida")
	}
	pool, err := pgxpool.New(ctx, c.cfg.AdminDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	return sqlcdb.New(pool), pool.Close, nil
}

func (c cli) projectCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("project create", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	slug := fs.String("slug", "", "identificador do projeto")
	name := fs.String("name", "", "nome do projeto")
	description := fs.String("description", "", "descrição opcional")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" || *name == "" {
		return fmt.Errorf("%w: --slug e --name são obrigatórios", ErrUsage)
	}
	q, closeFn, err := c.queries(ctx)
	if err != nil {
		return err
	}
	defer closeFn()

	var desc *string
	if *description != "" {
		desc = description
	}
	p, err := q.InsertProject(ctx, sqlcdb.InsertProjectParams{Slug: *slug, Name: *name, Description: desc})
	if err != nil {
		return fmt.Errorf("criar projeto: %w", postgres.MapError(err))
	}
	fmt.Fprintf(c.stdout, "projeto criado: %s (%s)\n", p.Slug, p.ID)
	return nil
}

func (c cli) keyCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("key create", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	project := fs.String("project", "", "slug do projeto")
	name := fs.String("name", "", "nome da chave (ex.: aplicação que a usa)")
	scopesCSV := fs.String("scopes", "", "escopos separados por vírgula")
	expiresIn := fs.Duration("expires-in", 0, "validade (ex.: 720h); vazio = sem expiração")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" || *name == "" {
		return fmt.Errorf("%w: --project, --name e --scopes são obrigatórios", ErrUsage)
	}
	scopes, err := domain.ParseScopes(*scopesCSV)
	if err != nil {
		return err
	}
	var expiresAt *time.Time
	if *expiresIn > 0 {
		t := time.Now().Add(*expiresIn)
		expiresAt = &t
	}

	q, closeFn, err := c.queries(ctx)
	if err != nil {
		return err
	}
	defer closeFn()

	p, err := q.GetProjectBySlug(ctx, *project)
	if err != nil {
		return fmt.Errorf("projeto %q: %w", *project, postgres.MapError(err))
	}
	key, err := auth.GenerateKey()
	if err != nil {
		return err
	}
	scopeStrings := make([]string, len(scopes))
	for i, s := range scopes {
		scopeStrings[i] = string(s)
	}
	row, err := q.InsertAPIKey(ctx, sqlcdb.InsertAPIKeyParams{
		ProjectID:  p.ID,
		Prefix:     key.Prefix,
		SecretHash: key.SecretHash,
		Name:       *name,
		Scopes:     scopeStrings,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		return fmt.Errorf("criar chave: %w", postgres.MapError(err))
	}
	fmt.Fprintf(c.stderr, "chave %q criada para o projeto %s (prefixo %s, escopos %s).\n",
		row.Name, p.Slug, row.Prefix, strings.Join(row.Scopes, ","))
	fmt.Fprintln(c.stderr, "Guarde a chave abaixo agora: ela não será exibida novamente.")
	fmt.Fprintln(c.stdout, key.Token)
	return nil
}

func (c cli) keyRevoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("key revoke", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	prefix := fs.String("prefix", "", "prefixo da chave")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prefix == "" {
		return fmt.Errorf("%w: --prefix é obrigatório", ErrUsage)
	}
	q, closeFn, err := c.queries(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	n, err := q.RevokeAPIKey(ctx, *prefix)
	if err != nil {
		return postgres.MapError(err)
	}
	if n == 0 {
		return fmt.Errorf("chave %q não encontrada ou já revogada", *prefix)
	}
	fmt.Fprintf(c.stdout, "chave %s revogada\n", *prefix)
	return nil
}

func (c cli) keyList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("key list", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	project := fs.String("project", "", "slug do projeto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" {
		return fmt.Errorf("%w: --project é obrigatório", ErrUsage)
	}
	q, closeFn, err := c.queries(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	rows, err := q.ListAPIKeys(ctx, *project)
	if err != nil {
		return postgres.MapError(err)
	}
	tw := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PREFIXO\tNOME\tESCOPOS\tSTATUS\tCRIADA\tÚLTIMO USO")
	now := time.Now()
	for _, r := range rows {
		status := "ativa"
		switch {
		case r.RevokedAt != nil:
			status = "revogada"
		case r.ExpiresAt != nil && !r.ExpiresAt.After(now):
			status = "expirada"
		}
		lastUsed := "-"
		if r.LastUsedAt != nil {
			lastUsed = r.LastUsedAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Prefix, r.Name, strings.Join(r.Scopes, ","),
			status, r.CreatedAt.UTC().Format(time.RFC3339), lastUsed)
	}
	return tw.Flush()
}
