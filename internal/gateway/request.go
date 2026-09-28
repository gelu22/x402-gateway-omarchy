// HTTP result conversion for Gateway fetches.
package gateway

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
)

// maxSellerContentBytes bounds one seller response body (fail-closed: a hostile
// seller cannot fill the daemon's memory after we already signed). Mirrors
// maxBodyBytes (server, requests in) and maxCachedBody (MFA result cache).
const maxSellerContentBytes = 1 << 20

// toResult converts a seller response, refusing bodies beyond the cap instead
// of buffering them (T1: the signature may already be paid at this point).
// At most maxSellerContentBytes+1 bytes are ever read; nothing partial is
// returned on overflow.
func toResult(res *http.Response) (*FetchResult, error) {
	hdrs := map[string]string{}
	for _, name := range []string{"Content-Type", "Payment-Response", "X-Payment-Response"} {
		if v := res.Header.Get(name); v != "" {
			hdrs[name] = v
		}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxSellerContentBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxSellerContentBytes {
		return nil, fmt.Errorf("%w: seller content exceeds %d bytes", ErrContentTooLarge, maxSellerContentBytes)
	}
	return &FetchResult{Status: res.StatusCode, Headers: hdrs, BodyB64: base64.StdEncoding.EncodeToString(raw)}, nil
}
