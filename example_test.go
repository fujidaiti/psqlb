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
	users        = sb.I("users")
	usersID      = sb.I("users.id")
	usersName    = sb.I("users.name")
	usersCreated = sb.I("users.created_at")
)

func Example() {
	sql, args, err := sb.ToSQL(
		SELECT, usersID, usersName,
		FROM, users,
		WHERE, sb.P(usersCreated, usersID), LT, sb.P(sb.V("2025-06-01"), sb.V(500)),
		ORDER, BY, usersCreated, DESC, usersID, DESC, NULLS, LAST,
		LIMIT, sb.V(20),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(sql)
	fmt.Println(args...)

	// Output:
	// SELECT users.id, users.name FROM users WHERE (users.created_at, users.id) < ($1, $2) ORDER BY users.created_at DESC, users.id DESC NULLS LAST LIMIT $3
	// 2025-06-01 500 20
}
