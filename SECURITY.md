# Security

## Reporting a vulnerability

Report it privately, with GitHub's private vulnerability reporting. Don't open an issue, a pull request or a discussion about it.

1. Open the [Security tab](https://github.com/irie-people/irie/security) of irie-people/irie.
2. Choose **Report a vulnerability**.
3. Say what you found, how to reproduce it, and what it lets someone do.

The report goes to the project's maintainers through its account on GitHub, and to nobody else. There is no security email address.

If the button isn't there, open an issue that asks for a private way to report, and keep every detail out of it.

## What happens next

A maintainer looks at the report and works on a fix with you in a private security advisory. When the fix is out, the advisory is published, with credit to you unless you'd rather not be named. There are no promised response times.

## Supported versions

Irie has no releases yet. Fixes go to the main branch.

## Scope

In scope is Irie's own code: the `irie` program, its core and the socket it serves, the hook it adds to agents, and the website in `site/`.

Irie listens on no network port. Its core talks over a socket file that only your user can open, in a folder only your user can read.

The agents Irie starts, such as Claude Code, and tmux are their own projects. Report problems in them to those projects.
