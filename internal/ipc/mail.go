package ipc

import "time"

// OpMail reads and acts on the mail plugin's accounts
// (docs/proposals/mail.md): a folder's messages across the accounts, one
// message, a flag or a move, a message sent, an account added, checked or
// removed. Mail is the person's alone: the core refuses every call from
// inside an agent, a look as much as a send. It answers a MailResult.
const OpMail = "mail"

// What MailArgs.Do asks for.
const (
	MailList   = ""       // a folder's messages across the accounts
	MailRead   = "read"   // one message, whole
	MailFlag   = "flag"   // mark a message read, unread, starred or not
	MailMove   = "move"   // move a message to another folder
	MailSend   = "send"   // send a message
	MailAdd    = "add"    // add an account, once its password works
	MailRemove = "remove" // remove an account and its password
	MailTest   = "test"   // sign in to an account again, to check it
	MailTrust  = "trust"  // trust the certificate a server on this computer shows now
)

// The folders every account has, by what they are for: Irie finds each
// account's own by the server's special-use marks, else by its usual name.
const (
	FolderInbox   = "inbox"
	FolderStarred = "starred"
	FolderSent    = "sent"
	FolderDrafts  = "drafts"
	FolderArchive = "archive"
	FolderJunk    = "junk"
	FolderTrash   = "trash"
)

// MailFolders are the folders, in the order a client lists them.
var MailFolders = []string{FolderInbox, FolderStarred, FolderSent, FolderDrafts, FolderArchive, FolderJunk, FolderTrash}

// The flags MailArgs.Flag sets.
const (
	FlagRead   = "read"
	FlagUnread = "unread"
	FlagStar   = "star"
	FlagUnstar = "unstar"
)

// MailArgs ask the mail op for something; Do says what.
type MailArgs struct {
	Do string `json:"do,omitempty"`
	// Accounts, for the list, are the accounts to read, by name; empty =
	// every one.
	Accounts []string `json:"accounts,omitempty"`
	// Folder is the folder to list, such as inbox, the default.
	Folder string `json:"folder,omitempty"`
	// Query keeps, in the list, only the messages with these words.
	Query string `json:"query,omitempty"`
	// Limit is how many of each account's newest messages the list
	// holds; 0 = mail.fetch.
	Limit int `json:"limit,omitempty"`
	// Refresh reads the accounts again now, however fresh the last look.
	Refresh bool `json:"refresh,omitempty"`
	// ID is the message to read, flag or move.
	ID string `json:"id,omitempty"`
	// Seen marks the message read as it is read.
	Seen bool `json:"seen,omitempty"`
	// Flag is the flag to set: read, unread, star or unstar.
	Flag string `json:"flag,omitempty"`
	// To is the folder to move the message to: inbox, archive, junk or
	// trash.
	To string `json:"to,omitempty"`
	// Draft is the message to send.
	Draft *MailDraft `json:"draft,omitempty"`
	// New is the account to add.
	New *MailNew `json:"new,omitempty"`
	// Name is the account to remove, test or trust.
	Name string `json:"name,omitempty"`
}

// MailNew is an account to add: what config.toml takes, and its password,
// which goes to the Keychain once it signs in.
type MailNew struct {
	Name     string `json:"name,omitempty"` // "" = from the address
	Kind     string `json:"kind"`           // gmail, proton or imap
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	IMAP     string `json:"imap,omitempty"`
	SMTP     string `json:"smtp,omitempty"`
	Colour   int    `json:"colour,omitempty"`
	Password string `json:"password"`
}

// MailDraft is a message to send.
type MailDraft struct {
	From    string   `json:"from"` // the account it goes from, by name
	To      []string `json:"to"`
	Cc      []string `json:"cc,omitempty"`
	Bcc     []string `json:"bcc,omitempty"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	// Reply is the message it answers, by ID: the answer threads under it,
	// and the message is marked answered.
	Reply string `json:"reply,omitempty"`
}

// MailResult is the mail op's answer.
type MailResult struct {
	// Accounts are every account, with how each stands.
	Accounts []MailAccount `json:"accounts"`
	// Messages are the list's, newest first, every account's together.
	Messages []MailMessage `json:"messages,omitempty"`
	// Message is the message read.
	Message *MailBody `json:"message,omitempty"`
	// Text is the answer in words, for a task bar and irie mail.
	Text string `json:"text,omitempty"`
}

// MailAccount is one account, and how it stands.
type MailAccount struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`     // gmail, proton or imap
	Provider string `json:"provider"` // Gmail, Proton Mail or Mail, as it shows
	Address  string `json:"address"`
	// Colour is its colour among Irie's six, 0 to 5, as the agents'
	// colours are numbered.
	Colour int `json:"colour"`
	// Unread is how many messages in its inbox are unread, by the last
	// look.
	Unread int `json:"unread"`
	// State is ready, checking (not read yet), needs (it lacks its
	// password) or error (the last look failed: Problem says why).
	State   string    `json:"state"`
	Problem string    `json:"problem,omitempty"`
	Fetched time.Time `json:"fetched,omitzero"` // when its messages were last read
	// Web opens its mail in the provider's own web page.
	Web string `json:"web,omitempty"`
}

// A mail account's states.
const (
	MailReady    = "ready"
	MailChecking = "checking"
	MailNeeds    = "needs"
	MailError    = "error"
)

// MailMessage is one message as a list shows it.
type MailMessage struct {
	// ID names the message to the mail op: its account, its folder and
	// its place there.
	ID       string    `json:"id"`
	Account  string    `json:"account"`
	Folder   string    `json:"folder"` // such as inbox
	From     string    `json:"from"`   // the sender's name, else address
	FromAddr string    `json:"from_addr,omitempty"`
	To       []string  `json:"to,omitempty"`
	Subject  string    `json:"subject"`
	Date     time.Time `json:"date"`
	Unread   bool      `json:"unread,omitempty"`
	Starred  bool      `json:"starred,omitempty"`
	Answered bool      `json:"answered,omitempty"`
	Size     int64     `json:"size,omitempty"`
}

// MailBody is one message, whole.
type MailBody struct {
	MailMessage
	Cc      []string `json:"cc,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"` // where an answer goes, when not to the sender
	// Text is the message as plain text: its own text part, else its
	// HTML read as text.
	Text string `json:"text"`
	// HTML is its HTML part, when it has one, for a client that shows it
	// with remote content, scripts and forms blocked.
	HTML        string           `json:"html,omitempty"`
	Attachments []MailAttachment `json:"attachments,omitempty"`
	// MessageID and References thread an answer under it.
	MessageID  string   `json:"message_id,omitempty"`
	References []string `json:"references,omitempty"`
	// Web opens it in the provider's web mail, where it can.
	Web string `json:"web,omitempty"`
}

// MailAttachment is a file a message carries.
type MailAttachment struct {
	Name string `json:"name"`
	Type string `json:"type"` // such as application/pdf
	Size int64  `json:"size"` // in bytes, as sent, so about a third more than the file
}
