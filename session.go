package main

import (
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type Session struct {
	id        string
	CreatedAt time.Time
	ptmx      *os.File
	cmd       *exec.Cmd
	clients   map[*websocket.Conn]bool
	mu        sync.RWMutex
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

var manager = SessionManager{
	sessions: make(map[string]*Session),
}

func (sm *SessionManager) getOrCreateSession(id string, command []string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[id]; exists {
		return session, nil
	}

	log.Printf("Creating new session: %s", id)
	cmd := exec.Command(command[0], command[1:]...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	session := &Session{
		id:        id,
		CreatedAt: time.Now(),
		ptmx:      ptmx,
		clients:   make(map[*websocket.Conn]bool),
	}
	sm.sessions[id] = session

	go session.run()

	return session, nil
}

// run - главный цикл сессии, читает из pty и транслирует многим (броадкастит).
func (s *Session) run() {
	// Очистка при завершении функции
	defer func() {
		manager.mu.Lock()
		delete(manager.sessions, s.id)
		manager.mu.Unlock()
		s.ptmx.Close()
		log.Printf("Session %s closed and cleaned up.", s.id)
	}()

	buffer := make([]byte, 1024)
	for {
		n, err := s.ptmx.Read(buffer)
		if err != nil {
			log.Printf("PTY for session %s closed: %v", s.id, err)
			s.mu.RLock()
			for client := range s.clients {
				client.Close()
			}
			s.mu.RUnlock()
			return
		}
		s.broadcast(buffer[:n])
	}
}

// broadcast отправляет данные из PTY всем клиентам в сессии.
func (s *Session) broadcast(message []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.clients {
		if err := client.WriteMessage(websocket.BinaryMessage, message); err != nil {
			log.Printf("Broadcast error (client will be removed): %v", err)
		}
	}
}

// addClient добавляет нового клиента в сессию.
func (s *Session) addClient(conn *websocket.Conn) {
	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()
	log.Printf("Client added to session %s. Total clients: %d", s.id, len(s.clients))
}

// removeClient удаляет клиента из сессии.
func (s *Session) removeClient(conn *websocket.Conn) {
	s.mu.Lock()
	delete(s.clients, conn)
	s.mu.Unlock()
	log.Printf("Client removed from session %s. Total clients: %d", s.id, len(s.clients))
	conn.Close()
}
