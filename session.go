package main

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type Session struct {
	User      *UserConfig
	ExpiresAt time.Time
}

type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	s := &SessionStore{sessions: make(map[string]*Session)}
	go s.cleanupLoop()
	return s
}

// Create: セッションを新規発行してIDを返す
func (s *SessionStore) Create(user *UserConfig, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := base64.URLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = &Session{User: user, ExpiresAt: time.Now().Add(ttl)}
	return id, nil
}

// Get: 有効なセッションからユーザーを取得(期限切れならnil)
func (s *SessionStore) Get(id string) *UserConfig {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, id)
		return nil
	}
	return sess.User
}

// cleanupLoop: 期限切れセッションを定期的に削除してメモリリークを防ぐ
func (s *SessionStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, sess := range s.sessions {
			if now.After(sess.ExpiresAt) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
