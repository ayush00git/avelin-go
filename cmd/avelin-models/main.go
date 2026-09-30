// Command avelin-models prints AVELIN's public model catalog as a table. It
// needs no API key. Set AVELIN_BASE_URL to read it from another server, such
// as cmd/mockserver.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ayush00git/avelin-go"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalog, err := avelin.NewClient().FetchCatalog(ctx)
	if err != nil {
		log.Fatal(err)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tFAMILY\tCONTEXT\tINPUT $/1M\tOUTPUT $/1M")
	for _, m := range catalog.Data {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.ID, family(m.ID), tokens(m.ContextLength),
			perMillion(m.Pricing.Prompt), perMillion(m.Pricing.Completion))
	}
	tw.Flush()
	fmt.Printf("\n%d models. Family is derived from the ID; the catalog has no family field.\n", len(catalog.Data))
}

// family derives a model's family from its ID. AVELIN's docs name IDs
// avelin-<family>-<tier>, except the "intelligence" family, whose IDs are
// avelin-<tier>.
func family(id string) string {
	rest, ok := strings.CutPrefix(id, "avelin-")
	if !ok {
		return "?"
	}
	i := strings.LastIndex(rest, "-")
	if i < 0 {
		return "intelligence"
	}
	return rest[:i]
}

// tokens formats a token count as 256K or 1M when it is a round number.
func tokens(n int) string {
	switch {
	case n > 0 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n > 0 && n%1_000 == 0:
		return fmt.Sprintf("%dK", n/1_000)
	}
	return fmt.Sprint(n)
}

// perMillion converts a USD-per-token decimal string to USD per million
// tokens, exactly, with at least two decimals.
func perMillion(perToken string) string {
	r, ok := new(big.Rat).SetString(perToken)
	if !ok {
		return "?"
	}
	s := r.Mul(r, big.NewRat(1_000_000, 1)).FloatString(6)
	for strings.HasSuffix(s, "0") && len(s)-strings.IndexByte(s, '.') > 3 {
		s = s[:len(s)-1]
	}
	return s
}
