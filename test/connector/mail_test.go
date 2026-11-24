//go:build !slow

package connector_test

import (
	"net"
	"testing"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/smtpmail"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// unreachableMailers are mailers each given a server on this machine that
// cannot take mail: the stub told so, and the real mailer given a port
// nothing listens on and a listener that never answers, which it gives
// up on after a tenth of a second.
var unreachableMailers = map[string]func(t *testing.T) (connector.Mailer, connector.MailServer){
	"stub": func(*testing.T) (connector.Mailer, connector.MailServer) {
		m := &testsupport.Mailer{}
		m.FailWith("connection refused")
		return m, connector.MailServer{Host: "127.0.0.1", Port: 25, Security: connector.MailNone}
	},
	"smtp, nothing listening": func(t *testing.T) (connector.Mailer, connector.MailServer) {
		l := listen(t)
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return smtpmail.New(time.Second, nil), connector.MailServer{Host: "127.0.0.1", Port: port, Security: connector.MailNone}
	},
	"smtp, never answering": func(t *testing.T) (connector.Mailer, connector.MailServer) {
		l := listen(t)
		port := l.Addr().(*net.TCPAddr).Port
		return smtpmail.New(100*time.Millisecond, nil), connector.MailServer{Host: "127.0.0.1", Port: port, Security: connector.MailNone}
	},
}

// listen is a listener on a free port of this machine that accepts
// connections and never says anything on them.
func listen(t *testing.T) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var held []net.Conn
		for {
			conn, err := l.Accept()
			if err != nil {
				for _, c := range held {
					_ = c.Close()
				}
				return
			}
			held = append(held, conn)
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		<-done
	})
	return l
}

func TestMailerReportsAServerItCannotTalkTo(t *testing.T) {
	for name, open := range unreachableMailers {
		t.Run(name, func(t *testing.T) {
			mailer, server := open(t)

			err := mailer.Send(t.Context(), server, connector.Envelope{From: "doorcode@example.com", To: []string{"ann@example.com"}}, testMessage("ann@example.com"))

			if err == nil {
				t.Error("sending succeeded, want an error")
			}
		})
	}
}
