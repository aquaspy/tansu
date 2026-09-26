// Command kurapeople is the KuraPeople server. Subcommands:
//
//	serve              run the web server (default)
//	users              list users
//	create             create a user (EMAIL=, PASSWORD= env)
//	password           reset a password (EMAIL=, PASSWORD= env)
package main

import (
	"fmt"
	"os"

	"github.com/aquasp/kurapeople/internal/config"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg := config.Load()
	var err error
	switch cmd {
	case "serve":
		err = runServe(cfg)
	case "users":
		err = runUsers(cfg)
	case "create":
		err = runCreate(cfg)
	case "password":
		err = runPassword(cfg)
	default:
		err = fmt.Errorf("unknown command %q (serve|users|create|password)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kurapeople:", err)
		os.Exit(1)
	}
}
