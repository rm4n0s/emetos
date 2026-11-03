package engine

import (
	"crypto/md5" //nolint:gosec // MD5 is an algorithm of RFC 7616 that servers still use
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/rm4n0s/errors"
)

// digestChallenge is a Digest challenge of a WWW-Authenticate header (RFC 7616).
type digestChallenge struct {
	realm, nonce, opaque string
	// algorithm is "MD5", "MD5-sess", "SHA-256" or "SHA-256-sess".
	algorithm string
	// qopAuth says that the server offered qop=auth.
	qopAuth bool
	// userhash is not supported; the field is only read to refuse it.
	userhash bool
}

// parseDigest finds the Digest challenge to answer among the values of the WWW-Authenticate headers: SHA-256 is preferred to MD5.
// found is false when there is no Digest challenge at all.
func parseDigest(values []string) (best digestChallenge, found bool, err error) {
	var unsupported string
	for _, v := range values {
		for _, params := range splitChallenges(v) {
			ch, ok := challengeFrom(params)
			if !ok {
				continue
			}
			found = true
			if ch.nonce == "" {
				return digestChallenge{}, true, errors.New("EngineDigestChallengeInvalid", "the Digest challenge has no nonce", "challenge", v)
			}
			if ch.userhash {
				unsupported = "userhash"
				continue
			}
			switch ch.algorithm {
			case "MD5", "MD5-SESS", "SHA-256", "SHA-256-SESS":
			default:
				unsupported = ch.algorithm
				continue
			}
			if best.nonce == "" || strings.HasPrefix(ch.algorithm, "SHA-256") && !strings.HasPrefix(best.algorithm, "SHA-256") {
				best = ch
			}
		}
	}
	if found && best.nonce == "" {
		return digestChallenge{}, true, errors.New("EngineDigestUnsupported", "the server offers no Digest algorithm that is built", "algorithm", unsupported)
	}
	return best, found, nil
}

// splitChallenges returns the parameters of each Digest challenge of one header value (a value may hold several challenges,
// each starting with its scheme).
func splitChallenges(v string) []map[string]string {
	var out []map[string]string
	rest := strings.TrimSpace(v)
	for rest != "" {
		scheme := rest
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			scheme, rest = rest[:i], strings.TrimSpace(rest[i:])
		} else {
			rest = ""
		}
		params := map[string]string{}
		for rest != "" {
			eq := strings.IndexByte(rest, '=')
			comma := strings.IndexByte(rest, ',')
			if eq < 0 || (comma >= 0 && comma < eq) {
				// the next challenge starts here (a scheme without parameters, or a new scheme)
				if comma >= 0 && comma < eq {
					rest = strings.TrimSpace(rest[comma+1:])
					continue
				}
				break
			}
			key := strings.ToLower(strings.TrimSpace(rest[:eq]))
			if strings.ContainsAny(key, " \t") { // "scheme2 key=" : a new challenge
				break
			}
			rest = strings.TrimSpace(rest[eq+1:])
			var val string
			if strings.HasPrefix(rest, `"`) {
				var b strings.Builder
				i := 1
				for ; i < len(rest) && rest[i] != '"'; i++ {
					if rest[i] == '\\' && i+1 < len(rest) {
						i++
					}
					b.WriteByte(rest[i])
				}
				val = b.String()
				if i < len(rest) {
					i++
				}
				rest = rest[i:]
			} else {
				end := strings.IndexByte(rest, ',')
				if end < 0 {
					end = len(rest)
				}
				val = strings.TrimSpace(rest[:end])
				rest = rest[end:]
			}
			params[key] = val
			rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ","))
		}
		if strings.EqualFold(scheme, "Digest") {
			params["\x00scheme"] = "Digest"
			out = append(out, params)
		}
	}
	return out
}

func challengeFrom(p map[string]string) (digestChallenge, bool) {
	if p["\x00scheme"] != "Digest" {
		return digestChallenge{}, false
	}
	ch := digestChallenge{realm: p["realm"], nonce: p["nonce"], opaque: p["opaque"], algorithm: strings.ToUpper(p["algorithm"])}
	if ch.algorithm == "" {
		ch.algorithm = "MD5"
	}
	for _, q := range strings.Split(p["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			ch.qopAuth = true
		}
	}
	ch.userhash = strings.EqualFold(p["userhash"], "true")
	return ch, true
}

// newCnonce returns a random client nonce. crypto/rand.Read does not fail (it ends the program if the system has no randomness).
func newCnonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// digestAuthorization makes the value of the Authorization header that answers ch (RFC 7616 section 3.4), with the nonce count 1.
func digestAuthorization(ch digestChallenge, user, pass, method, uri, cnonce string) string {
	h := md5.New //nolint:gosec // see the import
	algName := "MD5"
	if strings.HasPrefix(ch.algorithm, "SHA-256") {
		h, algName = sha256.New, "SHA-256"
	}
	sum := func(s string) string {
		hh := h()
		hh.Write([]byte(s))
		return hex.EncodeToString(hh.Sum(nil))
	}
	const nc = "00000001"
	ha1 := sum(user + ":" + ch.realm + ":" + pass)
	if strings.HasSuffix(ch.algorithm, "-SESS") {
		ha1 = sum(ha1 + ":" + ch.nonce + ":" + cnonce)
	}
	ha2 := sum(method + ":" + uri)
	var response string
	if ch.qopAuth {
		response = sum(ha1 + ":" + ch.nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	} else {
		response = sum(ha1 + ":" + ch.nonce + ":" + ha2)
	}
	q := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	var b strings.Builder
	fmt.Fprintf(&b, "Digest username=%s, realm=%s, nonce=%s, uri=%s, algorithm=%s", q(user), q(ch.realm), q(ch.nonce), q(uri), algName)
	if strings.HasSuffix(ch.algorithm, "-SESS") {
		b.WriteString("-sess")
	}
	if ch.qopAuth {
		fmt.Fprintf(&b, ", qop=auth, nc=%s, cnonce=%s", nc, q(cnonce))
	}
	fmt.Fprintf(&b, ", response=%s", q(response))
	if ch.opaque != "" {
		fmt.Fprintf(&b, ", opaque=%s", q(ch.opaque))
	}
	return b.String()
}
