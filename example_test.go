package guslookup_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	nip "github.com/janexpl/guslookup"
)

func ExampleClient_LookupNIP() {
	server := newExampleServer()
	defer server.Close()

	client := nip.NewClient(server.URL, "secret")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close(context.Background())

	company, err := client.LookupNIP(ctx, "8381771140")
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("%s (NIP %s)\n", company.Name, company.NIP)
	// Output:
	// Acme Sp. z o.o. (NIP 8381771140)
}

func ExampleClient_LookupNIP_notFound() {
	server := newExampleServer()
	defer server.Close()

	client := nip.NewClient(server.URL, "secret")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close(context.Background())

	_, err := client.LookupNIP(ctx, "8381771240")
	var fault *nip.FaultError
	if errors.As(err, &fault) {
		fmt.Printf("%s (%s)\n", fault.MessagePL, fault.Code)
		return
	}

	fmt.Println(err)
	// Output:
	// Nie znaleziono podmiotu dla podanych kryteriów wyszukiwania. (4)
}

func newExampleServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		contentType := r.Header.Get("Content-Type")

		switch {
		case strings.Contains(contentType, "Zaloguj"):
			writeExampleMultipartSOAPResponse(w, exampleSOAPEnvelope(`<ZalogujResponse><ZalogujResult>session-123</ZalogujResult></ZalogujResponse>`))
		case strings.Contains(contentType, "Wyloguj"):
			writeExampleMultipartSOAPResponse(w, exampleSOAPEnvelope(`<WylogujResponse><WylogujResult>true</WylogujResult></WylogujResponse>`))
		case strings.Contains(contentType, "DaneSzukajPodmioty") && strings.Contains(string(body), "<dat:Nip>8381771140</dat:Nip>"):
			writeExampleMultipartSOAPResponse(w, exampleSOAPEnvelope(
				`<DaneSzukajPodmiotyResponse><DaneSzukajPodmiotyResult><![CDATA[<root><dane><Regon>123456789</Regon><Nip>8381771140</Nip><StatusNip>Czynny</StatusNip><Nazwa>Acme Sp. z o.o.</Nazwa><Wojewodztwo>mazowieckie</Wojewodztwo><Powiat>warszawski</Powiat><Gmina>Centrum</Gmina><Miejscowosc>Warszawa</Miejscowosc><KodPocztowy>00-001</KodPocztowy><Ulica>Prosta</Ulica><NrNieruchomosci>1</NrNieruchomosci><NrLokalu>2</NrLokalu></dane></root>]]></DaneSzukajPodmiotyResult></DaneSzukajPodmiotyResponse>`,
			))
		default:
			writeExampleMultipartSOAPResponse(w, exampleSOAPEnvelope(
				`<DaneSzukajPodmiotyResponse><DaneSzukajPodmiotyResult><![CDATA[<root><dane><ErrorCode>4</ErrorCode><ErrorMessagePl>Nie znaleziono podmiotu dla podanych kryteriów wyszukiwania.</ErrorMessagePl><ErrorMessageEn>No data found for the specified search criteria.</ErrorMessageEn><Nip>8381771240</Nip></dane></root>]]></DaneSzukajPodmiotyResult></DaneSzukajPodmiotyResponse>`,
			))
		}
	}))
}

func exampleSOAPEnvelope(body string) string {
	return `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` + body + `</s:Body></s:Envelope>`
}

func writeExampleMultipartSOAPResponse(w http.ResponseWriter, soapBody string) {
	const boundary = "uuid:example-boundary"

	w.Header().Set("Content-Type", `multipart/related; type="application/xop+xml"; start="<http://tempuri.org/0>"; boundary="`+boundary+`"; start-info="application/soap+xml"`)
	fmt.Fprintf(
		w,
		"--%s\r\nContent-ID: <http://tempuri.org/0>\r\nContent-Transfer-Encoding: 8bit\r\nContent-Type: application/xop+xml;charset=utf-8;type=\"application/soap+xml\"\r\n\r\n%s\r\n--%s--\r\n",
		boundary,
		soapBody,
		boundary,
	)
}
