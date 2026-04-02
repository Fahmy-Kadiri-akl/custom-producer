module github.com/akeylesslabs/custom-producer/go

go 1.22

require (
	github.com/aws/aws-lambda-go v1.23.0
	github.com/go-acme/lego/v4 v4.3.1
	github.com/go-chi/chi/v5 v5.0.12
	github.com/gorilla/mux v1.8.0
	github.com/rs/zerolog v1.32.0
)

require (
	github.com/aws/aws-sdk-go v1.37.27 // indirect
	github.com/cenkalti/backoff/v4 v4.1.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/miekg/dns v1.1.40 // indirect
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad // indirect
	golang.org/x/net v0.0.0-20210220033124-5f55cee0dc0d // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.3.4 // indirect
	gopkg.in/square/go-jose.v2 v2.5.1 // indirect
)

exclude github.com/labstack/echo/v4 v4.1.11
