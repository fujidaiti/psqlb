package sb_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fujidaiti/psqlb/sb"
)

// The shared test support: the names every test writes statements about, and the
// two table runners. Every test file in this package is external, so all of them
// are written the way a user writes a statement — the keywords by dot-import,
// everything else through sb — and none can reach an unexported name by accident.

const (
	Users          = sb.I("users")
	UsersID        = sb.I("users.id")
	UsersName      = sb.I("users.name")
	UsersEmail     = sb.I("users.email")
	UsersStatus    = sb.I("users.status")
	UsersAge       = sb.I("users.age")
	UsersCreated   = sb.I("users.created_at")
	UsersMeta      = sb.I("users.meta")
	UsersIsPaid    = sb.I("users.paid")
	UsersHasTicket = sb.I("users.has_ticket")
	UsersParentID  = sb.I("users.parent_id")

	Orders        = sb.I("orders")
	OrdersID      = sb.I("orders.id")
	OrdersUserID  = sb.I("orders.user_id")
	OrdersTotal   = sb.I("orders.total")
	OrdersCreated = sb.I("orders.created_at")
)

// stmt collects its arguments into a []any, so a test case can write stmt(...)
// instead of []any{...} at each call to assertSQL or assertErr.
func stmt(items ...any) []any { return items }

// assertSQL builds s and compares both halves of the result. Every argument
// bound by these examples is a comparable scalar, so == is enough, and it
// avoids the empty-versus-nil slice question.
func assertSQL(t *testing.T, stmt []any, wantSQL string, wantArgs ...any) {
	t.Helper()
	sql, args, err := sb.ToSQL(stmt...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sql != wantSQL {
		t.Errorf("sql:\n got: %s\nwant: %s", sql, wantSQL)
	}
	if len(args) != len(wantArgs) {
		t.Errorf("args:\n got: %#v\nwant: %#v", args, wantArgs)
		return
	}
	for i := range args {
		if args[i] != wantArgs[i] {
			t.Errorf("args[%d]:\n got: %#v\nwant: %#v", i, args[i], wantArgs[i])
		}
	}
}

// assertErr builds s and requires an error mentioning wantSubstr. Nothing in
// this package panics, so a panic here fails the test the same way.
func assertErr(t *testing.T, stmt []any, wantSubstr string) {
	t.Helper()
	sql, _, err := sb.ToSQL(stmt...)
	if err == nil {
		t.Fatalf("want error containing %q, got no error and sql %q", wantSubstr, sql)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error:\n got: %v\nwant substring: %s", err, wantSubstr)
	}
}

type gcase struct {
	name string
	stmt []any
	want string
	args []any
}

func run(t *testing.T, cases []gcase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertSQL(t, c.stmt, c.want, c.args...)
		})
	}
}

type ecase struct {
	name string
	stmt []any
	want string // a substring of the message
}

func runErrs(t *testing.T, cases []ecase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertErr(t, c.stmt, c.want)
		})
	}
}

// assertUnsupported requires the error to be an *sb.UnsupportedError naming the
// clause, so that a user can tell "I wrote this wrong" from "this package has not
// got there yet".
func assertUnsupported(t *testing.T, stmt []any, wantSubstr string) {
	t.Helper()
	_, _, err := sb.ToSQL(stmt...)
	var unsupported *sb.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("want *sb.UnsupportedError, got %v", err)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error:\n got: %v\nwant substring: %s", err, wantSubstr)
	}
}
