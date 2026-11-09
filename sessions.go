package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// The session manager is a cornerstone of this application. It powers everything from authentication, to instant revokations, to permission management.
// This would usually be an incredibly difficult and tricky to get right building by hand (ie, what data structure do you use to reduce CPU waste and maximize memory usage? What lookup time?)
// Luckily, we have NATS kv! Nats comes with a Time To Live (TTL) and Limits on Key-Values, so NATS takes care of ensuring key-values are removed when their time to live is up
// ie. the simplest way to implement a service like this is
// 1. User goes to website, for our cases they'll log in first. If there's no session_id cookie on their requests, create a new cookie, create a new session_id token, and attach it to
// their requests from now on.
// 2. Since this session_id in our NATS kv store has a time to live, let's say it's allowed to be alive for 1 hour, we set it an forget it.
// 3. After an hour NATS will delete that key, and since we'll do checks on user routes, if their session is deleted redirect them back to the login page
// 4. If a user logs out directly? just delete their session from NATS kv
//
// For now it's really simple, we haven't figured out Role Based Access Control yet so don't worry about not revoking on permissions, policies, roles etc.
// Probably not a bad idea to actually include sessionManager as a field on our App struct, so we can call (a *App) on these functions
//
// example usage:
// sessionManager, err := SessionManagerInit(nc)
// if err != nil {/*do something to panic*/}
func SessionManagerInit(nc *nats.Conn) (jetstream.KeyValue, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:         "session_manager",
		Description:    "key-value store sessions",
		History:        1,
		LimitMarkerTTL: 1 * time.Second,
	})
	return kv, err
}

// example usage:
// newSession := GenerateSessionToken(...)
// err := SessionManagerCreate(r.Context(), kv, newSession, []byte(userID))
func SessionManagerCreate(ctx context.Context, kv jetstream.KeyValue, session string, value []byte) (uint64, error) {
	return kv.Create(ctx, session, value, jetstream.KeyTTL(1*time.Hour))
}

// example usage:
// cookie_session, err := r.Cookie("session_id") // retrieve session_id value from cookie
// id, err := SessionManagerGet(r.Context(), kv, cookie_session)
// ...
// then use that SessionManagerGet to retrieve user data, so something like
// data, err := app.queries.GetUserPermissionsFromId(r.Context(), id)
// ...
func SessionManagerGet(ctx context.Context, kv jetstream.KeyValue, session string) (string, error) {
	entry, err := kv.Get(ctx, session)
	if err != nil {
		return "", err
	}
	return string(entry.Value()), nil
}

// example usage:
// cookie_session, err := r.Cookie("session_id") // retrieve session_id value from cookie
// err := SessionManagerDelete(r.Context(), kv, cookie_session)
// ... should be deleted ...
// datastar.NewSSE(w, r).Redirect("/login") // redirect since they shouldn't have access without session id
func SessionManagerDelete(ctx context.Context, kv jetstream.KeyValue, session string) error {
	return kv.Delete(ctx, session)
}

// we're going to use this to generate our session tokens. We don't have to use RawURLEncoding, but it helps since we can guarantee it'll be safe for our URLS.
// This means we can generate a session_id token, and then use that inside our Templ URL builder which can do really interesting stuff like
//
//	templ SpecialHtml(session string){
//	     <a href={ fmt.Sprintf("%s/user-resources", session) }>Special Link Build just for our Users from session_id</a>
//	}
func GenerateSessionToken() (string, error) {
	// create a token that's 32 bits
	token := make([]byte, 32)
	// call the random.Read function from crypto/rand, which will randomly insert data, so our token is always random
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// example usage:
// token, err := GeneratedSessionToken()
// if err != nil {/*break stuff*/}
// http.SetCookie(w, SessionCookie(token))
// use this function to attach session_id cookie to the HTTP header request/responses
func SessionCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     "session_id",
		Value:    value,
		Path:     "/",
		MaxAge:   int((1 * time.Hour).Seconds()), // one hour in seconds
		HttpOnly: true,
		Secure:   true,
	}
}
