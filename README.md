# nipcli

Go module and CLI for looking up Polish companies in the GUS registry by NIP.

## Library

```go
client := nip.NewClient(gusURL, gusToken)

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := client.Login(ctx); err != nil {
 log.Fatal(err)
}
defer client.Close(context.Background())

company, err := client.LookupNIP(ctx, "1112223344")
if err != nil {
 var fault *nip.FaultError
 if errors.As(err, &fault) {
  log.Printf("lookup failed: %s", fault.MessagePL)
  return
 }
 log.Fatal(err)
}

fmt.Println(company.Name)
```

Runnable package examples live in [`example_test.go`](./example_test.go).

## CLI

```bash
go run ./cmd/nipcli -token "$GUS_TOKEN" 1112223344
go run ./cmd/nipcli -token "$GUS_TOKEN" -format json 1112223344 5556667788
```

The CLI accepts one or more NIPs, supports `text` and `json` output, and loads configuration from:

- `-token` / `GUS_TOKEN`
- `-url` / `GUS_URL`
- `-env-file path/to/.env`

If `GUS_URL` is not provided, the CLI uses the public GUS test endpoint by default.
