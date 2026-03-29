package nip

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClientDoesNotLogin(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")

	if client.sid != "" {
		t.Fatalf("expected empty SID, got %q", client.sid)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected constructor to avoid network calls, got %d", calls.Load())
	}
}

func TestLoginSetsSID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("expected POST request, got %s", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "Zaloguj") {
			t.Fatalf("expected login action in Content-Type, got %q", got)
		}
		writeMultipartSOAPResponse(w, soapEnvelope(`<ZalogujResponse><ZalogujResult>session-123</ZalogujResult></ZalogujResponse>`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")

	if err := client.Login(context.Background()); err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if client.sid != "session-123" {
		t.Fatalf("expected SID to be set, got %q", client.sid)
	}
}

func TestLoginHandlesMultipartResponseWithDifferentEnvelopePrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMultipartSOAPResponse(w, soapEnvelopeWithPrefix("soapenv", `<ZalogujResponse><ZalogujResult>session-alt</ZalogujResult></ZalogujResponse>`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")

	if err := client.Login(context.Background()); err != nil {
		t.Fatalf("Login returned error for alternate prefix: %v", err)
	}
	if client.sid != "session-alt" {
		t.Fatalf("expected SID to be set from alternate envelope prefix, got %q", client.sid)
	}
}

func TestLookupNIPReturnsMappedCompanyAndSendsSID(t *testing.T) {
	var gotSID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSID = r.Header.Get("sid")
		writeMultipartSOAPResponse(w, soapEnvelope(fmt.Sprintf(
			`<DaneSzukajPodmiotyResponse><DaneSzukajPodmiotyResult><![CDATA[%s]]></DaneSzukajPodmiotyResult></DaneSzukajPodmiotyResponse>`,
			`<root><dane><Regon>123456789</Regon><Nip>8381771140</Nip><StatusNip>Czynny</StatusNip><Nazwa>Acme Sp. z o.o.</Nazwa><Wojewodztwo>mazowieckie</Wojewodztwo><Powiat>warszawski</Powiat><Gmina>Centrum</Gmina><Miejscowosc>Warszawa</Miejscowosc><KodPocztowy>00-001</KodPocztowy><Ulica>Prosta</Ulica><NrNieruchomosci>1</NrNieruchomosci><NrLokalu>2</NrLokalu></dane></root>`,
		)))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")
	client.sid = "session-123"

	company, err := client.LookupNIP(context.Background(), "8381771140")
	if err != nil {
		t.Fatalf("LookupNIP returned error: %v", err)
	}
	if gotSID != "session-123" {
		t.Fatalf("expected sid header to be propagated, got %q", gotSID)
	}
	if company.NIP != "8381771140" {
		t.Fatalf("expected NIP to be mapped, got %q", company.NIP)
	}
	if company.REGON != "123456789" {
		t.Fatalf("expected REGON to be mapped, got %q", company.REGON)
	}
	if company.Name != "Acme Sp. z o.o." {
		t.Fatalf("expected Name to be mapped, got %q", company.Name)
	}
	if company.City != "Warszawa" {
		t.Fatalf("expected City to be mapped, got %q", company.City)
	}
	if company.Status != "Czynny" {
		t.Fatalf("expected Status to be mapped, got %q", company.Status)
	}
}

func TestLookupNIPReturnsFaultErrorWhenServiceReportsNoData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMultipartSOAPResponse(w, soapEnvelope(fmt.Sprintf(
			`<DaneSzukajPodmiotyResponse><DaneSzukajPodmiotyResult><![CDATA[%s]]></DaneSzukajPodmiotyResult></DaneSzukajPodmiotyResponse>`,
			`<root><dane><ErrorCode>4</ErrorCode><ErrorMessagePl>Nie znaleziono podmiotu dla podanych kryteriów wyszukiwania.</ErrorMessagePl><ErrorMessageEn>No data found for the specified search criteria.</ErrorMessageEn><Nip>8381771240</Nip></dane></root>`,
		)))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")
	client.sid = "session-123"

	company, err := client.LookupNIP(context.Background(), "8381771240")
	if err == nil {
		t.Fatal("expected LookupNIP to return an error")
	}
	if company != (Company{}) {
		t.Fatalf("expected zero-value company on lookup error, got %+v", company)
	}

	var fault *FaultError
	if !errors.As(err, &fault) {
		t.Fatalf("expected FaultError, got %T (%v)", err, err)
	}
	if fault.Code != "4" {
		t.Fatalf("expected fault code 4, got %q", fault.Code)
	}
	if fault.NIP != "8381771240" {
		t.Fatalf("expected fault NIP to be propagated, got %q", fault.NIP)
	}
	if fault.MessagePL != "Nie znaleziono podmiotu dla podanych kryteriów wyszukiwania." {
		t.Fatalf("unexpected polish fault message: %q", fault.MessagePL)
	}
	if fault.MessageEN != "No data found for the specified search criteria." {
		t.Fatalf("unexpected english fault message: %q", fault.MessageEN)
	}
}

func TestCloseReturnsErrorWhenServiceRejects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMultipartSOAPResponse(w, soapEnvelope(`<WylogujResponse><WylogujResult>false</WylogujResult></WylogujResponse>`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")
	client.sid = "session-123"

	err := client.Close(context.Background())
	if err == nil {
		t.Fatal("expected Close to return an error")
	}
	if got := err.Error(); got != "failed to logout" {
		t.Fatalf("expected logout error, got %q", got)
	}
}

func TestLookupNIPHonorsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeMultipartSOAPResponse(w, soapEnvelope(`<DaneSzukajPodmiotyResponse><DaneSzukajPodmiotyResult><![CDATA[<root></root>]]></DaneSzukajPodmiotyResult></DaneSzukajPodmiotyResponse>`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.LookupNIP(ctx, "8381771140")
	if err == nil {
		t.Fatal("expected LookupNIP to fail on context deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestLookupNIPReturnsErrorWhenEnvelopeIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>bad gateway</body></html>`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")

	_, err := client.LookupNIP(context.Background(), "8381771140")
	if err == nil {
		t.Fatal("expected LookupNIP to return an error")
	}
	if !strings.Contains(err.Error(), "expected element type") {
		t.Fatalf("expected XML envelope decode error, got %v", err)
	}
}

func soapEnvelope(body string) string {
	return soapEnvelopeWithPrefix("s", body)
}

func soapEnvelopeWithPrefix(prefix string, body string) string {
	return fmt.Sprintf(`<%[1]s:Envelope xmlns:%[1]s="http://schemas.xmlsoap.org/soap/envelope/"><%[1]s:Body>%[2]s</%[1]s:Body></%[1]s:Envelope>`, prefix, body)
}

func writeMultipartSOAPResponse(w http.ResponseWriter, soapBody string) {
	const boundary = "uuid:test-boundary"

	w.Header().Set("Content-Type", `multipart/related; type="application/xop+xml"; start="<http://tempuri.org/0>"; boundary="`+boundary+`"; start-info="application/soap+xml"`)
	fmt.Fprintf(
		w,
		"--%s\r\nContent-ID: <http://tempuri.org/0>\r\nContent-Transfer-Encoding: 8bit\r\nContent-Type: application/xop+xml;charset=utf-8;type=\"application/soap+xml\"\r\n\r\n%s\r\n--%s--\r\n",
		boundary,
		soapBody,
		boundary,
	)
}
