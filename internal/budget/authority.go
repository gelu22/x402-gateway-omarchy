// Package budget provides a single atomic authority over the daily payment
// budget: Authorize/Commit/Release in one transaction (mutex + durable
// persist). The daily cap and the per-domain sub-cap are both checked and
// reserved atomically — concurrent requests to different targets can no longer
// each pass against the same remaining budget. A write failure means no
// authorization (fail-closed): a payment can never proceed uncounted.
//
// Reservation semantics: Authorize is a durable charge. Commit moves the
// amount from Reserved to Spent; Release removes it. A crash between
// Authorize and Commit/Release leaves the amount reserved until the TTL
// sweeps it (the budget gets tighter, never looser).
package budget

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
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

// reservation is a pending charge: the amount, the domain it counts against
// (for the sub-cap), and when it expires.
type reservation struct {
	AmountMicro int64     `json:"amount_micro"`
	Domain      string    `json:"domain"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// state is the on-disk shape. Reserved is keyed by token.
type state struct {
	Day      string                 `json:"day"`
	Spent    int64                  `json:"spent_micro"`
	Reserved map[string]reservation `json:"reserved"`
}

// Authority is the single owner of the daily budget. All mutations go
// through Authorize/Commit/Release under mu; persistence is tmp+rename.
type Authority struct {
	stateDir string
	now      func() time.Time
	mu       sync.Mutex
}

// NewAuthority creates the authority rooted at stateDir (0700 dir, 0600 file).
func NewAuthority(stateDir string, now func() time.Time) *Authority {
	if now == nil {
		now = time.Now
	}
	return &Authority{stateDir: stateDir, now: now}
}

func (a *Authority) path() string { return filepath.Join(a.stateDir, "budget.json") }

// load reads today's state, sweeping expired reservations and rolling over
// on day change. Corrupt file → fresh day (caps still enforced, fail-closed).
func (a *Authority) load() (state, error) {
	var st state
	raw, err := os.ReadFile(a.path())
	if os.IsNotExist(err) {
		return state{Day: a.today(), Reserved: map[string]reservation{}}, nil
	}
	if err != nil {
		return st, fmt.Errorf("budget: read: %w", err)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return state{Day: a.today(), Reserved: map[string]reservation{}}, nil
	}
	today := a.today()
	if st.Day != today {
		st = state{Day: today, Reserved: map[string]reservation{}}
	}
	if st.Reserved == nil {
		st.Reserved = map[string]reservation{}
	}
	// Sweep expired reservations (crash recovery: the budget gets looser
	// only after the TTL, never before).
	for k, r := range st.Reserved {
		if a.now().After(r.ExpiresAt) {
			delete(st.Reserved, k)
		}
	}
	// Hand-edited negatives must never loosen the budget.
	if st.Spent < 0 {
		st.Spent = 0
	}
	return st, nil
}

// persist writes the state atomically (tmp+rename, 0600).
func (a *Authority) persist(st state) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := a.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, filePerms); err != nil {
		return fmt.Errorf("budget: write: %w", err)
	}
	return os.Rename(tmp, a.path())
}

// reservedTotal returns the sum of all active reservation amounts.
func reservedTotal(st state) int64 {
	var sum int64
	for _, r := range st.Reserved {
		if sum > math.MaxInt64-r.AmountMicro {
			return math.MaxInt64
		}
		sum += r.AmountMicro
	}
	return sum
}

// reservedForDomain returns the sum of reservations for one domain.
func reservedForDomain(st state, domain string) int64 {
	var sum int64
	for _, r := range st.Reserved {
		if r.Domain != domain {
			continue
		}
		if sum > math.MaxInt64-r.AmountMicro {
			return math.MaxInt64
		}
		sum += r.AmountMicro
	}
	return sum
}

// Authorize atomically checks the daily cap and the per-domain sub-cap,
// then durably reserves the amount. Returns a token for Commit/Release.
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
	token := fmt.Sprintf("r%d", a.now().UnixNano())
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
	if st.Spent > math.MaxInt64-r.AmountMicro {
		st.Spent = math.MaxInt64
	} else {
		st.Spent += r.AmountMicro
	}
	return a.persist(st)
}

// Release removes a reservation without charging. Unknown token = no-op.
func (a *Authority) Release(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return err
	}
	if _, ok := st.Reserved[token]; !ok {
		return nil
	}
	delete(st.Reserved, token)
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

func (a *Authority) today() string { return a.now().Format("2006-01-02") }
