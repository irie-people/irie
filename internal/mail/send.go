package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// sendWait is how long a send may take, the message's upload included.
const sendWait = 90 * time.Second

// Recipients reads a draft's To, Cc and Bcc lines, each of which may hold
// several addresses, "Name <a@b>" or a@b, split by commas.
func Recipients(lines []string) ([]*netmail.Address, error) {
	var out []*netmail.Address
	for _, l := range lines {
		l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), ","))
		if l == "" {
			continue
		}
		list, err := netmail.ParseAddressList(l)
		if err != nil {
			return nil, fmt.Errorf("%q isn't an address "+brand.Name+" can send to", l)
		}
		for _, a := range list {
			if !config.MailAddress(a.Address) {
				return nil, fmt.Errorf("%q isn't an address "+brand.Name+" can send to", a.Address)
			}
			out = append(out, a)
		}
	}
	return out, nil
}

// Compose writes the draft as a message from the account, threaded under
// the message it answers when there is one. It says the message's own ID.
func Compose(a Account, d ipc.MailDraft, answers *ipc.MailBody, now time.Time) ([]byte, string, error) {
	to, err := Recipients(d.To)
	if err != nil {
		return nil, "", err
	}
	cc, err := Recipients(d.Cc)
	if err != nil {
		return nil, "", err
	}
	if len(to)+len(cc) == 0 {
		if bcc, _ := Recipients(d.Bcc); len(bcc) == 0 {
			return nil, "", errors.New("the message goes to no one: give it an address in To")
		}
	}
	_, domain, _ := strings.Cut(a.Address, "@")
	var nonce [12]byte
	rand.Read(nonce[:])
	id := fmt.Sprintf("%d.%s@%s", now.Unix(), hex.EncodeToString(nonce[:]), domain)

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", (&netmail.Address{Address: a.Address}).String())
	if len(to) > 0 {
		header("To", list(to))
	}
	if len(cc) > 0 {
		header("Cc", list(cc))
	}
	header("Subject", mime.QEncoding.Encode("utf-8", oneLine(d.Subject)))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+id+">")
	if answers != nil && answers.MessageID != "" {
		header("In-Reply-To", "<"+answers.MessageID+">")
		refs := append(append([]string{}, answers.References...), answers.MessageID)
		if len(refs) > 20 {
			refs = append(refs[:1], refs[len(refs)-19:]...)
		}
		header("References", "<"+strings.Join(refs, "> <")+">")
	}
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	w := quotedprintable.NewWriter(&b)
	text := strings.ReplaceAll(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\r", "\n")
	if _, err := w.Write([]byte(text)); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	b.WriteString("\r\n")
	return b.Bytes(), id, nil
}

func list(as []*netmail.Address) string {
	s := make([]string, len(as))
	for i, a := range as {
		s[i] = a.String()
	}
	return strings.Join(s, ", ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// send sends the message through the account's SMTP server, to every
// address of the draft, Bcc's too.
func send(ctx context.Context, a Account, password string, pins *Pins, d ipc.MailDraft, msg []byte) error {
	var rcpts []string
	for _, l := range [][]string{d.To, d.Cc, d.Bcc} {
		as, err := Recipients(l)
		if err != nil {
			return err
		}
		for _, x := range as {
			rcpts = append(rcpts, x.Address)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, sendWait)
	defer cancel()
	dialer := &net.Dialer{Timeout: dialWait}
	var conn net.Conn
	var err error
	if a.SMTP.TLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: pins.TLS(a.SMTP)}).DialContext(ctx, "tcp", a.SMTP.Addr())
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", a.SMTP.Addr())
	}
	if err != nil {
		return explainSMTP(a, err)
	}
	defer context.AfterFunc(ctx, func() { conn.Close() })()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, a.SMTP.Host)
	if err != nil {
		conn.Close()
		return explainSMTP(a, err)
	}
	defer c.Close()
	if !a.SMTP.TLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s doesn't offer STARTTLS, so "+brand.Name+" won't send the password over it", a.SMTP.Addr())
		}
		if err := c.StartTLS(pins.TLS(a.SMTP)); err != nil {
			return explainSMTP(a, err)
		}
	}
	if err := c.Auth(smtp.PlainAuth("", a.Username, password, a.SMTP.Host)); err != nil {
		return explainSMTP(a, err)
	}
	if err := c.Mail(a.Address); err != nil {
		return explainSMTP(a, err)
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("%s turned down %s: %v", a.SMTP.Host, r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return explainSMTP(a, err)
	}
	if _, err := w.Write(msg); err != nil {
		return explainSMTP(a, err)
	}
	if err := w.Close(); err != nil {
		return explainSMTP(a, err)
	}
	return c.Quit()
}

func explainSMTP(a Account, err error) error {
	msg := err.Error()
	switch {
	case errors.Is(err, ErrPinChanged):
		return err
	case strings.HasPrefix(msg, "535") || strings.Contains(msg, "Username and Password not accepted"):
		if a.Kind == config.MailGmail {
			return fmt.Errorf("Gmail turned the password down for sending as %s: it takes an app password (https://myaccount.google.com/apppasswords)", a.Address)
		}
		if a.Kind == config.MailProton {
			return fmt.Errorf("Proton Mail Bridge turned the password down for sending as %s: it takes the Bridge password from Bridge's mailbox details", a.Address)
		}
		return fmt.Errorf("the server turned the password down for sending as %s", a.Address)
	case a.Kind == config.MailProton && a.SMTP.Local() && strings.Contains(msg, "connection refused"):
		return fmt.Errorf("Proton Mail Bridge isn't running, or isn't signed in: open it and sign in to %s", a.Address)
	}
	return fmt.Errorf("sending as %s: %v", a.Address, err)
}
