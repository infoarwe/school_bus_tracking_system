// Command admin holds one-off operator commands. It works in every environment,
// including production (unlike cmd/seed).
//
//	go run ./cmd/admin create-super-admin --email owner@school.org --name "Owner"
//	docker compose run --rm -it api /app/admin create-super-admin --email ... --name ...
//
// The password is never a flag (it would end up in shell history and process
// lists): it is typed at a hidden prompt, twice, or read from the first line of
// standard input when that is not a terminal.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/bootstrap"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/config"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/database"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

const usage = `usage: admin create-super-admin --email EMAIL --name NAME

Creates the first Super Admin on a database that has none (bootstrap). Refuses if
any Super Admin already exists or the email is in use. The password is asked for
at a hidden prompt (or read from the first line of stdin when it is not a terminal).
With REQUIRE_SUPER_ADMIN_2FA (always on in production) the new admin must set up
an authenticator app at first login.`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "create-super-admin" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("create-super-admin", flag.ContinueOnError)
	email := fs.String("email", "", "login email of the new Super Admin")
	name := fs.String("name", "", "display name")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" || *name == "" || fs.NArg() > 0 {
		return errors.New(usage)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	password, err := readPassword(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := database.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	u, err := bootstrap.CreateFirstSuperAdmin(ctx, store.New(pool), *name, *email, password)
	if err != nil {
		return err
	}
	fmt.Printf("Created Super Admin %s (%s), id %s. Log in to the admin web", *u.Email, u.Name, u.ID)
	if cfg.RequireSuperAdmin2FA || cfg.IsProduction() { // always forced in production
		fmt.Print(" and set up two-factor authentication when asked")
	}
	fmt.Println(".")
	return nil
}

// readPassword asks twice at a hidden prompt on a terminal; otherwise it reads
// the first line of in (for scripted use, e.g. from a secret file).
func readPassword(in *os.File, prompt io.Writer) (string, error) {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		return readLine(in)
	}
	fmt.Fprint(prompt, "Password: ")
	p1, err := term.ReadPassword(fd)
	fmt.Fprintln(prompt)
	if err != nil {
		return "", err
	}
	fmt.Fprint(prompt, "Repeat password: ")
	p2, err := term.ReadPassword(fd)
	fmt.Fprintln(prompt)
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", errors.New("the passwords do not match")
	}
	return string(p1), nil
}

func readLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("no password given on standard input")
	}
	return line, nil
}
