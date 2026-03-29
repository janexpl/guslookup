package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	nip "github.com/janexpl/guslookup"
	"github.com/joho/godotenv"
)

const (
	defaultGUSEndpoint = "https://wyszukiwarkaregontest.stat.gov.pl/wsbir/uslugabirzewnpubl.svc"
	defaultTimeout     = 5 * time.Second
)

const usageText = `Usage:
  nipcli [flags] NIP [NIP...]

Look up one or more Polish NIP numbers in the GUS registry.

Examples:
  nipcli 8381771140
  nipcli -format json 8381771140 8381771240
  nipcli -env-file .env -timeout 10s 8381771140

Environment:
  GUS_TOKEN  API key used to log in to GUS
  GUS_URL    SOAP endpoint; defaults to the public GUS test endpoint

Flags:
`

type cliConfig struct {
	endpoint string
	token    string
	envFile  string
	format   string
	timeout  time.Duration
	nips     []string
}

type lookupResult struct {
	NIP     string          `json:"nip"`
	Company *nip.Company    `json:"company,omitempty"`
	Error   *nip.FaultError `json:"error,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	cfg, err := parseCLIConfig(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeUsage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n\n", err)
		writeUsage(stderr)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return execute(ctx, cfg, stdout, stderr)
}

func parseCLIConfig(args []string) (cliConfig, error) {
	cfg := cliConfig{
		format:  "text",
		timeout: defaultTimeout,
	}

	fs := flag.NewFlagSet("nipcli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.endpoint, "url", "", "GUS SOAP endpoint")
	fs.StringVar(&cfg.token, "token", "", "GUS API token")
	fs.StringVar(&cfg.envFile, "env-file", "", "path to a .env file")
	fs.StringVar(&cfg.format, "format", cfg.format, "output format: text or json")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "timeout per API call")

	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	if cfg.timeout <= 0 {
		return cliConfig{}, errors.New("timeout must be greater than 0")
	}

	if err := loadEnvironment(cfg.envFile); err != nil {
		return cliConfig{}, err
	}

	cfg.endpoint = firstNonEmpty(cfg.endpoint, os.Getenv("GUS_URL"), defaultGUSEndpoint)

	switch cfg.format {
	case "json", "text":
	default:
		return cliConfig{}, fmt.Errorf("unsupported format %q; use text or json", cfg.format)
	}

	nips, err := normalizeNIPs(fs.Args())
	if err != nil {
		return cliConfig{}, err
	}
	cfg.nips = nips

	cfg.token = firstNonEmpty(cfg.token, os.Getenv("GUS_TOKEN"))
	if cfg.token == "" {
		return cliConfig{}, errors.New("missing GUS token; set GUS_TOKEN, use -env-file, or pass -token")
	}

	return cfg, nil
}

func execute(ctx context.Context, cfg cliConfig, stdout io.Writer, stderr io.Writer) (exitCode int) {
	client := nip.NewClient(cfg.endpoint, cfg.token)

	if err := callWithTimeout(ctx, cfg.timeout, client.Login); err != nil {
		fmt.Fprintf(stderr, "login failed: %v\n", err)
		return 1
	}

	defer func() {
		if err := callWithTimeout(context.Background(), cfg.timeout, client.Close); err != nil {
			fmt.Fprintf(stderr, "logout failed: %v\n", err)
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}()

	results := make([]lookupResult, 0, len(cfg.nips))
	for _, nipValue := range cfg.nips {
		company, err := lookupWithTimeout(ctx, cfg.timeout, client, nipValue)
		if err != nil {
			if fault, ok := errors.AsType[*nip.FaultError](err); ok {
				results = append(results, lookupResult{
					NIP:   nipValue,
					Error: fault,
				})
				if exitCode == 0 {
					exitCode = 3
				}
				continue
			}

			fmt.Fprintf(stderr, "lookup failed for %s: %v\n", nipValue, err)
			return 1
		}

		companyCopy := company
		results = append(results, lookupResult{
			NIP:     nipValue,
			Company: &companyCopy,
		})
	}

	if err := renderResults(stdout, cfg.format, results); err != nil {
		fmt.Fprintf(stderr, "rendering output failed: %v\n", err)
		return 1
	}

	return exitCode
}

func callWithTimeout(parent context.Context, timeout time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	return fn(ctx)
}

func lookupWithTimeout(parent context.Context, timeout time.Duration, client *nip.Client, nipValue string) (nip.Company, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	return client.LookupNIP(ctx, nipValue)
}

func renderResults(w io.Writer, format string, results []lookupResult) error {
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	for i, result := range results {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		if result.Company != nil {
			if _, err := fmt.Fprintf(
				w,
				"NIP: %s\nName: %s\nREGON: %s\nStatus: %s\nAddress: %s\nLocation: %s\n",
				result.Company.NIP,
				result.Company.Name,
				result.Company.REGON,
				emptyFallback(result.Company.Status, "unknown"),
				formatAddress(*result.Company),
				formatLocation(*result.Company),
			); err != nil {
				return err
			}
			continue
		}

		if _, err := fmt.Fprintf(
			w,
			"NIP: %s\nResult: not found\nCode: %s\nMessage: %s\n",
			result.NIP,
			emptyFallback(result.Error.Code, "unknown"),
			emptyFallback(result.Error.MessagePL, result.Error.MessageEN),
		); err != nil {
			return err
		}
	}

	return nil
}

func formatAddress(company nip.Company) string {
	street := strings.TrimSpace(strings.Join([]string{company.Street, company.HouseNumber}, " "))
	if company.Apartment != "" {
		street = strings.TrimSpace(street + "/" + company.Apartment)
	}

	parts := []string{
		street,
		company.PostalCode,
		company.City,
	}

	return strings.Join(nonEmpty(parts), ", ")
}

func formatLocation(company nip.Company) string {
	return strings.Join(nonEmpty([]string{
		company.Commune,
		company.County,
		company.Voivodeship,
	}), ", ")
}

func normalizeNIPs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("provide at least one NIP, for example: nipcli 8381771140")
	}

	seen := make(map[string]struct{}, len(values))
	nips := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			nipValue := normalizeNIP(item)
			if nipValue == "" {
				continue
			}
			if err := validateNIP(nipValue); err != nil {
				return nil, err
			}
			if _, ok := seen[nipValue]; ok {
				continue
			}
			seen[nipValue] = struct{}{}
			nips = append(nips, nipValue)
		}
	}

	if len(nips) == 0 {
		return nil, errors.New("provide at least one NIP, for example: nipcli 8381771140")
	}

	return nips, nil
}

func normalizeNIP(value string) string {
	replacer := strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "")
	return replacer.Replace(strings.TrimSpace(value))
}

func validateNIP(value string) error {
	if len(value) != 10 {
		return fmt.Errorf("invalid NIP %q: expected 10 digits", value)
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return fmt.Errorf("invalid NIP %q: expected digits only", value)
		}
	}
	return nil
}

func loadEnvironment(envFile string) error {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			return fmt.Errorf("load env file %q: %w", envFile, err)
		}
		return nil
	}

	_ = godotenv.Load(".env")
	_ = godotenv.Load("cmd/nipcli/.env")
	return nil
}

func writeUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
	flagSet := flag.NewFlagSet("nipcli", flag.ContinueOnError)
	flagSet.SetOutput(w)
	flagSet.String("url", "", "GUS SOAP endpoint")
	flagSet.String("token", "", "GUS API token")
	flagSet.String("env-file", "", "path to a .env file")
	flagSet.String("format", "text", "output format: text or json")
	flagSet.Duration("timeout", defaultTimeout, "timeout per API call")
	flagSet.PrintDefaults()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonEmpty(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, strings.TrimSpace(value))
		}
	}
	return filtered
}

func emptyFallback(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
