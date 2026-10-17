// tml-baseline never resets or replaces a database. Production replacement is a
// separate operator action after reviewing the manifest and stopping TML only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
	_ "time/tzdata"
	"traffic-manager-lite/internal/baseline"
)

func main() {
	source := flag.String("source", "", "existing TML database (read-only)")
	out := flag.String("output", "", "new private backup directory; must not exist")
	zone := flag.String("timezone", "Asia/Shanghai", "new database aggregation timezone")
	flag.Parse()
	if *source == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, e := baseline.Prepare(ctx, *source, *out, *zone)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(r)
}
