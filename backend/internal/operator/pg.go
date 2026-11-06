package operator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrExists    = errors.New("already exists")
	ErrProtected = errors.New("protected")
	ErrDisabled  = errors.New("disabled")
	ErrBusy      = errors.New("busy")
	ErrBadName   = errors.New("invalid name")
)

var dbNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)

type Database struct {
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	SizeBytes int64  `json:"size_bytes"`
	Protected bool   `json:"protected"`
}

type NewDatabase struct {
	Name        string `json:"name"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DatabaseURL string `json:"database_url"`
}

type pg struct {
	pool *pgxpool.Pool
	cfg  *Config
}

func (p *pg) list(ctx context.Context) ([]Database, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT datname, pg_get_userbyid(datdba), pg_database_size(datname)
		  FROM pg_database
		 WHERE NOT datistemplate AND datname <> 'postgres'
		 ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	dbs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Database, error) {
		var d Database
		err := r.Scan(&d.Name, &d.Owner, &d.SizeBytes)
		return d, err
	})
	for i := range dbs {
		dbs[i].Protected = p.cfg.Protected[dbs[i].Name]
	}
	return dbs, err
}

func (p *pg) exists(ctx context.Context, name string) (bool, error) {
	var ok bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&ok)
	return ok, err
}

// exec runs a statement built server-side with format(), so identifiers and
// literals are quoted by Postgres itself.
func (p *pg) exec(ctx context.Context, tmpl string, args ...any) error {
	var stmt string
	if err := p.pool.QueryRow(ctx, "SELECT format($1, "+placeholders(len(args))+")", append([]any{tmpl}, args...)...).Scan(&stmt); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, stmt)
	return err
}

func placeholders(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("$%d::text", i+2)
	}
	return s
}

// create makes a database owned by a login of the same name that can only
// connect to that database.
func (p *pg) create(ctx context.Context, name string) (*NewDatabase, error) {
	if !p.cfg.ManagedDatabases {
		return nil, ErrDisabled
	}
	if !dbNameRE.MatchString(name) || p.cfg.Protected[name] {
		return nil, ErrBadName
	}
	if ok, err := p.exists(ctx, name); err != nil {
		return nil, err
	} else if ok {
		return nil, ErrExists
	}
	password, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	var roleExists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, name).Scan(&roleExists); err != nil {
		return nil, err
	}
	if roleExists {
		err = p.exec(ctx, "ALTER ROLE %I LOGIN PASSWORD %L", name, password)
	} else {
		err = p.exec(ctx, "CREATE ROLE %I LOGIN PASSWORD %L", name, password)
	}
	if err != nil {
		return nil, err
	}
	if err := p.exec(ctx, "CREATE DATABASE %I OWNER %I", name, name); err != nil {
		return nil, err
	}
	if err := p.exec(ctx, "REVOKE CONNECT, TEMPORARY ON DATABASE %I FROM PUBLIC", name); err != nil {
		return nil, err
	}
	if err := p.exec(ctx, "GRANT CONNECT, TEMPORARY ON DATABASE %I TO %I", name, name); err != nil {
		return nil, err
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(name, password),
		Host:     p.cfg.ModuleDBHost,
		Path:     "/" + name,
		RawQuery: "sslmode=disable",
	}
	return &NewDatabase{Name: name, Username: name, Password: password, DatabaseURL: u.String()}, nil
}

// drop disconnects every session, drops the database and, when the owner
// login carries the same name, that login too.
func (p *pg) drop(ctx context.Context, name string) error {
	var owner string
	var super bool
	err := p.pool.QueryRow(ctx, `
		SELECT r.rolname, r.rolsuper
		  FROM pg_database d JOIN pg_roles r ON r.oid = d.datdba
		 WHERE d.datname = $1`, name).Scan(&owner, &super)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := p.exec(ctx, "DROP DATABASE %I WITH (FORCE)", name); err != nil {
		return err
	}
	if owner == name && !super && !p.cfg.Protected[owner] {
		if err := p.exec(ctx, "DROP ROLE IF EXISTS %I", owner); err != nil {
			return fmt.Errorf("database dropped, but its login %q could not be removed: %w", owner, err)
		}
	}
	return nil
}

// dbURL is the admin connection pointed at another database.
func (p *pg) dbURL(name string) string {
	u, err := url.Parse(p.cfg.PGAdminURL)
	if err != nil {
		return p.cfg.PGAdminURL
	}
	u.Path = "/" + name
	return u.String()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
