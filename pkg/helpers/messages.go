package helpers

// Messages shared by more than one transport (HTTP middleware, AMQP
// commands). They are client-facing detail, so they name what is wrong
// without saying why: the reason stays in the server log.
const (
	// MsgMissingBearer: no Authorization header, or not the Bearer scheme.
	MsgMissingBearer = "missing bearer token"
	// MsgInvalidToken: the token was checked and rejected.
	MsgInvalidToken = "invalid token"
	// MsgIssuerUnavailable: the token's issuer could not be reached, so the
	// token could not be checked.
	MsgIssuerUnavailable = "token issuer unavailable"
	// MsgUnauthenticated: a scope check ran without verified claims.
	MsgUnauthenticated = "unauthenticated"
	// MsgTimedOut: the operation ran past its deadline.
	MsgTimedOut = "timed out"
)

// MsgLacksScope is the detail for a verified token without scope.
func MsgLacksScope(scope string) string { return "token lacks scope " + scope }
