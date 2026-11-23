package rebuild

import (
	"time"

	"github.com/irie-people/irie/internal/ledger"
)

// Rates are a model's list prices, in dollars a million tokens
// (ledger.Rates).
type Rates = ledger.Rates

// Price is model's list prices today for a request whose prompt is prompt
// tokens long, and false for a model whose prices Irie does not know, such
// as a local one (ledger.At).
func Price(model string, prompt int64) (Rates, bool) {
	return ledger.At(model, prompt, time.Now())
}
