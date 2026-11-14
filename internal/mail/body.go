package mail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"mime/quotedprintable"
	"net/textproto"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/charset"

	"github.com/irie-people/irie/internal/htmltext"
	"github.com/irie-people/irie/internal/ipc"
)

// partMax is the most of a text part Irie reads: enough for any letter,
// and a bound on what one message can cost.
const partMax = 2 << 20

// part is a text part of a message, by its place in the structure.
type part struct {
	path []int
	bs   *imap.BodyStructureSinglePart
}

// read reads the message whole: its headers, its text and HTML parts and
// what files it carries. With seen, it is marked read.
func (s *session) read(id MessageID, seen bool) (ipc.MailBody, error) {
	if err := s.at(id); err != nil {
		return ipc.MailBody{}, err
	}
	uid := imap.UIDSetNum(imap.UID(id.UID))
	header := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
	opts := envelopeItems
	opts.BodyStructure = &imap.FetchItemBodyStructure{Extended: true}
	opts.BodySection = []*imap.FetchItemBodySection{header}
	bufs, err := s.c.Fetch(uid, &opts).Collect()
	if err != nil {
		return ipc.MailBody{}, err
	}
	if len(bufs) == 0 || bufs[0].UID == 0 {
		return ipc.MailBody{}, errMoved
	}
	b := bufs[0]
	role := s.roleOf(id.Mailbox)
	body := ipc.MailBody{MailMessage: summary(s.acct, role, id.Mailbox, id.Validity, b)}
	if e := b.Envelope; e != nil {
		body.Cc = addrs(e.Cc)
		if len(e.ReplyTo) > 0 && (len(e.From) == 0 || e.ReplyTo[0].Addr() != e.From[0].Addr()) {
			body.ReplyTo = addrs(e.ReplyTo[:1])[0]
		}
		body.MessageID = e.MessageID
	}
	if h, err := textproto.NewReader(headerReader(b.FindBodySection(header))).ReadMIMEHeader(); err == nil || len(h) > 0 {
		body.References = msgIDs(h.Get("References"))
		if body.MessageID == "" {
			body.MessageID = strings.Trim(strings.TrimSpace(h.Get("Message-Id")), "<>")
		}
	}
	body.Web = WebMessage(s.acct, body.MessageID)

	plain, html, files := parts(b.BodyStructure)
	body.Attachments = files
	var want []*imap.FetchItemBodySection
	for _, p := range []*part{plain, html} {
		if p == nil {
			continue
		}
		sec := &imap.FetchItemBodySection{Part: p.path, Peek: true}
		if int64(p.bs.Size) > partMax {
			sec.Partial = &imap.SectionPartial{Offset: 0, Size: partMax}
		}
		want = append(want, sec)
	}
	if len(want) > 0 {
		got, err := s.c.Fetch(uid, &imap.FetchOptions{UID: true, BodySection: want}).Collect()
		if err != nil {
			return ipc.MailBody{}, err
		}
		if len(got) > 0 {
			for i, p := range []*part{plain, html} {
				if p == nil {
					continue
				}
				var sec *imap.FetchItemBodySection
				for _, w := range want {
					if slices.Equal(w.Part, p.path) {
						sec = w
					}
				}
				text := decode(got[0].FindBodySection(sec), p.bs)
				if i == 0 {
					body.Text = text
				} else {
					body.HTML = text
				}
			}
		}
	}
	// A message with only HTML, or a text part that only says to read the
	// HTML, reads as the HTML's text.
	if body.HTML != "" && len(strings.TrimSpace(body.Text)) < 40 {
		if t := strings.TrimSpace(htmltext.Read([]byte(body.HTML), nil).Text); t != "" {
			body.Text = t
		}
	}
	body.Text = strings.TrimRight(body.Text, " \t\r\n")
	if seen && body.Unread {
		if err := s.flag(id, ipc.FlagRead); err == nil {
			body.Unread = false
		}
	}
	return body, nil
}

// roleOf is the role of a mailbox, else the mailbox's own name.
func (s *session) roleOf(mailbox string) string {
	for _, role := range ipc.MailFolders {
		if s.folders[role] == mailbox {
			return role
		}
	}
	return mailbox
}

// parts finds a message's text part and HTML part, the first of each that
// is no attachment, and the files it carries.
func parts(bs imap.BodyStructure) (plain, html *part, files []ipc.MailAttachment) {
	if bs == nil {
		return nil, nil, nil
	}
	bs.Walk(func(path []int, p imap.BodyStructure) bool {
		sp, ok := p.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		mt := sp.MediaType()
		name := sp.Filename()
		disp := ""
		if d := sp.Disposition(); d != nil {
			disp = strings.ToLower(d.Value)
		}
		attached := disp == "attachment" || (name != "" && disp != "inline")
		switch {
		case !attached && mt == "text/plain" && plain == nil:
			plain = &part{path: slices.Clone(path), bs: sp}
		case !attached && mt == "text/html" && html == nil:
			html = &part{path: slices.Clone(path), bs: sp}
		case attached || name != "" || mt == "message/rfc822" || (!strings.HasPrefix(mt, "text/") && !strings.HasPrefix(mt, "multipart/")):
			if name == "" {
				switch {
				case mt == "message/rfc822":
					name = "message.eml"
				case sp.ID != "":
					// An inline picture of the message's HTML.
					return true
				default:
					name = "file"
				}
			}
			files = append(files, ipc.MailAttachment{Name: clean(name), Type: mt, Size: int64(sp.Size)})
		}
		return true
	})
	return plain, html, files
}

// decode undoes a part's transfer encoding and charset, giving UTF-8.
func decode(raw []byte, bs *imap.BodyStructureSinglePart) string {
	var r io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(bs.Encoding) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, &noSpace{r: r})
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	cs := strings.ToLower(bs.Params["charset"])
	if cs != "" && cs != "utf-8" && cs != "utf8" && cs != "us-ascii" {
		if cr, err := charset.Reader(cs, r); err == nil {
			r = cr
		}
	}
	b, _ := io.ReadAll(io.LimitReader(r, partMax))
	s := string(b)
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// noSpace drops the line breaks and spaces of base64 text, which Go's
// decoder would refuse.
type noSpace struct{ r io.Reader }

func (n *noSpace) Read(p []byte) (int, error) {
	for {
		k, err := n.r.Read(p)
		j := 0
		for _, c := range p[:k] {
			if c != '\r' && c != '\n' && c != ' ' && c != '\t' {
				p[j] = c
				j++
			}
		}
		if j > 0 || err != nil {
			return j, err
		}
	}
}

// msgIDs reads a References header's message IDs, without their angle
// brackets.
func msgIDs(h string) []string {
	var out []string
	for _, f := range strings.Fields(h) {
		if id := strings.Trim(f, "<>,"); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// headerReader is a textproto reader's source over a header block, which
// it reads to its blank line.
func headerReader(b []byte) *bufio.Reader {
	if !bytes.HasSuffix(b, []byte("\r\n\r\n")) && !bytes.HasSuffix(b, []byte("\n\n")) {
		b = append(slices.Clone(b), "\r\n\r\n"...)
	}
	return bufio.NewReader(bytes.NewReader(b))
}
