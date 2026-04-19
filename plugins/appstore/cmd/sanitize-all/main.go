// sanitize-all reads seed_apps.json.gz and prints the SanitizeCompose output
// of each app's compose_file, delimited by sentinel lines. Used as the oracle
// for byte-parity checking against scripts/appstore-batch-test/batch_test.py
// which reimplements the same logic in Python.
//
// Usage:
//	go run ./plugins/appstore/cmd/sanitize-all <seed.json.gz>
//
// Output format (stdout, UTF-8):
//	===APP app_id===
//	<sanitized compose, verbatim>
//	===END===
//	... (repeated per app, sorted by app_id for stable diffs)
package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/web-casa/webcasa/plugins/appstore"
)

type seedApp struct {
	AppID       string `json:"app_id"`
	ComposeFile string `json:"compose_file"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <seed.json.gz>\n", os.Args[0])
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var apps []seedApp
	if err := json.NewDecoder(gz).Decode(&apps); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].AppID < apps[j].AppID })
	for _, a := range apps {
		if a.ComposeFile == "" {
			continue
		}
		fmt.Printf("===APP %s===\n%s\n===END===\n", a.AppID, appstore.SanitizeCompose(a.ComposeFile))
	}
}
