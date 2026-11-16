//go:build !noterms

package core

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/extension"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/terms"
)

// Terms in short's page (DESIGN §4.17): while the plugin is on, the core
// serves it on this computer, for the Mac app's Terms tab, which shows it
// as the plugin's web pane. go build -tags noterms leaves this file out,
// and terms_off.go stands in.

// termsLook is how often the core looks whether the plugin is on: one
// turned on serves its page within it.
var termsLook = 5 * time.Second

// termsAddr is where the page is served; a test takes a port of its own.
var termsAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(terms.PagePort))

// serveTerms serves the page while the plugin is on, until ctx ends. A
// port another program holds is tried again at every look, and said in
// the log once in a while; the plugin's check says so too.
func (c *Core) serveTerms(ctx context.Context) {
	var stop context.CancelFunc
	var done chan struct{}
	var warned time.Time
	t := time.NewTicker(termsLook)
	defer t.Stop()
	for {
		if done != nil {
			select {
			case <-done:
				// It ended by itself: serve again at the next look.
				stop, done = nil, nil
			default:
			}
		}
		on := c.cfg().TermsOn()
		switch {
		case on && done == nil:
			ln, err := net.Listen("tcp", termsAddr)
			if err != nil {
				if time.Since(warned) > 10*time.Minute {
					c.log.Warn("terms: can't serve its page", "addr", termsAddr, "err", err)
					warned = time.Now()
				}
				break
			}
			serve, cancel := context.WithCancel(ctx)
			stop, done = cancel, make(chan struct{})
			go func(done chan struct{}) {
				defer close(done)
				err := extension.ServeWeb(serve, ln, extension.Options{
					Paths:   c.paths,
					Load:    func() (config.Config, error) { return *c.cfg(), nil },
					Why:     plugins.Why,
					Servers: terms.Servers,
					Log:     c.log,
				})
				if err != nil {
					c.log.Warn("terms: its page", "err", err)
				}
			}(done)
			c.log.Info("terms: serving its page", "addr", termsAddr)
		case !on && done != nil:
			stop()
			<-done
			stop, done = nil, nil
			c.log.Info("terms: stopped serving its page")
		}
		select {
		case <-ctx.Done():
			if done != nil {
				stop()
				<-done
			}
			return
		case <-t.C:
		}
	}
}
