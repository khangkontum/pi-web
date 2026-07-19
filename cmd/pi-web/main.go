// Command pi-web serves the pi coding agent over HTTP: an embedded chat UI
// plus a small JSON API, with sessions shared with the pi CLI.
package main

import (
	"os"

	"github.com/khangkontum/pi-web/internal/piweb"
)

func main() {
	os.Exit(piweb.Main(os.Args[1:], os.Stdout, os.Stderr))
}
