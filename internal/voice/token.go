// Package voice mints access tokens for an optional LiveKit SFU. A LiveKit access
// token is just an HS256 JWT with a `video` grant claim, so we sign it with the
// existing golang-jwt lib — no need to pull in the heavy server-sdk-go (pion/webrtc,
// protobuf, …). See SPEC "mesh → OSS SFU (LiveKit) scale path".
//
// The token is always minted SERVER-SIDE from the verified user (Rule C) and scoped
// to one room (the channel); the HTTP layer gates it on channel membership (Rule B).
package voice

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// videoGrant is LiveKit's `video` claim — what the bearer may do in the room.
// Field names/JSON tags match LiveKit's VideoGrant exactly.
type videoGrant struct {
	Room           string `json:"room"`
	RoomJoin       bool   `json:"roomJoin"`
	CanPublish     bool   `json:"canPublish"`
	CanSubscribe   bool   `json:"canSubscribe"`
	CanPublishData bool   `json:"canPublishData"`
}

// livekitClaims is the full JWT payload LiveKit expects: standard registered claims
// (iss = API key, sub = participant identity, exp/nbf) plus the video grant and an
// optional display name.
type livekitClaims struct {
	Video videoGrant `json:"video"`
	Name  string     `json:"name,omitempty"`
	jwt.RegisteredClaims
}

// MintToken builds a LiveKit join token for `identity` (display `name`) in `room`,
// valid for `ttl`, signed with the LiveKit API secret. `apiKey` becomes the issuer.
// The grant allows publish + subscribe (a normal voice participant); it does NOT
// grant room admin/create, so a leaked token can't reconfigure rooms.
func MintToken(secret, apiKey, room, identity, name string, ttl time.Duration, now time.Time) (string, error) {
	claims := livekitClaims{
		Video: videoGrant{
			Room:           room,
			RoomJoin:       true,
			CanPublish:     true,
			CanSubscribe:   true,
			CanPublishData: true,
		},
		Name: name,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    apiKey,
			Subject:   identity,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}
