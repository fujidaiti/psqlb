// Package sb builds Postgres statements from a flat list of tokens.
//
// A statement is written as one sequence of tokens, in the order the SQL reads:
//
//	sb.S(
//		SELECT, UsersID, UsersName,
//		FROM, Users,
//		WHERE, sb.S(UsersCreated, UsersID), LT, sb.S(sb.Lit("2025-06-01"), sb.Lit(500)),
//		ORDER_BY, UsersCreated, DESC, UsersID, DESC, NULLS_LAST,
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
// Note where the commas landed. After SELECT, FROM and ORDER BY the items are
// joined with commas, WHERE returns to spaces, and the package works out for
// itself that DESC and NULLS LAST belong to the key before them rather than
// starting a new one. Producing that is the whole job of this package, and the
// next section is how it is done.
//
// # The two packages
//
// The SQL keywords live in package psqlb and everything else lives here. A
// user dot-imports the keywords, so that they read as SQL, and reaches this
// package through its name:
//
//	import (
//		. "github.com/fujidaiti/psqlb"
//		"github.com/fujidaiti/psqlb/sb"
//	)
//
// That is the whole reason for the split. Dot-importing one package that held
// both would also put S, Func, Id, Lit and Raw into the user's file scope,
// where they collide with ordinary Go names and no longer look different from
// the SQL vocabulary.
//
// The keyword types that the constants are declared with (ListKw, PrefixKw and
// the rest) are in internal/kw, which neither package re-exports. A user
// therefore cannot declare a keyword of their own; a keyword this package does
// not model is written with Op or Kw.
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
//     something must be written for them in the DSL: S, which is the only
//     group there is. Parentheses are never added or removed on the reader's
//     behalf.
//
// SQL has one kind of parenthesis, and what separates the items inside it is
// decided by the keywords, not by the parentheses. S is therefore one
// constructor rather than two: it starts comma-separated and a clause keyword
// switches it to spaces, exactly as in the middle of any token list.
//
// A keyword that SQL always follows with one parenthesised group, such as IN or
// OVER, is a constant like any other, and the group after it is written with S.
// Nothing checks that the group is there: IN, Lit(1), Lit(2) compiles and
// produces "IN $1, $2", in the same way that writing "IN 'a', 'b'" in SQL is
// accepted by the Go compiler and rejected by Postgres. Getting the parentheses
// right is the writer's job, exactly as it is when writing SQL by hand.
//
// DISTINCT_ON is the one keyword that still carries its own parentheses, for a
// reason recorded on it.
//
// There are two other standing exceptions, both to rule 2, both kept for
// usability and both recorded where they are declared: SET strips the table
// qualifier from the name that begins each assignment, and EXCLUDED consumes the
// Id after it.
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
//	SELECT, UsersAge, GTE, sb.Lit(18)      // infix:   users.age >= $1
//
// The remaining two choose the separator. A list keyword opens a
// comma-separated list; a clause keyword closes it and returns to spaces:
//
//	SELECT, UsersID, UsersName, FROM, Users, WHERE, UsersID, EQ, sb.Lit(1)
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
//	Shape in SQL                 | Shape in Go              | Example
//	-----------------------------|--------------------------|-----------------------------------
//	Statement, or any group      | sb.S(...)                | sb.S(x, y)        -> (x, y)
//	                             |                          | sb.S(WHERE, a, OR, b) -> (WHERE a OR b)
//	Any keyword                  | Constant                 | SELECT FROM WHERE IN OVER
//	Infix operator               | Constant or sb.Op()      | EQ LIKE, sb.Op("@>")
//	Function call                | sb.Func(name, ...)       | sb.Func("COUNT", STAR)
//	Identifier                   | sb.Id constant           | UsersID
//	Value                        | sb.Lit()                 | sb.Lit(42)
//	Anything unmodelled          | sb.Raw() sb.Op() sb.Kw() | sb.Raw("x = $0", 1)
//
// Every keyword is a constant. DISTINCT_ON is the sole function, for the reason
// recorded on it.
//
// # Parentheses
//
// S is the only group. It is bare at the outermost level and parenthesised
// whenever it is nested, so nesting in Go is nesting in SQL:
//
//	sb.S(a, sb.S(WHERE, b, OR, c))  // a, (WHERE b OR c)
//	FROM, sb.S(sub), AS, sb.Id("x") // FROM (SELECT ...) AS x
//
// Its items are comma-separated to begin with, and a clause keyword switches it
// to spaces. That is the same rule that governs the middle of a token list, so
// there is nothing extra to know about the head of a group:
//
//	sb.S(sb.Lit("active"), sb.Lit("trial"))     // ($1, $2)
//	sb.S(WHERE, UsersIsPaid, OR, UsersHasTicket) // (WHERE users.paid OR users.has_ticket)
//	sb.S(SELECT, OrdersUserID, FROM, Orders)     // (SELECT orders.user_id FROM orders)
//
// There is no way to nest an S without parentheses, and no automatic removal of
// them. To reuse a fragment where parentheses are unwanted, keep it as a
// []Clause and spread it:
//
//	inner := []sb.Clause{SELECT, OrdersUserID, FROM, Orders}
//	sb.S(SELECT, STAR, FROM, Users, WHERE, UsersID, IN, sb.S(inner...))
//
// One more level of nesting is one more pair of parentheses, and after a keyword
// like IN that chooses the query. Both are legal SQL:
//
//	UsersID, IN, sb.S(SELECT, OrdersUserID, FROM, Orders) // IN (SELECT ...)   membership
//	UsersID, IN, sb.S(sb.S(inner...))                     // IN ((SELECT ...)) one-element list
//
// The second compares against a list of one scalar subquery, so it fails at
// execution if the subquery returns more than one row.
//
// Because S and Func run the same token walker, a multi-token item needs no
// wrapper of its own:
//
//	sb.Func("string_agg", UsersName, sb.Lit(","), ORDER_BY, UsersID) // string_agg(users.name, $1 ORDER BY users.id)
//	sb.Func("COUNT", DISTINCT, UsersID)                             // COUNT(DISTINCT users.id)
//
// An empty group is still a pair of parentheses, since parentheses are always
// rendered:
//
//	sb.Kw("GROUPING SETS"), sb.S(sb.S(UsersID), sb.S()) // GROUPING SETS ((users.id), ())
//
// An optional token is written nil rather than sb.S(): nil renders nothing and
// is skipped without leaving a dangling comma, which sb.S() no longer is.
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
// An empty render is not an error either: a nil token and Id("") both produce
// nothing and are skipped without leaving a dangling comma, so an optional token
// can be left in place. S() is not one of them: an empty group still renders its
// parentheses.
//
// # Known limitations
//
//   - No type safety and no arity checking. SELECT, FROM, Users produces
//     "SELECT FROM users", which only Postgres rejects.
//
//   - A keyword's kind is fixed, so a keyword serving two roles needs two
//     spellings. NOT is the one case: it is a prefix, correct in WHERE NOT a,
//     NOT, EXISTS, S(...) and SELECT NOT flag, but wrong as a modifier inside
//     a comma-separated list. There, write Op("NOT IN"), S(...).
//
//   - Nothing checks that a keyword needing a parenthesised group has one.
//     IN, Lit(1) produces "IN $1", which only Postgres rejects.
//
//   - A fragment that must attach to the token before it, in the middle of a
//     comma-separated list, has no kind of its own. Fold it into one Raw.
//
//   - A reusable multi-token fragment cannot be an S where parentheses
//     are unwanted. Keep it as a []Clause and spread it.
//
//   - SET (a, b) = (1, 2) does not strip table qualifiers, because the token
//     that begins the item is a group rather than an Id. Write Id("a"), Id("b").
//     The column lists of INSERT_INTO and ON_CONFLICT do not strip them either,
//     since they are ordinary groups. Only SET does, which is inconsistent; it is
//     the price of keeping that one convenience.
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
package sb

import (
	"fmt"
	"strings"

	"github.com/fujidaiti/psqlb/internal/kw"
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

// token is a token whose text is not known until build time: Lit and Raw. Both
// are operands, which is what kw.KindOf returns for anything that does not say
// otherwise, so it carries no kind of its own.
type token func([]any) (string, []any, error)

func (t token) BuildSQL(args []any) (string, []any, error) { return t(args) }

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

// walk renders a token list. Every group starts comma-separated, and a clause
// keyword switches it to spaces, which is the same rule that governs the middle
// of a list. There is one starting mode rather than two because a statement
// never observes it: it begins with a list or a clause keyword, which sets the
// mode on the first token.
//
// Each token is rendered before its separator is chosen, and a token that
// renders empty is skipped without advancing any state. That ordering is what
// keeps a nil token from leaving a dangling comma behind it.
func walk(items []Clause, args []any) (string, []any, error) {
	m := listMode
	var b strings.Builder
	pos := posHead
	inSet := false

	for i := 0; i < len(items); i++ {
		var (
			k   = kw.KindOperand
			sql string
			err error
		)

		switch t := items[i].(type) {
		case nil:
			// An optional token left in place. It produces nothing.
			continue

		case kw.ExcludedKw:
			// EXCLUDED.name is one operand spelled as two tokens. Reading the
			// next token here, as a typed value, is why it needs no mode
			// threaded through BuildSQL.
			if i+1 >= len(items) {
				return "", args, fmt.Errorf("psqlb: EXCLUDED is the last token, but it must be followed by an Id")
			}
			col, ok := items[i+1].(Id)
			if !ok {
				return "", args, fmt.Errorf("psqlb: EXCLUDED must be followed by an Id, got %T", items[i+1])
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
			// the outermost level, so a Statement seen here is always nested and
			// always parenthesised, empty or not.
			k = t.kind
			sql, args, err = t.BuildSQL(args)
			if err != nil {
				return "", args, err
			}
			sql = t.name + "(" + sql + ")"

		default:
			k = kw.KindOf(items[i])
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
			case kw.KindOperand, kw.KindPrefix:
				if m == listMode && pos == posNext {
					sep = comma
				}
			}
			b.WriteString(sep)
		}
		b.WriteString(sql)

		switch k {
		case kw.KindList:
			m, pos = listMode, posHead
			_, inSet = items[i].(kw.SetKw)
		case kw.KindClause:
			m, pos, inSet = spaceMode, posGlue, false
		case kw.KindPrefix, kw.KindInfix:
			pos = posGlue
		case kw.KindOperand, kw.KindPostfix:
			pos = posNext
		}
	}

	return b.String(), args, nil
}

// ===========================================================================
// The group
// ===========================================================================

// Statement is a group of tokens: the whole statement at the outermost level, a
// parenthesised group anywhere below it.
type Statement struct {
	// name is the text emitted immediately before the opening parenthesis: ""
	// for S, the function name for Func and the keyword plus a space for
	// PrefixGroup.
	name  string
	kind  kw.Kind
	items []Clause
}

// S builds a group. It is the only way to write a statement and the only way to
// write parentheses, so a subquery, a grouped condition, a row constructor, an
// IN list, a window specification and a CTE body are all spelled the same way.
// The keywords inside it decide whether its items are joined with commas or
// with spaces.
//
//	S(SELECT, UsersID, UsersName)      // SELECT users.id, users.name
//	S(a, S(WHERE, b, OR, c))           // a (WHERE b OR c)
//	S(Lit("active"), Lit("trial"))     // ($1, $2)
func S(items ...Clause) Statement { return Statement{items: dup(items)} }

func (s Statement) SQLKind() kw.Kind { return s.kind }

func (s Statement) BuildSQL(args []any) (string, []any, error) {
	return walk(s.items, args)
}

// ToSQL assembles the whole statement. No parentheses wrap the outermost level.
func (s Statement) ToSQL() (string, []any, error) { return s.BuildSQL(nil) }

// PrefixGroup renders items as a parenthesised group with the given keyword in
// front of it, as a prefix token. DISTINCT_ON is its only caller, and it is
// exported for that reason alone.
func PrefixGroup(keyword string, items ...Clause) Clause {
	return Statement{name: keyword + " ", kind: kw.KindPrefix, items: dup(items)}
}

// Func covers any SQL function, which is what spares the package from growing
// separate COUNT, SUM and COALESCE helpers. An argument spanning several tokens
// needs no wrapper.
//
//	Func("COUNT", STAR)                                       // COUNT(*)
//	Func("COUNT", DISTINCT, UsersID)                          // COUNT(DISTINCT users.id)
//	Func("string_agg", UsersName, Lit(","), ORDER_BY, UsersID) // string_agg(users.name, $1 ORDER BY users.id)
func Func(name string, items ...Clause) Clause {
	return Statement{name: name, items: dup(items)}
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
//	S(WHERE, UsersAge, GTE, Lit(18)) // WHERE users.age >= $1
func Lit(v any) Clause {
	return token(func(args []any) (string, []any, error) {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args)), args, nil
	})
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
		err = fmt.Errorf("psqlb: Raw: fragment has %d $0 marker(s) but %d value(s): %s",
			len(parts)-1, len(cp), sql)
	}
	return token(func(args []any) (string, []any, error) {
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
	})
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

// Op is an infix operator written by hand. It exists because operators are not
// a fixed list: extensions add their own.
//
//	S(WHERE, UsersMeta, Op("@>"), Lit(`{"vip":true}`)) // WHERE users.meta @> $1
func Op(sql string) Clause { return kw.InfixKw(sql) }

// Kw is a clause keyword written by hand, for a clause the package does not
// model. Being a clause keyword, it closes any open comma-separated list, which
// is what an unmodelled clause almost always wants.
//
//	Kw("ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW")
func Kw(sql string) Clause { return kw.ClauseKw(sql) }

func unqualify(i Id) string {
	s := string(i)
	if n := strings.LastIndex(s, "."); n >= 0 {
		return s[n+1:]
	}
	return s
}
