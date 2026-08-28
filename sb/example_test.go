package sb_test

// The package example. Every test here is external, so all of them are written
// the way a user writes a statement — the keywords by dot-import, everything
// else through sb — and none of them can reach an unexported name by accident.

import (
	"fmt"

	. "github.com/fujidaiti/psqlb/kw"
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
		WHERE, sb.P(usersCreated, usersID), LT, sb.P("2025-06-01", 500),
		ORDER, BY, usersCreated, DESC, usersID, DESC, NULLS, LAST,
		LIMIT, 20,
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
