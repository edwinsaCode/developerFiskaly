package domain

// Role is a user's permission level within a tenant.
type Role string

const (
	RoleOwner      Role = "owner"      // full access, manage users
	RoleAccountant Role = "accountant" // create/post journals, read all
	RoleMarketing  Role = "marketing"  // sales workflow only; no accounting data (W-12)
	RoleViewer     Role = "viewer"     // read-only
)

// CanWrite returns true for roles that may create or post journals.
//
// W-12: marketing is deliberately NOT here. Marketing sells; it never posts an
// accounting entry. Its own writes are guarded by CanSell / auth.RequireSalesWrite.
func (r Role) CanWrite() bool {
	return r == RoleOwner || r == RoleAccountant
}

// CanSell returns true for roles that may run the sales workflow: leads,
// customers, bookings, sale contracts, and payment schedules.
func (r Role) CanSell() bool {
	return r == RoleOwner || r == RoleAccountant || r == RoleMarketing
}

// SeesAccounting returns true for roles allowed to READ financial data — the
// ledger, chart of accounts, payables, costs, budgets, and reports.
//
// This is the read boundary the system did not have before W-12: every GET used
// to be open to any authenticated role. Marketing is the one role excluded, so
// the guard is expressed as a capability, not as "not marketing" — a future
// role gets its answer here instead of inheriting whatever the last check meant.
func (r Role) SeesAccounting() bool {
	return r == RoleOwner || r == RoleAccountant || r == RoleViewer
}

// CanManageUsers returns true for roles that may invite or change other users.
func (r Role) CanManageUsers() bool {
	return r == RoleOwner
}

// Valid returns true if r is one of the defined roles.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAccountant, RoleMarketing, RoleViewer:
		return true
	}
	return false
}
