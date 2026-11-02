package store

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session is a WeCom chat that has already talked to the bot.
// Proactive pushes only work for chats recorded here.
type Session struct {
	ChatID    string    `json:"chat_id"`
	ChatType  int       `json:"chat_type"`
	UserID    string    `json:"user_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Task links a template-card button to one container update.
type Task struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContainerID  string `json:"container_id"`
	Image        string `json:"image"`
	RemoteDigest string `json:"remote_digest"`
}

type state struct {
	Sessions []Session         `json:"sessions"`
	Ignored  map[string]string `json:"ignored"`
	Notified map[string]string `json:"notified"`
	Tasks    map[string]Task   `json:"tasks"`
}

// Store is a small JSON file. It is safe for one process.
type Store struct {
	mu    sync.Mutex
	path  string
	state state
}

// Open loads path, creating it if needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: path, state: state{
		Ignored:  map[string]string{},
		Notified: map[string]string{},
		Tasks:    map[string]Task{},
	}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, s.saveLocked()
		}
		return nil, err
	}
	if len(raw) == 0 {
		return s, nil
	}
	if err := unmarshalState(raw, &s.state); err != nil {
		return nil, err
	}
	if s.state.Ignored == nil {
		s.state.Ignored = map[string]string{}
	}
	if s.state.Notified == nil {
		s.state.Notified = map[string]string{}
	}
	if s.state.Tasks == nil {
		s.state.Tasks = map[string]Task{}
	}
	return s, nil
}

// Remember records a chat so later scans can push to it.
func (s *Store) Remember(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.UpdatedAt = time.Now()
	for i, old := range s.state.Sessions {
		if old.ChatID == sess.ChatID && old.ChatType == sess.ChatType {
			s.state.Sessions[i] = sess
			return s.saveLocked()
		}
	}
	s.state.Sessions = append(s.state.Sessions, sess)
	if len(s.state.Sessions) > 100 {
		s.state.Sessions = s.state.Sessions[len(s.state.Sessions)-100:]
	}
	return s.saveLocked()
}

// Sessions returns a copy of known chats.
func (s *Store) Sessions() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, len(s.state.Sessions))
	copy(out, s.state.Sessions)
	return out
}

// Ignore suppresses notifications for this exact remote digest.
func (s *Store) Ignore(name, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Ignored[name] = digest
	return s.saveLocked()
}

// ClearIgnore allows notifications for the container again.
func (s *Store) ClearIgnore(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Ignored, name)
	return s.saveLocked()
}

// Ignored returns the digest currently ignored for name.
func (s *Store) Ignored(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Ignored[name]
}

// Notified returns the last digest we already pushed for name.
func (s *Store) Notified(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Notified[name]
}

// MarkNotified records that this digest was pushed.
func (s *Store) MarkNotified(name, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Notified[name] = digest
	return s.saveLocked()
}

// PutTask stores a card action.
func (s *Store) PutTask(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Tasks[task.ID] = task
	return s.saveLocked()
}

// TakeTask removes and returns a card action. The second click finds nothing.
func (s *Store) TakeTask(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.state.Tasks[id]
	if !ok {
		return Task{}, false
	}
	delete(s.state.Tasks, id)
	_ = s.saveLocked()
	return task, true
}
