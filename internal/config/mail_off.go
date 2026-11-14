//go:build nomail

package config

// MailBuilt says whether this build has the mail plugin (DESIGN §4.17);
// this one, built with -tags nomail, has not.
const MailBuilt = false
