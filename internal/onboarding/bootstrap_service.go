package onboarding

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/pki"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// BootstrapService handles node registration and certificate renewal.
type BootstrapService struct {
	db             *store.DB
	tokens         *TokenStore
	signer         *pki.Signer
	renewThreshold time.Duration // renew allowed when remaining < this
}

// NewBootstrapService creates a BootstrapService.
func NewBootstrapService(db *store.DB, tokens *TokenStore, signer *pki.Signer) *BootstrapService {
	return &BootstrapService{
		db:             db,
		tokens:         tokens,
		signer:         signer,
		renewThreshold: 30 * 24 * time.Hour,
	}
}

// BootstrapInput is the validated input for Bootstrap.
type BootstrapInput struct {
	Token        string
	GatewayID    string
	CSRPEM       []byte
	Serial       string
	HardwareInfo map[string]string
	Labels       map[string]string
}

// BootstrapOutput is the result of a successful Bootstrap.
type BootstrapOutput struct {
	NodeID    string
	CertPEM   []byte
	CAPEM     []byte
	ExpiresAt time.Time
}

// Bootstrap performs atomic node registration: validate token → create node →
// create identity → sign certificate.
func (s *BootstrapService) Bootstrap(ctx context.Context, in BootstrapInput) (*BootstrapOutput, error) {
	if len(in.CSRPEM) == 0 {
		return nil, errCSRInvalid("empty CSR")
	}

	var out BootstrapOutput
	err := s.db.WithTx(ctx, func(tx *store.Tx) error {
		// 1. Validate and consume the token
		_, err := s.tokens.ValidateAndConsume(ctx, tx, in.Token, in.GatewayID)
		if err != nil {
			return err
		}

		// 2. Check serial conflict: ensure no Active/Suspended identity with this serial
		if in.Serial != "" {
			var existingID string
			err := tx.QueryRowContext(ctx,
				`SELECT id FROM edge_identities WHERE serial = $1 AND status IN ('Active', 'Suspended')`,
				in.Serial,
			).Scan(&existingID)
			if err == nil {
				return errIdentityConflict()
			}
			if err != sql.ErrNoRows {
				return fmt.Errorf("check serial conflict: %w", err)
			}
		}

		// 3. Create EdgeNode
		hwJSON, _ := json.Marshal(in.HardwareInfo)
		if in.HardwareInfo == nil {
			hwJSON = []byte("{}")
		}
		labelsJSON, _ := json.Marshal(in.Labels)
		if in.Labels == nil {
			labelsJSON = []byte("{}")
		}

		var nodeID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO edge_nodes (gateway_id, labels, hardware_info, status)
			VALUES ($1, $2, $3, 'Active')
			RETURNING id`,
			in.GatewayID, labelsJSON, hwJSON,
		).Scan(&nodeID)
		if err != nil {
			return fmt.Errorf("create edge node: %w", err)
		}

		// 4. Sign CSR
		signResult, err := s.signer.SignCSR(in.CSRPEM, nodeID)
		if err != nil {
			return errCSRInvalid(err.Error())
		}

		// 5. Create EdgeIdentity
		_, err = tx.ExecContext(ctx, `
			INSERT INTO edge_identities (node_id, gateway_id, serial, fingerprint, status, certificate_pem, expires_at)
			VALUES ($1, $2, $3, $4, 'Active', $5, $6)`,
			nodeID, in.GatewayID, in.Serial, signResult.Fingerprint,
			string(signResult.CertPEM), signResult.ExpiresAt,
		)
		if err != nil {
			if store.IsUniqueViolation(err) {
				return errIdentityConflict()
			}
			return fmt.Errorf("create identity: %w", err)
		}

		out = BootstrapOutput{
			NodeID:    nodeID,
			CertPEM:   signResult.CertPEM,
			CAPEM:     s.signer.CAPem(),
			ExpiresAt: signResult.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RenewInput is the validated input for Renew.
type RenewInput struct {
	NodeID string
	CSRPEM []byte
}

// RenewOutput is the result of a successful Renew.
type RenewOutput struct {
	CertPEM   []byte
	CAPEM     []byte
	ExpiresAt time.Time
}

// Renew reissues a certificate for an existing node. The caller's identity
// must already be authenticated via mTLS.
func (s *BootstrapService) Renew(ctx context.Context, in RenewInput) (*RenewOutput, error) {
	if len(in.CSRPEM) == 0 {
		return nil, errCSRInvalid("empty CSR")
	}

	// Verify the identity exists and is Active
	var identityID string
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT id, expires_at FROM edge_identities WHERE node_id = $1 AND status = 'Active'`,
		in.NodeID,
	).Scan(&identityID, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, errIdentityRevoked()
	}
	if err != nil {
		return nil, fmt.Errorf("query identity: %w", err)
	}

	// Check renewal threshold
	remaining := time.Until(expiresAt)
	if remaining > s.renewThreshold {
		return nil, fmt.Errorf("%s: certificate not yet eligible for renewal (expires in %s)", ReasonRenewNotAllowed, remaining)
	}

	// Sign new CSR
	signResult, err := s.signer.SignCSR(in.CSRPEM, in.NodeID)
	if err != nil {
		return nil, errCSRInvalid(err.Error())
	}

	// Update identity with new cert/fingerprint/expiry
	_, err = s.db.ExecContext(ctx, `
		UPDATE edge_identities
		SET fingerprint = $1, certificate_pem = $2, expires_at = $3, issued_at = now(), updated_at = now()
		WHERE id = $4`,
		signResult.Fingerprint, string(signResult.CertPEM), signResult.ExpiresAt, identityID)
	if err != nil {
		return nil, fmt.Errorf("update identity: %w", err)
	}

	return &RenewOutput{
		CertPEM:   signResult.CertPEM,
		CAPEM:     s.signer.CAPem(),
		ExpiresAt: signResult.ExpiresAt,
	}, nil
}

// RevokeNode revokes a node's identity and records an audit event.
func (s *BootstrapService) RevokeNode(ctx context.Context, nodeID, reason string) error {
	return s.db.WithTx(ctx, func(tx *store.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE edge_identities SET status = 'Revoked', revoked_at = now(), updated_at = now()
			WHERE node_id = $1 AND status IN ('Active', 'Suspended')`,
			nodeID)
		if err != nil {
			return fmt.Errorf("revoke identity: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return errIdentityRevoked()
		}

		// Update node status
		_, err = tx.ExecContext(ctx,
			`UPDATE edge_nodes SET status = 'Revoked', updated_at = now() WHERE id = $1`, nodeID)
		if err != nil {
			return fmt.Errorf("update node status: %w", err)
		}

		// Audit event
		_, err = tx.ExecContext(ctx, `
			INSERT INTO task_events (task_id, event_type, actor, detail)
			SELECT id, 'IDENTITY_REVOKED', 'admin', $2::jsonb
			FROM edge_identities WHERE node_id = $1`,
			nodeID, fmt.Sprintf(`{"reason": %q}`, reason))
		// Audit is best-effort for now
		_ = err

		return nil
	})
}
