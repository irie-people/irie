// Command design keeps Irie's copies of its design library's values in
// step with the library (github.com/irie-people/roots;
// internal/designcheck). With no argument it says which are not; sync
// writes the library's values into them.
package main

import (
	"fmt"
	"os"

	"github.com/irie-people/irie/internal/designcheck"
	"github.com/irie-people/roots"
)

func main() {
	if len(os.Args) > 2 || len(os.Args) == 2 && os.Args[1] != "sync" {
		fmt.Fprintln(os.Stderr, "usage: tools/design.sh [sync]")
		os.Exit(2)
	}
	if len(os.Args) == 2 {
		changed, probs := designcheck.Sync(".")
		for _, c := range changed {
			fmt.Println("wrote", c)
		}
		report(probs)
		return
	}
	probs := designcheck.Check(".")
	if len(probs) == 0 {
		fmt.Println("every copy is the library's")
	}
	report(probs)
}

func report(probs []roots.Problem) {
	for _, p := range probs {
		fmt.Fprintln(os.Stderr, p)
	}
	if len(probs) > 0 {
		fmt.Fprintln(os.Stderr, "tools/design.sh sync writes the library's values into the copies")
		os.Exit(1)
	}
}
