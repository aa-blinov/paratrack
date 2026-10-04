package appmodel

// AuthSessionCredential is the one-time transport result needed to establish
// a browser session. It deliberately carries no persistence metadata and must
// never be serialized as JSON.
type AuthSessionCredential struct {
	Token string `json:"-"`
}
