package mail

import (
	"io"
	"os"
	"testing"
)

func TestMessageWriter(t *testing.T) {

	buf, err := os.Create("test.msg")
	if err != nil {
		t.Fatal(err)
	}

	email := NewMSG()
	email.SetFrom("test@gmail.com")
	email.SetSubject("test")
	email.AddTo("test@gmail.com")
	email.AddAttachment("10.enc.mp3", "application/octet-stream", "FNS_1GN-IP-ZAICEV_5902_de0978f9b9e611ebb485574d8d9a55b9_01_01_01.zip")
	//email.AddAttachment("email_test.go")
	email.SetBody(TextPlain, "just test\r\n")
	email.Encoding = EncodingNone

	email.AddAttachmentBase64("dGVzdCBiYXJvdHJhdW1hCg==", "NO_NDFL6_5902_5902_5902174276590201001_20200506_29d5b070-828f-4f7e-afe3-3bf8dd75034d.xml")

	msg := newMessage(email)

	println("Got size", msg.GetSize())
	test := 0
	for {
		buff := make([]byte, 1024*1024)
		n, err := msg.Read(buff)
		println("Got n bytes", n)
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		test += n
		if n > 0 {
			buff = buff[:n]
			buff = append(buff, '\n')
			_, wrErr := buf.Write(buff)
			if wrErr != nil {
				t.Fatal(wrErr)
			}
		}
		if err == io.EOF {
			break
		}
	}

	println("Writes: ", test)

}
