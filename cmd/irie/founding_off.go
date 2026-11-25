//go:build nofounding

package main

import (
	"errors"

	"github.com/irie-people/irie/internal/brand"
)

// This build leaves the founding plugin out (go build -tags nofounding,
// DESIGN §4.17): `irie ctl founding` and `irie ctl schools` say so.

const foundingUsage = ""

func ctlFounding([]string) error {
	return errors.New("the Charters plugin isn't in this build of " + brand.Name)
}

func ctlSchools([]string) error {
	return errors.New("the Charters plugin, and Common Schools with it, isn't in this build of " + brand.Name)
}
