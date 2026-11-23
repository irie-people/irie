# A cost ledger with dated prices

**Status:** built (2026-11-23). DESIGN §6.3 ("The ledger") says how it works, and §6.4 how the router reads it.

## The problem

The router picks the profile with the lowest cost per success (DESIGN §6.4). Its cost per attempt is what a profile's sessions of a class cost on average, read from Claude Code's status line (`cost.total_cost_usd`, kept in `status.json`). Irie checked that figure against its own pricing. The test covered the 545 sessions Irie started from 2026-10-27 to 2026-11-23. Each request in their transcripts was priced at Anthropic's list prices, and 480 sessions had both figures to compare:

- **They agree overall.** The status lines came to $8,697 and the transcripts to $8,972. 355 sessions are within 10% of each other, and the median ratio is 1.02.
- **A reopen starts the figure at $0.** A reopened session is a new Claude Code process, so its figure leaves out everything spent before. In 32 sessions the figure is under half of what the transcripts price, and 30 of those were reopened:
  - 21 show $0.00. Their calls came to $524, and the router skips them.
  - 11 show part of their cost, $173 short in all, and the router averages them in.
- **Local models get a price.** Claude Code prices a model it does not know. 30 calls to `dory-4b` and `bonsai-2:9b` showed $0.80, though both run on this Mac or on the Asahi, where they cost nothing. The router skips that figure for an open local session, but takes it for a closed one.
  - That would move work off the free model. Dory wins small work today because it is free. After three Dory sessions priced at about $0.36, its cost per success for small work would be 0.031 against Haiku's 0.0014. Small work would then go to Haiku, against the owner's rule that local models and our own servers cost $0.
- **Some calls are left out of the top-level usage.** In 82 sessions the status line is over 10% above the transcripts' top-level usage, $404 in all.
  - Anthropic's docs say a compaction's summary and the advisor tool's sub-inference are reported only in `usage.iterations`. Web searches cost $10 a thousand on top of tokens. Irie's tally read none of these.
  - The rest is calls Claude Code makes outside the transcript, such as the prompt hooks behind Mikael's door.
- **Irie's own calls are free, and nothing records them.** `route.embed`, `find`, materialized cards (20, all on `bonsai-2:9b`) and terms all run on local models. Only failures reach `core.log`: 22 embedding calls that could not connect, and one card that missed its schema.
  - None was refused for something the model can't do. So learning a model's limits from its refusals, as Blitz does (spec_056 CA-13), would have nothing to learn yet.

The two faults that matter are both of Claude Code's figure: it can send the free model's work to a paid one, and it makes reopened sessions look cheap. The status line can't fix either. Irie already reads every request's usage from the transcripts, for the token tally and the prompt-cache meter (§6.3), so the fix is to price those requests itself.

## What it does

1. **Dated prices.** Each list price is a row with the day it took effect. A call is priced by the row in effect when it was made. A price change is a new row, so earlier calls keep the price they were charged, and a build made before a change still prices calls from before it correctly. Anthropic's page shows these changes happen: Sonnet 5 launched at an introductory price set to end on 2026-08-31, and the rise that was to follow was later called off. Web searches are priced per search.
2. **A call-by-call ledger.** It is read from the transcripts Irie already follows. Each request counts once, by its message id:
   - its usage by kind, with the `usage.iterations` the top level leaves out, and its web searches;
   - its price on that day;
   - its purpose: `turn`, `helper`, `compaction` or `advisor`.

   Two rules decide a call's price:
   - **Free.** A local profile's calls are free, never Claude Code's guess. That covers this Mac, Irie's llama-server and a peer's server.
   - **Unknown.** A call to a model with no price makes its session's cost unknown, never $0 and never a part: "$12.40, plus 3 calls with no price".

   Each session's totals, by day, model and purpose, go into `sessions/<id>/costs.json`, so a closed session keeps them. Sessions closed before the ledger are priced once from their transcripts, in the background.
3. **Cost per success reads the ledger.** A session's cost is the sum of its calls. When any call is unpriced the cost is unknown, and the session is left out of the average. A status-line figure counts only for a session with no ledger, and never for a local profile's session.
4. **Irie's own calls.** Each embedding (`route.embed`, `find`) and each card the core asks a model for is a row in `ledger.jsonl`, with its purpose, model, tokens and time. Such a call is free on this machine and on the owner's own servers, and unpriced on Ollama's cloud.
5. **`irie ctl costs`.** Shows today, the week and 30 days by purpose and model, with unpriced calls counted apart. The costliest sessions are listed with Claude Code's figure beside the ledger's.

## Measured after

These numbers come from the live machine on 2026-11-23, through `irie ctl costs --json`, once the sweep had priced the closed sessions:

- **The month.** 77,242 calls cost $9,368 at list price. 30 calls ran on `dory-4b` and `bonsai-2:9b` and were free. Every other model had a price, so no call was unpriced.
- **Against the status lines.** 508 sessions have both figures: the ledger says $9,036 and the status lines $7,609.
  - 68 status lines read under half of the ledger, and 55 of those read $0.00. $1,884 is missing in all.
  - The morning's crash made it worse: `reopen --lost` started every card's process again. Of the open sessions, 45 read $0.00 against $1,707 in the ledger.
- **The other way.** 376 sessions were never reopened, and their status lines are 6.4% above the ledger. The gap is 8.0% for the 161 that used WebSearch or WebFetch and 4.5% for those that did not. 64 of the 72 sessions over 1.1 times used those tools, with 1,341 searches and 2,022 fetches between them. Both tools make model calls of their own that the transcript does not record. Those calls, like the prompt hooks behind Mikael's door, show only in Claude Code's own figure.
- **The router.** Sonnet's cost for medium work, measured against Opus's, fell from 0.16 to 0.12. That is because Opus sessions no longer read low after a reopen. Each class still goes where it went: small work to Dory, medium to Sonnet, hard to Opus.
- **Fixed after the first live reading.** The first build counted the attempt a fallback model answered twice, 7 calls and $1.72 in the month. It also counted 443 replies that Claude Code writes itself as $0 calls. Version 2 prices fallbacks and refusals as Anthropic bills them, and prices every closed session again.

## Not in it

- **A model's limits learned from its refusals** (Blitz's CA-13). A month of Irie's own calls had no such refusal.
- **The session row's cost.** The Mac app, the phone apps and the sidebar still show Claude Code's figure.
- **Calls Claude Code makes outside the transcript**, such as the prompt hooks behind Mikael's door and titles. Only Claude Code's own figure has them.
- **Pricing past usage at today's prices.** For routing that would answer what a profile costs now, not what it cost then.
- **The answers of `irie terms`.** It runs as a process of its own.
