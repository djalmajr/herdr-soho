// Package base holds the pre-change transfer implementation of the
// seeded reviewer fixture. A transfer debits the actor and credits the
// target account by exactly the same amount after validating the
// request.
package base

import (
	"errors"
	"math"
)

// Actor is a ledger actor identified by a stable ID.
type Actor struct {
	ID string
}

// Account is a ledger account owned by an actor.
type Account struct {
	ID      string
	OwnerID string
}

// Ledger records debits and credits against account IDs.
type Ledger interface {
	Debit(id string, amount float64)
	Credit(id string, amount float64)
}

// Options carries one transfer request. Actor and Account are required
// (non-nil). Amount is a finite number expressed in whole-or-fractional
// transfer units and must be strictly positive. The account must be
// owned by the actor.
type Options struct {
	Actor   *Actor
	Account *Account
	Amount  float64
	Ledger  Ledger
}

// The sentinel errors returned by Transfer.
var (
	ErrRequired      = errors.New("actor and account are required")
	ErrAmount        = errors.New("amount must be positive")
	ErrNotAuthorized = errors.New("not authorized")
)

// Transfer validates the request, then debits the actor by Amount and
// credits the account by exactly the same amount.
func Transfer(opts Options) error {
	if opts.Actor == nil || opts.Account == nil {
		return ErrRequired
	}
	if math.IsNaN(opts.Amount) || math.IsInf(opts.Amount, 0) || opts.Amount <= 0 {
		return ErrAmount
	}
	if opts.Account.OwnerID != opts.Actor.ID {
		return ErrNotAuthorized
	}
	opts.Ledger.Debit(opts.Actor.ID, opts.Amount)
	opts.Ledger.Credit(opts.Account.ID, opts.Amount)
	return nil
}
