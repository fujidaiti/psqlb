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
//		WHERE(UsersStatus, EQ, "active", AND, UsersAge, GTE, 18),
//		ORDER_BY(Stm(UsersID, DESC)),
//		LIMIT(20),
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
//	Keyword with operands Function                 SELECT(...) FROM(...) LIMIT(20)
//	Keyword without       Raw constant             AND OR NOT DESC JOIN UNION_ALL
//	Infix operator        Raw constant / Raw()     EQ GT LIKE / Raw("@>")
//	Enclosing             Stm(...) Row(...) FUNC()
//	Identifier            Id constant              UsersID
//	Literal               A plain Go value         42, "active"
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
// afterwards. value() is the one and only branch on how a value is treated:
// if something implements Clause it is expanded as SQL, and if it does not, it
// becomes a $N. Position has no say in that; it decides only whether a nested
// Stm is parenthesised.
//
//	UsersStatus, EQ, "active"   // users.status = $1   <- a string, so it is bound
//	OrdersUserID, EQ, UsersID   // orders.user_id = users.id  <- an Id, so it is inlined
//
// Because args is handed along in expansion order, the numbering stays
// sequential no matter how deeply things nest. Statements are values, so they
// can be assembled without holding a binder and composed later.
//
// ---------------------------------------------------------------------------
// Known limitations
// ---------------------------------------------------------------------------
//
//   - Raw(sql string) performs no escaping whatsoever, so passing user input to
//     it leads directly to SQL injection. Pass only constants, or strings
//     assembled in code.
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
// Builds under Go 1.22 and passes go vet and gofmt. The 15 examples in
// idea_test.go check the generated SQL and the bound arguments against the
// strings they are expected to produce; run `go test` to check them. Nothing
// has yet been run against a real database.
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

// value is the only branch in this package. A Clause is expanded as SQL;
// anything else is bound and becomes a $N.
//
// group says whether the enclosing sequence is space-separated. A Statement
// nested there is an operand, so it is parenthesised. An item of a
// comma-separated clause is a single expression, which SQL never parenthesises.
//
// Every entry point runs through here, except the four functions that demand
// identifiers (INSERT_INTO / ON_CONFLICT / SET / EXCLUDED).
func value(args []any, v any, group bool) (string, []any) {
	switch c := v.(type) {
	case nil:
		return "", args
	case Statement:
		sql, args := c.BuildSQL(args)
		if sql == "" || !group {
			return sql, args
		}
		return "(" + sql + ")", args
	case Clause:
		return c.BuildSQL(args)
	}
	args = append(args, v)
	return fmt.Sprintf("$%d", len(args)), args
}

// join concatenates with sep. sep is always comma or space, and it is what
// tells value() whether a nested Statement is an operand.
func join(args []any, sep string, vs []any) (string, []any) {
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

func dup(vs []any) []any {
	cp := make([]any, len(vs))
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
type Statement struct{ items []any }

// Stm builds a space-separated group. It is the only way to write a statement.
// Nesting it inside another space-separated sequence adds parentheses; nesting
// it as an item of a comma-separated clause does not.
//
//	Stm(a, Stm(b, c))            -> "a (b c)"
//	ORDER_BY(Stm(UsersID, DESC)) -> "ORDER BY users.id DESC"
//
// Subqueries, grouped conditions and multi-token items of a comma-separated
// clause are all spelled the same way.
func Stm(items ...any) Statement { return Statement{items: dup(items)} }

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

// Raw is a string embedded into the output as-is. Keywords, operator symbols
// and arbitrary SQL fragments are all spelled with it: the fixed keyword
// constants below, and Raw("@>") for an infix operator that is not among them.
// It can be declared as a constant, and its zero value is the empty string,
// which is skipped during joining.
//
// No escaping is performed at all. Pass only constants, or strings assembled
// in code.
type Raw string

func (r Raw) BuildSQL(args []any) (string, []any) { return string(r), args }

// Lit forces a value to be bound. It is needed only when you want to pass an
// Id or a Raw as a value rather than as an identifier. An ordinary value
// becomes a $N just by being written as-is.
func Lit(v any) Clause {
	return clauseFunc(func(args []any) (string, []any) {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args)), args
	})
}

// Row is a comma-separated parenthesised list. Row values, the list in an IN,
// and a single row of VALUES are all spelled the same way.
//
//	Row("bob", 42)             -> "($1, $2)"
//	Row(UsersCreated, UsersID) -> "(users.created_at, users.id)"
func Row(vs ...any) Clause {
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
	AND Raw = "AND"
	OR  Raw = "OR"
	NOT Raw = "NOT"

	EQ    Raw = "="
	NE    Raw = "<>"
	GT    Raw = ">"
	GTE   Raw = ">="
	LT    Raw = "<"
	LTE   Raw = "<="
	LIKE  Raw = "LIKE"
	ILIKE Raw = "ILIKE"

	IS_NULL     Raw = "IS NULL"
	IS_NOT_NULL Raw = "IS NOT NULL"

	ASC         Raw = "ASC"
	DESC        Raw = "DESC"
	NULLS_FIRST Raw = "NULLS FIRST"
	NULLS_LAST  Raw = "NULLS LAST"

	JOIN       Raw = "JOIN"
	LEFT_JOIN  Raw = "LEFT JOIN"
	INNER_JOIN Raw = "INNER JOIN"
	CROSS_JOIN Raw = "CROSS JOIN"
	LATERAL    Raw = "LATERAL"

	UNION        Raw = "UNION"
	UNION_ALL    Raw = "UNION ALL"
	INTERSECT    Raw = "INTERSECT"
	EXCEPT       Raw = "EXCEPT"
	MATERIALIZED Raw = "MATERIALIZED"

	DO_UPDATE  Raw = "DO UPDATE"
	DO_NOTHING Raw = "DO NOTHING"

	CASE Raw = "CASE"
	END  Raw = "END"

	OVER   Raw = "OVER"
	FILTER Raw = "FILTER"

	STAR     Raw = "*"
	DISTINCT Raw = "DISTINCT"
	TRUE     Raw = "TRUE"
	FALSE    Raw = "FALSE"
	NULL     Raw = "NULL"
)

// ---------------------------------------------------------------------------
// Keyword functions — the ones that take operands
// ---------------------------------------------------------------------------

func keyword(kw, sep string, vs []any) Clause {
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

func SELECT(vs ...any) Clause         { return keyword("SELECT", comma, vs) }
func FROM(vs ...any) Clause           { return keyword("FROM", comma, vs) }
func WHERE(vs ...any) Clause          { return keyword("WHERE", space, vs) }
func GROUP_BY(vs ...any) Clause       { return keyword("GROUP BY", comma, vs) }
func HAVING(vs ...any) Clause         { return keyword("HAVING", space, vs) }
func ORDER_BY(vs ...any) Clause       { return keyword("ORDER BY", comma, vs) }
func ON(vs ...any) Clause             { return keyword("ON", space, vs) }
func RETURNING(vs ...any) Clause      { return keyword("RETURNING", comma, vs) }
func PARTITION_BY(vs ...any) Clause   { return keyword("PARTITION BY", comma, vs) }
func WITH(vs ...any) Clause           { return keyword("WITH", comma, vs) }
func WITH_RECURSIVE(vs ...any) Clause { return keyword("WITH RECURSIVE", comma, vs) }
func WHEN(vs ...any) Clause           { return keyword("WHEN", space, vs) }
func VALUES(rows ...any) Clause       { return keyword("VALUES", comma, rows) }

// Keywords that take a single value.
func valueKeyword(kw string, v any) Clause {
	return clauseFunc(func(args []any) (string, []any) {
		s, args := value(args, v, true)
		return kw + " " + s, args
	})
}

func LIMIT(v any) Clause  { return valueKeyword("LIMIT", v) }
func OFFSET(v any) Clause { return valueKeyword("OFFSET", v) }
func THEN(v any) Clause   { return valueKeyword("THEN", v) }
func ELSE(v any) Clause   { return valueKeyword("ELSE", v) }

// IN / EXISTS / ANY reuse whatever parentheses their operand already carries.
// Pass Row(...) for a list, or Stm(...) for a subquery.
//
//	IN(Row(1, 2, 3))          -> "IN ($1, $2, $3)"
//	IN(Stm(SELECT(...), ...)) -> "IN (SELECT ...)"
func IN(v any) Clause     { return valueKeyword("IN", v) }
func EXISTS(v any) Clause { return valueKeyword("EXISTS", v) }
func ANY(v any) Clause    { return valueKeyword("ANY", v) }
func CAST(v any) Clause   { return valueKeyword("::", v) }

// Where parentheses are mandatory (no shorthand form exists), the function
// emits them itself.
func DISTINCT_ON(vs ...any) Clause {
	cp := dup(vs)
	return clauseFunc(func(args []any) (string, []any) {
		s, args := join(args, comma, cp)
		return "DISTINCT ON (" + s + ")", args
	})
}

// FUNC covers any SQL function. It is the single entry point that spares us
// from growing separate COUNT / SUM / COALESCE / PERCENTILE_CONT helpers.
func FUNC(name string, vs ...any) Clause {
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
func DEF(name string, q any) Clause {
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
