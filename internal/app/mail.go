package app

import (
	"context"
	"net/http"
	"net/mail"
	"strconv"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/email"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/settings"
)

type testMailSent struct {
	SentTo   string `json:"sent_to" doc:"The address the test email went to: the caller's own."`
	Host     string `json:"host" doc:"The mail server it went through."`
	Port     int    `json:"port" doc:"The mail server's port."`
	Security string `json:"security" doc:"How the connection to the mail server was protected: tls, starttls, or none."`
}

// mailTestPath is where the settings page's Send test email button posts.
const mailTestPath = "/admin/settings/mail/test"

func (a *App) mailRoutes() []route {
	admin := holding(auth.RoleAdmin)
	return []route{
		pageRoute(http.MethodPost, mailTestPath, admin, a.sendTestMailFromForm),
		apiRoute(http.MethodPost, "/settings/mail/test", admin, a.sendTestMail, apiDoc{
			id: "sendTestMail", summary: "Send a test email to yourself", tag: "settings",
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The mail server took the test email, sent to the caller's own address through the mail settings as saved.", Body: testMailSent{SentTo: "admin@example.com", Host: "smtp.example.com", Port: 465, Security: "tls"}},
				{Status: http.StatusConflict, Description: "The caller's account has no email address mail can be sent to, or mail is not set up or is set up with a value it does not take.", Body: errorBody{}},
				{Status: http.StatusBadGateway, Description: "The mail server could not be reached or did not take the test email; the error says what it reported.", Body: errorBody{}},
			},
		}),
	}
}

// mailStatuses maps each refusal of a test email to the status the API
// answers it with.
var mailStatuses = []refusalStatus{
	{email.ErrNoAddress, http.StatusConflict},
	{settings.ErrMailNotConfigured, http.StatusConflict},
	{settings.ErrInvalid, http.StatusConflict},
	{email.ErrNotSent, http.StatusBadGateway},
}

// testMail sends viewer a test email to their own address through the
// mail settings as saved, and says where it went.
func (a *App) testMail(ctx context.Context, viewer auth.User) (testMailSent, error) {
	to, err := email.AddressOf(viewer.Email)
	if err != nil {
		return testMailSent{}, err
	}
	site, err := a.settings.Mail(ctx)
	if err != nil {
		return testMailSent{}, err
	}
	server := site.Server
	text := "This is a test email from " + site.Site + ". " + viewer.DisplayName +
		" sent it from the Mail settings to check that the site can send mail.\n\n" +
		"It went through the mail server " + server.Host + " on port " + strconv.Itoa(server.Port) +
		" with connection security " + string(server.Security) + ", from " + site.From.String() + ".\n"
	msg := email.Message{To: []*mail.Address{to}, Subject: "Test email from " + site.Site, Text: text}
	if err := a.mail.Send(ctx, site, msg); err != nil {
		return testMailSent{}, err
	}
	return testMailSent{SentTo: to.Address, Host: server.Host, Port: server.Port, Security: string(server.Security)}, nil
}

func (a *App) sendTestMail(w http.ResponseWriter, r *http.Request, caller auth.User) {
	sent, err := a.testMail(r.Context(), caller)
	if err != nil {
		a.refuse(w, r, err, mailStatuses)
		return
	}
	writeJSON(w, http.StatusOK, sent)
}

// sendTestMailFromForm sends the viewer a test email and goes back to the
// Mail tab saying so, or shows the tab with why it was not sent.
func (a *App) sendTestMailFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	_, err := a.testMail(r.Context(), viewer)
	if status, ok := statusOf(err, mailStatuses); ok {
		a.showSettings(w, r, status, viewer, settings.TabMail, nil, nil, "No test email was sent: "+err.Error()+".")
		return
	}
	a.changed(w, r, err, settingsTabPath(settings.TabMail), "mail-test-sent")
}
