package nip

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Client manages a logged-in session with the GUS SOAP API.
type Client struct {
	address string
	key     string
	sid     string
}

// Company represents a company returned by the GUS registry lookup.
type Company struct {
	NIP         string `json:"nip"`
	REGON       string `json:"regon"`
	Name        string `json:"name"`
	Voivodeship string `json:"voivodeship"`
	County      string `json:"county"`
	Commune     string `json:"commune"`
	City        string `json:"city"`
	PostalCode  string `json:"postal_code"`
	Street      string `json:"street"`
	HouseNumber string `json:"house_number"`
	Apartment   string `json:"apartment"`
	Status      string `json:"status"`
}

// FaultError describes a business-level lookup error returned by GUS.
type FaultError struct {
	Code      string `json:"code"`
	MessagePL string `json:"message_pl"`
	MessageEN string `json:"message_en"`
	NIP       string `json:"nip"`
}

// Dane - Struct with search results
type dane struct {
	XMLName                    xml.Name `xml:"dane"`
	ErrorCode                  string   `xml:"ErrorCode"`
	ErrorMessagePl             string   `xml:"ErrorMessagePl"`
	ErrorMessageEn             string   `xml:"ErrorMessageEn"`
	Regon                      string
	Nip                        string
	StatusNip                  string
	Nazwa                      string
	Wojewodztwo                string
	Powiat                     string
	Gmina                      string
	Miejscowosc                string
	KodPocztowy                string
	Ulica                      string
	NrNieruchomosci            string
	NrLokalu                   string
	Typ                        string
	SilosID                    string
	DataZakonczeniaDzialanosci string
	MiejscowoscPoczty          string
}
type soapRQ struct {
	XMLName   xml.Name `xml:"soap:Envelope"`
	XMLNsSoap string   `xml:"xmlns:soap,attr"`
	XMLNsNs   string   `xml:"xmlns:ns,attr"`
	XMLNsDat  string   `xml:"xmlns:dat,attr"`
	Header    soapHeader
	Body      soapBody
}

type soapBody struct {
	XMLName xml.Name `xml:"soap:Body"`
	Payload any
}

type soapHeader struct {
	XMLName   xml.Name `xml:"soap:Header"`
	XMLNsWSA  string   `xml:"xmlns:wsa,attr"`
	XMLAction string   `xml:"wsa:Action"`
	XMLTo     string   `xml:"wsa:To"`
}
type resultHeaderAction struct {
	XMLName     xml.Name `xml:"Action"`
	XMLActionMU string   `xml:"s:mustUnderstand,attr"`
}
type resultHeader struct {
	XMLName      xml.Name `xml:"Header"`
	XMLResAction resultHeaderAction
}

// Error returns the business error in a user-facing form.
func (e *FaultError) Error() string {
	return fmt.Sprintf("lookup failed for NIP %s: %s", e.NIP, e.MessagePL)
}

// NewClient creates a client configured for a specific GUS endpoint and API key.
func NewClient(address string, key string) *Client {
	gc := Client{
		address,
		key,
		"",
	}

	return &gc
}

func (gc *Client) soapCallHandleResponse(ctx context.Context, ws string, action string, payloadBodyInterface any, result any) error {
	response, err := gc.soapCall(ctx, ws, action, payloadBodyInterface)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return fmt.Errorf("unexpected status %d", response.StatusCode)
		}
		return fmt.Errorf("unexpected status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := gc.readSOAPBody(response)
	if err != nil {
		return err
	}
	return xml.Unmarshal(body, result)
}

// Close logs the client out from the current GUS session.
func (gc *Client) Close(ctx context.Context) error {
	type soapWyloguj struct {
		XMLName xml.Name `xml:"ns:Wyloguj"`
		XMLSID  string   `xml:"ns:pIdentyfikatorSesji"`
	}
	type wylogujResponse struct {
		XMLName       xml.Name `xml:"WylogujResponse"`
		XMLNsResp     string   `xml:"xmlns,attr"`
		WylogujResult string
	}
	type resultBody struct {
		XMLName         xml.Name `xml:"Body"`
		WylogujResponse wylogujResponse
	}
	type soapWylogujResponse struct {
		XMLName    xml.Name `xml:"Envelope"`
		XMLRespNSs string   `xml:"xmlns:s,attr"`
		XMLRespNSa string   `xml:"xmlns:a,attr"`
		XMLHeader  resultHeader
		ResultBody resultBody
	}
	wyloguj := soapWyloguj{
		XMLSID: gc.sid,
	}
	s := &soapWylogujResponse{}
	err := gc.soapCallHandleResponse(ctx, gc.address, "http://CIS/BIR/PUBL/2014/07/IUslugaBIRzewnPubl/Wyloguj", wyloguj, s)
	if err != nil {
		return err
	}

	if s.ResultBody.WylogujResponse.WylogujResult != "true" {
		return errors.New("failed to logout")
	}
	return nil
}

// LookupNIP searches GUS for a company identified by the given NIP.
func (gc *Client) LookupNIP(ctx context.Context, nip string) (Company, error) {
	type soapSzukajParametr struct {
		XMLName xml.Name `xml:"ns:pParametryWyszukiwania"`
		XMLNIP  string   `xml:"dat:Nip"`
	}
	type soapSzukaj struct {
		XMLName           xml.Name `xml:"ns:DaneSzukajPodmioty"`
		XMLSzukajParametr soapSzukajParametr
	}

	type szukajResponse struct {
		XMLName                  xml.Name `xml:"DaneSzukajPodmiotyResponse"`
		XMLNsResp                string   `xml:"xmlns,attr"`
		DaneSzukajPodmiotyResult string
	}
	type resultBody struct {
		XMLName                    xml.Name `xml:"Body"`
		DaneSzukajPodmiotyResponse szukajResponse
	}
	type soapSzukajResponse struct {
		XMLName    xml.Name `xml:"Envelope"`
		XMLRespNSs string   `xml:"xmlns:s,attr"`
		XMLRespNSa string   `xml:"xmlns:a,attr"`
		XMLHeader  resultHeader
		ResultBody resultBody
	}

	type nipResult struct {
		XMLName xml.Name `xml:"root"`
		Dane    dane
	}

	nipSearch := soapSzukaj{
		XMLSzukajParametr: soapSzukajParametr{
			XMLNIP: nip,
		},
	}
	nipResponse := &soapSzukajResponse{}
	err := gc.soapCallHandleResponse(ctx, gc.address, "http://CIS/BIR/PUBL/2014/07/IUslugaBIRzewnPubl/DaneSzukajPodmioty", nipSearch, nipResponse)
	if err != nil {
		return Company{}, err
	}

	nipRes := nipResult{}
	err = xml.Unmarshal([]byte(nipResponse.ResultBody.DaneSzukajPodmiotyResponse.DaneSzukajPodmiotyResult), &nipRes)
	if err != nil {
		return Company{}, err
	}
	if nipRes.Dane.ErrorCode != "" {
		return Company{}, &FaultError{
			Code:      nipRes.Dane.ErrorCode,
			MessagePL: nipRes.Dane.ErrorMessagePl,
			MessageEN: nipRes.Dane.ErrorMessageEn,
			NIP:       nip,
		}
	}
	return mapCompany(nipRes.Dane), nil
}

// Login starts a new authenticated GUS session and stores the returned session ID.
func (gc *Client) Login(ctx context.Context) error {
	type soapZaloguj struct {
		XMLName  xml.Name `xml:"ns:Zaloguj"`
		XMLKlucz string   `xml:"ns:pKluczUzytkownika"`
	}

	type zalogujResponse struct {
		XMLName       xml.Name `xml:"ZalogujResponse"`
		XMLNsResp     string   `xml:"xmlns,attr"`
		ZalogujResult string
	}
	type resultBody struct {
		XMLName         xml.Name `xml:"Body"`
		ZalogujResponse zalogujResponse
	}
	type soapZalogujResponse struct {
		XMLName    xml.Name `xml:"Envelope"`
		XMLRespNSs string   `xml:"xmlns:s,attr"`
		XMLRespNSa string   `xml:"xmlns:a,attr"`
		XMLHeader  resultHeader
		ResultBody resultBody
	}

	zaloguj := soapZaloguj{
		XMLKlucz: gc.key,
	}
	s := &soapZalogujResponse{}
	err := gc.soapCallHandleResponse(ctx, gc.address, "http://CIS/BIR/PUBL/2014/07/IUslugaBIRzewnPubl/Zaloguj", zaloguj, s)
	if err != nil {
		return err
	}
	gc.sid = s.ResultBody.ZalogujResponse.ZalogujResult
	return nil
}

func (gc *Client) soapCall(ctx context.Context, ws string, action string, payloadBodyInterface any) (*http.Response, error) {
	v := soapRQ{
		XMLNsSoap: "http://www.w3.org/2003/05/soap-envelope",
		XMLNsNs:   "http://CIS/BIR/PUBL/2014/07",
		XMLNsDat:  "http://CIS/BIR/PUBL/2014/07/DataContract",
		Header: soapHeader{
			XMLNsWSA:  "http://www.w3.org/2005/08/addressing",
			XMLAction: action,
			XMLTo:     ws,
		},
		Body: soapBody{
			Payload: payloadBodyInterface,
		},
	}
	payload, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(5 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ws, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "gzip,deflate")
	contype := fmt.Sprintf("application/soap+xml; charset=utf-8; action=\"%v\"", action)
	req.Header.Set("Content-Type", contype)
	if gc.sid != "" {
		req.Header.Set("sid", gc.sid)
	}

	return client.Do(req)
}

func (gc *Client) readSOAPBody(response *http.Response) ([]byte, error) {
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		return io.ReadAll(response.Body)
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("parse content type: %w", err)
	}

	if mediaType != "multipart/related" {
		return io.ReadAll(response.Body)
	}

	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("missing multipart boundary")
	}

	reader := multipart.NewReader(response.Body, boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read multipart response: %w", err)
		}

		partType, partParams, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			continue
		}

		if partType == "application/soap+xml" || partParams["type"] == "application/soap+xml" {
			body, err := io.ReadAll(part)
			if err != nil {
				return nil, fmt.Errorf("read soap part: %w", err)
			}
			return body, nil
		}
	}

	return nil, errors.New("soap part not found in multipart response")
}

func mapCompany(d dane) Company {
	return Company{
		NIP:         d.Nip,
		REGON:       d.Regon,
		Name:        d.Nazwa,
		Voivodeship: d.Wojewodztwo,
		County:      d.Powiat,
		Commune:     d.Gmina,
		City:        d.Miejscowosc,
		PostalCode:  d.KodPocztowy,
		Street:      d.Ulica,
		HouseNumber: d.NrNieruchomosci,
		Apartment:   d.NrLokalu,
		Status:      d.StatusNip,
	}
}
