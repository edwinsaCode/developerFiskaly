package domain

// AccountType classifies an account in the chart of accounts.
type AccountType string

const (
	AccountAsset     AccountType = "asset"
	AccountLiability AccountType = "liability"
	AccountEquity    AccountType = "equity"
	AccountRevenue   AccountType = "revenue"
	AccountExpense   AccountType = "expense"
)

// NormalBalance is the side (debit or credit) that increases an account's balance.
type NormalBalance string

const (
	NormalBalanceDebit  NormalBalance = "debit"
	NormalBalanceCredit NormalBalance = "credit"
)

// NormalBalanceFor returns the conventional normal balance for an account type.
// Assets and expenses increase with debits; liabilities, equity, and revenue with credits.
func NormalBalanceFor(t AccountType) NormalBalance {
	switch t {
	case AccountAsset, AccountExpense:
		return NormalBalanceDebit
	default:
		return NormalBalanceCredit
	}
}
