package voice

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestMintToken proves the minted token is a well-formed LiveKit access token:
// HS256-signed with the API secret, issuer = API key, subject = identity, and a
// `video` grant scoped to the room. Verified without a live SFU by decoding the JWT.
func TestMintToken(t *testing.T) {
	const secret = "sfu-secret-supersecret"
	now := time.Now() // exp must be in the future or ParseWithClaims rejects it

	tok, err := MintToken(secret, "APIabc", "opencord-ch-7", "u42", "alice", time.Hour, now)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	// A leaked API key alone can't forge tokens — the wrong secret must NOT verify.
	if _, err := jwt.Parse(tok, func(*jwt.Token) (any, error) { return []byte("wrong-secret"), nil }); err == nil {
		t.Fatal("token verified under the wrong secret — minting is not secret-bound")
	}

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(tok, &claims, func(tk *jwt.Token) (any, error) {
		if _, ok := tk.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("parse: err=%v valid=%v", err, parsed.Valid)
	}

	if claims["iss"] != "APIabc" {
		t.Errorf("iss = %v, want APIabc", claims["iss"])
	}
	if claims["sub"] != "u42" {
		t.Errorf("sub = %v, want u42", claims["sub"])
	}
	if claims["name"] != "alice" {
		t.Errorf("name = %v, want alice", claims["name"])
	}
	video, ok := claims["video"].(map[string]any)
	if !ok {
		t.Fatalf("video grant claim missing or wrong type: %#v", claims["video"])
	}
	if video["room"] != "opencord-ch-7" {
		t.Errorf("grant room = %v, want opencord-ch-7", video["room"])
	}
	if video["roomJoin"] != true || video["canPublish"] != true || video["canSubscribe"] != true {
		t.Errorf("grant flags wrong: %#v", video)
	}
	exp, _ := claims["exp"].(float64)
	if int64(exp) != now.Add(time.Hour).Unix() {
		t.Errorf("exp = %d, want %d", int64(exp), now.Add(time.Hour).Unix())
	}
}
