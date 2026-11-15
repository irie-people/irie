# Setting up Variorum's door for Irie

The writer makes every step. You say what to do, one step at a time, and wait for them.

1. **Variorum.** It runs on a Mac with Apple silicon and macOS 26 or later. If they don't have it yet, it is free from https://stargazing.tech/variorum.
2. **Open the door.** In Variorum, open Settings ▸ MCP (⌥⌘, goes straight there) and turn on **Enable the MCP server**.
3. **Copy the setup.** On the **Connect** tab, under **Connect a client**, choose **Claude Code** and copy the setup with the copy icon in the upper right of its code block. Each copy holds a fresh key, so it doesn't belong in this chat. Tell them not to paste it here.
4. **Give it to Irie.** They press **Connect** on the Variorum tab, in the right sidebar of Irie's Mac app, or run `pbpaste | irie variorum connect` in a terminal. Irie keeps the key and gives Variorum's door to the sessions `:write` starts, and to no others. They don't run the setup in Terminal.
5. **Start again.** A session finds the door when it starts, so Variorum must be open by then. Ask them to close this session in Irie and reopen it (`:reopen`), and the conversation carries on with Variorum's tools. A new `:write` works as well.
6. **Let it in.** When the first writing session starts, a connection request from `claude-code` waits above the tabs in Settings ▸ MCP. **Approve…** opens a sheet: approving alone lets it in with nothing shared, and they can share the job's place in the same sheet. Later writing sessions get in without a new request, and as they share things, the tools reach a running session without a restart.
7. **Share.** They share the note, book, gathering or wing that the job needs with `claude-code`, at the level it needs (rule 2 of the skill): the open note with Share This Note with AI (⌥⌘A), anything else with **Share another place…** on the **Shared with AI** tab.
8. **The Variorum tab, if they want it.** The tab shows the note a writing session works on, and never writes. It needs its own setup: in Variorum, choose **Any MCP client**, copy, and press **Connect** on the tab again. The tab connects as a client of its own, `irie`: they approve it when it first reads, and share with it, at Read, what they want to see there.

If they ran Variorum's Claude Code setup in Terminal before, ask them to remove it with `claude mcp remove -s user variorum`. It connects every Claude Code session on the Mac, which can use up Variorum's 16 connections. And Variorum lets in only the newest Claude Code copy, so once Irie's copy is in use, sessions that use the old setup are turned away, and a copy run in Terminal later would turn Irie's writing sessions away instead.

If they set Claude Code up again later, a new Claude Code copy pressed into **Connect** replaces the old one and keeps the approval and what they shared. A new **Any MCP client** copy for the tab waits for their OK under Waiting, as "New setup for irie". `irie variorum forget` deletes both setups.
