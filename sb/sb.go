// Package sb builds Postgres statements from a flat list of tokens.
//
// A statement is written as one sequence of tokens, in the order the SQL reads:
//
//	sb.S(
//		SELECT, UsersID, UsersName,
//		FROM, Users,
//		WHERE, sb.S(UsersCreated, UsersID), LT, sb.S(sb.Lit("2025-06-01"), sb.Lit(500)),
//		ORDER, BY, UsersCreated, DESC, UsersID, DESC, NULLS, LAST,
//		LIMIT, sb.Lit(20),
//	)
//
//	// SELECT users.id, users.name
//	// FROM users
//	// WHERE (users.created_at, users.id) < ($1, $2)
//	// ORDER BY users.created_at DESC, users.id DESC NULLS LAST
//	// LIMIT $3
//	// args=[2025-06-01 500 20]
//
// Note where the commas landed. The select list and the ORDER BY list are
// comma-separated, the WHERE clause is not, and DESC and NULLS LAST belong to
// the sort key before them rather than starting a new one. Producing that is
// the whole job of this package, and the next sections are how it is done.
//
// # The two packages
//
// The SQL keywords live in package psqlb and everything else lives here. A user
// dot-imports the keywords, so that they read as SQL, and reaches this package
// through its name:
//
//	import (
//		. "github.com/fujidaiti/psqlb"
//		"github.com/fujidaiti/psqlb/sb"
//	)
//
// That is the whole reason for the split. Dot-importing one package that held
// both would also put S, Func, Id, Lit and Raw into the user's file scope, where
// they collide with ordinary Go names and no longer look different from the SQL
// vocabulary.
//
// The keyword type is in internal/kw, which neither package re-exports, so a
// user cannot declare a keyword of their own. That matters more here than it
// would in a renderer: a keyword the parser does not know is a keyword the
// parser cannot place.
//
// # Golden rules
//
// Three rules decide every design question in this package. When a convenience
// and a rule disagree, the rule wins.
//
//  1. The DSL should look like the raw SQL it produces. Tokens are written in
//     the order SQL reads them, and a reader who knows SQL should be able to
//     read the Go without learning a second vocabulary.
//
//  2. Do not introduce a special rule or a keyword that does not exist in SQL.
//     The package models SQL; it does not extend it. A construct that has no
//     counterpart in SQL has no place here, and neither does a keyword that
//     behaves differently from the one it is named after.
//
//  3. Parentheses are explicit. If parentheses are wanted in the SQL string,
//     something must be written for them in the DSL: S, which is the only group
//     there is. Parentheses are never added or removed on the reader's behalf.
//
// # The central idea
//
// The position knows what the token may be. ToSQL parses the token list against
// the PostgreSQL grammar, so at every point the parser knows which production it
// is in, and that decides which tokens are legal there, how many of them the
// current item consumes, and what separates this item from the next.
//
// Three things follow.
//
// Item boundaries are found rather than guessed. A list production parses one
// item at a time, and an item ends when its own grammar is complete. The sort
// key is "expression [ASC|DESC] [NULLS FIRST|LAST]", so it consumes DESC and
// NULLS LAST itself and the list production then writes the comma before the
// next key. No token has to be marked as attaching to the one before it.
//
// The same token may mean different things in different places. ON introduces a
// join condition inside FROM and names a conflict target after INSERT; Id is a
// table in FROM and a column reference in a WHERE; a group is a subquery after
// FROM, an expression list after IN and a row constructor in an expression. No
// token needs a second spelling for a second role.
//
// An unparseable sequence is an error. The package is not a renderer that emits
// whatever it is given: a sequence that is not valid PostgreSQL for the
// supported subset is reported and never rendered.
//
// # Tokens
//
// A token is data the parser inspects, not something that renders itself. The
// set is closed:
//
//	Token                        | Written as              | Example
//	-----------------------------|-------------------------|--------------------------
//	Keyword                      | a constant from psqlb   | SELECT FROM WHERE IN OVER
//	Operator                     | a constant, or sb.Op()  | EQ LIKE, sb.Op("@>")
//	Identifier                   | sb.Id                   | UsersID
//	Value                        | sb.Lit()                | sb.Lit(42)
//	Group, and the statement     | sb.S(...)               | sb.S(x, y) -> (x, y)
//	Function call                | sb.Func(name, ...)      | sb.Func("COUNT", STAR)
//	Hand-written expression      | sb.Raw()                | sb.Raw("x = $0", 1)
//
// Every keyword is one word. A phrase is written as the words it is made of:
// GROUP, BY and IS, NOT, NULL and LEFT, OUTER, JOIN. The parser reads sequences
// natively, so a phrase needs no constant of its own, optional words combine
// instead of multiplying, and the token list is the SQL text word for word.
//
// # The parser
//
// Hand-written recursive descent, one function per grammar production, each
// named after the production in the PostgreSQL reference it implements and
// carrying that synopsis as its doc comment. Parsing and emitting are one pass:
// each production writes its own fragment as it recognises it, and there is no
// syntax tree. The separator therefore belongs to the production that owns it,
// and placeholder numbering is simply emission order.
//
// There is at most two tokens of lookahead and no backtracking. Two are needed
// where a split keyword phrase decides the branch, such as NOT IN against a NOT
// that is not an infix at all.
//
// Expressions are parsed without precedence. The DSL requires explicit
// parentheses and the emitter never adds any, so operators are emitted in the
// order they are written. What is validated is shape — an operand, then an
// operator, then an operand:
//
//	WHERE, a, AND, b // fine
//	WHERE, a, b      // an error
//
// # Parentheses
//
// S is the only group and the only source of parentheses. It is bare at the
// outermost level, where it is the statement, and parenthesised whenever it is
// nested, so nesting in Go is nesting in SQL. What it holds is decided by the
// position it is written in:
//
//	FROM, sb.S(SELECT, ...), AS, sb.Id("x")      // a subquery, aliased
//	IN, sb.S(sb.Lit("active"), sb.Lit("trial"))  // an expression list
//	IN, sb.S(SELECT, OrdersUserID, FROM, Orders) // a subquery
//	WHERE, sb.S(a, OR, b)                        // a parenthesised condition
//	WHERE, sb.S(a, b), EQ, sb.S(x, y)            // two row constructors
//
// A position that is parenthesised in SQL requires a group and reports its
// absence, so a keyword that SQL always follows with one — IN, EXISTS, ANY —
// is a constant like any other and needs no parentheses of its own.
//
// There is no way to nest an S without parentheses and no automatic removal of
// them. To reuse a fragment where parentheses are unwanted, keep it as a
// []Token and spread it:
//
//	inner := []sb.Token{SELECT, OrdersUserID, FROM, Orders}
//	sb.S(SELECT, STAR, FROM, Users, WHERE, UsersID, IN, sb.S(inner...))
//
// One more level of nesting is one more pair of parentheses, and after IN it is
// a different query. Both are legal SQL:
//
//	UsersID, IN, sb.S(SELECT, OrdersUserID, FROM, Orders) // membership
//	UsersID, IN, sb.S(sb.S(inner...))                     // a one-element list
//
// The second compares against a list of one scalar subquery, so it fails at
// execution if the subquery returns more than one row.
//
// # Values and $N
//
// A plain Go value is not a token. It enters a statement only through Lit,
// which binds it and produces its placeholder, or through the "$0" markers of
// Raw. That is what keeps S(SELECT, "id") from compiling.
//
// Placeholders are numbered in emission order, which is token order, so a
// nested statement numbers correctly at any depth with no counter and no
// renumbering pass.
//
// # Errors
//
// ToSQL returns an error and nothing panics. There are three classes:
//
//   - SyntaxError: the token is not legal at this point in the grammar. The
//     message names the production, what was expected and what was found.
//   - MissingError: the position requires something that is not there — a group
//     where SQL parenthesises, or the alias PostgreSQL requires on a subquery in
//     FROM. The message names the way to write it.
//   - UnsupportedError: the construct is legal PostgreSQL but outside the
//     modelled subset. It is a distinct type so that "I wrote this wrong" can be
//     told from "this package has not got there yet".
//
// Two further errors belong to Raw and are reported at ToSQL: a fragment whose
// "$0" marker count does not match its value count, and a fragment containing
// "$N" for any N other than 0. The second would run, which is exactly the
// problem: it silently reads another clause's value.
//
// A nil token means "this token is absent" and is dropped before parsing, which
// is how an optional item is left in place. Removing a token can make the
// sequence ungrammatical, and that is now reported rather than emitted.
//
// # Scope
//
// The grammar is built in phases, and anything outside the current one is
// reported as not supported yet. What is modelled today:
//
//	SELECT [ALL | DISTINCT [ON (...)]] expression [AS name] [, ...]
//	    [FROM from_item [, ...]]
//	    [WHERE condition]
//	    [GROUP BY expression [, ...]] [HAVING condition]
//	    [ORDER BY expression [ASC|DESC] [NULLS {FIRST|LAST}] [, ...]]
//	    [LIMIT {count | ALL}] [OFFSET start]
//	    [{UNION | INTERSECT | EXCEPT} [ALL | DISTINCT] query]
//
// A from_item is a table, a parenthesised subquery with an AS alias, or a
// function call, each optionally preceded by LATERAL, and any of them may be
// joined:
//
//	from_item [NATURAL] [INNER | {LEFT|RIGHT|FULL} [OUTER] | CROSS] JOIN from_item
//	    {ON condition | USING (column [, ...])}
//
//	INSERT INTO table [AS alias] [(column [, ...])]
//	    {VALUES (expression [, ...]) [, ...] | query}
//	    [ON CONFLICT [(column [, ...])] DO NOTHING
//	                                   | DO UPDATE SET ... [WHERE condition]]
//	    [RETURNING expression [AS name] [, ...]]
//
//	UPDATE table [AS alias] SET assignment [, ...]
//	    [FROM from_item [, ...]] [WHERE condition] [RETURNING ...]
//
//	DELETE FROM table [AS alias]
//	    [USING from_item [, ...]] [WHERE condition] [RETURNING ...]
//
// An assignment is "column = expression" or
// "(column [, ...]) = (expression [, ...])".
//
// Expressions: column references, Lit, Raw, Func, named and hand-written
// operators, AND, OR, NOT, IS [NOT] NULL/TRUE/FALSE/UNKNOWN, IS [NOT] DISTINCT
// FROM, [NOT] IN, [NOT] BETWEEN, [NOT] LIKE/ILIKE, EXISTS, a quantified
// comparison with ANY/SOME/ALL, CASE, "::" written TYPECAST with the type name
// as an Id, row constructors, scalar subqueries and parenthesised expressions.
//
// Still to come: WITH, window functions and COLLATE. DDL, MERGE, locking
// clauses and array and JSON path syntax are not planned.
//
// # Known limitations
//
//   - SET strips the table qualifier from the name that begins each assignment,
//     since "SET users.status = ..." is not legal. This is the one rule the
//     package has that SQL does not, kept because the qualified column constant
//     is the one already at hand. It applies at that grammar position only, so
//     the expression to the right of the "=" keeps its qualifiers and so does
//     the column list of INSERT.
//
//   - An alias must be written with AS. SELECT, UsersID, sb.Id("x") emits
//     "SELECT users.id, x", never "SELECT users.id AS x". SQL allows the
//     implicit form and the DSL does not, because the two are the same token
//     sequence: the DSL has no comma token, so both readings are valid SQL and
//     a heuristic would reject the ordinary "SELECT users.id, name".
//
//   - The package is only as useful as its coverage. There is no escape hatch
//     for an unmodelled clause: the answer is to model it. Raw covers an
//     unmodelled expression, which is where the open-ended part of SQL sits.
//
//   - Raw is one opaque expression. The parser checks where it may appear and
//     never looks inside it, so a fragment that contains a comma or a whole
//     clause can still break a statement.
//
//   - No type safety and no semantic checking. Whether a column exists and
//     whether two types unify are the database's business.
//
//   - Raw performs no escaping at all. Pass only constants, or strings
//     assembled in code.
//
//   - Aliasing a table needs its own Id constant. There is no Users.As("u").
//
//   - Nothing here has been run against a real database. The tests check the
//     generated string and the bound args, nothing more.
package sb

import (
	"fmt"
	"strings"

	"github.com/fujidaiti/psqlb/internal/kw"
)

// ===========================================================================
// Tokens
// ===========================================================================

// Token is a single token of a statement. The set is closed: a keyword, an
// operator, an identifier, a value, a group and a hand-written fragment. A
// token is data that the parser inspects; it does not render itself, because
// what it renders depends on where it is written.
//
// Every position in the DSL takes a Token, never an any. A plain Go value is
// not a token: it enters a statement only through Lit, or through the "$0"
// markers of Raw. That is what keeps S(SELECT, "id") from compiling.
type Token = kw.Token

// Id is an identifier: a table name, a column name, an alias, or a simple type
// name. Its underlying type is string so that it can be declared with const.
// The text is emitted verbatim; nothing is quoted and nothing is validated.
type Id string

func (Id) SQLToken() {}

// value is what Lit produces.
type value struct{ v any }

func (value) SQLToken() {}

// Lit binds a value and produces its placeholder.
//
//	S(SELECT, STAR, FROM, Users, WHERE, UsersAge, GTE, Lit(18)) // ... WHERE users.age >= $1
func Lit(v any) Token { return value{v} }

// rawFrag is what Raw produces. The fragment is split at its "$0" markers when
// it is written, so a malformed fragment is caught at the call rather than at
// build time; err carries that failure until ToSQL is reached.
type rawFrag struct {
	parts []string
	vals  []any
	err   error
}

func (rawFrag) SQLToken() {}

// Raw is an expression written by hand: the escape hatch for an expression
// this package does not model. It is one complete expression wherever an
// expression may begin, and the parser never looks inside the string.
//
// Each "$0" in the fragment becomes the placeholder for the next value, so the
// values stay where they are written instead of being split into separate
// tokens.
//
//	Raw("users.meta->'profile'->>'city' = $0", "Tokyo")
//	// users.meta->'profile'->>'city' = $2   (if the fragment came second)
//
// A fragment cannot know that number in advance, which is why it does not write
// one. Only "$0" marks a value, and any other number is an error: it would
// otherwise produce a query that runs and quietly reads another clause's value.
// A marker count that does not match the value count is an error too.
//
// Raw does not look inside quoted regions, because a fragment is a piece of a
// statement and may begin or end inside one. The consequence is that a "$"
// followed by digits cannot appear as literal text. Dollar quoting itself still
// works, since only a digit after the "$" makes a marker, so "$$body$$" and
// "$tag$body$tag$" pass through untouched.
//
// No escaping is performed at all. Pass only constants, or strings assembled in
// code.
func Raw(sql string, vals ...any) Token {
	parts, err := splitMarkers(sql)
	cp := dup(vals)
	if err == nil && len(parts)-1 != len(cp) {
		err = fmt.Errorf("psqlb: Raw: fragment has %d $0 marker(s) but %d value(s): %s",
			len(parts)-1, len(cp), sql)
	}
	return rawFrag{parts: parts, vals: cp, err: err}
}

// Op is an operator written by hand. It exists because operators are not a
// fixed list: extensions add their own. It is an infix operator wherever one
// may appear, and the symbol is emitted verbatim.
//
//	S(..., WHERE, UsersMeta, Op("@>"), Lit(`{"vip":true}`)) // WHERE users.meta @> $1
func Op(sql string) Token { return kw.Operator(sql) }

// ===========================================================================
// Groups
// ===========================================================================

// Group is a parenthesised list of tokens, and at the outermost level the whole
// statement. What a group means is decided by the position it is written in and
// not by the group itself: after FROM it is a subquery, after IN it is an
// expression list or a subquery, in an expression it is a parenthesised
// expression or a row constructor, and after VALUES it is one row.
type Group struct {
	// name is emitted immediately before the opening parenthesis: "" for S and
	// the function name for Func.
	name  string
	items []Token
}

func (Group) SQLToken() {}

// S builds a group. It is the only way to write a statement and the only source
// of parentheses in the generated SQL, so a subquery, a grouped condition, a
// row constructor, an IN list and a window specification are all spelled the
// same way.
//
//	S(SELECT, UsersID, FROM, Users)          // SELECT users.id FROM users
//	S(UsersIsPaid, OR, UsersHasTicket)       // (users.paid OR users.has_ticket)
//	S(Lit("active"), Lit("trial"))           // ($1, $2)
func S(items ...Token) Group { return Group{items: dup(items)} }

// Func is a function call. It covers any SQL function, which is what spares the
// package from growing separate COUNT, SUM and COALESCE helpers.
//
//	Func("COUNT", STAR)              // COUNT(*)
//	Func("COUNT", DISTINCT, UsersID) // COUNT(DISTINCT users.id)
func Func(name string, items ...Token) Group {
	return Group{name: name, items: dup(items)}
}

// ToSQL parses the group as a statement and returns the SQL text and the values
// bound to its $N placeholders. The outermost level is never parenthesised;
// every nested group is.
func (g Group) ToSQL() (string, []any, error) {
	e := &emitter{}
	p := &parser{toks: compact(g.items), e: e}
	if err := p.statement(); err != nil {
		return "", nil, err
	}
	if !p.done() {
		return "", nil, p.unexpected("statement", "the end of the statement")
	}
	return e.b.String(), e.args, nil
}

// ===========================================================================
// Helpers
// ===========================================================================

// dup is generic because Raw holds values, []any, while a group holds tokens,
// []Token.
func dup[T any](vs []T) []T {
	cp := make([]T, len(vs))
	copy(cp, vs)
	return cp
}

// compact drops the nil tokens. A nil token means "this token is absent", which
// is how an optional item is left in place; dropping them before parsing is
// what keeps the grammar from having to mention them.
func compact(items []Token) []Token {
	out := make([]Token, 0, len(items))
	for _, t := range items {
		if t != nil {
			out = append(out, t)
		}
	}
	return out
}

func splitMarkers(sql string) ([]string, error) {
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
			return nil, fmt.Errorf("psqlb: Raw: fragment contains $%s, but only $0 marks a value: %s",
				rest[:n], sql)
		}
		parts = append(parts, buf.String())
		buf.Reset()
		rest = rest[n:]
	}
	return append(parts, buf.String()), nil
}

// unqualify strips a table qualifier from a name. It is used at exactly one
// grammar position, the target of a SET assignment.
func unqualify(i Id) string {
	s := string(i)
	if n := strings.LastIndex(s, "."); n >= 0 {
		return s[n+1:]
	}
	return s
}
