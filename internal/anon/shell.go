package anon

import (
	"regexp"
	"slices"
	"strings"
)

// ForwardPort is the first port on 127.0.0.1 a session's forwarder may
// take (forward.go), above Irie's Tor ports from 9150 and outside the
// ports the system hands out for its own connections.
const ForwardPort = 9250

// Credentials are where the user's sign-ins live: keys, tokens, keychains
// and browsers' cookies. An anonymous session may read none of them, by
// its shell or its Read tool: one used through the exit would tie the user
// to it. A path is from ~/, and may end in a wildcard.
var Credentials = []string{
	"~/.ssh", "~/.gnupg", "~/.aws", "~/.azure", "~/.config/gcloud", "~/.kube", "~/.docker",
	"~/.netrc", "~/.git-credentials", "~/.config/git/credentials", "~/.config/gh*",
	"~/.npmrc", "~/.pypirc", "~/.cargo/credentials*", "~/.password-store",
	"~/Library/Keychains", "~/Library/Cookies", "~/Library/Safari", "~/Library/Containers/com.apple.Safari",
	"~/Library/Application Support/Google/Chrome", "~/Library/Application Support/Chromium",
	"~/Library/Application Support/BraveSoftware", "~/Library/Application Support/Microsoft Edge",
	"~/Library/Application Support/Arc", "~/Library/Application Support/Firefox",
	"~/.mozilla", "~/.config/google-chrome", "~/.config/chromium", "~/.config/BraveSoftware",
	"~/.local/share/keyrings",
}

// secrets are the variables an anonymous session's shell never sees, as
// well as those whose names say they hold a secret.
var secrets = []string{
	"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITLAB_TOKEN", "NPM_TOKEN", "NODE_AUTH_TOKEN",
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "GOOGLE_APPLICATION_CREDENTIALS",
	"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "HF_TOKEN", "SSH_AUTH_SOCK",
	// Claude Code's own, which it may set for its commands itself.
	"CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_CLIENT_KEY_PASSPHRASE",
}

var secretName = regexp.MustCompile(`(?i)token|secret|passw|api_?key|private_?key|credential|cookie|_auth$|^auth_|_auth_|oauth`)

// SecretNames are the variables of environ, and the usual ones, whose
// values an anonymous session's shell is kept from: signing in with one
// through the exit would tie the user to it.
func SecretNames(environ []string) []string {
	out := slices.Clone(secrets)
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if k != "" && secretName.MatchString(k) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
