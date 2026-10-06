// Package budget provides a single atomic authority over the daily payment
// budget: Authorize/Commit/Release/MarkSigned in one transaction (mutex +
// durable persist). The daily cap and the per-domain sub-cap are both checked
// and reserved atomically — concurrent requests cannot each pass against the
// same remaining budget. A write failure means no authorization (fail-closed).
//
// Reservation semantics: Authorize is a durable hold. MarkSigned records that
// Payment-Signature is about to leave the process. Commit moves Reserved →
// Spent; Release drops an **unsigned** hold only. Signed Release promotes to
// Spent (never refund — NEW-P3-1 / 44.repass.2). TTL sweep: unsigned → delete;
// signed → promote to Spent (local settlement reconciliation — HANCORE b / 44.4).
// Day rollover: signed Reserved carry into new-day Spent; unsigned drop;
// yesterday's committed Spent resets (44.4b). Missing JSON "signed" ⇒ false.
package budget

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// ErrBudget means the authorization would exceed the daily cap. ErrSubCap means
// it would exceed the per-domain cap. ErrAgentCap means the per-agent daily
// limit. They are distinct so the caller can name the sharper limit.
var (
	ErrBudget   = fmt.Errorf("budget: exceeded")
	ErrSubCap   = fmt.Errorf("budget: domain cap exceeded")
	ErrAgentCap = fmt.Errorf("budget: agent cap exceeded")
)

// ReservationTTL bounds how long an uncommitted reservation holds budget
// after a crash. 15 min > MFA wait (180 s), so an in-flight payment that
// is waiting for a code keeps its reservation.
const ReservationTTL = 15 * time.Minute

const filePerms = 0o600

// reservation is a pending charge: amount, domain (sub-cap), expiry, and
// whether Payment-Signature has left (or is about to leave) the process.
type reservation struct {
	AmountMicro int64     `json:"amount_micro"`
	Domain      string    `json:"domain"`
	Agent       string    `json:"agent"`
	ExpiresAt   time.Time `json:"expires_at"`
	Signed      bool      `json:"signed"`
}

// state is the on-disk shape. Reserved is keyed by token. SpentByDomain /
// SpentByAgent are committed totals kept HERE so every cap is decided in one
// transaction with the reservation (47.1 / 55.3).
type state struct {
	Day           string                 `json:"day"`
	Spent         int64                  `json:"spent_micro"`
	SpentByDomain map[string]int64       `json:"spent_by_domain_micro"`
	SpentByAgent  map[string]int64       `json:"spent_by_agent_micro"`
	Reserved      map[string]reservation `json:"reserved"`
}

// Authority is the single owner of the daily budget. All mutations go
// through Authorize/Commit/Release/MarkSigned under mu; persistence is tmp+rename.
type Authority struct {
	stateDir string
	now      func() time.Time
	seq      uint64 // token uniqueness; guarded by mu (46.6, F10)
	mu       sync.Mutex
}

// NewAuthority creates the authority rooted at stateDir (0700 dir, 0600 file).
func NewAuthority(stateDir string, now func() time.Time) *Authority {
	if now == nil {
		now = time.Now
	}
	return &Authority{stateDir: stateDir, now: now}
}

func (a *Authority) today() string { return a.now().Format("2006-01-02") }

// satAddSpent adds amount to spent, clamping at MaxInt64.
func satAddSpent(spent, amount int64) int64 {
	if spent > math.MaxInt64-amount {
		return math.MaxInt64
	}
	return spent + amount
}

// Hold is the amount, seller domain, and agent label for one Authorize call.
type Hold struct {
	AmountMicro int64
	Domain      string
	Agent       string
}

// Caps are the daily, per-domain, and per-agent limits for one Authorize call.
// DomainMicro <= 0 disables the domain sub-cap; AgentMicro <= 0 disables the
// agent cap (ledger for the label still grows on promote).
type Caps struct {
	DailyMicro  int64
	DomainMicro int64
	AgentMicro  int64
}

// Authorize atomically checks the daily cap and the per-domain sub-cap,
// then durably reserves the amount. Returns a token for Commit/Release/MarkSigned.
// A persistence failure returns an error — the caller must NOT sign.
func (a *Authority) Authorize(h Hold, c Caps) (string, error) {
	if h.AmountMicro <= 0 {
		return "", fmt.Errorf("budget: refusing non-positive amount %d", h.AmountMicro)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return "", err
	}
	total := st.Spent + reservedTotal(st)
	if total > math.MaxInt64-h.AmountMicro || total+h.AmountMicro > c.DailyMicro {
		return "", ErrBudget
	}
	if c.DomainMicro > 0 {
		// Committed plus in-flight, both from THIS state and under THIS lock.
		// Reading the committed per-domain total from another store left a
		// window: a settled payment had already left Reserved but was not yet
		// visible to the other store, so a concurrent request authorised against
		// a stale balance and two ordinary requests could exceed the cap (47.1).
		domainTotal := satAddSpent(st.SpentByDomain[h.Domain], reservedForDomain(st, h.Domain))
		if domainTotal > math.MaxInt64-h.AmountMicro || domainTotal+h.AmountMicro > c.DomainMicro {
			return "", ErrSubCap
		}
	}
	if c.AgentMicro > 0 {
		agentTotal := satAddSpent(st.SpentByAgent[h.Agent], reservedForAgent(st, h.Agent))
		if agentTotal > math.MaxInt64-h.AmountMicro || agentTotal+h.AmountMicro > c.AgentMicro {
			return "", ErrAgentCap
		}
	}
	// Token = wall clock + monotonic sequence (46.6, F10). The clock alone is
	// not unique: a coarse or stepped clock can return the same nanosecond
	// twice, and a collision would overwrite the first reservation in the
	// map — that charge would then never be counted.
	a.seq++
	token := fmt.Sprintf("r%d-%d", a.now().UnixNano(), a.seq)
	st.Reserved[token] = reservation{
		AmountMicro: h.AmountMicro,
		Domain:      h.Domain,
		Agent:       h.Agent,
		ExpiresAt:   a.now().Add(ReservationTTL),
	}
	if err := a.persist(st); err != nil {
		return "", err // fail-closed: no persist, no authorization
	}
	return token, nil
}
