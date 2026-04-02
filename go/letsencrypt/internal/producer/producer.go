// Package producer implements Let's Encrypt certificate automation using
// Akeyless Dynamic Secrets.
package producer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"os"
	"strings"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/route53"
	"github.com/go-acme/lego/v4/registration"
)

const dryRunAccessID = "p-custom"
const envLEEmail = "LE_EMAIL"

// ErrMissingSubClaim is returned when the original user doesn't have an
// "email" sub-claim in their access credentials.
var ErrMissingSubClaim = fmt.Errorf("email sub-claim is required")

// Producer is an implementation of Akeyless Custom Producer.
type Producer interface {
	Create(*CreateRequest) (*CreateResponse, error)
	Revoke(*RevokeRequest) (*RevokeResponse, error)
}

// New creates a new Producer with the provided options.
func New(opts ...Option) (Producer, error) {
	p := &producer{}

	for _, opt := range opts {
		opt(p)
	}

	return p, nil
}

type producer struct {
	dryRunEmail  string
	dryRunDomain string
}

func (p *producer) Create(r *CreateRequest) (*CreateResponse, error) {
	// dry run mode only makes sure that the producer configuration is valid,
	// not that the implementation is correct, so it's enough to return a valid
	// response without actually obtaining a certificate
	if r.ClientInfo.AccessID == dryRunAccessID {
		return &CreateResponse{}, nil
	}

	var email string

	switch emailClaims := r.ClientInfo.SubClaims["email"]; len(emailClaims) {
	case 0:
		// if no email sub-claim is set, try using env var
		envEmail, ok := os.LookupEnv(envLEEmail)
		if !ok {
			return nil, ErrMissingSubClaim
		}

		email = envEmail
	default:
		email = emailClaims[0]
	}

	certOut, err := obtainCertificate(email, r.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain a new certificate: %w", err)
	}

	return &CreateResponse{
		ID:       "",
		Response: certOut,
	}, nil
}

func (p *producer) Revoke(r *RevokeRequest) (*RevokeResponse, error) {
	// This producer doesn't allow to revoke temporary credentials.
	// Here we only return the same user ids that we received even though we do
	// nothing with them.

	return &RevokeResponse{
		Revoked: r.IDs,
	}, nil
}

// obtainCertificate requests a new certificate from Let's Encrypt and attempts
// to solve the challenge to prove our identity.
//
// Currently, only DNS challenge can be solved, and only Route53 (AWS) is
// supported. To support other cloud environments, this function and its input
// should be modified.
//
// The environment that runs this producer must be able to authenticate
// seamlessly with the cloud provider and have sufficient permissions to manage
// DNS records.
func obtainCertificate(email string, inp Input) (*certOutput, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("can't generate private key: %w", err)
	}

	user := leUser{email: email, key: privateKey}
	config := lego.NewConfig(&user)

	if inp.UseStaging {
		config.CADirURL = lego.LEDirectoryStaging
	}

	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("can't crate lets encrypt client: %w", err)
	}

	r53, err := route53.NewDNSProvider()
	if err != nil {
		return nil, fmt.Errorf("can't create a new route53 dns provider: %w", err)
	}

	if err := client.Challenge.SetDNS01Provider(r53); err != nil {
		return nil, fmt.Errorf("can't setup a new dns challenge using route53 provider: %w", err)
	}

	user.registration, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("can't obtain lets encrypt registration for %s: %w", email, err)
	}

	domainList := strings.Split(inp.Domain, ",")
	out, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: domainList})
	if err != nil {
		return nil, fmt.Errorf("can't obtain certificates for domain %v: %w", inp.Domain, err)
	}

	// we use an internal type instead of reusing lego type since we want to
	// include every field in json while lego type skips some of them
	return &certOutput{
		Domain:            out.Domain,
		CertURL:           out.CertURL,
		CertStableURL:     out.CertStableURL,
		PrivateKey:        out.PrivateKey,
		Certificate:       out.Certificate,
		IssuerCertificate: out.IssuerCertificate,
		CSR:               out.CSR,
	}, nil
}
