package dms

import (
	"bytes"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Shortcut is the iPhone's one-tap check-in, "I'm OK": an Apple Shortcuts
// file in the older, unsigned format, as python-shortcuts (MIT) writes it.
// For each ping URL it has a URL action and, after it, a Get Contents of URL
// action, which takes the URL as its input and posts to it. `shortcuts sign`
// signs a file of that format, which an iPhone needs before it imports it.
func Shortcut(pingURLs []string) ([]byte, error) {
	if len(pingURLs) == 0 {
		return nil, fmt.Errorf("no ping URLs: make the checks first with `irie dms checks`")
	}
	var actions []any
	for _, u := range pingURLs {
		p, err := url.Parse(u)
		if err != nil || p.Scheme != "https" || p.Host == "" {
			return nil, fmt.Errorf("%q is not an https ping URL", u)
		}
		actions = append(actions,
			plistDict{
				{"WFWorkflowActionIdentifier", "is.workflow.actions.url"},
				{"WFWorkflowActionParameters", plistDict{{"WFURLActionURL", u}}},
			},
			plistDict{
				{"WFWorkflowActionIdentifier", "is.workflow.actions.downloadurl"},
				{"WFWorkflowActionParameters", plistDict{{"WFHTTPMethod", "POST"}}},
			},
		)
	}
	doc := plistDict{
		{"WFWorkflowActions", actions},
		{"WFWorkflowClientRelease", "2.0"},
		{"WFWorkflowClientVersion", "700"},
		{"WFWorkflowIcon", plistDict{
			{"WFWorkflowIconGlyphNumber", 59511},
			{"WFWorkflowIconImageData", []byte{}},
			{"WFWorkflowIconStartColor", 431817727},
		}},
		{"WFWorkflowImportQuestions", []any{}},
		{"WFWorkflowInputContentItemClasses", []any{"WFStringContentItem", "WFURLContentItem"}},
		{"WFWorkflowTypes", []any{"NCWidget", "WatchKit"}},
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	writePlist(&b, doc, 0)
	b.WriteString("</plist>\n")
	return b.Bytes(), nil
}

// plistDict is a property list dictionary whose keys keep their order.
type plistDict []struct {
	k string
	v any
}

// writePlist writes v as XML property list elements (Apple's plist(5)):
// dict, array, string, integer and data.
func writePlist(b *bytes.Buffer, v any, depth int) {
	pad := strings.Repeat("\t", depth)
	switch v := v.(type) {
	case plistDict:
		b.WriteString(pad + "<dict>\n")
		for _, kv := range v {
			b.WriteString(pad + "\t<key>" + xmlText(kv.k) + "</key>\n")
			writePlist(b, kv.v, depth+1)
		}
		b.WriteString(pad + "</dict>\n")
	case []any:
		if len(v) == 0 {
			b.WriteString(pad + "<array/>\n")
			return
		}
		b.WriteString(pad + "<array>\n")
		for _, x := range v {
			writePlist(b, x, depth+1)
		}
		b.WriteString(pad + "</array>\n")
	case string:
		b.WriteString(pad + "<string>" + xmlText(v) + "</string>\n")
	case int:
		fmt.Fprintf(b, "%s<integer>%d</integer>\n", pad, v)
	case []byte:
		if len(v) == 0 {
			b.WriteString(pad + "<data></data>\n")
			return
		}
		panic("plist: data that is not empty")
	default:
		panic(fmt.Sprintf("plist: %T", v))
	}
}

func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// CheckAddress is the email address that checks a check in: its UUID at the
// ping URL's host, hc-ping.com for the hosted service.
func CheckAddress(r Ref, domain string) (string, error) {
	if domain == "" {
		u, err := url.Parse(r.PingURL)
		if err != nil || u.Hostname() == "" {
			return "", fmt.Errorf("“%s” has no ping URL", r.Name)
		}
		domain = u.Hostname()
	}
	if r.UUID == "" || strings.ContainsAny(r.UUID, "@ ,<>") || strings.ContainsAny(domain, "@ ,<>/") {
		return "", fmt.Errorf("“%s” has no address", r.Name)
	}
	return r.UUID + "@" + domain, nil
}

// Contact is the check-in for any phone: a contact card (vCard 3.0, which
// phones' address books import) called "I'm OK", with both checks' email
// addresses and a mailto: link that writes to both at once (RFC 6068), for
// one email, sent, to check in.
func Contact(addresses []string) ([]byte, error) {
	if len(addresses) == 0 || slices.Contains(addresses, "") {
		return nil, fmt.Errorf("no addresses: make the checks first with `irie dms checks`")
	}
	link := "mailto:" + strings.Join(addresses, ",") + "?subject=" + url.PathEscape("I'm OK") + "&body=" + url.PathEscape("I'm OK.")
	var b bytes.Buffer
	line := func(s string) { fold(&b, s) }
	line("BEGIN:VCARD")
	line("VERSION:3.0")
	line("FN:I'm OK")
	line("N:;I'm OK;;;")
	for _, a := range addresses {
		line("EMAIL;TYPE=INTERNET:" + a)
	}
	line("URL:" + link)
	line("NOTE:" + vcardText("To check in to your dead man's switch, open the link above and send the email it starts: it goes to both checks. Keep it short; whoever gets the alarm sees your last check-in."))
	line("END:VCARD")
	return b.Bytes(), nil
}

// vcardText escapes a vCard 3.0 text value (RFC 2426 §4).
func vcardText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(s)
}
