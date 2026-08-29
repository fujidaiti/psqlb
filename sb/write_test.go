package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// insert: INSERT INTO table [(columns)] {VALUES (...) [, ...] | query}
//
//	[ON CONFLICT ...] [RETURNING ...]
func TestInsert(t *testing.T) {
	run(t, []gcase{
		{
			name: "one row",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a", "b")),
			want: "INSERT INTO users VALUES ($1, $2)",
			args: []any{"a", "b"},
		},
		{
			name: "named columns and several rows",
			stmt: stmt(INSERT, INTO, Users, sb.P(sb.I("name"), sb.I("email")),
				VALUES, sb.P("a", "b"), sb.P("c", DEFAULT)),
			want: "INSERT INTO users (name, email) VALUES ($1, $2), ($3, DEFAULT)",
			args: []any{"a", "b", "c"},
		},
		{
			// The query is not parenthesised, so it is written in place.
			name: "from a query",
			stmt: stmt(INSERT, INTO, Users, sb.P(sb.I("id")),
				SELECT, OrdersUserID, FROM, Orders),
			want: "INSERT INTO users (id) SELECT orders.user_id FROM orders",
		},
		{
			name: "DO NOTHING",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a"),
				ON, CONFLICT, DO, NOTHING),
			want: "INSERT INTO users VALUES ($1) ON CONFLICT DO NOTHING",
			args: []any{"a"},
		},
		{
			// excluded.name is an ordinary qualified name. SET strips the
			// qualifier from the target on the left of the "=" only, so the
			// expression on the right keeps its own.
			name: "DO UPDATE with a condition",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a"),
				ON, CONFLICT, sb.P(sb.I("email")),
				DO, UPDATE, SET, UsersName, "=", sb.I("excluded.name"),
				WHERE, UsersStatus, "<>", "banned",
				RETURNING, UsersID, AS, sb.I("i")),
			want: "INSERT INTO users VALUES ($1)" +
				" ON CONFLICT (email)" +
				" DO UPDATE SET name = excluded.name" +
				" WHERE users.status <> $2" +
				" RETURNING users.id AS i",
			args: []any{"a", "banned"},
		},
	})
}

// update: UPDATE table [AS alias] SET ... [FROM ...] [WHERE ...] [RETURNING ...]
func TestUpdate(t *testing.T) {
	run(t, []gcase{
		{
			name: "several assignments",
			stmt: stmt(UPDATE, Users, SET,
				UsersStatus, "=", "vip",
				UsersName, "=", sb.F("lower", UsersName),
				WHERE, UsersID, "=", 1),
			want: "UPDATE users SET status = $1, name = lower(users.name) WHERE users.id = $2",
			args: []any{"vip", 1},
		},
		{
			// The multi-column form is supported, and its targets are
			// unqualified for the same reason the single-column form's are.
			name: "the multi-column form",
			stmt: stmt(UPDATE, Users, SET,
				sb.P(UsersName, UsersStatus), "=", sb.P("a", "b")),
			want: "UPDATE users SET (name, status) = ($1, $2)",
			args: []any{"a", "b"},
		},
		{
			name: "aliased table and RETURNING",
			stmt: stmt(UPDATE, Users, AS, sb.I("u"), SET, UsersStatus, "=", DEFAULT,
				RETURNING, "*"),
			want: "UPDATE users AS u SET status = DEFAULT RETURNING *",
		},
	})
}

// delete: DELETE FROM table [AS alias] [USING ...] [WHERE ...] [RETURNING ...]
func TestDelete(t *testing.T) {
	run(t, []gcase{
		{
			name: "with a condition",
			stmt: stmt(DELETE, FROM, Users, WHERE, UsersID, "=", 1),
			want: "DELETE FROM users WHERE users.id = $1",
			args: []any{1},
		},
		{
			name: "USING and RETURNING",
			stmt: stmt(DELETE, FROM, Users, USING, Orders,
				WHERE, OrdersUserID, "=", UsersID,
				RETURNING, UsersID),
			want: "DELETE FROM users USING orders" +
				" WHERE orders.user_id = users.id" +
				" RETURNING users.id",
		},
	})
}

// The write statements are checked the same way. SET is the one position where
// a name is rewritten, so it is also the one that has to say what it expects.
func TestWriteStatements(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "UPDATE without SET",
			stmt: stmt(UPDATE, Users, WHERE, UsersID, "=", 1),
			want: "UPDATE: expected keyword SET",
		},
		{
			name: "an assignment without =",
			stmt: stmt(UPDATE, Users, SET, UsersStatus, "vip"),
			want: `SET: expected operator "="`,
		},
		{
			name: "an assignment target that is not a name",
			stmt: stmt(UPDATE, Users, SET, 1, "=", 2),
			want: "SET: expected a column name written with sb.I",
		},
		{
			name: "DELETE without FROM",
			stmt: stmt(DELETE, Users),
			want: "DELETE: expected keyword FROM",
		},
		{
			name: "INSERT with neither VALUES nor a query",
			stmt: stmt(INSERT, INTO, Users, sb.P(sb.I("name"))),
			want: "INSERT: expected keyword VALUES or a query",
		},
		{
			name: "VALUES without a group",
			stmt: stmt(INSERT, INTO, Users, VALUES, "a", "b"),
			want: "a parenthesised row after VALUES is required",
		},
		{
			name: "ON CONFLICT without DO",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a"), ON, CONFLICT, NOTHING),
			want: "ON CONFLICT: expected keyword DO",
		},
		{
			name: "DO followed by neither NOTHING nor UPDATE",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a"), ON, CONFLICT, DO, SET),
			want: "ON CONFLICT: expected keyword NOTHING or UPDATE",
		},
	})
}

func TestOnConflictOnConstraintIsNotSupported(t *testing.T) {
	assertUnsupported(t,
		stmt(INSERT, INTO, Users, VALUES, sb.P(1), ON, CONFLICT, ON, sb.I("c")),
		"ON CONFLICT ON CONSTRAINT")
}
