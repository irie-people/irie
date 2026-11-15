# Irie on Windows

Irie runs every agent in tmux, which is a Linux and macOS program, so on Windows it runs inside WSL 2, Windows' own Linux. There it is the same Irie as on any Linux: the same install, the same `irie setup`, the same sessions. A small Windows program, `irie.exe`, sets WSL up and starts Irie there from a Windows terminal, the Start menu or sign-in. DESIGN §2.7 and §7.3 say where this sits in the design.

## Install

In PowerShell, as yourself (not as an administrator):

```
powershell -ExecutionPolicy ByPass -c "irm https://irie-people.github.io/irie/install.ps1 | iex"
```

`site/install.ps1` downloads `irie-windows-amd64.zip` (or `-arm64`) from the latest release, checks its SHA-256 against the release's `SHA256SUMS`, puts `irie.exe` in `%LOCALAPPDATA%\irie\bin`, adds that folder to your PATH, and runs `irie setup`. It reads `IRIE_VERSION`, `IRIE_NO_SETUP` and `IRIE_DOWNLOADS` from the environment, as `install.sh` does.

## `irie setup` on Windows

It looks at how far the computer is set up and offers each step in order, running each one only after a yes (`--yes` takes them all; `--check` only reports):

| Step | What it does | Needs |
|---|---|---|
| Windows | Windows 10 version 2004 (build 19041) or later, or Windows 11 | an update, if older |
| WSL | `wsl --install --no-distribution` | an administrator's OK, then a restart; virtualization on in the firmware |
| Ubuntu | `wsl --install -d Ubuntu`: WSL's default distribution, which asks you to choose a user name and password for Linux; type `exit` to come back | |
| WSL 2 | `wsl --set-version <distro> 2`, for a distribution on WSL 1 | |
| Irie | `install.sh` inside the distribution, whose own `irie setup` then offers tmux, Claude Code and its sign-in | the Linux password, for `sudo apt install tmux` |
| Keep running | `instanceIdleTimeout=-1` in `[general]` of `%USERPROFILE%\.wslconfig`, so WSL keeps Linux, and your agents, running once the last window closes | |
| Start menu | an `Irie` shortcut, and an `Irie` profile in Windows Terminal (a fragment in `%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\Irie`) | |
| At sign-in | a shortcut in the Startup folder that runs `irie up`, minimized | |

`irie setup --distro NAME` runs Irie in another distribution you have, such as `Ubuntu-24.04` or `Debian`, and remembers it in `%LOCALAPPDATA%\irie\distro`; `IRIE_WSL_DISTRO` does the same for one shell. With neither, Irie runs in `Ubuntu`, or in your default distribution when it is on WSL 2 and is not a container tool's own, such as Docker Desktop's.

## Using it

`irie` opens Irie in the terminal it is typed in, as on Linux; the Start menu's Irie opens it in a terminal of its own. Every command but `setup` is Irie's own, run in WSL: `irie ctl list`, `irie config set profiles.helper.folder ~/Helper`, `irie version`. A Windows path among the arguments, such as `C:\Users\alice\site`, becomes its WSL path, `/mnt/c/Users/alice/site`. Agents work on Linux's own files fastest; a folder of yours on Windows is under `/mnt/c`.

The Mac app and its Ghostty terminals are macOS's only. The phone app (docs/PHONE.md) runs from WSL as from any Linux, though WSL's own network is behind Windows', so a phone on your Wi-Fi reaches it only with WSL's mirrored networking (`networkingMode=mirrored` in `.wslconfig`, Windows 11 22H2 or later).

## How it works

`irie.exe` (`cmd/windows`, with its logic in `internal/wsl`) never runs agents itself. It runs `wsl.exe -d <distro> --cd ~ -e sh -lc 'exec "$d/irie/bin/irie" "$@"'`, where `$d` is the distribution's `XDG_DATA_HOME` or `~/.local/share`, so the login shell gives Irie the PATH a terminal there has. It finds WSL by its files, `%ProgramW6432%\WSL\wsl.exe` for the Store's and `%SystemRoot%\System32\lxss\LxssManager.dll` for Windows' own, rather than by asking `wsl.exe`, which on a Windows without WSL may offer to install it and wait for a key. It reads `wsl.exe`'s output in UTF-8 (`WSL_UTF8=1`) or UTF-16, which older versions write regardless. Ctrl+C goes to Irie inside WSL; the wrapper waits for it.

Restarting Windows ends WSL, and with it every agent, as a restart of any computer does; `irie ctl history` still lists their sessions.

## Remove it

Delete `%LOCALAPPDATA%\irie`, the Irie shortcuts in the Start menu and its Startup folder, and `%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\Irie`; take `irie\bin` out of your PATH, and `instanceIdleTimeout` out of `.wslconfig`. Irie inside WSL comes out as on Linux. Removing the distribution itself (`wsl --unregister <distro>`) deletes everything in it, so that one is yours to decide.

## Tests

`internal/wsl`: `wsl.exe`'s list in both encodings and in another language, the pick of a distribution, Windows' build from `ver`, Windows paths, the command line, `.wslconfig` edited key by key with its line endings kept, the shortcut's PowerShell and the Terminal fragment, and `irie setup`'s walk through every step against a stand-in Windows: each yes, a no to what blocks and to an extra, a fix that leaves its step undone, a restart after WSL, `--check` and `--yes`. On Windows itself, the `windows` job of the test workflow runs those, then `install.ps1` from a local mirror twice, `irie.exe version` and `setup --check`, and checks that a wrong checksum installs nothing. GitHub's Windows has no WSL 2, so the steps inside WSL are tested on Linux, where they are Irie's own.
