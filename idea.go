// sqlbuilder — a thin SQL builder for Postgres (design in progress)
//
// The implementation works, but it does not cover the whole of SQL syntax.
// Feedback welcome.
//
// The implementation is this one file. The examples are in idea_test.go.
//
// The package is named `sql` because it is meant to be dot-imported by callers:
//
//	import . "github.com/awesomepackage/sql"
//
// `sql.SELECT(...)` would work too, but it is verbose. Instead every DSL
// function is uppercased, so it is obvious at a glance that a given identifier
// belongs to the SQL DSL.
//
// ---------------------------------------------------------------------------
// What this is
// ---------------------------------------------------------------------------
//
// A package for writing SQL in Go. It is not an ORM. What comes out is a SQL
// string plus a slice of arguments for the placeholders; executing it is left
// to pgx, database/sql, or whatever else you use.
//
//	sql, args := Stm(
//		SELECT(UsersID, UsersName),
//		FROM(Users),
//		WHERE(UsersStatus, EQ, Lit("active"), AND, UsersAge, GTE, Lit(18)),
//		ORDER_BY(Stm(UsersID, DESC)),
//		LIMIT(Lit(20)),
//	).ToSQL()
//
//	// SELECT users.id, users.name FROM users
//	// WHERE users.status = $1 AND users.age >= $2 ORDER BY users.id DESC LIMIT $3
//	// args=[active 18 20]
//
// ---------------------------------------------------------------------------
// Problems this aims to solve
// ---------------------------------------------------------------------------
//
//  1. Keeping table and column names in constants. Writing everything as string
//     literals scatters the edits a rename requires, and does nothing about
//     typos. But pulling the names out into constants tends to bury the shape
//     of the original SQL under `+` concatenation and fmt.Sprintf placeholders.
//
//  2. Building queries dynamically. "The WHERE clause grows or shrinks
//     depending on the API's query parameters" is a constant requirement.
//     Doing it with pure string manipulation is fiddly, and the $N numbering
//     breaks easily.
//
// ---------------------------------------------------------------------------
// Problems this does not solve
// ---------------------------------------------------------------------------
//
// These are deliberate omissions. The position taken is that raw SQL carries
// the same risks, so they are acceptable.
//
//   - Type safety — `WHERE users.name = 49` is not rejected statically. Doing
//     that with Go's type system, without code generation, is impractical.
//   - Syntactic correctness — a sequence like `SELECT(...), EQ, EQ` is
//     perfectly writable. Arity is not checked either. This is not a SQL parser.
//   - Sugar — no conveniences that do not exist in SQL. The goal is to write
//     SQL in Go, not to write SQL more briefly.
//
// ---------------------------------------------------------------------------
// The central idea
// ---------------------------------------------------------------------------
//
// Go values appear in the same order the SQL tokens do. That is the whole of
// it. The aim is that you never have to stop and ask "how do I spell that bit
// of SQL in Go again?"
//
//	Shape in SQL          Shape in Go              Example
//	--------------------  -----------------------  ------------------------------
//	Keyword with operands Function                 SELECT(...) FROM(...) LIMIT(...)
//	Keyword without       Constant                 AND OR NOT DESC JOIN UNION_ALL
//	Infix operator        Constant / Raw()         EQ GT LIKE / Raw("@>")
//	Enclosing             Stm(...) Row(...) FUNC()
//	Identifier            Id constant              UsersID
//	Literal               Lit()                    Lit(42), Lit("active")
//
// There are no methods for building expressions. Operators and keywords alike
// sit in the same flat token sequence. The reasons are that nesting stays
// shallow, and that no fixed list of operators has to be maintained.
//
// Columns are declared as constants holding qualified names.
//
//	const (
//		Users       = Id("users")
//		UsersID     = Id("users.id")
//		UsersStatus = Id("users.status")
//	)
//
// ---------------------------------------------------------------------------
// How parentheses are handled
// ---------------------------------------------------------------------------
//
// This is the single biggest design decision. SQL's parentheses are split into
// two kinds.
//
// Parentheses with no shorthand form — COUNT(x), IN (...), VALUES (...),
// DISTINCT ON (...) — map directly onto the parentheses of the Go call:
// FUNC / Row / IN / DISTINCT_ON.
//
// Parentheses that may or may not be present — grouping, FROM (SELECT ...) —
// are written with Stm, and whether they appear depends on where the Stm sits.
// A space-separated sequence is where SQL may need grouping, so a nested Stm is
// parenthesised there. An item of a comma-separated clause is a single
// expression, and SQL never puts parentheses around one, so a nested Stm is not
// parenthesised there.
//
//	WHERE(Stm(a, OR, b), AND, c)                      // WHERE (a OR b) AND c
//	WHERE(a, OR, b, AND, c)                           // WHERE a OR b AND c
//	ORDER_BY(Stm(UsersID, DESC), Stm(UsersName, ASC)) // ORDER BY users.id DESC, users.name ASC
//
// By SQL's precedence rules the second means a OR (b AND c). That is intended.
// The builder never guesses at precedence and inserts parentheses on your
// behalf. What you write is what comes out, so the generated SQL shows you the
// cause.
//
// One consequence is worth knowing. A subquery used as a bare item of a
// comma-separated clause needs one Stm around it, because that is what places
// it in a space-separated sequence.
//
//	SELECT(Stm(sub))          // SELECT (SELECT ...)
//	SELECT(Stm(sub, AS("n"))) // SELECT (SELECT ...) AS n
//	SELECT(sub)               // SELECT SELECT ...  <- wrong
//
// With an alias, which is the usual case, it costs nothing extra.
//
// AND / OR are binary operators, but since they are flat tokens, three or more
// operands are simply listed. If you want parentheses, spell them with Stm.
//
// ---------------------------------------------------------------------------
// Numbering the $N placeholders
// ---------------------------------------------------------------------------
//
// The design in which BuildSQL takes args and returns args is the entirety of
// the numbering scheme. There is no global counter, and no renumbering pass
// afterwards. Everything written in a statement is a Clause and is expanded as
// SQL. A value is not written directly; Lit binds it and produces its $N.
// Position has no say in that; it decides only whether a nested Stm is
// parenthesised.
//
//	UsersStatus, EQ, Lit("active")  // users.status = $1
//	OrdersUserID, EQ, UsersID       // orders.user_id = users.id
//
// Raw joins the same scheme. A fragment written by hand cannot know its own
// position, so it marks each value with "$0" and receives the real number when
// it is expanded.
//
//	Raw("meta->'profile'->>'city' = $0", "Tokyo")   // = $7, if six values came before
//
// Because args is handed along in expansion order, the numbering stays
// sequential no matter how deeply things nest. Statements are values, so they
// can be assembled without holding a binder and composed later.
//
// ---------------------------------------------------------------------------
// Why every position takes a Clause
// ---------------------------------------------------------------------------
//
// Nothing in the DSL takes `any`. Every operand of Stm, of a keyword function
// and of Row is a Clause, so a value has to be written as Lit(v). The reason is
// that `any` cannot distinguish a value from a mistake, and the two mistakes it
// admits are both silent.
//
//	SELECT("id", "name")        // would be SELECT $1, $2, binding "id" and "name"
//	Stm(SELECT, UsersID)        // would bind the SELECT function itself
//
// The first is the worse of the two. An identifier written as a string instead
// of an Id produces a query that Postgres accepts and runs, comparing two
// literals rather than a column, so it returns the wrong rows with no error
// anywhere. Requiring Lit makes both a compile error.
//
// The cost is that a value is three characters longer. The benefit is that
// there is exactly one way to write each of the three things a statement is
// made of: Id for an identifier, Lit for a value, and a keyword or clause for
// everything else.
//
// Two places still take `any`, and neither is ambiguous, because every argument
// they receive is a value: Lit itself, and the values Raw binds to its "$0"
// markers.
//
// ---------------------------------------------------------------------------
// Known limitations
// ---------------------------------------------------------------------------
//
//   - Raw performs no escaping whatsoever, so passing user input to it leads
//     directly to SQL injection. Pass only constants, or strings assembled in
//     code.
//   - Raw looks for "$0" without inspecting quoted regions, and rejects a "$"
//     followed by any other digits wherever it appears. Literal text of that
//     shape — a "$1" inside a dollar-quoted function body, say — therefore
//     cannot be written.
//   - Aliasing a table requires declaring a separate constant such as `t.id`.
//   - A subquery used as a bare item of a comma-separated clause needs one Stm
//     around it. Forgetting it produces wrong SQL rather than a compile error.
//   - Not yet started: verification against a real database, the WINDOW clause
//     and named windows, MERGE, GROUPING SETS / ROLLUP / CUBE.
//
// ---------------------------------------------------------------------------
// Verification status
// ---------------------------------------------------------------------------
//
// Builds under Go 1.22 and passes go vet and gofmt. The 16 examples in
// idea_test.go check the generated SQL and the bound arguments against the
// strings they are expected to produce, and further tests cover the edges of
// Raw; run `go test` to check them. Nothing has yet been run against a real
// database.
package sql

import (
	"fmt"
	"strings"
)

// ===========================================================================
// Implementation
// ===========================================================================

// ---------------------------------------------------------------------------
// Core
// ---------------------------------------------------------------------------

// Clause is a fragment of SQL. It takes args and returns the fragment plus
// "args with the values this fragment bound appended". Passing args along this
// bucket brigade is the entirety of the $N numbering scheme.
type Clause interface {
	BuildSQL(args []any) (string, []any)
}

// value is the only branch in this package. Every item is a Clause and is
// expanded as SQL. A nil item is the exception: it produces nothing, so that an
// optional item can be left in place.
//
// group says whether the enclosing sequence is space-separated. A Statement
// nested there is an operand, so it is parenthesised. An item of a
// comma-separated clause is a single expression, which SQL never parenthesises.
//
// Values are not items. They enter only through Lit and Raw, which bind them
// directly. That is what keeps a forgotten Id, SELECT("id"), and a keyword that
// was named but not called, Stm(SELECT, UsersID), from compiling.
func value(args []any, v Clause, group bool) (string, []any) {
	switch c := v.(type) {
	case nil:
		return "", args
	case Statement:
		sql, args := c.BuildSQL(args)
		if sql == "" || !group {
			return sql, args
		}
		return "(" + sql + ")", args
	}
	return v.BuildSQL(args)
}

// join concatenates with sep. sep is always comma or space, and it is what
// tells value() whether a nested Statement is an operand.
func join(args []any, sep string, vs []Clause) (string, []any) {
	parts := make([]string, 0, len(vs))
	var s string
	for _, v := range vs {
		s, args = value(args, v, sep == space)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, sep), args
}

// dup is generic because Raw holds values, []any, while everything else holds
// items, []Clause.
func dup[T any](vs []T) []T {
	cp := make([]T, len(vs))
	copy(cp, vs)
	return cp
}

// clauseFunc is a function adapter. It exists to avoid adding another
// exported type.
type clauseFunc func([]any) (string, []any)

func (f clauseFunc) BuildSQL(args []any) (string, []any) {
	if f == nil {
		return "", args
	}
	return f(args)
}

// ---------------------------------------------------------------------------
// Stm — space-separated concatenation
// ---------------------------------------------------------------------------

// Statement is a space-separated sequence of tokens.
type Statement struct{ items []Clause }

// Stm builds a space-separated group. It is the only way to write a statement.
// Nesting it inside another space-separated sequence adds parentheses; nesting
// it as an item of a comma-separated clause does not.
//
//	Stm(a, Stm(b, c))            -> "a (b c)"
//	ORDER_BY(Stm(UsersID, DESC)) -> "ORDER BY users.id DESC"
//
// Subqueries, grouped conditions and multi-token items of a comma-separated
// clause are all spelled the same way.
//
// Items are Clause, as they are everywhere in the DSL. A plain Go value is not
// an item; write Lit for that.
func Stm(items ...Clause) Statement { return Statement{items: dup(items)} }

func (s Statement) BuildSQL(args []any) (string, []any) {
	return join(args, space, s.items)
}

// ToSQL assembles the whole statement. No parentheses wrap the outermost level.
func (s Statement) ToSQL() (string, []any) { return s.BuildSQL(nil) }

// ---------------------------------------------------------------------------
// Basic types
// ---------------------------------------------------------------------------

// Id is an identifier. Its underlying type is string so that it can be
// declared with const.
type Id string

func (i Id) BuildSQL(args []any) (string, []any) { return string(i), args }

// rawSQL is a string embedded into the output verbatim. It exists so that the
// keyword constants below can be declared with const, and its zero value is
// the empty string, which is skipped during joining.
//
// It is unexported so that Raw is the only way to write a fragment. An exported
// verbatim type would let a fragment reach the output without its placeholders
// being checked, and a "$2" written that way would refer to another clause's
// value in a query that still runs.
type rawSQL string

func (r rawSQL) BuildSQL(args []any) (string, []any) { return string(r), args }

// Raw is the escape hatch for SQL this package does not model, and the only way
// to write a fragment by hand. The fragment is embedded as-is, and each "$0" in
// it is replaced with the placeholder for the next value.
//
//	Stm(
//		SELECT(UsersID),
//		FROM(Users),
//		WHERE(
//			UsersStatus, EQ, Lit("active"), AND,
//			Raw("users.meta->'profile'->>'city' = $0", "Tokyo"),
//		),
//	)
//
//	// SELECT users.id FROM users
//	// WHERE users.status = $1 AND users.meta->'profile'->>'city' = $2
//	// args=[active Tokyo]
//
// The "$0" became "$2" because the fragment came second. A fragment cannot know
// that number in advance, which is why it does not write one. With no values,
// Raw is just a fragment: Raw("@>") produces "@>".
//
// Only "$0" marks a value, and Raw panics on a "$" followed by any other
// number. Such a number is always a mistake, and one that would otherwise
// produce a query that runs and quietly reads another clause's value.
//
// Raw does not look inside quoted regions, because a fragment is a piece of a
// statement and may begin or end inside one. The consequence is that a "$"
// followed by digits cannot appear as literal text anywhere in a fragment.
// Dollar quoting itself still works, since only a digit after the "$" makes a
// marker: "$$body$$" and "$tag$body$tag$" pass through untouched.
//
// A count mismatch is left to the database rather than checked here. A "$0"
// with no value left stays in the output, and Postgres reports that there is no
// parameter $0; a surplus value is bound anyway, and Postgres reports the
// parameter count mismatch.
//
// No escaping is performed at all. Pass only constants, or strings assembled
// in code.
func Raw(sql string, vals ...any) Clause {
	// Split on "$0" once, here, so that a bad fragment panics at the call
	// rather than at build time. parts holds the text around the markers, so
	// there are len(parts)-1 places for a value.
	var parts []string
	var buf strings.Builder
	for rest := sql; ; {
		i := strings.IndexByte(rest, '$')
		if i < 0 {
			buf.WriteString(rest)
			break
		}
		buf.WriteString(rest[:i])
		rest = rest[i+1:]
		n := 0
		for n < len(rest) && '0' <= rest[n] && rest[n] <= '9' {
			n++
		}
		if n == 0 {
			// A "$" with no digits after it is ordinary text, such as the
			// delimiter of a dollar-quoted string.
			buf.WriteByte('$')
			continue
		}
		if rest[:n] != "0" {
			panic("sql: Raw: fragment contains $" + rest[:n] +
				", but only $0 marks a value: " + sql)
		}
		parts = append(parts, buf.String())
		buf.Reset()
		rest = rest[n:]
	}
	parts = append(parts, buf.String())

	cp := dup(vals)
	return clauseFunc(func(args []any) (string, []any) {
		var b strings.Builder
		b.WriteString(parts[0])
		for i, part := range parts[1:] {
			if i < len(cp) {
				args = append(args, cp[i])
				b.WriteString(fmt.Sprintf("$%d", len(args)))
			} else {
				// Fewer values than markers. The marker is left in place, and
				// Postgres reports that there is no parameter $0.
				b.WriteString("$0")
			}
			b.WriteString(part)
		}
		// Bind the surplus, if any, so that the mismatch surfaces at the
		// database instead of the query running with a value silently dropped.
		for i := len(parts) - 1; i < len(cp); i++ {
			args = append(args, cp[i])
		}
		return b.String(), args
	})
}

// Lit binds a value and produces its $N. It is the only way a value enters a
// statement, since every position in the DSL takes a Clause and a plain Go
// value is not one.
//
//	Stm(UPDATE(Users), SET(UsersStatus), EQ, Lit("vip")) // UPDATE users SET status = $1
//	WHERE(UsersAge, GTE, Lit(18))                        // WHERE users.age >= $1
//
// The values Raw binds to its "$0" markers are the exception. They are written
// as-is, because a "$0" slot can hold nothing but a value.
func Lit(v any) Clause {
	return clauseFunc(func(args []any) (string, []any) {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args)), args
	})
}

// Row is a comma-separated parenthesised list. Row values, the list in an IN,
// and a single row of VALUES are all spelled the same way.
//
//	Row(Lit("bob"), Lit(42))   -> "($1, $2)"
//	Row(UsersCreated, UsersID) -> "(users.created_at, users.id)"
func Row(vs ...Clause) Clause {
	cp := dup(vs)
	return clauseFunc(func(args []any) (string, []any) {
		sql, args := join(args, comma, cp)
		return "(" + sql + ")", args
	})
}

// ---------------------------------------------------------------------------
// Keyword constants — the ones that take no operands
// ---------------------------------------------------------------------------

const (
	AND rawSQL = "AND"
	OR  rawSQL = "OR"
	NOT rawSQL = "NOT"

	EQ    rawSQL = "="
	NE    rawSQL = "<>"
	GT    rawSQL = ">"
	GTE   rawSQL = ">="
	LT    rawSQL = "<"
	LTE   rawSQL = "<="
	LIKE  rawSQL = "LIKE"
	ILIKE rawSQL = "ILIKE"

	IS_NULL     rawSQL = "IS NULL"
	IS_NOT_NULL rawSQL = "IS NOT NULL"

	ASC         rawSQL = "ASC"
	DESC        rawSQL = "DESC"
	NULLS_FIRST rawSQL = "NULLS FIRST"
	NULLS_LAST  rawSQL = "NULLS LAST"

	JOIN       rawSQL = "JOIN"
	LEFT_JOIN  rawSQL = "LEFT JOIN"
	INNER_JOIN rawSQL = "INNER JOIN"
	CROSS_JOIN rawSQL = "CROSS JOIN"
	LATERAL    rawSQL = "LATERAL"

	UNION        rawSQL = "UNION"
	UNION_ALL    rawSQL = "UNION ALL"
	INTERSECT    rawSQL = "INTERSECT"
	EXCEPT       rawSQL = "EXCEPT"
	MATERIALIZED rawSQL = "MATERIALIZED"

	DO_UPDATE  rawSQL = "DO UPDATE"
	DO_NOTHING rawSQL = "DO NOTHING"

	CASE rawSQL = "CASE"
	END  rawSQL = "END"

	OVER   rawSQL = "OVER"
	FILTER rawSQL = "FILTER"

	STAR     rawSQL = "*"
	DISTINCT rawSQL = "DISTINCT"
	TRUE     rawSQL = "TRUE"
	FALSE    rawSQL = "FALSE"
	NULL     rawSQL = "NULL"
)

// ---------------------------------------------------------------------------
// Keyword functions — the ones that take operands
// ---------------------------------------------------------------------------

func keyword(kw, sep string, vs []Clause) Clause {
	cp := dup(vs)
	return clauseFunc(func(args []any) (string, []any) {
		s, args := join(args, sep, cp)
		if s == "" {
			return kw, args
		}
		return kw + " " + s, args
	})
}

const (
	comma = ", "
	space = " "
)

func SELECT(vs ...Clause) Clause         { return keyword("SELECT", comma, vs) }
func FROM(vs ...Clause) Clause           { return keyword("FROM", comma, vs) }
func WHERE(vs ...Clause) Clause          { return keyword("WHERE", space, vs) }
func GROUP_BY(vs ...Clause) Clause       { return keyword("GROUP BY", comma, vs) }
func HAVING(vs ...Clause) Clause         { return keyword("HAVING", space, vs) }
func ORDER_BY(vs ...Clause) Clause       { return keyword("ORDER BY", comma, vs) }
func ON(vs ...Clause) Clause             { return keyword("ON", space, vs) }
func RETURNING(vs ...Clause) Clause      { return keyword("RETURNING", comma, vs) }
func PARTITION_BY(vs ...Clause) Clause   { return keyword("PARTITION BY", comma, vs) }
func WITH(vs ...Clause) Clause           { return keyword("WITH", comma, vs) }
func WITH_RECURSIVE(vs ...Clause) Clause { return keyword("WITH RECURSIVE", comma, vs) }
func WHEN(vs ...Clause) Clause           { return keyword("WHEN", space, vs) }
func VALUES(rows ...Clause) Clause       { return keyword("VALUES", comma, rows) }

// Keywords that take a single value.
func valueKeyword(kw string, v Clause) Clause {
	return clauseFunc(func(args []any) (string, []any) {
		s, args := value(args, v, true)
		return kw + " " + s, args
	})
}

func LIMIT(v Clause) Clause  { return valueKeyword("LIMIT", v) }
func OFFSET(v Clause) Clause { return valueKeyword("OFFSET", v) }
func THEN(v Clause) Clause   { return valueKeyword("THEN", v) }
func ELSE(v Clause) Clause   { return valueKeyword("ELSE", v) }

// IN / EXISTS / ANY reuse whatever parentheses their operand already carries.
// Pass Row(...) for a list, or Stm(...) for a subquery.
//
//	IN(Row(Lit(1), Lit(2))) -> "IN ($1, $2)"
//	IN(Stm(SELECT(...)))    -> "IN (SELECT ...)"
func IN(v Clause) Clause     { return valueKeyword("IN", v) }
func EXISTS(v Clause) Clause { return valueKeyword("EXISTS", v) }
func ANY(v Clause) Clause    { return valueKeyword("ANY", v) }
func CAST(v Clause) Clause   { return valueKeyword("::", v) }

// Where parentheses are mandatory (no shorthand form exists), the function
// emits them itself.
func DISTINCT_ON(vs ...Clause) Clause {
	cp := dup(vs)
	return clauseFunc(func(args []any) (string, []any) {
		s, args := join(args, comma, cp)
		return "DISTINCT ON (" + s + ")", args
	})
}

// FUNC covers any SQL function. It is the single entry point that spares us
// from growing separate COUNT / SUM / COALESCE / PERCENTILE_CONT helpers.
func FUNC(name string, vs ...Clause) Clause {
	cp := dup(vs)
	return clauseFunc(func(args []any) (string, []any) {
		s, args := join(args, comma, cp)
		return name + "(" + s + ")", args
	})
}

// AS introduces an alias. It takes an identifier, so the parameter is fixed
// to string.
func AS(alias string) Clause { return Raw("AS " + alias) }

// DEF defines a CTE.
func DEF(name string, q Clause) Clause {
	return clauseFunc(func(args []any) (string, []any) {
		s, args := value(args, q, true)
		return name + " AS " + s, args
	})
}

// ---------------------------------------------------------------------------
// The four places that need unqualified names — fixing the parameter type to
// Id keeps expressions and Raw out
// ---------------------------------------------------------------------------

func unqualify(i Id) string {
	s := string(i)
	if n := strings.LastIndex(s, "."); n >= 0 {
		return s[n+1:]
	}
	return s
}

func unqualifiedList(cols []Id, alwaysParen bool) string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = unqualify(c)
	}
	joined := strings.Join(names, ", ")
	if alwaysParen || len(cols) > 1 {
		return "(" + joined + ")"
	}
	return joined
}

func INSERT_INTO(table Id, cols ...Id) Clause {
	return Raw("INSERT INTO " + string(table) + " " + unqualifiedList(cols, true))
}

func ON_CONFLICT(cols ...Id) Clause { return Raw("ON CONFLICT " + unqualifiedList(cols, true)) }

func SET(cols ...Id) Clause { return Raw("SET " + unqualifiedList(cols, false)) }

func EXCLUDED(col Id) Clause { return Raw("EXCLUDED." + unqualify(col)) }

func UPDATE(table Id) Clause      { return Raw("UPDATE " + string(table)) }
func DELETE_FROM(table Id) Clause { return Raw("DELETE FROM " + string(table)) }
