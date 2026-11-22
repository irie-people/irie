//go:build !darwin && !linux

package ab

import "os"

// maxRSS is 0 where the system gives no peak memory; the report then leaves
// memory out.
func maxRSS(*os.ProcessState) int64 { return 0 }
