package clusteredistore

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-redis/redis"
	"github.com/gorilla/sessions"
)

var (
	redisAddr  = "ubuntu.home:6379"
	redisAddrs = []string{
		"ubuntu.home:7000", "ubuntu.home:7001", "ubuntu.home:7002",
		"ubuntu.home:7003", "ubuntu.home:7004", "ubuntu.home:7005",
	}
	clustered = true
	keyPrefix = "a:" // hard coded in redisstore.go
	client    redis.UniversalClient
)

func createStore(t *testing.T, keyPrefix string, options sessions.Options) *RedisStore {
	store := NewRedisStore(client)
	store.KeyPrefix(keyPrefix)
	store.SetOptions(options)
	return store
}
func TestSuite_Basic(t *testing.T) {
	if clustered { // setup
		client = redis.NewClusterClient(&redis.ClusterOptions{Addrs: redisAddrs})
	} else {
		client = redis.NewClient(&redis.Options{Addr: redisAddr})
	}
	defer client.Close() // teardown

	t.Run("create store then request then session", func(t *testing.T) {
		store := createStore(t, keyPrefix, sessions.Options{Path: "/", Domain: "example.com", MaxAge: 60 * 5})
		request, err := http.NewRequest("GET", "http://www.example.com", nil)
		if err != nil {
			t.Fatal("failed to create request:", err)
		}
		session, err := store.New(request, "hello")
		if err != nil {
			t.Fatal("failed to create session:", err)
		}
		if session.IsNew == false {
			t.Fatal("session is not new")
		}
	})

	t.Run("setting options", func(t *testing.T) {
		store := createStore(t, keyPrefix, sessions.Options{Path: "/", Domain: "example.com", MaxAge: 60 * 5})
		opts := sessions.Options{Path: "/path", MaxAge: 99999}
		store.SetOptions(opts)
		request, err := http.NewRequest("GET", "http://www.example.com", nil)
		if err != nil {
			t.Fatal("failed to create request:", err)
		}
		session, _ := store.New(request, "hello")
		if session.Options.Path != opts.Path || session.Options.MaxAge != opts.MaxAge {
			t.Fatal("failed to set options")
		}
	})

	t.Run("saving session", func(t *testing.T) {
		store := createStore(t, keyPrefix, sessions.Options{Path: "/", Domain: "example.com", MaxAge: 60 * 5})
		request, err := http.NewRequest("GET", "http://www.example.com", nil)
		if err != nil {
			t.Fatal("failed to create request:", err)
		}
		w := httptest.NewRecorder()
		session, err := store.New(request, "hello")
		if err != nil {
			t.Fatal("failed to create session:", err)
		}
		session.Values["key"] = "value"
		err = session.Save(request, w)
		if err != nil {
			t.Fatal("failed to save:", err)
		}
	})

	t.Run("deleting session", func(t *testing.T) {
		store := createStore(t, keyPrefix, sessions.Options{Path: "/", Domain: "example.com", MaxAge: 60 * 5})
		request, err := http.NewRequest("GET", "http://www.example.com", nil)
		if err != nil {
			t.Fatal("failed to create request:", err)
		}
		w := httptest.NewRecorder()
		session, err := store.New(request, "hello")
		if err != nil {
			t.Fatal("failed to create session:", err)
		}
		session.Values["username"] = "henry"
		err = session.Save(request, w)
		if err != nil {
			t.Fatal("failed to save session:", err)
		}
		session.Options.MaxAge = -1 // comment this to see un-delete session still exists
		err = session.Save(request, w)
		if err != nil {
			t.Fatal("failed to delete session:", err)
		}
		target := keyPrefix + session.ID
		// session.Save() doesn't always reflect the key existence, using redis client to check
		if client.Exists(target).Val() == 1 {
			t.Fatal("delete target still exists:", session.ID)
		}
	})
}
