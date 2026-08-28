// Package kw holds the SQL keywords of the psqlb DSL, and nothing else. It is
// meant to be dot-imported, so that a statement reads like the SQL it produces:
//
//	import (
//		. "github.com/fujidaiti/psqlb/kw"
//		"github.com/fujidaiti/psqlb/sb"
//	)
//
//	sb.ToSQL(SELECT, UsersID, FROM, Users, WHERE, UsersAge, ">=", 18)
//
// Everything that is not a SQL keyword — ToSQL, P, F, I, Arg, RawExpr — lives
// in package sb and is written with that prefix, so that dot-importing this
// package brings only SQL vocabulary into scope. Not even the type of these
// constants is exported from here, for the same reason: it is in internal/tok.
// The prefix is also what lets the names in sb be one letter. The design
// document is the package doc comment of sb.
//
// There are no operators here. An operator is written as its SQL symbol — "=",
// ">=", "@>" — and package sb decides that a string is one by PostgreSQL's own
// lexical rule, so a constant with an invented name like EQ would be a second
// spelling for something that already has one. The same goes for the two pieces
// of punctuation that used to be keywords here, "*" and "::".
//
// Every keyword is one word. A phrase is written as the words it is made of:
// GROUP, BY and IS, NOT, NULL and LEFT, OUTER, JOIN. The parser knows where
// each one may appear, so a keyword needs no Go type of its own, no second
// spelling for a second role, and no parentheses of its own.
//
// The keywords are declared here once and the parser compares against these
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
