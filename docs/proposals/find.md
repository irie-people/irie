# Find a session by what it did

**Status:** built (2026-11-21). DESIGN §4.5 ("Find") says how it works.

## The problem

`irie ctl history WORDS` lists the closed sessions whose name or id has every word, among the newest 100. A person remembers what a session was about, not its name: "the one where the reinstall stole my focus", "that VPN thing". On 40 such lookups, written the way one user asks for sessions, over 493 of that user's sessions, history put the right session first 4 times (10%). Answering "what did that session do?" took a reopen, which starts an agent and costs tokens, though Irie already keeps the answer: `result.md`, what the agent said at the end of its last turn.

## What it does

`irie ctl find WORDS` searches every session Irie kept, open or closed, by its name, its task and the last thing its agent said. It prints the best few, each with whether it is open, closed (and whether reopen can still bring it back) and the first line of what its agent said, so "what did it do?" needs no reopen.

It ranks by two things at once:

- **Words**, by BM25 over the name, task and result (Robertson and Zaragoza 2009), as the terms reader already does.
- **Meaning**, by the embedding model the router already uses (`route.embed`, embeddinggemma-2 on the user's own Ollama). Each session has two vectors, its task and its result, and the nearer counts. The texts go in with EmbeddingGemma's search prompts (`task: search result | query: …` for the words looked up, `title: … | text: …` for a session).

The two orders are fused by reciprocal rank (Cormack, Clarke and Büttcher 2009), words at half the weight of meaning. A session is close when its cosine is 0.70 or more, or it has every word looked up. When none is, it says so and shows the nearest three.

## Measured

On the same 40 lookups and 493 sessions (kept out of the repository, being the user's own tasks):

| Ranking | First | In the top 5 |
|---|---|---|
| `ctl history` today | 10% | 10% |
| Words alone (BM25) | 70% | 90% |
| Meaning, one vector a session | 82% | 98% |
| Meaning, two vectors a session | 88% | 98% |
| Both, fused | 90% | 98% |

Half the lookups reuse a word of the session's, and half say it another way. Words alone found 50% of the second half first; meaning found 75%. The search prompts beat plain text (82% against 78%), and 512 of the model's 768 numbers ranked as well as all of them. Eight lookups that match no session scored at most 0.67, and the right session at least 0.72, which is where the 0.70 line comes from. 40 lookups is a small set: a difference of 5 points is two lookups.

## Costs and limits

- **The first find reads every session.** About 1,000 texts, which took 2 to 6 minutes on a busy Mac. They are read in the background, a batch at a time, so a find answers at once, by words where meaning is not ready, and says how many it is still reading. Later finds read only what is new.
- **Nothing new to run or install.** The model is the router's, on the user's own machine; with no server or no model, find ranks by words alone and says why.
- **The vectors are as private as the text.** An embedding can be turned back into much of its text, so they are kept like the tasks: `find-vectors.json` in Irie's state, readable only by the user.
- **It reads, and changes nothing** (reach: read), so an agent may run it without asking.

## Not in it

A search field in the Mac app, a `:find` in the task bar, and a "you already have a card for this" hint as a task is typed. Each can build on the same op once find has been used for a while.
