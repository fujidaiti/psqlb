// Package kw holds the SQL keywords and operators of the psqlb DSL, and
// nothing else. It is meant to be dot-imported, so that a statement reads like
// the SQL it produces:
//
//	import (
//		. "github.com/fujidaiti/psqlb/kw"
//		"github.com/fujidaiti/psqlb/sb"
//	)
//
//	sb.ToSQL(SELECT, UsersID, FROM, Users, WHERE, UsersAge, GTE, sb.V(18))
//
// Everything that is not a SQL keyword — ToSQL, P, F, I, V, RawExpr, RawOp —
// lives in package sb and is written with that prefix, so that dot-importing
// this package brings only SQL vocabulary into scope. Not even the type of
// these constants is exported from here, for the same reason: it is in
// internal/tok. The prefix is also what lets the names in sb be one letter. The
// design document is the package doc comment of sb.
//
// Every keyword is one word. A phrase is written as the words it is made of:
// GROUP, BY and IS, NOT, NULL and LEFT, OUTER, JOIN. The parser knows where
// each one may appear, so a keyword needs no Go type of its own, no second
// spelling for a second role, and no parentheses of its own.
//
// The constants are declared here once and the parser compares against these
// same values, so the spelling a user writes and the value the parser matches
// are the same thing. A keyword the parser does not know is reported as a
// syntax error and never reaches the output.
package kw

import "github.com/fujidaiti/psqlb/internal/tok"

// ===========================================================================
// Keywords
// ===========================================================================

// Statements and clauses.
const (
	SELECT    tok.Keyword = "SELECT"
	FROM      tok.Keyword = "FROM"
	WHERE     tok.Keyword = "WHERE"
	GROUP     tok.Keyword = "GROUP"
	BY        tok.Keyword = "BY"
	HAVING    tok.Keyword = "HAVING"
	WINDOW    tok.Keyword = "WINDOW"
	ORDER     tok.Keyword = "ORDER"
	LIMIT     tok.Keyword = "LIMIT"
	OFFSET    tok.Keyword = "OFFSET"
	INSERT    tok.Keyword = "INSERT"
	INTO      tok.Keyword = "INTO"
	VALUES    tok.Keyword = "VALUES"
	UPDATE    tok.Keyword = "UPDATE"
	SET       tok.Keyword = "SET"
	DELETE    tok.Keyword = "DELETE"
	USING     tok.Keyword = "USING"
	RETURNING tok.Keyword = "RETURNING"
	CONFLICT  tok.Keyword = "CONFLICT"
	DO        tok.Keyword = "DO"
	NOTHING   tok.Keyword = "NOTHING"
	WITH      tok.Keyword = "WITH"
	RECURSIVE tok.Keyword = "RECURSIVE"
)

// Modifiers of the SELECT list and of a sort key.
const (
	ALL      tok.Keyword = "ALL"
	DISTINCT tok.Keyword = "DISTINCT"
	AS       tok.Keyword = "AS"
	ASC      tok.Keyword = "ASC"
	DESC     tok.Keyword = "DESC"
	NULLS    tok.Keyword = "NULLS"
	FIRST    tok.Keyword = "FIRST"
	LAST     tok.Keyword = "LAST"
)

// Joins and set operations.
const (
	JOIN      tok.Keyword = "JOIN"
	LEFT      tok.Keyword = "LEFT"
	RIGHT     tok.Keyword = "RIGHT"
	FULL      tok.Keyword = "FULL"
	INNER     tok.Keyword = "INNER"
	OUTER     tok.Keyword = "OUTER"
	CROSS     tok.Keyword = "CROSS"
	NATURAL   tok.Keyword = "NATURAL"
	LATERAL   tok.Keyword = "LATERAL"
	ON        tok.Keyword = "ON"
	UNION     tok.Keyword = "UNION"
	INTERSECT tok.Keyword = "INTERSECT"
	EXCEPT    tok.Keyword = "EXCEPT"
)

// Expressions. A keyword that SQL always follows with a parenthesised group —
// IN, EXISTS, ANY, FILTER, OVER — is a constant like any other, and the group
// after it is written with sb.P. The parser requires it to be there.
const (
	AND       tok.Keyword = "AND"
	OR        tok.Keyword = "OR"
	NOT       tok.Keyword = "NOT"
	IS        tok.Keyword = "IS"
	NULL      tok.Keyword = "NULL"
	TRUE      tok.Keyword = "TRUE"
	FALSE     tok.Keyword = "FALSE"
	UNKNOWN   tok.Keyword = "UNKNOWN"
	IN        tok.Keyword = "IN"
	BETWEEN   tok.Keyword = "BETWEEN"
	EXISTS    tok.Keyword = "EXISTS"
	ANY       tok.Keyword = "ANY"
	SOME      tok.Keyword = "SOME"
	LIKE      tok.Keyword = "LIKE"
	ILIKE     tok.Keyword = "ILIKE"
	SIMILAR   tok.Keyword = "SIMILAR"
	TO        tok.Keyword = "TO"
	COLLATE   tok.Keyword = "COLLATE"
	CASE      tok.Keyword = "CASE"
	WHEN      tok.Keyword = "WHEN"
	THEN      tok.Keyword = "THEN"
	ELSE      tok.Keyword = "ELSE"
	END       tok.Keyword = "END"
	DEFAULT   tok.Keyword = "DEFAULT"
	FILTER    tok.Keyword = "FILTER"
	OVER      tok.Keyword = "OVER"
	PARTITION tok.Keyword = "PARTITION"
)

// Window frames.
const (
	ROW       tok.Keyword = "ROW"
	ROWS      tok.Keyword = "ROWS"
	RANGE     tok.Keyword = "RANGE"
	GROUPS    tok.Keyword = "GROUPS"
	UNBOUNDED tok.Keyword = "UNBOUNDED"
	PRECEDING tok.Keyword = "PRECEDING"
	FOLLOWING tok.Keyword = "FOLLOWING"
	CURRENT   tok.Keyword = "CURRENT"
)

// MATERIALIZED qualifies a CTE body.
const MATERIALIZED tok.Keyword = "MATERIALIZED"

// STAR is "*", the whole row. It is an expression on its own, in a SELECT list
// and as the argument of a function.
//
//	SELECT, STAR, FROM, Users     // SELECT * FROM users
//	sb.F("COUNT", STAR)           // COUNT(*)
const STAR tok.Keyword = "*"

// TYPECAST is "::". The name after it is a type name rather than an
// expression, and both are emitted with no spaces around the operator.
//
//	UsersMeta, TYPECAST, sb.I("jsonb") // users.meta::jsonb
//
// It is spelled TYPECAST rather than CAST because PostgreSQL calls "::" a
// typecast and reserves CAST for the CAST(x AS type) form, which is a different
// syntax.
const TYPECAST tok.Keyword = "::"

// ===========================================================================
// Operators
// ===========================================================================

// The operators that have a name here. Any other is written with sb.RawOp, since
// operators are not a fixed list: extensions add their own.
//
// TODO: EQ, NE, GTE and the rest are not SQL spellings, which is a deviation
// from the first golden rule. Go identifiers cannot be "=" or ">=", so the
// alternative is sb.RawOp(">=") everywhere.
const (
	EQ  tok.Operator = "="
	NE  tok.Operator = "<>"
	GT  tok.Operator = ">"
	GTE tok.Operator = ">="
	LT  tok.Operator = "<"
	LTE tok.Operator = "<="
)
