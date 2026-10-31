// Package version identifies an irie build.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

// Base is the release version.
const Base = "0.1.0" // x-release-please-version

// String is Base plus a hash of the running executable, so two different
// builds never report the same version. `irie up` compares it to replace a
// core or sidebar left over from another build.
var String = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return Base
	}
	f, err := os.Open(exe)
	if err != nil {
		return Base
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Base
	}
	return Base + "+" + hex.EncodeToString(h.Sum(nil))[:12]
})
