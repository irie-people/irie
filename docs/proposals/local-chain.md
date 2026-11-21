# The sidebar as a local chain

Date: 2026-11-21. Status: proposal. An agent (Claude Code) wrote it with the owner.

The owner asked for three things:

1. **The sidebar under version control.** "Our sidebar and groups and user grouping", kept "on chain like version controlled".
2. **No identity, no data.** "Make sure its known that this is not for collecting identity. It is meant to stay local and anonymous by default, and will never collect any data on any users."
3. **The strongest security.** "Absolutely max the security and privacy of the user, like post quantum hashing and encryption on vaults, just the whole nine yards."

## What it is

Every change to how you organise your sessions becomes a version: the order of the cards, Pinned and Parked, your groups and the groups inside them, your spaces and which space each card is in, and the names you give them (§4.1).

Each version records the whole organisation, names the version before it by its hash, and is named by its own hash. That makes a chain, like git's history, kept on your computer.

What it gives you:

- **History deeper than undo.** Undo keeps the last 100 actions (§4.5). The chain keeps every version, so "how did my sidebar look on Tuesday" has an answer, and any old version can be brought back, as a new version you can undo.
- **Proof.** A version's hash pins down exactly what the organisation was. An anchored hash (below) also proves when it existed.
- **Two machines that agree.** Peers (§2.8) compare their heads and exchange versions, rather than one overwriting the other.

## What it never is

- **Never identity.** A version holds no person's name, email address, account id, host name or machine serial. It says what was organised, never who organised it.
- **Never data collection.** Nothing leaves your computer by itself. Irie collects no data on its users, now or later. It has no telemetry, and the chain adds none.
- **Never tokens or money.** No coin and no wallet, and Irie holds no keys for funds. Anchoring is optional, carries only a hash, and is made by another tool.
- **Local and anonymous by default.** On a fresh install the chain exists only on your disk, encrypted, with anchoring off.

## Security and privacy

The choices follow the strictest public suite, the NSA's CNSA 2.0, as of November 2026.

- **Hashing.** A version is named by SHA-512 of its canonical bytes (Go's `crypto/sha512`). A quantum computer can at most halve a hash's strength (Grover's algorithm), so SHA-512 keeps a wide margin.
- **The vault, at rest.**
  - The chain's store is encrypted with AES-256-GCM (Go's `crypto/aes` and `crypto/cipher`), with a fresh random 96-bit nonce for each record, under a data key.
  - The data key is wrapped by a key kept in the system's own store: the login Keychain on macOS, the Secret Service on Linux. Where neither exists, the key comes from a passphrase through Argon2id.
  - No key ever sits beside the data in plain.
- **Between your machines.**
  - Peers already talk over SSH, whose key exchange has been a post-quantum hybrid by default since OpenSSH 9.0 (sntrup761x25519), and ML-KEM since 10.0 (mlkem768x25519).
  - Inside that tunnel, versions travel sealed: a hybrid of X25519 and ML-KEM-1024 (Go's `crypto/mlkem`), both shared secrets through HKDF-SHA-512 into AES-256-GCM. A tunnel recorded today can't be opened later.
- **Signatures.** Each machine signs the versions it writes with ML-DSA-87, under a key made at random for that install and never tied to a person. That's Go 1.27's `crypto/mldsa`, or `filippo.io/mldsa` (the same API) while Irie's go.mod says 1.26.
- **Anchors.**
  - An anchor is a transaction on a public chain carrying the head's hash, and nothing else. A separate tool makes it (Mycylic is one, anchoring hashes only). Irie never signs a transaction, so it still holds no keys or funds.
  - Public-chain accounts (secp256k1) aren't quantum-safe. An anchor carries no secret, though: the worst a future attacker could do is write a false anchor, which no local chain would match.

## How it would work

- **Where.** In `$XDG_STATE_HOME/irie/chain/`: `objects/` holds one encrypted record per version, named by its hash, next to `HEAD` and the wrapped key. `state.json` stays the core's fast snapshot (§5.4), and the chain is its history. Session contexts already live in git (§4.13). The chain sits beside them and holds the organisation only, never a conversation.
- **When.** Each action that changes the organisation appends a version once it has applied: move, pin, park, group, space and rename, the actions §4.5 can undo. Undo and redo append versions too, so history is never rewritten.
- **The canonical form.** JSON with sorted keys, no whitespace and integers only, so the same organisation always gives the same hash.
- **Ops:**
  - `irie ctl chain log`: versions, newest first.
  - `irie ctl chain show HASH`
  - `irie ctl chain restore HASH`: applies it as a new version, which you can undo.
  - `irie ctl chain head`: the hash an anchoring tool reads.
  - The Mac app gets a History item in the View menu.
- **Anchoring is opt-in.** `chain.anchor` is false by default. When it's on, Irie hands the head's hash to the anchoring tool the user configured, and nothing more.

## Steps

1. **The chain on disk.** SHA-512 ids, `ctl chain log`, `show` and `restore`. Tests: one version per action, restore, and a tampered record caught.
2. **The vault.** AES-256-GCM, with the key in the Keychain or the Secret Service, and an existing chain moved in. A test that nothing on disk reads without the key. Built first for zen's versions (zen.md §4): `internal/chain`'s vault and `internal/keyring`, with the key in the Keychain on macOS and a file of the user's alone elsewhere. The Secret Service and a passphrase through Argon2id are still to come.
3. **Peers.** Sealed versions (X25519 + ML-KEM-1024), ML-DSA-87 signatures, and heads compared and merged.
4. **The anchoring hand-off,** off by default.

## Open questions

- **Names inside versions.** Group and space names are the user's own, and session names come from tasks. Inside the encrypted vault they're private, but should a version hold only ids, with the names in a separate encrypted table that can be wiped on its own?
- **How long to keep versions.** All of them, or prune after a set time? Anchored hashes would keep the proof.
- **Linux with no desktop session,** where there's no Secret Service.

## Sources

- The NSA's CNSA 2.0 suite: https://en.wikipedia.org/wiki/Commercial_National_Security_Algorithm_Suite
- ML-DSA in Go: https://pkg.go.dev/crypto/mldsa and https://pkg.go.dev/filippo.io/mldsa
- OpenSSH's post-quantum key exchange: https://quantumcomputingreport.com/openssh-10-0-introduces-default-post-quantum-key-exchange-algorithm/
