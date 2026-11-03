// Package sesapi is a thin SES v2 API client for the sandbox-aware invite
// email controls: detect whether the account has production access, trigger a
// recipient verification email (the sandbox prerequisite), and check a
// recipient's verification status. Deliberately scoped to those three calls —
// sending stays on the SMTP mailer.
//
// Disabled (all methods error) when the API credentials are unconfigured; the
// console then hides the sandbox controls and behaves as before.
package sesapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// prodCacheTTL bounds how long a GetAccount answer is reused. Production
// access flips at most once in an account's life, so a stale "sandbox" for a
// few minutes after approval is harmless (the controls just linger briefly).
const prodCacheTTL = 10 * time.Minute

type Client struct {
	ses *sesv2.Client

	mu          sync.Mutex
	prodKnown   bool
	prodValue   bool
	prodChecked time.Time
}

// New returns a configured client, or nil when any credential piece is
// missing (feature off).
func New(region, accessKeyID, secretAccessKey string) *Client {
	region, accessKeyID, secretAccessKey =
		strings.TrimSpace(region), strings.TrimSpace(accessKeyID), strings.TrimSpace(secretAccessKey)
	if region == "" || accessKeyID == "" || secretAccessKey == "" {
		return nil
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil
	}
	return &Client{ses: sesv2.NewFromConfig(cfg)}
}

func (c *Client) Enabled() bool { return c != nil && c.ses != nil }

// ProductionAccess reports whether the SES account is out of the sandbox.
// Cached; an API error surfaces so callers can fail visible, not false-prod.
func (c *Client) ProductionAccess(ctx context.Context) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("ses api not configured")
	}
	c.mu.Lock()
	if c.prodKnown && time.Since(c.prodChecked) < prodCacheTTL {
		v := c.prodValue
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	out, err := c.ses.GetAccount(ctx, &sesv2.GetAccountInput{})
	if err != nil {
		return false, err
	}
	v := out.ProductionAccessEnabled
	c.mu.Lock()
	c.prodKnown, c.prodValue, c.prodChecked = true, v, time.Now()
	c.mu.Unlock()
	return v, nil
}

// VerifyIdentity asks SES to send its verification email to the address (the
// sandbox prerequisite for that address to receive mail). Idempotent: an
// already-registered identity is not an error.
func (c *Client) VerifyIdentity(ctx context.Context, email string) error {
	if !c.Enabled() {
		return errors.New("ses api not configured")
	}
	_, err := c.ses.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{
		EmailIdentity: aws.String(strings.TrimSpace(email)),
	})
	var already *types.AlreadyExistsException
	if errors.As(err, &already) {
		return nil
	}
	return err
}

// IdentityVerified reports whether the address has completed verification.
// An identity SES has never seen reads as unverified, not an error.
func (c *Client) IdentityVerified(ctx context.Context, email string) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("ses api not configured")
	}
	out, err := c.ses.GetEmailIdentity(ctx, &sesv2.GetEmailIdentityInput{
		EmailIdentity: aws.String(strings.TrimSpace(email)),
	})
	var nf *types.NotFoundException
	if errors.As(err, &nf) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return out.VerifiedForSendingStatus, nil
}
