package psqlb_test

// This file is the only external test: everything else is in package psqlb and
// so has the keywords in scope already. Here they arrive by dot-import, which
// is how a user gets them, so this compiles only if the split works from
// outside the module's own packages.

import (
	"fmt"

	. "github.com/fujidaiti/psqlb"
	"github.com/fujidaiti/psqlb/sb"
)

const (
	users        = sb.Id("users")
	usersID      = sb.Id("users.id")
	usersName    = sb.Id("users.name")
	usersCreated = sb.Id("users.created_at")
)

func Example() {
	stm := sb.Stm(
		SELECT, usersID, usersName,
		FROM, users,
		WHERE, sb.Row(usersCreated, usersID), LT, sb.Row(sb.Lit("2025-06-01"), sb.Lit(500)),
		ORDER_BY, usersCreated, DESC, usersID, DESC, NULLS_LAST,
		LIMIT, sb.Lit(20),
	)

	sql, args, err := stm.ToSQL()
	if err != nil {
		panic(err)
	}
	fmt.Println(sql)
	fmt.Println(args...)

	// Output:
	// SELECT users.id, users.name FROM users WHERE (users.created_at, users.id) < ($1, $2) ORDER BY users.created_at DESC, users.id DESC NULLS LAST LIMIT $3
	// 2025-06-01 500 20
}
