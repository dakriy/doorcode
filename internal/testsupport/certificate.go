package testsupport

import (
	"encoding/json"
	"strings"
)

// Certificate is a PEM certificate, of a CA named Example CA, such as a
// broker's CA certificate is given as.
const Certificate = `-----BEGIN CERTIFICATE-----
MIIBgTCCASegAwIBAgIUIW9LWeQu7aM0rOOnmHLp3yEFtbAwCgYIKoZIzj0EAwIw
FTETMBEGA1UEAwwKRXhhbXBsZSBDQTAgFw0yNjExMDkwOTQ2NTNaGA8yMTI2MTAx
NjA5NDY1M1owFTETMBEGA1UEAwwKRXhhbXBsZSBDQTBZMBMGByqGSM49AgEGCCqG
SM49AwEHA0IABPxWTnXE6a5v/V3u9UaqBbLVJMlUArfozKDI6Xb5wjPg3LVSxsd5
Fg9xI1IP3iy9qFvbjeFSsjWtaKDJonhp/HCjUzBRMB0GA1UdDgQWBBSRBtcpNtfC
MlBhWRcUDzjZgShngDAfBgNVHSMEGDAWgBSRBtcpNtfCMlBhWRcUDzjZgShngDAP
BgNVHRMBAf8EBTADAQH/MAoGCCqGSM49BAMCA0gAMEUCIQDwqWlH/eQbPFx5jdoa
49B1nOT8Zm4Rw/UcJVRo0uQOXgIgCebw0URNx4ahN57ca2qChtNueQYdB10D7NEh
PWG4Yk0=
-----END CERTIFICATE-----
`

// RetypedPEM is pem, PEM blocks such as Certificate, with each block
// labelled kind in place of its own label.
func RetypedPEM(pem, kind string) string {
	pem = strings.ReplaceAll(pem, "BEGIN CERTIFICATE", "BEGIN "+kind)
	return strings.ReplaceAll(pem, "END CERTIFICATE", "END "+kind)
}

// CRLF is text with each line ending in CR LF, as a browser sends a text
// area.
func CRLF(text string) string {
	return strings.ReplaceAll(text, "\n", "\r\n")
}

// Quoted is s as a JSON string, quotes included.
func Quoted(s string) string {
	quoted, _ := json.Marshal(s)
	return string(quoted)
}
