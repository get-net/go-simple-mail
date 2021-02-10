package mail

import (
	"fmt"
	"io"
	"os"
	"testing"
)

type BdatWriter struct {
	chunkSize int
	dst       io.Writer
}

func (bw *BdatWriter) Write(data []byte) (int, error) {
	chunkSize := len(data)
	n, err := fmt.Fprintf(bw.dst, "BDAT %d\n", chunkSize)
	if err != nil {
		return n, fmt.Errorf("writing BDAT LAST failed: %w", err)
	}
	nData, err := bw.dst.Write(data)
	if err != nil {
		return nData, err
	}
	if bw.chunkSize > 0 && bw.chunkSize > chunkSize {
		n, err := fmt.Fprintf(bw.dst, "BDAT 0 LAST\n")
		if err != nil {
			return n, fmt.Errorf("writing BDAT LAST failed: %w", err)
		}
	}
	bw.chunkSize = chunkSize
	return nData, nil
}

func TestMessageWriter(t *testing.T) {

	//buf := new(bytes.Buffer)

	buf, err := os.Create("test.msg")
	if err != nil {
		t.Fatal(err)
	}
	//
	//bdatW := &BdatWriter{dst: buf}
	//var wr = bufio.NewWriterSize(bdatW, 200)
	//defer wr.Flush()

	email := NewMSG()
	email.SetFrom("test@gmail.com")
	email.SetSubject("test")
	email.AddTo("test@gmail.com")
	email.AddAttachment("10.enc.mp3")
	//email.AddAttachment("email_test.go")
	email.SetBody(TextPlain, "just test\r\n")
	email.Encoding = EncodingNone

	email.AddAttachmentBase64("dGVzdCBiYXJvdHJhdW1hCg==", "NO_NDFL6_5902_5902_5902174276590201001_20200506_29d5b070-828f-4f7e-afe3-3bf8dd75034d.xml")

	msg := newMessage(email)

	//	msg.addFiles(email.inlines, true)
	//	if email.hasRelatedPart() {
	//		msg.closeMultipart()
	//	}

	//	msg.addFiles(email.attachments, false)
	//	if email.hasMixedPart() {
	//		msg.closeMultipart()
	//	}
	//io.CopyBuffer(buf,msg,make([]byte,100))
	n, err := io.Copy(buf, msg)
	if err != nil {
		t.Fatal(err)
	}
	println("Writes: ", n)
}
