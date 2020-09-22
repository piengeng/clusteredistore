package clusteredistore

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/gob"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/gorilla/sessions"
)

const randomByteLength = uint8(16)
const defaultMaxAge = 86400 * 1 // 1 day

// RedisStore stores gorilla sessions in Redis
type RedisStore struct {
	// client to connect to redis
	client redis.UniversalClient
	// default options to use when a new session is created
	Options *sessions.Options
	// key prefix with which the session will be stored
	keyPrefix string
	// key generator
	keyGen KeyGenFunc
	// session serializer
	serializer SessionSerializer
}

// KeyGenFunc defines a function used by store to generate a key
type KeyGenFunc func(uint8) (string, error)

// NewRedisStore returns a new RedisStore with default configuration
func NewRedisStore(client redis.UniversalClient) *RedisStore {
	rs := RedisStore{
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: defaultMaxAge,
		},
		client:     client,
		keyPrefix:  "sid:",
		keyGen:     generateRandomKeyByLength,
		serializer: GobSerializer{},
	}
	return &rs
}

// Get returns a session for the given name after adding it to the registry.
func (s *RedisStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New returns a session for the given name without adding it to the registry.
func (s *RedisStore) New(r *http.Request, name string) (*sessions.Session, error) {

	session := sessions.NewSession(s, name)
	session.Options = s.Options
	session.IsNew = true

	c, err := r.Cookie(name)
	if err != nil {
		return session, nil
	}
	session.ID = c.Value

	err = s.load(session)
	if err == nil {
		session.IsNew = false
	} else if err == redis.Nil {
		err = nil // no data stored
	}
	return session, err
}

// Save adds a single session to the response.
//
// If the Options.MaxAge of the session is < 0 then the session file will be
// deleted from the store. With this process it enforces the properly
// session cookie handling so no need to trust in the cookie management in the
// web browser.
func (s *RedisStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// https://github.com/gin-contrib/sessions/blob/master/session_options_go1.11.go#L15
	// MaxAge=0 means no 'Max-Age' attribute specified.

	// Delete if max-age is < 0
	if session.Options.MaxAge < 0 {
		if err := s.delete(session); err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	if session.ID == "" {
		id, err := s.keyGen(randomByteLength)
		if err != nil {
			return errors.New("redisstore: failed to generate session id")
		}
		session.ID = id
	}
	if err := s.save(session); err != nil {
		return err
	}

	http.SetCookie(w, sessions.NewCookie(session.Name(), session.ID, session.Options))
	return nil
}

// Options set options to use when a new session is created
func (s *RedisStore) SetOptions(opts sessions.Options) {
	s.Options = &opts
}

// KeyPrefix sets the key prefix to store session in Redis
func (s *RedisStore) KeyPrefix(keyPrefix string) {
	s.keyPrefix = keyPrefix
}

// KeyGen sets the key generator function
func (s *RedisStore) KeyGen(f KeyGenFunc) {
	s.keyGen = f
}

// Serializer sets the session serializer to store session
func (s *RedisStore) Serializer(ss SessionSerializer) {
	s.serializer = ss
}

// save writes session in Redis
func (s *RedisStore) save(session *sessions.Session) error {

	b, err := s.serializer.Serialize(session)
	if err != nil {
		return err
	}

	return s.client.Set(s.keyPrefix+session.ID, b, time.Duration(session.Options.MaxAge)*time.Second).Err()
}

// load reads session from Redis
func (s *RedisStore) load(session *sessions.Session) error {

	cmd := s.client.Get(s.keyPrefix + session.ID)
	if cmd.Err() != nil {
		return cmd.Err()
	}

	b, err := cmd.Bytes()
	if err != nil {
		return err
	}

	return s.serializer.Deserialize(b, session)
}

// delete deletes session in Redis
func (s *RedisStore) delete(session *sessions.Session) error {
	return s.client.Del(s.keyPrefix + session.ID).Err()
}

// SessionSerializer provides an interface for serialize/deserialize a session
type SessionSerializer interface {
	Serialize(s *sessions.Session) ([]byte, error)
	Deserialize(b []byte, s *sessions.Session) error
}

// Gob serializer
type GobSerializer struct{}

func (gs GobSerializer) Serialize(s *sessions.Session) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(s.Values)
	if err == nil {
		return buf.Bytes(), nil
	}
	return nil, err
}

func (gs GobSerializer) Deserialize(d []byte, s *sessions.Session) error {
	dec := gob.NewDecoder(bytes.NewBuffer(d))
	return dec.Decode(&s.Values)
}

func generateRandomKeyByLength(l uint8) (string, error) {
	// checking permutation of base32 and length
	// session should not persist for extended period of time, depends on use case, adjust accordingly
	// are you going to have this many unique sessions on your system?
	// P(32, 8) =                424,097,856,000 possibilities
	// P(32,12) =        108,155,131,628,544,000 possibilities
	// P(32,16) = 12,576,278,705,767,096,320,000 possibilities, this seems reasonable, lol
	// func generate P(32,26) string, with randomByteLength = 16, go figures
	k := make([]byte, l)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(k), "="), nil
}
