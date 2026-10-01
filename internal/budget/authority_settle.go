// authority_settle.go — settlement-side transitions on a reservation:
// marking the signature as sent, committing the charge, releasing a hold, and
// reading the per-domain total. Split out of authority.go to keep each file
// under the 200-line budget (AGENTS.md rule 5); the state type and Authorize
// stay in authority.go because they define the reservation itself.
package budget

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

// DomainTotal returns today's committed total for one domain, plus anything
// still reserved on it. This is the figure the per-domain cap is decided from,
// exposed so the ledger can be inspected without parsing budget.json (47.1).
func (a *Authority) DomainTotal(domain string) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.load()
	if err != nil {
		return 0, err
	}
	return satAddSpent(st.SpentByDomain[domain], reservedForDomain(st, domain)), nil
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
	// The domain total moves in the same transaction, so the per-domain cap can
	// never observe a committed payment as absent (47.1).
	st.SpentByDomain[r.Domain] = satAddSpent(st.SpentByDomain[r.Domain], r.AmountMicro)
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
