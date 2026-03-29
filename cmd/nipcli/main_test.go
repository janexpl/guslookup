package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunJSONSuccess(t *testing.T) {
	server := newCLITestServer()
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{
		"-url", server.URL,
		"-token", "secret",
		"-format", "json",
		"8381771140",
	}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var results []lookupResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal JSON output: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].Company == nil {
		t.Fatalf("expected company in output, got %+v", results[0])
	}
	if results[0].Company.Name != "Acme Sp. z o.o." {
		t.Fatalf("expected company name in output, got %q", results[0].Company.Name)
	}
}

func TestRunJSONLookupError(t *testing.T) {
	server := newCLITestServer()
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{
		"-url", server.URL,
		"-token", "secret",
		"-format", "json",
		"8381771240",
	}, &stdout, &stderr)

	if exitCode != 3 {
		t.Fatalf("expected exit code 3 for business lookup error, got %d, stderr=%q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var results []lookupResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal JSON output: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Fatalf("expected fault error in output, got %+v", results[0])
	}
	if results[0].Error.Code != "4" {
		t.Fatalf("expected fault code 4, got %q", results[0].Error.Code)
	}
}

func TestRunRejectsMalformedNIP(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"83-81"}, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("expected exit code 2 for invalid arguments, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "invalid NIP") {
		t.Fatalf("expected validation error on stderr, got %q", stderr.String())
	}
}

func newCLITestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		contentType := r.Header.Get("Content-Type")

		switch {
		case strings.Contains(contentType, "Zaloguj"):
			writeCLIMultipartSOAPResponse(w, cliSOAPEnvelope(`<ZalogujResponse><ZalogujResult>session-123</ZalogujResult></ZalogujResponse>`))
		case strings.Contains(contentType, "Wyloguj"):
			writeCLIMultipartSOAPResponse(w, cliSOAPEnvelope(`<WylogujResponse><WylogujResult>true</WylogujResult></WylogujResponse>`))
		case strings.Contains(contentType, "DaneSzukajPodmioty") && strings.Contains(string(body), "<dat:Nip>8381771140</dat:Nip>"):
			writeCLIMultipartSOAPResponse(w, cliSOAPEnvelope(
				`<DaneSzukajPodmiotyResponse><DaneSzukajPodmiotyResult><![CDATA[<root><dane><Regon>123456789</Regon><Nip>8381771140</Nip><StatusNip>Czynny</StatusNip><Nazwa>Acme Sp. z o.o.</Nazwa><Wojewodztwo>mazowieckie</Wojewodztwo><Powiat>warszawski</Powiat><Gmina>Centrum</Gmina><Miejscowosc>Warszawa</Miejscowosc><KodPocztowy>00-001</KodPocztowy><Ulica>Prosta</Ulica><NrNieruchomosci>1</NrNieruchomosci><NrLokalu>2</NrLokalu></dane></root>]]></DaneSzukajPodmiotyResult></DaneSzukajPodmiotyResponse>`,
			))
		default:
			writeCLIMultipartSOAPResponse(w, cliSOAPEnvelope(
				`<DaneSzukajPodmiotyResponse><DaneSzukajPodmiotyResult><![CDATA[<root><dane><ErrorCode>4</ErrorCode><ErrorMessagePl>Nie znaleziono podmiotu dla podanych kryteriów wyszukiwania.</ErrorMessagePl><ErrorMessageEn>No data found for the specified search criteria.</ErrorMessageEn><Nip>8381771240</Nip></dane></root>]]></DaneSzukajPodmiotyResult></DaneSzukajPodmiotyResponse>`,
			))
		}
	}))
}

func cliSOAPEnvelope(body string) string {
	return `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` + body + `</s:Body></s:Envelope>`
}

func writeCLIMultipartSOAPResponse(w http.ResponseWriter, soapBody string) {
	const boundary = "uuid:cli-boundary"

	w.Header().Set("Content-Type", `multipart/related; type="application/xop+xml"; start="<http://tempuri.org/0>"; boundary="`+boundary+`"; start-info="application/soap+xml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(
		w,
		"--"+boundary+"\r\n"+
			"Content-ID: <http://tempuri.org/0>\r\n"+
			"Content-Transfer-Encoding: 8bit\r\n"+
			"Content-Type: application/xop+xml;charset=utf-8;type=\"application/soap+xml\"\r\n\r\n"+
			soapBody+"\r\n"+
			"--"+boundary+"--\r\n",
	)
}
