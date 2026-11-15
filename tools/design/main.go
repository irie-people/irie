// Command design keeps the files that copy Irie's design library in step
// with it (design/README.md). With no argument it says which are not;
// sync writes the library's values into them and makes the generated
// files again.
package main

import (
	"fmt"
	"os"

	"github.com/irie-people/irie/design"
)

func main() {
	if len(os.Args) > 2 || len(os.Args) == 2 && os.Args[1] != "sync" {
		fmt.Fprintln(os.Stderr, "usage: tools/design.sh [sync]")
		os.Exit(2)
	}
	if len(os.Args) == 2 {
		changed, probs := design.Sync(".")
		for _, c := range changed {
			fmt.Println("wrote", c)
		}
		report(probs)
		return
	}
	probs := design.Check(".")
	if len(probs) == 0 {
		fmt.Println("every copy is the library's")
	}
	report(probs)
}

func report(probs []design.Problem) {
	for _, p := range probs {
		fmt.Fprintln(os.Stderr, p)
	}
	if len(probs) > 0 {
		fmt.Fprintln(os.Stderr, "tools/design.sh sync writes the library's values into the copies")
		os.Exit(1)
	}
}
