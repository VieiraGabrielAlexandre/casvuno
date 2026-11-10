// Package authctx carries the authenticated user and the household membership through the
// request context. It lives in platform so every module can read them without importing the
// identity or households modules (ADR 0001).
package authctx

import (
	"context"

	"github.com/google/uuid"
)

// User is the authenticated user of the current request.
type User struct {
	ID    uuid.UUID
	Name  string
	Email string
}

// Role is a member's role in a household.
type Role string

// Household roles.
const (
	RoleOwner  Role = "OWNER"
	RoleMember Role = "MEMBER"
)

// Membership is the caller's active membership in the household addressed by the URL.
// Household-scoped code must take HouseholdID from here, never from the request body.
type Membership struct {
	HouseholdID uuid.UUID
	MemberID    uuid.UUID
	UserID      uuid.UUID
	Role        Role
}

type userKey struct{}
type membershipKey struct{}

// WithUser stores the authenticated user in ctx.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}

// WithMembership stores the verified membership in ctx.
func WithMembership(ctx context.Context, m Membership) context.Context {
	return context.WithValue(ctx, membershipKey{}, m)
}

// MembershipFrom returns the verified membership, if any.
func MembershipFrom(ctx context.Context) (Membership, bool) {
	m, ok := ctx.Value(membershipKey{}).(Membership)
	return m, ok
}
