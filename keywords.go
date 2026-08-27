// Package psqlb holds the SQL keywords of the psqlb DSL, and nothing else. It
// is meant to be dot-imported, so that a statement reads like the SQL it
// produces:
//
//	import (
//		. "github.com/fujidaiti/psqlb"
//		"github.com/fujidaiti/psqlb/sb"
//	)
//
//	sb.Stm(SELECT, UsersID, FROM, Users, WHERE, UsersAge, GTE, sb.Lit(18))
//
// Everything that is not a SQL keyword — Stm, Row, Func, Id, Lit, Raw, Op, Kw —
// lives in package sb and is written with that prefix, so that dot-importing
// this package brings only SQL vocabulary into scope. The design document is
// the package doc comment of sb.
package psqlb

import (
	"github.com/fujidaiti/psqlb/internal/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// ===========================================================================
// Keyword constants
// ===========================================================================

// List keywords open a comma-separated list. Everything until the next clause
// keyword is joined with commas.
const (
	SELECT         kw.ListKw = "SELECT"
	FROM           kw.ListKw = "FROM"
	GROUP_BY       kw.ListKw = "GROUP BY"
	ORDER_BY       kw.ListKw = "ORDER BY"
	RETURNING      kw.ListKw = "RETURNING"
	PARTITION_BY   kw.ListKw = "PARTITION BY"
	WITH           kw.ListKw = "WITH"
	WITH_RECURSIVE kw.ListKw = "WITH RECURSIVE"
	VALUES         kw.ListKw = "VALUES"
)

// SET is a list keyword that also strips the table qualifier from the name that
// begins each assignment, since "SET users.status = ..." is not legal. The
// right-hand side is left alone.
//
//	sb.Stm(UPDATE, Users, SET, UsersStatus, EQ, sb.Func("upper", UsersName))
//	// UPDATE users SET status = upper(users.name)
//
// This is a deliberate exception to the second golden rule: SQL has no such
// rule, and the DSL text differs from the SQL text. It is kept because writing
// sb.Id("status") for every assignment is the common case and the qualified
// constant is the one already at hand.
const SET kw.SetKw = "SET"

// Clause keywords close a comma-separated list and return to space separation.
const (
	WHERE  kw.ClauseKw = "WHERE"
	HAVING kw.ClauseKw = "HAVING"
	ON     kw.ClauseKw = "ON"
	LIMIT  kw.ClauseKw = "LIMIT"
	OFFSET kw.ClauseKw = "OFFSET"

	UPDATE      kw.ClauseKw = "UPDATE"
	DELETE_FROM kw.ClauseKw = "DELETE FROM"

	// INSERT_INTO is followed by the table and, if the columns are named, by a
	// sb.Row of them. The names must be bare, since "INSERT INTO users
	// (users.name)" is not legal, so write sb.Id("name") rather than UsersName.
	//
	//	INSERT_INTO, Users, sb.Row(sb.Id("name"), sb.Id("email")), VALUES, sb.Row(...)
	//	// INSERT INTO users (name, email) VALUES (...)
	INSERT_INTO kw.ClauseKw = "INSERT INTO"

	// ON_CONFLICT takes a sb.Row naming the conflict target, or nothing at all.
	//
	//	ON_CONFLICT, sb.Row(sb.Id("email")), DO_UPDATE, SET, ...
	//	ON_CONFLICT, DO_NOTHING
	ON_CONFLICT kw.ClauseKw = "ON CONFLICT"

	JOIN       kw.ClauseKw = "JOIN"
	LEFT_JOIN  kw.ClauseKw = "LEFT JOIN"
	INNER_JOIN kw.ClauseKw = "INNER JOIN"
	CROSS_JOIN kw.ClauseKw = "CROSS JOIN"

	UNION     kw.ClauseKw = "UNION"
	UNION_ALL kw.ClauseKw = "UNION ALL"
	INTERSECT kw.ClauseKw = "INTERSECT"
	EXCEPT    kw.ClauseKw = "EXCEPT"

	DO_UPDATE  kw.ClauseKw = "DO UPDATE"
	DO_NOTHING kw.ClauseKw = "DO NOTHING"
)

// Infix tokens sit between two operands and keep the item open, so the operand
// after them takes no comma.
const (
	AND kw.InfixKw = "AND"
	OR  kw.InfixKw = "OR"

	// TODO: see TODO.md. EQ, NE, GTE and the rest are not SQL spellings, which
	// is a deviation from the first golden rule. Go identifiers cannot be "="
	// or ">=", so the alternative is sb.Op(">=") everywhere.
	EQ    kw.InfixKw = "="
	NE    kw.InfixKw = "<>"
	GT    kw.InfixKw = ">"
	GTE   kw.InfixKw = ">="
	LT    kw.InfixKw = "<"
	LTE   kw.InfixKw = "<="
	LIKE  kw.InfixKw = "LIKE"
	ILIKE kw.InfixKw = "ILIKE"
	CAST  kw.InfixKw = "::"

	// AS introduces an alias, and it is also how a CTE is named. The
	// parentheses a CTE body always has come from its being a nested sb.Stm.
	//
	//	sb.Stm(sub, AS, sb.Id("n"))                     // (SELECT ...) AS n
	//	sb.Stm(WITH, sb.Id("tree"), AS, body, SELECT, …) // WITH tree AS (SELECT ...) SELECT …
	AS kw.InfixKw = "AS"

	// MATERIALIZED is infix rather than a clause keyword, so that it does not
	// close the WITH list and swallow the comma before the next CTE.
	MATERIALIZED kw.InfixKw = "MATERIALIZED"

	// WHEN, THEN and ELSE keep a CASE expression open, which is what lets one
	// sit in the middle of a SELECT list without breaking the commas.
	WHEN kw.InfixKw = "WHEN"
	THEN kw.InfixKw = "THEN"
	ELSE kw.InfixKw = "ELSE"
)

// Prefix tokens begin an item and keep it open, so the operand after them takes
// no comma.
const (
	// NOT is correct in WHERE NOT a, in NOT, EXISTS(...) and in SELECT NOT flag.
	// As a modifier inside a comma-separated list it is not; write
	// sb.Op("NOT IN"), sb.Row(...) there.
	NOT kw.PrefixKw = "NOT"

	DISTINCT kw.PrefixKw = "DISTINCT"

	// LATERAL is a constant rather than a function, because it is not always
	// followed by a parenthesised group of its own: in
	// JOIN LATERAL unnest(a) AS t it applies to a function call.
	LATERAL kw.PrefixKw = "LATERAL"

	CASE kw.PrefixKw = "CASE"
)

// Postfix tokens attach to the operand before them and end the item, so the
// operand after them starts a new one and takes a comma.
const (
	IS_NULL     kw.PostfixKw = "IS NULL"
	IS_NOT_NULL kw.PostfixKw = "IS NOT NULL"

	ASC         kw.PostfixKw = "ASC"
	DESC        kw.PostfixKw = "DESC"
	NULLS_FIRST kw.PostfixKw = "NULLS FIRST"
	NULLS_LAST  kw.PostfixKw = "NULLS LAST"

	END kw.PostfixKw = "END"
)

// Operands are complete expressions on their own.
const (
	STAR  kw.OperandKw = "*"
	TRUE  kw.OperandKw = "TRUE"
	FALSE kw.OperandKw = "FALSE"
	NULL  kw.OperandKw = "NULL"
)

// EXCLUDED reads the name after it, so EXCLUDED, UsersName renders
// EXCLUDED.name. The qualifier is stripped, since only the column name is legal
// there.
//
// TODO: see TODO.md. This breaks the second golden rule twice over: two tokens
// produce one operand, and the name is rewritten. SQL has no such rule;
// EXCLUDED.name is an ordinary qualified name.
const EXCLUDED kw.ExcludedKw = "EXCLUDED"

// The keywords SQL always follows with one parenthesised group are constants
// like any other, and the group after them is written with sb.Row or sb.Stm. They are
// infixes, because each sits between the expression before it and that group:
//
//	UsersStatus, IN, sb.Row(sb.Lit("active"), sb.Lit("trial"))  // users.status IN ($1, $2)
//	UsersID, IN, sb.Stm(SELECT, OrdersUserID, FROM, Orders)     // users.id IN (SELECT ...)
//	UsersID, EQ, ANY, sb.Stm(SELECT, OrdersUserID, FROM, Orders)
//	sb.Func("COUNT", STAR), FILTER, sb.Stm(WHERE, UsersIsPaid)  // COUNT(*) FILTER (WHERE users.paid)
//	sb.Func("SUM", OrdersTotal), OVER, sb.Stm(PARTITION_BY, OrdersUserID)
//	sb.Func("SUM", OrdersTotal), OVER, sb.Id("w")               // SUM(...) OVER w
const (
	IN     kw.InfixKw = "IN"
	ANY    kw.InfixKw = "ANY"
	FILTER kw.InfixKw = "FILTER"
	OVER   kw.InfixKw = "OVER"
)

// EXISTS is a prefix rather than an infix, because it has no left operand.
//
//	WHERE, NOT, EXISTS, sb.Stm(SELECT, sb.Lit(1), FROM, Orders)
const EXISTS kw.PrefixKw = "EXISTS"

// DISTINCT_ON is the one keyword that still carries its own parentheses. As a
// constant it would work as far as the group, but the group is an operand and
// so ends the item, and the first column of the SELECT list would then take a
// comma: "SELECT DISTINCT ON (users.id), users.id". Writing the parentheses
// here keeps it glued past them.
//
//	SELECT, DISTINCT_ON(UsersID), UsersID, UsersName
//	// SELECT DISTINCT ON (users.id) users.id, users.name
//
// TODO: see TODO.md. Either find a spelling that obeys the parentheses rule or
// record this as a permanent exception.
func DISTINCT_ON(items ...sb.Clause) sb.Clause { return sb.PrefixGroup("DISTINCT ON", items...) }
