package mail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type message struct {
	body     io.Writer
	writers  []*multipart.Writer
	parts    uint8
	cids     map[string]string
	charset  string
	encoding encoding
}

func newMessage(email *Email, writer io.Writer) *message {
	message := message{
		cids:     make(map[string]string),
		charset:  email.Charset,
		encoding: email.Encoding}

	if writer != nil {
		message.body = writer
	} else {
		message.body = new(bytes.Buffer)
	}

	// add date if not exist
	if date := email.headers.Get("Date"); date == "" {
		email.headers.Set("Date", time.Now().Format(time.RFC1123Z))
	}

	message.write(email.headers, nil, message.encoding)

	return &message
}

func encodeHeader(text string, charset string, usedChars int) string {
	// create buffer
	buf := new(bytes.Buffer)

	// encode
	encoder := newEncoder(buf, charset, usedChars)
	encoder.encode([]byte(text))

	return buf.String()
}

func getHeaders(header textproto.MIMEHeader, charset string) string {
	var headers string
	for header, values := range header {
		headers += header + ": " + encodeHeader(strings.Join(values, ", "), charset, len(header)+2) + "\r\n"
	}
	return headers
}

// getCID gets the generated CID for the provided text
func (msg *message) getCID(text string) (cid string) {
	// set the date format to use
	const dateFormat = "20060102.150405"

	// get the cid if we have one
	cid, exists := msg.cids[text]
	if !exists {
		// generate a new cid
		cid = time.Now().Format(dateFormat) + "." + strconv.Itoa(len(msg.cids)+1) + "@mail.0"
		// save it
		msg.cids[text] = cid
	}

	return
}

// replaceCIDs replaces the CIDs found in a text string
// with generated ones
func (msg *message) replaceCIDs(text string) string {
	// regular expression to find cids
	re := regexp.MustCompile(`(src|href)="cid:(.*?)"`)
	// replace all of the found cids with generated ones
	for _, matches := range re.FindAllStringSubmatch(text, -1) {
		cid := msg.getCID(matches[2])
		text = strings.Replace(text, "cid:"+matches[2], "cid:"+cid, -1)
	}

	return text
}

// openMultipart creates a new part of a multipart message
func (msg *message) openMultipart(multipartType string) {
	// create a new multipart writer
	msg.writers = append(msg.writers, multipart.NewWriter(msg.body))
	// create the boundary
	contentType := "multipart/" + multipartType + "; boundary=" + msg.writers[msg.parts].Boundary()

	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", contentType)

	// if no existing parts, add header to main header group
	if msg.parts != 0 {
		msg.writers[msg.parts-1].CreatePart(header)
	}

	msg.write(header, nil, EncodingNone)
	msg.parts++
}

// closeMultipart closes a part of a multipart message
func (msg *message) closeMultipart() {
	if msg.parts > 0 {
		msg.writers[msg.parts-1].Close()
		msg.parts--
	}
}

// base64Encode base64 encodes the provided text with line wrapping
func base64Encode(text []byte) []byte {
	// create buffer
	buf := new(bytes.Buffer)

	// create base64 encoder that linewraps
	encoder := base64.NewEncoder(base64.StdEncoding, &base64LineWrap{writer: buf})

	// write the encoded text to buf
	encoder.Write(text)
	encoder.Close()

	return buf.Bytes()
}

// qpEncode uses the quoted-printable encoding to encode the provided text
func qpEncode(text []byte) []byte {
	// create buffer
	buf := new(bytes.Buffer)

	encoder := quotedprintable.NewWriter(buf)

	encoder.Write(text)
	encoder.Close()

	return buf.Bytes()
}

const maxLineChars = 400

type base64LineWrap struct {
	writer       io.Writer
	numLineChars int
}

func (e *base64LineWrap) Write(p []byte) (n int, err error) {
	n = 0
	// while we have more chars than are allowed
	for len(p)+e.numLineChars > maxLineChars {
		numCharsToWrite := maxLineChars - e.numLineChars
		// write the chars we can
		e.writer.Write(p[:numCharsToWrite])
		// write a line break
		e.writer.Write([]byte("\r\n"))
		// reset the line count
		e.numLineChars = 0
		// remove the chars that have been written
		p = p[numCharsToWrite:]
		// set the num of chars written
		n += numCharsToWrite
	}

	// write what is left
	e.writer.Write(p)
	e.numLineChars += len(p)
	n += len(p)

	return
}

func (msg *message) write(header textproto.MIMEHeader, body []byte, encoding encoding) {
	if msg.parts == 0 {
		headers := getHeaders(header, msg.charset)
		msg.writeBody([]byte(headers), EncodingQuotedPrintable)
	} else {
		msg.writers[msg.parts-1].CreatePart(header)
	}
	if len(body) != 0 {
		msg.writeBody(body, encoding)
	}
}

func (msg *message) writeBody(body []byte, encoding encoding) {
	// encode and write the body
	switch encoding {
	case EncodingQuotedPrintable:
		msg.body.Write(qpEncode(body))
	case EncodingBase64:
		msg.body.Write(base64Encode(body))
	default:
		msg.body.Write(body)
	}
}

func (msg *message) addBody(contentType string, body []byte) {
	body = []byte(msg.replaceCIDs(string(body)))

	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", contentType+"; charset="+msg.charset)
	header.Set("Content-Transfer-Encoding", msg.encoding.string())
	msg.write(header, body, msg.encoding)
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

func (msg *message) addFiles(files []*file, inline bool) {
	encoding := EncodingBase64
	if msg.encoding == EncodingNone {
		encoding = EncodingNone
	}

	for _, file := range files {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", file.mimeType+"; name=\""+encodeHeader(escapeQuotes(file.filename), msg.charset, 6)+`"`)
		header.Set("Content-Transfer-Encoding", encoding.string())

		if inline {
			header.Set("Content-Disposition", "inline; filename=\""+encodeHeader(escapeQuotes(file.filename), msg.charset, 10)+`"`)
			header.Set("Content-ID", "<"+msg.getCID(file.filename)+">")
		} else {
			header.Set("Content-Disposition", "attachment; filename=\""+encodeHeader(escapeQuotes(file.filename), msg.charset, 10)+`"`)
		}
		msg.write(header, nil, EncodingQuotedPrintable)
		if len(file.data) > 0 {
			msg.body.Write(file.data)
		} else {
			reader, err := os.Open(file.filename)
			if err == nil {
				if encoding == EncodingBase64 {
					encoder := base64.NewEncoder(base64.StdEncoding, msg.body)
					io.Copy(encoder, reader)
				} else {
					io.Copy(msg.body, reader)
				}
				reader.Close()
			}
		}
	}
}
