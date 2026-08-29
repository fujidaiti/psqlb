package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// with_query: WITH [RECURSIVE] name [(columns)] AS [[NOT] MATERIALIZED] (query)
func TestCTEs(t *testing.T) {
	body := sb.P(SELECT, UsersID, FROM, Users)
	run(t, []gcase{
		{
			name: "one query",
			stmt: stmt(WITH, sb.I("t"), AS, body, SELECT, "*", FROM, sb.I("t")),
			want: "WITH t AS (SELECT users.id FROM users) SELECT * FROM t",
		},
		{
			name: "several queries",
			stmt: stmt(WITH, sb.I("a"), AS, body, sb.I("b"), AS, body,
				SELECT, "*", FROM, sb.I("a")),
			want: "WITH a AS (SELECT users.id FROM users)," +
				" b AS (SELECT users.id FROM users)" +
				" SELECT * FROM a",
		},
		{
			name: "named columns and MATERIALIZED",
			stmt: stmt(WITH, sb.I("t"), sb.P(sb.I("id")), AS, MATERIALIZED, body,
				SELECT, "*", FROM, sb.I("t")),
			want: "WITH t (id) AS MATERIALIZED (SELECT users.id FROM users) SELECT * FROM t",
		},
		{
			name: "NOT MATERIALIZED",
			stmt: stmt(WITH, sb.I("t"), AS, NOT, MATERIALIZED, body, SELECT, "*", FROM, sb.I("t")),
			want: "WITH t AS NOT MATERIALIZED (SELECT users.id FROM users) SELECT * FROM t",
		},
		{
			name: "in front of a write statement",
			stmt: stmt(WITH, sb.I("t"), AS, body,
				DELETE, FROM, Users, WHERE, UsersID, IN, sb.P(SELECT, "*", FROM, sb.I("t"))),
			want: "WITH t AS (SELECT users.id FROM users)" +
				" DELETE FROM users WHERE users.id IN (SELECT * FROM t)",
		},
	})
}

// A CTE body is parenthesised in SQL, so it is written with sb.P like every other
// group, and the statement the queries are for has to follow them.
func TestCTEErrors(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "a CTE without AS",
			stmt: stmt(WITH, sb.I("t"), sb.P(SELECT, UsersID, FROM, Users), SELECT, "*"),
			want: "WITH: expected keyword AS",
		},
		{
			name: "a CTE body that is not a group",
			stmt: stmt(WITH, sb.I("t"), AS, SELECT, UsersID, FROM, Users),
			want: "a parenthesised query as the body is required",
		},
		{
			name: "WITH with no statement after it",
			stmt: stmt(WITH, sb.I("t"), AS, sb.P(SELECT, UsersID, FROM, Users)),
			want: "statement: expected keyword SELECT",
		},
	})
}
