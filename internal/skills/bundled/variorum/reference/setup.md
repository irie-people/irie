# Setting up Variorum's door for Irie

The writer makes every step. You say what to do, one step at a time, and wait for them.

1. **Variorum.** It runs on a Mac with Apple silicon and macOS 26 or later. If they don't have it yet, it is free from https://stargazing.tech/variorum.
2. **Open the door.** In Variorum, open Settings ▸ MCP and turn on **Enable the MCP server**.
3. **Copy the setup.** On the **Connect** tab, choose **Claude Code** and copy the setup with the copy icon in the corner of its code block. The setup holds a key for this connection, so it doesn't belong in this chat. Tell them not to paste it here.
4. **Run it in Terminal, from this session's folder.** Claude Code keeps a server for the folder it was added in, unless the setup says `--scope user`, and all of Irie's writing sessions run in this folder. Run `pwd` yourself, then give them one line to type in Terminal first: `cd` and the folder, in quotes. Then they paste the setup and press Return.
5. **Start again.** Claude Code looks for servers when it starts. Ask them to close this session in Irie and reopen it (`:reopen`), and the conversation carries on with Variorum's tools. A new `:write` works as well.
6. **Let it in.** The first time a session reaches Variorum, a connection request appears in Settings ▸ MCP. Approving it lets the session in, with no access yet.
7. **Share.** They share the note, book, gathering or wing that the job needs, at the level it needs (rule 2 of the skill).

If they set Claude Code up again later, a new setup copied from Claude Code's own tab replaces the old one and keeps what they shared.
