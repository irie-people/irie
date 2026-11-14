//go:build !nomail

package config

// MailBuilt says whether this build has the mail plugin (DESIGN §4.17);
// go build -tags nomail leaves it out.
const MailBuilt = true
