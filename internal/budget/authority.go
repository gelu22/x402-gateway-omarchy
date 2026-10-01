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

// ErrBudget means the authorization would exceed the daily cap or the
// per-domain sub-cap. The caller maps this to the existing budget_exceeded
// path (overridable).
var ErrBudget = fmt.Errorf("budget: exceeded")

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
	ExpiresAt   time.Time `json:"expires_at"`
	Signed      bool      `json:"signed"`
}

// state is the on-disk shape. Reserved is keyed by token.
type state struct {
	Day      string                 `json:"day"`
	Spent    int64                  `json:"spent_micro"`
	Reserved map[string]reservation `json:"reserved"`
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

// Authorize atomically checks the daily cap and the per-domain sub-cap,
// then durably reserves the amount. Returns a token for Commit/Release/MarkSigned.
// A persistence failure returns an error — the caller must NOT sign.
// subcapMicro <= 0 disables the sub-cap for this call.
func (a *Authority) Authorize(amountMicro, capMicro, subcapMicro int64, domain string) (string, error) {
	if amountMicro <= 0 {
		return "", fmt.Errorf("budget: refusing non-positive amount %d", amountMicro)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return "", err
	}
	total := st.Spent + reservedTotal(st)
	if total > math.MaxInt64-amountMicro || total+amountMicro > capMicro {
		return "", ErrBudget
	}
	if subcapMicro > 0 {
		domainTotal := reservedForDomain(st, domain)
		if domainTotal > math.MaxInt64-amountMicro || domainTotal+amountMicro > subcapMicro {
			return "", ErrBudget
		}
	}
	// Token = wall clock + monotonic sequence (46.6, F10). The clock alone is
	// not unique: a coarse or stepped clock can return the same nanosecond
	// twice, and a collision would overwrite the first reservation in the
	// map — that charge would then never be counted.
	a.seq++
	token := fmt.Sprintf("r%d-%d", a.now().UnixNano(), a.seq)
	st.Reserved[token] = reservation{
		AmountMicro: amountMicro,
		Domain:      domain,
		ExpiresAt:   a.now().Add(ReservationTTL),
	}
	if err := a.persist(st); err != nil {
		return "", err // fail-closed: no persist, no authorization
	}
	return token, nil
}

// MarkSigned records that Payment-Signature is about to be sent. Fail-closed
// persist; unknown token = no-op (idempotent after Commit). Renews ExpiresAt
// so a slow seller response does not race the original Authorize TTL.
func (a *Authority) MarkSigned(token string) error {
	if token == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return err
	}
	r, ok := st.Reserved[token]
	if !ok {
		return nil
	}
	r.Signed = true
	r.ExpiresAt = a.now().Add(ReservationTTL)
	st.Reserved[token] = r
	return a.persist(st)
}

// Commit moves a reservation into Spent. Unknown token = no-op (idempotent:
// a crash between persist and the caller's next step must not double-count).
func (a *Authority) Commit(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return err
	}
	r, ok := st.Reserved[token]
	if !ok {
		return nil
	}
	delete(st.Reserved, token)
	st.Spent = satAddSpent(st.Spent, r.AmountMicro)
	return a.persist(st)
}

// Release drops an unsigned reservation without charging. If the reservation
// is Signed (Payment-Signature at risk), Release must NOT refund: promote to
// Spent instead (NEW-P3-1 / 44.repass.2). Unknown token = no-op.
func (a *Authority) Release(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return err
	}
	r, ok := st.Reserved[token]
	if !ok {
		return nil
	}
	delete(st.Reserved, token)
	if r.Signed {
		st.Spent = satAddSpent(st.Spent, r.AmountMicro)
	}
	return a.persist(st)
}

// Today returns the current daily total (spent + reserved) for display.
func (a *Authority) Today() (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return 0, err
	}
	return st.Spent + reservedTotal(st), nil
}
