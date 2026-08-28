// Package sb builds Postgres statements from a flat list of tokens.
//
// A statement is written as one sequence of tokens, in the order the SQL reads:
//
//	sb.ToSQL(
//		SELECT, UsersID, UsersName,
//		FROM, Users,
//		WHERE, sb.P(UsersCreated, UsersID), "<", sb.P("2025-06-01", 500),
//		ORDER, BY, UsersCreated, DESC, UsersID, DESC, NULLS, LAST,
//		LIMIT, 20,
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
// The SQL keywords live in package kw and everything else lives here. A user
// dot-imports the keywords, so that they read as SQL, and reaches this package
// through its name:
//
//	import (
//		. "github.com/fujidaiti/psqlb/kw"
//		"github.com/fujidaiti/psqlb/sb"
//	)
//
// That is the whole reason for the split. Dot-importing one package that held
// both would also put P, F, I, Arg and RawExpr into the user's file scope,
// where they collide with ordinary Go names and no longer look different from
// the SQL vocabulary. The prefix is what lets them be this short: sb.I(x) is
// unambiguous where a bare I(x) would not be.
//
// Each keyword is declared once, in kw, and this package compares against those
// same constants, so the spelling a user writes and the value the parser
// matches are the same thing and a misspelling here does not compile.
//
// The token types are in internal/tok rather than in kw, so that a dot-import
// brings SQL words into scope and nothing else. Token is the one name aliased
// out of it, by this package, because a user can see it: it is what Arg and
// RawExpr return. A statement assembled in pieces is a []any.
// Nothing else about the types is public, so a user cannot declare a keyword of
// their own. That matters more here than it would in a renderer: a keyword the
// parser does not know is a keyword the parser cannot place.
//
// # Golden rules
//
// Three rules decide every design question in this package. When a convenience
// and a rule disagree, the rule wins.
//
//  1. The DSL should look like the raw SQL it produces. Tokens are written in
//     the order SQL reads them, and a reader who knows SQL should be able to
//     read the Go without learning a second vocabulary. An operator is
//     therefore its own symbol, ">=" and not GTE, and a value is itself.
//
//  2. Do not introduce a special rule or a keyword that does not exist in SQL.
//     The package models SQL; it does not extend it. A construct that has no
//     counterpart in SQL has no place here, and neither does a keyword that
//     behaves differently from the one it is named after.
//
//  3. Parentheses are explicit. If parentheses are wanted in the SQL string,
//     something must be written for them in the DSL: P, which is the only group
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
// join condition inside FROM and names a conflict target after INSERT; I is a
// table in FROM and a column reference in a WHERE; a group is a subquery after
// FROM, an expression list after IN and a row constructor in an expression. No
// token needs a second spelling for a second role.
//
// An unparseable sequence is an error. The package is not a renderer that emits
// whatever it is given: a sequence that is not valid PostgreSQL for the
// supported subset is reported and never rendered.
//
// There is one decision the package takes before the position is known, and it
// is lexical rather than grammatical: an item that is not already a token
// becomes one at the boundary, in ToSQL, P and F. A string that is a
// well-formed PostgreSQL operator name becomes an operator and everything else
// becomes a value to bind. Nothing else is inferred, and Arg is the override
// for a value whose Go string would otherwise lex as an operator.
//
// # Tokens
//
// A token is data the parser inspects, not something that renders itself. The
// set is closed:
//
//	Token                    | Written as                | Example
//	-------------------------|---------------------------|---------------------------
//	Keyword                  | a constant from kw        | SELECT FROM WHERE IN OVER
//	Operator                 | a plain string            | "=", ">=", "@>", "<->"
//	Identifier               | sb.I                      | UsersID
//	Value                    | a plain Go value          | 42, "active", nil
//	Group                    | sb.P(...)                 | sb.P(x, y) -> (x, y)
//	Function call            | sb.F(name, ...)           | sb.F("COUNT", "*")
//	Hand-written expression  | sb.RawExpr()              | sb.RawExpr("x = $0", 1)
//
// The statement itself is not a token: it is written as the arguments of
// sb.ToSQL, which is the outermost form and the only one that is not
// parenthesised.
//
// ToSQL, P and F take any, not Token, and normalize what they are given. A
// value that already is a token stays what it is; a string that is a
// well-formed PostgreSQL operator name becomes an operator; anything else,
// including nil, becomes a value to bind. The rule for an operator name is
// PostgreSQL's own (reference manual 4.1.3): every character drawn from
//
//	"+-*/<>=~!@#%^&|`?"
//
// at most 63 of them, neither "--" nor "/*" anywhere in it, and a
// multi-character name not ending in "+" or "-" unless it also holds one of
// the characters that cannot end one. The rule is lexical only, so "==" is
// well-formed and PostgreSQL rejects it at execution.
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
// P is the only group and the only source of parentheses. It always writes a
// pair, so nesting in Go is nesting in SQL, and the statement is written with
// ToSQL rather than with P because a statement is not parenthesised. What a
// group holds is decided by the position it is written in:
//
//	FROM, sb.P(SELECT, ...), AS, sb.I("x")       // a subquery, aliased
//	IN, sb.P("active", "trial")                  // an expression list
//	IN, sb.P(SELECT, OrdersUserID, FROM, Orders) // a subquery
//	WHERE, sb.P(a, OR, b)                        // a parenthesised condition
//	WHERE, sb.P(a, b), EQ, sb.P(x, y)            // two row constructors
//
// A position that is parenthesised in SQL requires a group and reports its
// absence, so a keyword that SQL always follows with one — IN, EXISTS, ANY —
// is a constant like any other and needs no parentheses of its own.
//
// There is no way to nest a P without parentheses and no automatic removal of
// them. To reuse a fragment where parentheses are unwanted, keep it as a
// []any and spread it:
//
//	inner := []any{SELECT, OrdersUserID, FROM, Orders}
//	sb.ToSQL(SELECT, "*", FROM, Users, WHERE, UsersID, IN, sb.P(inner...))
//
// One more level of nesting is one more pair of parentheses, and after IN it is
// a different query. Both are legal SQL:
//
//	UsersID, IN, sb.P(SELECT, OrdersUserID, FROM, Orders) // membership
//	UsersID, IN, sb.P(sb.P(inner...))                     // a one-element list
//
// The second compares against a list of one scalar subquery, so it fails at
// execution if the subquery returns more than one row.
//
// # Values and $N
//
// A plain Go value is written as itself and is bound. So is nil. There is no
// wrapper on the common case:
//
//	WHERE, UsersAge, ">=", 18, AND, UsersDeleted, "IS", NULL // ">=" is an operator, 18 is bound
//
// V is gone and Arg has taken its place, for the one case the lexical rule
// gets wrong: a value that is itself made only of operator characters, such as
// a "%" LIKE pattern. Without it, "%" would be taken as an operator.
//
//	LIKE, sb.Arg("%") // users.name LIKE $1
//
// Arg is also the way to bind a []any, since a bare one is taken as a slice
// that was meant to be spread.
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
// Two further errors belong to RawExpr and are reported at ToSQL: a fragment
// whose "$0" marker count does not match its value count, and one containing
// "$N" for any N other than 0. The second would run, which is exactly the
// problem: it silently reads another clause's value.
//
// A slice passed without "..." is reported too. ToSQL(parts) compiles, since a
// slice is an any, and would otherwise bind the whole statement as a single
// parameter.
//
// # Scope
//
// The grammar covers the statements an application writes. Anything outside it
// is reported as not supported yet, by name. What is modelled:
//
//	[WITH [RECURSIVE] name [(column [, ...])] AS [[NOT] MATERIALIZED] (query) [, ...]]
//
//	SELECT [ALL | DISTINCT [ON (...)]] expression [AS name] [, ...]
//	    [FROM from_item [, ...]]
//	    [WHERE condition]
//	    [GROUP BY expression [, ...]] [HAVING condition]
//	    [ORDER BY expression [ASC|DESC] [NULLS {FIRST|LAST}] [, ...]]
//	    [WINDOW name AS (window_definition) [, ...]]
//	    [LIMIT {count | ALL}] [OFFSET start]
//	    [{UNION | INTERSECT | EXCEPT} [ALL | DISTINCT] query]
//
//	VALUES (expression [, ...]) [, ...]
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
// Expressions: column references, bound values, RawExpr, F, operators, AND, OR,
// NOT, IS [NOT] NULL/TRUE/FALSE/UNKNOWN, IS [NOT] DISTINCT FROM, [NOT] IN,
// [NOT] BETWEEN, [NOT] LIKE/ILIKE, [NOT] SIMILAR TO, COLLATE, EXISTS, a
// quantified comparison with ANY/SOME/ALL, CASE, "::" with the type name as an
// I, row constructors, scalar subqueries and parenthesised expressions. A function call may take ALL or DISTINCT, may
// order its input with ORDER BY, and may carry FILTER and OVER, the latter
// taking a window name or a definition with PARTITION BY, ORDER BY and a RANGE,
// ROWS or GROUPS frame.
//
// Not modelled: a type name with a modifier or more than one word, which is
// written with RawExpr; the CAST(x AS type) form, written "::";
// OPERATOR(schema.operator), since an operator string is emitted verbatim and
// cannot carry a schema; frame
// exclusion; ORDER BY ... USING; ON CONFLICT ON CONSTRAINT; FETCH and locking clauses;
// TABLESAMPLE; DDL; MERGE; and array and JSON path syntax. Each is a candidate
// for later work, and none of them blocks the statements above.
//
// # Known limitations
//
//   - A bare Go value is bound wherever an expression may appear, so
//     ToSQL(SELECT, "id") compiles and produces "SELECT $1" with the argument
//     "id". An identifier is written with I and a keyword is a constant from kw,
//     and nothing can tell that a value was meant to be one of them. This is
//     what taking an any costs, and what writing an operator as its own symbol
//     buys.
//
//   - A string value made only of operator characters is taken for an operator,
//     so a LIKE pattern such as "%" must be written Arg("%"). Normalization runs
//     before parsing and cannot consult the position. It is reported rather than
//     emitted: an operator where an operand belongs is a syntax error.
//
//   - Passing a slice without "..." — ToSQL(parts) for ToSQL(parts...) — is a
//     syntax error rather than a compile error, since a slice is an any. To bind
//     a []any deliberately, write Arg(slice).
//
//   - The operator rule is lexical only. "==" is a well-formed operator name, so
//     it is emitted and PostgreSQL rejects it at execution, and so is any
//     well-formed name with no operator behind it.
//
//   - SELECT, UsersID, "*" is "users.id * ...", not "SELECT users.id, *". The
//     operator position is read first, which is how SQL reads it too, and the
//     DSL has no comma token to separate the two. SELECT, "*" and F("COUNT",
//     "*") are unaffected.
//
//   - ",", "(" and ")" are bound as values: none of them is an operator
//     character, and parentheses are P.
//
//   - SET strips the table qualifier from the name that begins each assignment,
//     since "SET users.status = ..." is not legal. This is the one rule the
//     package has that SQL does not, kept because the qualified column constant
//     is the one already at hand. It applies at that grammar position only, so
//     the expression to the right of the "=" keeps its qualifiers and so does
//     the column list of INSERT.
//
//   - An alias must be written with AS. SELECT, UsersID, sb.I("x") emits
//     "SELECT users.id, x", never "SELECT users.id AS x". SQL allows the
//     implicit form and the DSL does not, because the two are the same token
//     sequence: the DSL has no comma token, so both readings are valid SQL and
//     a heuristic would reject the ordinary "SELECT users.id, name".
//
//   - The package is only as useful as its coverage. There is no escape hatch
//     for an unmodelled clause: the answer is to model it. RawExpr covers an
//     unmodelled expression, which is where the open-ended part of SQL sits.
//
//   - RawExpr is one opaque expression. The parser checks where it may appear
//     and never looks inside it, so a fragment that contains a comma or a
//     whole clause can still break a statement.
//
//   - No type safety and no semantic checking. Whether a column exists and
//     whether two types unify are the database's business.
//
//   - RawExpr performs no escaping at all. Pass only constants, or strings
//     assembled in code.
//
//   - Aliasing a table needs its own I constant. There is no Users.As("u").
//
//   - Nothing here has been run against a real database. The tests check the
//     generated string and the bound args, nothing more.
package sb

import (
	"fmt"
	"strings"

	"github.com/fujidaiti/psqlb/internal/tok"
)

// ===========================================================================
// Tokens
// ===========================================================================

// Token is a single token of a statement. The set is closed: a keyword, an
// operator, an identifier, a value, a group and a hand-written fragment. A
// token is data that the parser inspects; it does not render itself, because
// what it renders depends on where it is written.
//
// It gates nothing at compile time. Every position in the DSL takes an any,
// and normalization decides what an item that is not already a token becomes.
// Token is still the type Arg and RawExpr return, and it is still the set the
// parser accepts, but a reusable fragment is a []any and not a []Token: Go
// will not spread the latter into a variadic any.
type Token = tok.Token

// I is an identifier: a table name, a column name, an alias, or a simple type
// name. Its underlying type is string so that it can be declared with const.
// The text is emitted verbatim; nothing is quoted and nothing is validated.
type I string

func (I) SQLToken() {}

// value is what Arg and normalization produce.
type value struct{ v any }

func (value) SQLToken() {}

// Arg binds a value and produces its placeholder. A plain Go value is bound
// already, so this is not how a value is normally written: it is the override
// for the two cases normalization gets wrong.
//
// The first is a value made only of operator characters, which the lexical
// rule would take for an operator:
//
//	LIKE, Arg("%") // users.name LIKE $1
//
// The second is a []any, which is taken for a fragment that was meant to be
// spread.
func Arg(v any) Token { return value{v} }

// rawFrag is what RawExpr produces. The fragment is split at its "$0" markers
// when it is written, so a malformed fragment is caught at the call rather than
// at build time; err carries that failure until ToSQL is reached.
type rawFrag struct {
	parts []string
	vals  []any
	err   error
}

func (rawFrag) SQLToken() {}

// RawExpr is an expression written by hand: the escape hatch for an expression
// this package does not model. It is one complete expression wherever an
// expression may begin, and the parser never looks inside the string.
//
// Each "$0" in the fragment becomes the placeholder for the next value, so the
// values stay where they are written instead of being split into separate
// tokens.
//
//	RawExpr("users.meta->'profile'->>'city' = $0", "Tokyo")
//	// users.meta->'profile'->>'city' = $2   (if the fragment came second)
//
// A fragment cannot know that number in advance, which is why it does not write
// one. Only "$0" marks a value, and any other number is an error: it would
// otherwise produce a query that runs and quietly reads another clause's value.
// A marker count that does not match the value count is an error too.
//
// RawExpr does not look inside quoted regions, because a fragment is a piece of
// a statement and may begin or end inside one. The consequence is that a "$"
// followed by digits cannot appear as literal text. Dollar quoting itself still
// works, since only a digit after the "$" makes a marker, so "$$body$$" and
// "$tag$body$tag$" pass through untouched.
//
// No escaping is performed at all. Pass only constants, or strings assembled in
// code.
func RawExpr(sql string, vals ...any) Token {
	parts, err := splitMarkers(sql)
	cp := dup(vals)
	if err == nil && len(parts)-1 != len(cp) {
		err = fmt.Errorf("psqlb: RawExpr: fragment has %d $0 marker(s) but %d value(s): %s",
			len(parts)-1, len(cp), sql)
	}
	return rawFrag{parts: parts, vals: cp, err: err}
}

// ===========================================================================
// Groups
// ===========================================================================

// Group is a parenthesised list of tokens. What a group means is decided by the
// position it is written in and not by the group itself: after FROM it is a
// subquery, after IN it is an expression list or a subquery, in an expression
// it is a parenthesised expression or a row constructor, and after VALUES it is
// one row.
type Group struct {
	// name is emitted immediately before the opening parenthesis: "" for P and
	// the function name for F.
	name string
	// items came from normalize, like every []Token in this package, so it
	// holds no Go nil. That is what lets nil mean the end of the input and
	// nothing else while the parser reads it.
	items []Token
}

func (Group) SQLToken() {}

// P builds a group. It is the only source of parentheses in the generated SQL,
// so a subquery, a grouped condition, a row constructor, an IN list and a
// window specification are all spelled the same way. A statement is written
// with ToSQL instead, because it is the one thing that is not parenthesised.
//
//	P(SELECT, UsersID, FROM, Users)    // (SELECT users.id FROM users)
//	P(UsersIsPaid, OR, UsersHasTicket) // (users.paid OR users.has_ticket)
//	P("active", "trial")               // ($1, $2)
func P(items ...any) Group { return Group{items: normalize(items)} }

// F is a function call. It covers any SQL function, which is what spares the
// package from growing separate COUNT, SUM and COALESCE helpers.
//
//	F("COUNT", "*")               // COUNT(*)
//	F("COUNT", DISTINCT, UsersID) // COUNT(DISTINCT users.id)
func F(name string, items ...any) Group {
	return Group{name: name, items: normalize(items)}
}

// ===========================================================================
// The statement
// ===========================================================================

// ToSQL parses items as a complete statement and returns the SQL text and the
// values bound to its $N placeholders.
//
// This is the outermost form, and the only one: a statement is not
// parenthesised, so it is not written with P. Every group inside it is.
//
//	ToSQL(SELECT, UsersID, FROM, Users, WHERE, UsersAge, GTE, V(18))
//	// SELECT users.id FROM users WHERE users.age >= $1, args=[18]
func ToSQL(items ...any) (string, []any, error) {
	e := &emitter{}
	p := &parser{toks: normalize(items), e: e}
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

// dup copies the values a RawExpr was given, so that the fragment cannot
// change afterwards.
func dup(vs []any) []any {
	cp := make([]any, len(vs))
	copy(cp, vs)
	return cp
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
			return nil, fmt.Errorf("psqlb: RawExpr: fragment contains $%s, but only $0 marks a value: %s",
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
func unqualify(i I) string {
	s := string(i)
	if n := strings.LastIndex(s, "."); n >= 0 {
		return s[n+1:]
	}
	return s
}
