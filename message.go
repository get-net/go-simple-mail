package mail

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type message struct {
	bodySend        bool
	fileHeaderSend  bool
	body            *bytes.Buffer
	writers         []*multipart.Writer
	encoderBuff     bytes.Buffer
	encoder         io.WriteCloser
	attachmentIndex int
	inlineIndex     int
	attachments     []*file
	inlines         []*file
	parts           uint8
	cids            map[string]string
	charset         string
	encoding        encoding
}

func newMessage(email *Email) *message {
	message := message{
		cids:            make(map[string]string),
		charset:         email.Charset,
		encoding:        email.Encoding,
		attachmentIndex: 0,
		attachments:     email.attachments,
		inlineIndex:     0,
		inlines:         email.inlines,
		bodySend:        false,
		fileHeaderSend:  false,
		body:            new(bytes.Buffer),
	}

	// add date if not exist
	if date := email.headers.Get("Date"); date == "" {
		email.headers.Set("Date", time.Now().Format(time.RFC1123Z))
	}

	message.write(email.headers, nil, message.encoding)

	message.encoder = base64.NewEncoder(base64.StdEncoding, &base64LineWrap{writer: &message.encoderBuff})

	if email.hasMixedPart() {
		message.openMultipart("mixed")
	}

	if email.hasRelatedPart() {
		message.openMultipart("related")
	}

	if email.hasAlternativePart() {
		message.openMultipart("alternative")
	}

	message.writeBody([]byte("\r\n"), EncodingNone)
	for _, part := range email.parts {
		message.addBody(part.contentType, part.body.Bytes())
	}

	if email.hasAlternativePart() {
		message.closeMultipart()
	}

	return &message
}

func encodeHeader(text string, charset string, usedChars int, limit bool) string {
	// create buffer
	buf := new(bytes.Buffer)

	// encode
	encoder := newEncoder(buf, charset, usedChars, limit)
	encoder.encode([]byte(text))

	return buf.String()
}

func getHeaders(header textproto.MIMEHeader, charset string, limit bool) string {
	var headers string
	for header, values := range header {
		headers += header + ": " + encodeHeader(strings.Join(values, ", "), charset, len(header)+2, limit) + "\r\n"
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
	if msg.parts == 0 {
		msg.write(header, nil, EncodingQuotedPrintable)
	} else {
		msg.writers[msg.parts-1].CreatePart(header)
	}

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
	limit := true
	if encoding == EncodingNone {
		limit = false
	}

	if msg.parts == 0 {
		headers := getHeaders(header, msg.charset, limit)
		msg.writeBody([]byte(headers), EncodingNone)
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
	limit := true
	if msg.encoding == EncodingNone {
		encoding = EncodingNone
		limit = false
	}

	for _, file := range files {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", file.mimeType+"; name=\""+encodeHeader(escapeQuotes(file.filename),
			msg.charset, 6, limit)+`"`)
		header.Set("Content-Transfer-Encoding", encoding.string())
		if file.size > 0 {
			header.Set("Content-Length", strconv.FormatInt(file.size, 10))
		}

		if inline {
			header.Set("Content-Disposition", "inline; filename=\""+encodeHeader(escapeQuotes(file.filename),
				msg.charset, 10, limit)+`"`)
			header.Set("Content-ID", "<"+msg.getCID(file.filename)+">")
		} else {
			header.Set("Content-Disposition", "attachment; filename=\""+
				encodeHeader(escapeQuotes(file.filename), msg.charset, 10, limit)+`"`)
		}
		msg.write(header, nil, EncodingQuotedPrintable)
		if encoding == EncodingBase64 {
			encoder := base64.NewEncoder(base64.StdEncoding, msg.body)
			io.Copy(encoder, file.reader)
		} else {
			io.Copy(msg.body, file.reader)
		}
	}
}

func (msg *message) AddFileHeaders(index int, inline bool) error {
	var files []*file

	encoding := EncodingBase64
	limit := false
	//if msg.encoding == EncodingNone {
	//	encoding = EncodingNone
	//}
	//
	//if msg.encoding == EncodingBase64 {
	//	limit = true
	//}

	if inline {
		files = msg.inlines
	} else {
		files = msg.attachments
	}
	if !inline && index >= len(files) {
		return errors.New("index out of range")
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", files[index].mimeType+"; name=\""+
		encodeHeader(escapeQuotes(files[index].filename), msg.charset, 6, limit)+`"`)
	println("Content-Type", files[index].mimeType+"; name=\""+
		encodeHeader(escapeQuotes(files[index].filename), msg.charset, 6, limit)+`"`)
	header.Set("Content-Transfer-Encoding", encoding.string())

	if files[index].size > 0 {
		header.Set("Content-Length", strconv.FormatInt(files[index].size, 10))
	}

	if inline {
		header.Set("Content-Disposition", "inline; filename=\""+
			encodeHeader(escapeQuotes(files[index].filename), msg.charset, 10, limit)+`"`)
		header.Set("Content-ID", "<"+msg.getCID(files[index].filename)+">")
	} else {
		header.Set("Content-Disposition", "attachment; filename=\""+
			encodeHeader(escapeQuotes(files[index].filename), msg.charset, 10, limit)+`"`)
	}
	msg.write(header, nil, EncodingQuotedPrintable)
	return nil
}

func (msg *message) ReadFile(p []byte, index int, inline bool) (n int, err error) {
	var files []*file
	pLen := len(p)
	binaryLen := (pLen / 100) * 70
	binBuff := make([]byte, binaryLen)

	if inline {
		files = msg.inlines
	} else {
		files = msg.attachments
	}
	if !inline && index >= len(files) {
		return 0, errors.New("index out of range")
	}

	if msg.encoding == EncodingNone {
		return files[index].reader.Read(p)
	} else {
		nBin, nErr := files[index].reader.Read(binBuff)
		if nErr != nil && nErr != io.EOF {
			return 0, nErr
		}
		binBuff = binBuff[:nBin]

		n, err = msg.encoder.Write(binBuff)
		if nErr == io.EOF {
			msg.encoder.Close()
		}
		if err != nil {
			return
		}
		n, err = msg.encoderBuff.Read(p)
		if err != nil {
			return
		}
		return
	}
}

func (msg *message) GetSize() int64 {

	// calc estimate size of
	bodyLength := msg.body.Len()
	var fileSize int64
	for _, file := range msg.attachments {
		fileSize += file.size
		bodyLength += len(file.filename)*2 + len(file.mimeType) + 116
	}
	for _, file := range msg.inlines {
		fileSize += file.size
		bodyLength += len(file.filename)*2 + len(file.mimeType) + 148
	}
	if msg.parts > 0 {
		bodyLength += (len(msg.writers[0].Boundary()) + 4) * int(msg.parts+1) * 2
	}
	if msg.encoding != EncodingNone {
		fileSize = fileSize / 100 * 136
	}
	return int64(bodyLength) + fileSize
}

func (msg *message) Read(p []byte) (n int, err error) {
	var nBody int
	offset := 0

	lenP := len(p)
	if lenP == 0 {
		return 0, errors.New("buffer should be greater than 0")
	}

	for {
		if !msg.bodySend {
			nBody, err = msg.body.Read(p[offset:])
			offset = offset + nBody
			if (err != nil && err != io.EOF) || nBody == lenP {
				break
			}
		}

		if err == io.EOF {
			msg.bodySend = true
		}

		if msg.bodySend && len(msg.attachments) == msg.attachmentIndex && len(msg.inlines) == msg.inlineIndex {
			break
		}

		if msg.bodySend {
			if len(msg.attachments) > 0 && len(msg.attachments) > msg.attachmentIndex {
				if !msg.fileHeaderSend {
					msg.AddFileHeaders(msg.attachmentIndex, false)
					msg.fileHeaderSend = true
					msg.bodySend = false
					continue
				}
				n, err = msg.ReadFile(p[offset:], msg.attachmentIndex, false)
				offset = offset + n
				if offset == lenP || (err != nil && err != io.EOF) {
					break
				}
				if err == io.EOF {
					msg.attachmentIndex++
					if len(msg.attachments) > msg.attachmentIndex {
						msg.fileHeaderSend = false
						continue
					} else {
						msg.closeMultipart()
						msg.bodySend = false
						continue
					}
				}
			}
			if len(msg.inlines) > 0 && len(msg.inlines) > msg.inlineIndex {
				if !msg.fileHeaderSend {
					msg.AddFileHeaders(msg.inlineIndex, true)
					msg.fileHeaderSend = true
					msg.bodySend = false
					continue
				}
				n, err = msg.ReadFile(p[offset:], msg.inlineIndex, true)
				offset = offset + n
				if offset == lenP || (err != nil && err != io.EOF) {
					break
				}
				if err == io.EOF {
					msg.inlineIndex++
					if len(msg.inlines) > msg.inlineIndex {
						msg.fileHeaderSend = false
						continue
					} else {
						msg.closeMultipart()
						msg.bodySend = false
						continue
					}

				}
			}
		}
		if offset == lenP {
			break
		}

	}

	return offset, err
}
