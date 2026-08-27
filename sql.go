// Package sql builds Postgres statements from a flat list of tokens.
//
// A statement is written as one sequence of tokens, in the order the SQL reads:
//
//	Stm(
//		SELECT, UsersID, UsersName,
//		FROM, Users,
//		WHERE, Row(UsersCreated, UsersID), LT, Row(Lit("2025-06-01"), Lit(500)),
//		ORDER_BY, UsersCreated, DESC, UsersID, DESC, NULLS_LAST,
//		LIMIT, Lit(20),
//	)
//
//	// SELECT users.id, users.name
//	// FROM users
//	// WHERE (users.created_at, users.id) < ($1, $2)
//	// ORDER BY users.created_at DESC, users.id DESC NULLS LAST
//	// LIMIT $3
//	// args=[2025-06-01 500 20]
//
// Note where the commas landed. Stm joins with spaces, except that after
// SELECT, FROM and ORDER BY it joins the items of the list with commas, and it
// works out for itself that DESC and NULLS LAST belong to the key before them
// rather than starting a new one. Producing that is the whole job of this
// package, and the next section is how it is done.
//
// The DSL is meant to be dot-imported, which is why every exported name is
// upper-case:
//
//	import . "github.com/fujidaiti/psqlb"
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
//     something must be written for them in the DSL: Stm for a
//     space-separated group, Row for a comma-separated one. Parentheses are
//     never added or removed on the reader's behalf.
//
// The exception to rule 3 is a keyword that SQL itself always follows with one
// parenthesised group, such as IN or OVER. There the parentheses belong to the
// keyword rather than to the expression inside them, so the function call that
// spells the keyword also spells its parentheses.
//
// # Token kinds
//
// Every token has a kind, and the kind is its Go type. There are six. Four of
// them are the product of two independent questions — does this token begin a
// new item of a comma-separated list, and does the token after it continue the
// same item:
//
//	                  | clears glue | sets glue
//	  begins an item  | operand     | prefix
//	  continues one   | postfix     | infix
//
//	SELECT, UsersID, UsersName          // operand: users.id, users.name
//	SELECT, DISTINCT, UsersID           // prefix:  DISTINCT users.id
//	ORDER_BY, UsersID, DESC, UsersName  // postfix: users.id DESC, users.name
//	SELECT, UsersAge, GTE, Lit(18)      // infix:   users.age >= $1
//
// The remaining two choose the separator. A list keyword opens a
// comma-separated list; a clause keyword closes it and returns to spaces:
//
//	SELECT, UsersID, UsersName, FROM, Users, WHERE, UsersID, EQ, Lit(1)
//	//     `------ list ------'        `-'         `-------------'
//	//     SELECT opens              FROM opens   WHERE closes
//
// Neither ever takes a comma itself. Only operands and prefixes do, and only
// when a list is open and the previous item has ended. That is the entire rule.
//
// It is why a CASE expression can sit in the middle of a SELECT list without
// destroying it: CASE is a prefix, WHEN, THEN and ELSE are infixes and END is a
// postfix, so none of them opens a list or takes a comma.
//
// # Shapes
//
//	Shape in SQL                 | Shape in Go       | Example
//	-----------------------------|-------------------|------------------------------------
//	Space-separated group        | Stm(...)          | Stm(a, OR, b)      -> (a OR b)
//	Comma-separated list         | Row(...)          | Row(x, y)          -> (x, y)
//	Keyword with mandatory ( )   | Function          | IN(...) EXISTS(...) OVER(...)
//	Any other keyword            | Constant          | SELECT FROM WHERE ORDER_BY
//	Infix operator               | Constant or Op()  | EQ LIKE, Op("@>")
//	Function call                | FUNC(name, ...)   | FUNC("COUNT", STAR)
//	Identifier                   | Id constant       | UsersID
//	Value                        | Lit()             | Lit(42)
//	Anything unmodelled          | Raw() Op() Kw()   | Raw("x = $0", 1)
//
// A keyword is a function exactly when SQL always follows it with one
// parenthesised group, or when it takes a bare name rather than an expression.
// Everything else is a constant.
//
// # Parentheses
//
// Stm and Row are the two groups, and their names say which separator you get.
// Stm joins with spaces, Row with commas. Row is always parenthesised. Stm is
// parenthesised whenever it is nested and bare at the outermost level, so
// nesting in Go is nesting in SQL:
//
//	Stm(a, Stm(b, OR, c))      // a (b OR c)
//	FROM, Stm(sub), AS, Id("x")  // FROM (SELECT ...) AS x
//
// There is no way to nest a Stm without parentheses, and no automatic removal
// of them. To reuse a fragment where parentheses are unwanted, keep it as a
// []Clause and spread it:
//
//	inner := []Clause{SELECT, OrdersUserID, FROM, Orders}
//	Stm(WHERE, UsersID, IN(inner...))    // WHERE users.id IN (SELECT orders.user_id FROM orders)
//	Stm(WHERE, UsersID, IN(Stm(inner...))) // WHERE users.id IN ((SELECT orders.user_id FROM orders))
//
// Both compile and both run, but they are different queries. The first is a
// membership test. The second compares against a one-element list holding a
// scalar subquery, so it fails at execution if the subquery returns more than
// one row. Spread for membership.
//
// Because Row and the paren-emitting keyword functions run the same token
// walker, a multi-token item needs no wrapper. A leading list keyword takes
// over the separator, which is what lets one function serve both a list and a
// subquery:
//
//	IN(Lit("active"), Lit("trial"))                          // IN ($1, $2)
//	IN(SELECT, OrdersUserID, FROM, Orders)                   // IN (SELECT orders.user_id FROM orders)
//	FUNC("string_agg", UsersName, Lit(","), ORDER_BY, UsersID) // string_agg(users.name, $1 ORDER BY users.id)
//	FUNC("COUNT", DISTINCT, UsersID)                         // COUNT(DISTINCT users.id)
//
// # Values and $N
//
// A plain Go value is not a token. It enters a statement only through Lit,
// which binds it and produces its placeholder, or through the "$0" markers of
// Raw. That is what keeps SELECT("id") and a forgotten Id from compiling.
//
// Placeholders are numbered in expansion order. Each clause receives the args
// bound so far and returns them with its own appended, so the number a value
// gets is decided where it is written, with no counter and no renumbering pass.
// A nested statement therefore numbers correctly at any depth.
//
// # Errors
//
// ToSQL returns an error, and the package never panics. An error is reported
// only where the alternative is emitting a string Postgres rejects:
//
//   - A Raw fragment whose "$0" marker count does not match its value count.
//   - EXCLUDED not followed by an Id.
//
// One further case is reported although the string would run, because that is
// exactly the problem: a Raw fragment containing "$N" for any N other than 0
// silently reads another clause's value. A fragment cannot know its own
// position, so such a number is always a mistake.
//
// Nothing else is checked. Arity, types and syntax are the database's business.
// An empty render is not an error either: a nil token, Stm(), and Id("") all
// produce nothing and are skipped without leaving a dangling comma, so an
// optional token can be left in place.
//
// # Known limitations
//
//   - No type safety and no arity checking. SELECT, FROM, Users produces
//     "SELECT FROM users", which only Postgres rejects.
//
//   - A keyword's kind is fixed, so a keyword serving two roles needs two
//     spellings. NOT is the one case: it is a prefix, correct in WHERE NOT a,
//     NOT, EXISTS(...) and SELECT NOT flag, but wrong as a modifier inside a
//     comma-separated list. There, write Op("NOT IN"), Row(...).
//
//   - A fragment that must attach to the token before it, in the middle of a
//     comma-separated list, has no kind of its own. Fold it into one Raw.
//
//   - A reusable multi-token fragment cannot be a Statement where parentheses
//     are unwanted. Keep it as a []Clause and spread it.
//
//   - SET (a, b) = (1, 2) does not strip table qualifiers, because the token
//     that begins the item is a Row rather than an Id. Write Id("a"), Id("b").
//
//   - Raw performs no escaping at all. Pass only constants, or strings
//     assembled in code.
//
//   - Aliasing a table needs its own Id constant. There is no Users.As("u").
//
//   - The keyword vocabulary is a small subset of Postgres. Anything missing is
//     written with Raw, Op or Kw.
//
//   - Nothing here has been run against a real database. The tests check the
//     generated string and the bound args, nothing more.
package sql

import (
	"fmt"
	"strings"
)

// ===========================================================================
// Core
// ===========================================================================

// Clause is a fragment of SQL. It takes the args bound so far and returns the
// fragment plus those args with the values this fragment bound appended.
// Passing args along this bucket brigade is the entirety of the $N numbering
// scheme.
type Clause interface {
	BuildSQL(args []any) (string, []any, error)
}

// kind is what the walker needs to know about a token: whether it takes a comma
// and what the token after it does. See the package doc.
type kind int

const (
	kindOperand kind = iota // begins an item, clears glue
	kindPrefix              // begins an item, sets glue
	kindPostfix             // continues an item, clears glue
	kindInfix               // continues an item, sets glue
	kindList                // opens a comma-separated list
	kindClause              // closes it, back to space-separated
)

// kinded is implemented by every token that is not an operand. Id, Statement,
// EXCLUDED and any Clause implemented outside this package do not implement it,
// and are operands: that is the common case, so it is the default.
type kinded interface{ sqlKind() kind }

func kindOf(c Clause) kind {
	if k, ok := c.(kinded); ok {
		return k.sqlKind()
	}
	return kindOperand
}

// The five keyword types. Their underlying type is string so that the keywords
// below can be declared with const, which is what lets the walker read a
// token's kind before building it.
type (
	operandKw string
	prefixKw  string
	postfixKw string
	infixKw   string
	listKw    string
	clauseKw  string

	// setKw is a list keyword that also opens the scope in which a
	// table-qualified column name is emitted bare. SET is its only value.
	setKw string

	// excludedKw consumes the token after it. EXCLUDED is its only value.
	excludedKw string
)

func (k operandKw) sqlKind() kind { return kindOperand }
func (k prefixKw) sqlKind() kind  { return kindPrefix }
func (k postfixKw) sqlKind() kind { return kindPostfix }
func (k infixKw) sqlKind() kind   { return kindInfix }
func (k listKw) sqlKind() kind    { return kindList }
func (k clauseKw) sqlKind() kind  { return kindClause }
func (k setKw) sqlKind() kind     { return kindList }

func (k operandKw) BuildSQL(args []any) (string, []any, error) { return string(k), args, nil }
func (k prefixKw) BuildSQL(args []any) (string, []any, error)  { return string(k), args, nil }
func (k postfixKw) BuildSQL(args []any) (string, []any, error) { return string(k), args, nil }
func (k infixKw) BuildSQL(args []any) (string, []any, error)   { return string(k), args, nil }
func (k listKw) BuildSQL(args []any) (string, []any, error)    { return string(k), args, nil }
func (k clauseKw) BuildSQL(args []any) (string, []any, error)  { return string(k), args, nil }
func (k setKw) BuildSQL(args []any) (string, []any, error)     { return string(k), args, nil }

// EXCLUDED and Id satisfy Clause so that they can be written as tokens, but the
// walker intercepts both types before building them: EXCLUDED to read the name
// after it, Id to strip a qualifier inside SET. These two methods are therefore
// never reached from walk.
func (k excludedKw) BuildSQL(args []any) (string, []any, error) { return string(k), args, nil }

// token is every token whose text is not known until build time: Lit, Raw, Row,
// FUNC and the paren-emitting keyword functions. It carries its kind explicitly
// because it has no dedicated type to carry it.
type token struct {
	k     kind
	build func([]any) (string, []any, error)
}

func (t token) sqlKind() kind { return t.k }

func (t token) BuildSQL(args []any) (string, []any, error) { return t.build(args) }

// dup is generic because Raw holds values, []any, while everything else holds
// tokens, []Clause.
func dup[T any](vs []T) []T {
	cp := make([]T, len(vs))
	copy(cp, vs)
	return cp
}

// ===========================================================================
// The walker
// ===========================================================================

type mode int

const (
	spaceMode mode = iota
	listMode
)

// position says what the next operand does. It is the only state a comma
// decision depends on, and it matters only while a list is open.
type position int

const (
	posHead position = iota // the first item; no comma
	posGlue                 // continues the current item; no comma
	posNext                 // begins a new item; comma first
)

const (
	comma = ", "
	space = " "
)

// walk renders a token list. m is the separator it starts with: spaceMode for
// Stm, listMode for Row, FUNC and the paren-emitting keyword functions. A
// leading list keyword overrides it, which is what lets IN(...) hold either a
// list or a subquery.
//
// Each token is rendered before its separator is chosen, and a token that
// renders empty is skipped without advancing any state. That ordering is what
// keeps a nil token from leaving a dangling comma behind it.
func walk(items []Clause, m mode, args []any) (string, []any, error) {
	var b strings.Builder
	pos := posHead
	inSet := false

	for i := 0; i < len(items); i++ {
		var (
			k   = kindOperand
			sql string
			err error
		)

		switch t := items[i].(type) {
		case nil:
			// An optional token left in place. It produces nothing.
			continue

		case excludedKw:
			// EXCLUDED.name is one operand spelled as two tokens. Reading the
			// next token here, as a typed value, is why it needs no mode
			// threaded through BuildSQL.
			if i+1 >= len(items) {
				return "", args, fmt.Errorf("sql: EXCLUDED is the last token, but it must be followed by an Id")
			}
			col, ok := items[i+1].(Id)
			if !ok {
				return "", args, fmt.Errorf("sql: EXCLUDED must be followed by an Id, got %T", items[i+1])
			}
			i++
			sql = "EXCLUDED." + unqualify(col)

		case Id:
			// The item-initial name of a SET assignment is emitted bare, since
			// "SET users.status = ..." is not legal. A name in any other
			// position keeps its qualifier, which is what leaves the right-hand
			// side of the assignment alone.
			if inSet && (pos == posHead || pos == posNext) {
				sql = unqualify(t)
			} else {
				sql = string(t)
			}

		case Statement:
			// Nesting in Go is nesting in SQL. walk is only ever reached below
			// the outermost level, so a Statement seen here is always nested.
			sql, args, err = t.BuildSQL(args)
			if err != nil {
				return "", args, err
			}
			if sql != "" {
				sql = "(" + sql + ")"
			}

		default:
			k = kindOf(items[i])
			sql, args, err = items[i].BuildSQL(args)
			if err != nil {
				return "", args, err
			}
		}

		if sql == "" {
			continue
		}

		if b.Len() > 0 {
			sep := space
			switch k {
			case kindOperand, kindPrefix:
				if m == listMode && pos == posNext {
					sep = comma
				}
			}
			b.WriteString(sep)
		}
		b.WriteString(sql)

		switch k {
		case kindList:
			m, pos = listMode, posHead
			_, inSet = items[i].(setKw)
		case kindClause:
			m, pos, inSet = spaceMode, posGlue, false
		case kindPrefix, kindInfix:
			pos = posGlue
		case kindOperand, kindPostfix:
			pos = posNext
		}
	}

	return b.String(), args, nil
}

// ===========================================================================
// The two groups
// ===========================================================================

// Statement is a space-separated group of tokens.
type Statement struct{ items []Clause }

// Stm builds a space-separated group. It is the only way to write a statement,
// and it is parenthesised whenever it is nested, so a subquery, a grouped
// condition, a window specification and a CTE body are all spelled the same
// way. Use Row where a comma-separated list is wanted.
//
//	Stm(a, Stm(b, OR, c))         // a (b OR c)
//	Stm(SELECT, UsersID, UsersName) // SELECT users.id, users.name
func Stm(items ...Clause) Statement { return Statement{items: dup(items)} }

func (s Statement) BuildSQL(args []any) (string, []any, error) {
	return walk(s.items, spaceMode, args)
}

// ToSQL assembles the whole statement. No parentheses wrap the outermost level.
func (s Statement) ToSQL() (string, []any, error) { return s.BuildSQL(nil) }

// paren renders items in list mode inside parentheses, with kw in front of them
// if there is one. Every parenthesised construct in the package goes through it.
func paren(kw string, k kind, items []Clause) Clause {
	cp := dup(items)
	return token{k: k, build: func(args []any) (string, []any, error) {
		s, args, err := walk(cp, listMode, args)
		if err != nil {
			return "", args, err
		}
		if kw == "" {
			return "(" + s + ")", args, nil
		}
		return kw + " (" + s + ")", args, nil
	}}
}

// Row is a comma-separated parenthesised list. A row constructor, a row of
// VALUES and the list in an IN are all spelled the same way.
//
//	Row(Lit("bob"), Lit(42))   // ($1, $2)
//	Row(UsersCreated, UsersID) // (users.created_at, users.id)
//
// Row() renders "()", which GROUPING SETS accepts.
func Row(items ...Clause) Clause { return paren("", kindOperand, items) }

// FUNC covers any SQL function, which is what spares the package from growing
// separate COUNT, SUM and COALESCE helpers. Its arguments are comma-separated,
// and an argument spanning several tokens needs no wrapper.
//
//	FUNC("COUNT", STAR)                                       // COUNT(*)
//	FUNC("COUNT", DISTINCT, UsersID)                          // COUNT(DISTINCT users.id)
//	FUNC("string_agg", UsersName, Lit(","), ORDER_BY, UsersID) // string_agg(users.name, $1 ORDER BY users.id)
func FUNC(name string, items ...Clause) Clause {
	cp := dup(items)
	return token{k: kindOperand, build: func(args []any) (string, []any, error) {
		s, args, err := walk(cp, listMode, args)
		if err != nil {
			return "", args, err
		}
		return name + "(" + s + ")", args, nil
	}}
}

// ===========================================================================
// Identifiers, values and hand-written fragments
// ===========================================================================

// Id is an identifier. Its underlying type is string so that it can be declared
// with const.
type Id string

func (i Id) BuildSQL(args []any) (string, []any, error) { return string(i), args, nil }

// Lit binds a value and produces its placeholder. It is the only way a value
// enters a statement, since every position in the DSL takes a Clause and a
// plain Go value is not one.
//
//	Stm(WHERE, UsersAge, GTE, Lit(18)) // WHERE users.age >= $1
func Lit(v any) Clause {
	return token{k: kindOperand, build: func(args []any) (string, []any, error) {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args)), args, nil
	}}
}

// Raw is an operand written by hand: the escape hatch for an expression this
// package does not model. Each "$0" in the fragment becomes the placeholder for
// the next value, so the values stay where they are written instead of being
// split into separate tokens.
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
func Raw(sql string, vals ...any) Clause {
	// Split once, here, so that the fragment is checked at the call rather than
	// at build time. parts holds the text around the markers, so there are
	// len(parts)-1 places for a value.
	parts, err := splitMarkers(sql)
	cp := dup(vals)
	if err == nil && len(parts)-1 != len(cp) {
		err = fmt.Errorf("sql: Raw: fragment has %d $0 marker(s) but %d value(s): %s",
			len(parts)-1, len(cp), sql)
	}
	return token{k: kindOperand, build: func(args []any) (string, []any, error) {
		if err != nil {
			return "", args, err
		}
		var b strings.Builder
		b.WriteString(parts[0])
		for i, part := range parts[1:] {
			args = append(args, cp[i])
			b.WriteString(fmt.Sprintf("$%d", len(args)))
			b.WriteString(part)
		}
		return b.String(), args, nil
	}}
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
			return nil, fmt.Errorf("sql: Raw: fragment contains $%s, but only $0 marks a value: %s",
				rest[:n], sql)
		}
		parts = append(parts, buf.String())
		buf.Reset()
		rest = rest[n:]
	}
	return append(parts, buf.String()), nil
}

// Op is an infix operator written by hand. It exists because operators are not
// a fixed list: extensions add their own.
//
//	Stm(WHERE, UsersMeta, Op("@>"), Lit(`{"vip":true}`)) // WHERE users.meta @> $1
func Op(sql string) Clause { return infixKw(sql) }

// Kw is a clause keyword written by hand, for a clause the package does not
// model. Being a clause keyword, it closes any open comma-separated list, which
// is what an unmodelled clause almost always wants.
//
//	Kw("ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW")
func Kw(sql string) Clause { return clauseKw(sql) }

// ===========================================================================
// Keyword constants
// ===========================================================================

// List keywords open a comma-separated list. Everything until the next clause
// keyword is joined with commas.
const (
	SELECT         listKw = "SELECT"
	FROM           listKw = "FROM"
	GROUP_BY       listKw = "GROUP BY"
	ORDER_BY       listKw = "ORDER BY"
	RETURNING      listKw = "RETURNING"
	PARTITION_BY   listKw = "PARTITION BY"
	WITH           listKw = "WITH"
	WITH_RECURSIVE listKw = "WITH RECURSIVE"
	VALUES         listKw = "VALUES"
)

// SET is a list keyword that also strips the table qualifier from the name that
// begins each assignment, since "SET users.status = ..." is not legal. The
// right-hand side is left alone.
//
//	Stm(UPDATE, Users, SET, UsersStatus, EQ, FUNC("upper", UsersName))
//	// UPDATE users SET status = upper(users.name)
const SET setKw = "SET"

// Clause keywords close a comma-separated list and return to space separation.
const (
	WHERE  clauseKw = "WHERE"
	HAVING clauseKw = "HAVING"
	ON     clauseKw = "ON"
	LIMIT  clauseKw = "LIMIT"
	OFFSET clauseKw = "OFFSET"

	UPDATE      clauseKw = "UPDATE"
	DELETE_FROM clauseKw = "DELETE FROM"

	JOIN       clauseKw = "JOIN"
	LEFT_JOIN  clauseKw = "LEFT JOIN"
	INNER_JOIN clauseKw = "INNER JOIN"
	CROSS_JOIN clauseKw = "CROSS JOIN"

	UNION     clauseKw = "UNION"
	UNION_ALL clauseKw = "UNION ALL"
	INTERSECT clauseKw = "INTERSECT"
	EXCEPT    clauseKw = "EXCEPT"

	DO_UPDATE  clauseKw = "DO UPDATE"
	DO_NOTHING clauseKw = "DO NOTHING"
)

// Infix tokens sit between two operands and keep the item open, so the operand
// after them takes no comma.
const (
	AND infixKw = "AND"
	OR  infixKw = "OR"

	EQ    infixKw = "="
	NE    infixKw = "<>"
	GT    infixKw = ">"
	GTE   infixKw = ">="
	LT    infixKw = "<"
	LTE   infixKw = "<="
	LIKE  infixKw = "LIKE"
	ILIKE infixKw = "ILIKE"
	CAST  infixKw = "::"

	// AS introduces an alias, and it is also how a CTE is named. The
	// parentheses a CTE body always has come from its being a nested Stm.
	//
	//	Stm(sub, AS, Id("n"))                     // (SELECT ...) AS n
	//	Stm(WITH, Id("tree"), AS, body, SELECT, …) // WITH tree AS (SELECT ...) SELECT …
	AS infixKw = "AS"

	// MATERIALIZED is infix rather than a clause keyword, so that it does not
	// close the WITH list and swallow the comma before the next CTE.
	MATERIALIZED infixKw = "MATERIALIZED"

	// WHEN, THEN and ELSE keep a CASE expression open, which is what lets one
	// sit in the middle of a SELECT list without breaking the commas.
	WHEN infixKw = "WHEN"
	THEN infixKw = "THEN"
	ELSE infixKw = "ELSE"
)

// Prefix tokens begin an item and keep it open, so the operand after them takes
// no comma.
const (
	// NOT is correct in WHERE NOT a, in NOT, EXISTS(...) and in SELECT NOT flag.
	// As a modifier inside a comma-separated list it is not; write
	// Op("NOT IN"), Row(...) there.
	NOT prefixKw = "NOT"

	DISTINCT prefixKw = "DISTINCT"

	// LATERAL is a constant rather than a function, because it is not always
	// followed by a parenthesised group of its own: in
	// JOIN LATERAL unnest(a) AS t it applies to a function call.
	LATERAL prefixKw = "LATERAL"

	CASE prefixKw = "CASE"
)

// Postfix tokens attach to the operand before them and end the item, so the
// operand after them starts a new one and takes a comma.
const (
	IS_NULL     postfixKw = "IS NULL"
	IS_NOT_NULL postfixKw = "IS NOT NULL"

	ASC         postfixKw = "ASC"
	DESC        postfixKw = "DESC"
	NULLS_FIRST postfixKw = "NULLS FIRST"
	NULLS_LAST  postfixKw = "NULLS LAST"

	END postfixKw = "END"
)

// Operands are complete expressions on their own.
const (
	STAR  operandKw = "*"
	TRUE  operandKw = "TRUE"
	FALSE operandKw = "FALSE"
	NULL  operandKw = "NULL"
)

// EXCLUDED reads the name after it, so EXCLUDED, UsersName renders
// EXCLUDED.name. The qualifier is stripped, since only the column name is legal
// there.
const EXCLUDED excludedKw = "EXCLUDED"

// ===========================================================================
// Keyword functions — the ones SQL always follows with one parenthesised group
// ===========================================================================

// IN takes either a list or a subquery. Spread a []Clause into it for a
// membership test; passing a single Stm nests it, which is a scalar comparison.
//
//	IN(Lit("active"), Lit("trial"))         // IN ($1, $2)
//	IN(SELECT, OrdersUserID, FROM, Orders)  // IN (SELECT orders.user_id FROM orders)
//
// It is a postfix: it swallows its own right operand, so relative to the tokens
// around it the whole of IN(...) attaches to the expression before it and ends
// the item. The same is true of FILTER and OVER.
func IN(items ...Clause) Clause { return paren("IN", kindPostfix, items) }

// FILTER restricts an aggregate. Its contents are a WHERE clause.
//
//	FUNC("COUNT", STAR), FILTER(WHERE, UsersIsPaid) // COUNT(*) FILTER (WHERE users.paid)
func FILTER(items ...Clause) Clause { return paren("FILTER", kindPostfix, items) }

// OVER carries a window specification, or the name of one.
//
//	OVER(PARTITION_BY, OrdersUserID, ORDER_BY, OrdersCreated)
//	// OVER (PARTITION BY orders.user_id ORDER BY orders.created_at)
func OVER(items ...Clause) Clause { return paren("OVER", kindPostfix, items) }

// EXISTS holds a subquery. It is an operand: it begins an item of its own, so
// NOT, EXISTS(...) takes no comma and SELECT, x, EXISTS(...) does.
func EXISTS(items ...Clause) Clause { return paren("EXISTS", kindOperand, items) }

// ANY holds a subquery or an array expression. "= ANY" does not accept a list.
func ANY(items ...Clause) Clause { return paren("ANY", kindOperand, items) }

// DISTINCT_ON is a prefix, so that it stays glued past its own parentheses to
// the first item of the SELECT list.
//
//	SELECT, DISTINCT_ON(UsersID), UsersID, UsersName
//	// SELECT DISTINCT ON (users.id) users.id, users.name
func DISTINCT_ON(items ...Clause) Clause { return paren("DISTINCT ON", kindPrefix, items) }

// ===========================================================================
// The positions that take a bare column name
// ===========================================================================

// Fixing the parameter type to Id keeps expressions and Raw out of a position
// where only a name is legal.

func unqualify(i Id) string {
	s := string(i)
	if n := strings.LastIndex(s, "."); n >= 0 {
		return s[n+1:]
	}
	return s
}

func unqualifiedList(cols []Id) string {
	if len(cols) == 0 {
		return ""
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = unqualify(c)
	}
	return " (" + strings.Join(names, ", ") + ")"
}

// INSERT_INTO names the table and its columns. With no columns it emits no
// parentheses, since "INSERT INTO users ()" is not legal.
//
//	INSERT_INTO(Users, UsersName, UsersEmail) // INSERT INTO users (name, email)
//
// It is a clause keyword, so the VALUES that follows opens its own list.
func INSERT_INTO(table Id, cols ...Id) Clause {
	return clauseKw("INSERT INTO " + string(table) + unqualifiedList(cols))
}

// ON_CONFLICT names the conflict target, or nothing at all.
//
//	ON_CONFLICT(UsersEmail) // ON CONFLICT (email)
//	ON_CONFLICT()           // ON CONFLICT
func ON_CONFLICT(cols ...Id) Clause {
	return clauseKw("ON CONFLICT" + unqualifiedList(cols))
}
